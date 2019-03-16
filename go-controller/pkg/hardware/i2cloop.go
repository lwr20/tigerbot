package hardware

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/tigerbot-team/tigerbot/go-controller/pkg/pca9685"

	"github.com/tigerbot-team/tigerbot/go-controller/pkg/screen"

	"github.com/tigerbot-team/tigerbot/go-controller/pkg/ina219"

	"github.com/tigerbot-team/tigerbot/go-controller/pkg/mux"
	"github.com/tigerbot-team/tigerbot/go-controller/pkg/tofsensor"

	"github.com/tigerbot-team/tigerbot/go-controller/pkg/propeller"
)

const NumServoPorts = 16

type I2CController struct {
	lock sync.Mutex

	// Desired values.  Stored off in case we need to re-initialise the hardware.
	motorL, motorR      int8
	pwmPorts            map[int]pwmTypes // Either servoPosition or pwmValue
	pwmPortsWithUpdates map[int]bool

	prop        propeller.Interface
	tofsEnabled bool

	distanceReadings DistanceReadings
}

type pwmTypes interface {
	pwmsOnly()
}

type servoPosition float64

func (servoPosition) pwmsOnly() {}

type pwmValue float64

func (pwmValue) pwmsOnly() {}

func NewI2CController() *I2CController {
	return &I2CController{
		pwmPorts:            map[int]pwmTypes{},
		pwmPortsWithUpdates: map[int]bool{},

		tofsEnabled: true,
	}
}

func (c *I2CController) SetToFsEnabled(enabled bool) {
	c.lock.Lock()
	c.tofsEnabled = enabled
	c.lock.Unlock()
}

func (c *I2CController) SetMotorSpeeds(left, right int8) {
	c.lock.Lock()
	c.motorL = left
	c.motorR = right
	c.lock.Unlock()
}

func (c *I2CController) SetServo(n int, value float64) {
	c.lock.Lock()
	c.pwmPorts[n] = servoPosition(value)
	c.pwmPortsWithUpdates[n] = true
	c.lock.Unlock()
}

func (c *I2CController) SetPWM(n int, value float64) {
	c.lock.Lock()
	c.pwmPorts[n] = pwmValue(value)
	c.pwmPortsWithUpdates[n] = true
	c.lock.Unlock()
}

func (c *I2CController) CurrentDistanceReadings() DistanceReadings {
	c.lock.Lock()
	defer c.lock.Unlock()

	// Avoid returning anything until the first reading has completed.
	for c.distanceReadings.CaptureTime.IsZero() {
		c.lock.Unlock()
		time.Sleep(10 * time.Millisecond)
		c.lock.Lock()
	}

	return c.distanceReadings
}

func (c *I2CController) Loop(ctx context.Context, initDone *sync.WaitGroup) {
	fmt.Println("I2C loop started")
	for {
		c.loopUntilSomethingBadHappens(ctx, initDone)
		if ctx.Err() != nil {
			return
		}
		fmt.Println("===== !!! WARNING !!! I2C FAILURE; TRYING TO RECOVER =====")
		initDone = nil
	}
}

func (c *I2CController) loopUntilSomethingBadHappens(ctx context.Context, initDone *sync.WaitGroup) {
	defer func() {
		if initDone != nil {
			initDone.Done()
		}
	}()

	mx, err := mux.New("/dev/i2c-1")
	if err != nil {
		fmt.Println("Failed to open mux", err)
		return
	}
	defer mx.Close()

	err = mx.SelectSinglePort(mux.BusPropeller)
	if err != nil {
		fmt.Println("Failed to select mux port", err)
		return
	}
	prop, err := propeller.New()
	if err != nil {
		fmt.Println("Failed to open prop", err)
		return
	}
	defer prop.Close()

	var tofs []tofsensor.Interface
	defer func() {
		for _, tof := range tofs {
			_ = tof.Close()
		}
	}()
	for _, port := range []int{
		0, 1, 2, 3, 4, 5,
	} {
		fmt.Println("Initialising ToF ", port)

		err := mx.SelectSinglePort(port)
		if err != nil {
			fmt.Println("Failed to select mux port", err)
			return
		}

		tof, err := tofsensor.New("/dev/i2c-1", 0x29, byte(0x30+port))
		if err != nil {
			tof, err = tofsensor.New("/dev/i2c-1", byte(0x30+port))
			if err != nil {
				fmt.Println("Failed to open sensor", err)
				return
			}
		}

		err = tof.StartContinuousMeasurements()
		if err != nil {
			fmt.Println("Failed to start continuous measurements", err)
			return
		}
		tofs = append(tofs, tof)
	}

	readTofs := func() (DistanceReadings, error) {
		err := mx.SelectMultiplePorts(0x3f)
		readings := DistanceReadings{
			CaptureTime: time.Now(),
			Readings:    make([]Reading, len(tofs)),
		}
		if err != nil {
			return readings, err
		}
		for j, tof := range tofs {
			readingInMM, err := tof.GetNextContinuousMeasurement()
			readings.Readings[j] = Reading{
				DistanceMM: readingInMM,
				Error:      err,
			}
		}
		return readings, nil
	}

	err = mx.SelectSinglePort(mux.BusOthers)
	var powerSensors []ina219.Interface
	for _, addr := range []int{0x41, 0x44} {
		pwrSen, err := ina219.NewI2C("/dev/i2c-1", addr)
		if err != nil {
			fmt.Println("Failed to open power sensor; ignoring! ", err)
			continue
		}
		shuntOhms := 0.1
		if addr == ina219.Addr2 {
			// Motor sensor has a smaller shunt.
			shuntOhms = 0.05
		}
		err = pwrSen.Configure(shuntOhms, 10)
		if err != nil {
			fmt.Println("Failed to open power sensor; ignoring! ", err)
			continue
		}
		powerSensors = append(powerSensors, pwrSen)
	}

	servos, err := pca9685.New("/dev/i2c-1")
	if err != nil {
		fmt.Println("Failed open PCA9685 ", err)
		return
	}
	defer func() {
		err = mx.SelectSinglePort(mux.BusOthers)
		if err != nil {
			fmt.Println("Failed to select port when shutting down servos: ", err)
			return
		}
		_ = servos.Close()
	}()
	err = servos.Configure()
	if err != nil {
		fmt.Println("Failed configure PCA9685 ", err)
		return
	}
	// We may have been reset, queue servo updates for all the ports.
	c.lock.Lock()
	for n := range c.pwmPorts {
		c.pwmPortsWithUpdates[n] = true
	}
	c.lock.Unlock()

	ticker := time.NewTicker(25 * time.Millisecond)

	var lastL, lastR int8
	var lastPowerReadingTime time.Time

	if initDone != nil {
		initDone.Done()
		initDone = nil
	}

	for ctx.Err() == nil {
		<-ticker.C

		c.lock.Lock()
		tofsEnabled := c.tofsEnabled
		c.lock.Unlock()

		if tofsEnabled {
			readings, err := readTofs()
			if err != nil {
				fmt.Println("Failed to read tofs", err)
				return
			}
			fmt.Println("ToF readings:", readings)
			c.lock.Lock()
			c.distanceReadings = readings
			c.lock.Unlock()
		}

		c.lock.Lock()
		l, r := c.motorL, c.motorR
		c.lock.Unlock()
		// Speeds have changed
		err = mx.SelectSinglePort(mux.BusPropeller)
		if err != nil {
			fmt.Println("Failed to update mux port", err)
			return
		}
		if lastL != l || lastR != r {
			err = prop.SetMotorSpeeds(l, r)
			if err != nil {
				fmt.Println("Failed to update motor speeds", err)
				return
			}
			lastL, lastR = l, r
		}

		m1, m2, err := prop.GetEncoderPositions()
		if err == nil {
			fmt.Println("Motor positions: ", m1, " ", m2)
			err = prop.StartEncoderRead()
			if err != nil {
				fmt.Println("Failed to start encoder read", err)
				return
			}
		} else if err != propeller.ErrNotReady {
			fmt.Println("Failed to read encoders", err)
		}

		c.lock.Lock()
		pwmUpdates := c.pwmPortsWithUpdates
		c.pwmPortsWithUpdates = make(map[int]bool)
		c.lock.Unlock()

		err = mx.SelectSinglePort(mux.BusOthers)
		for n := range pwmUpdates {
			c.lock.Lock()
			value := c.pwmPorts[n]
			c.lock.Unlock()

			switch v := value.(type) {
			case servoPosition:
				err = servos.SetServo(n, float64(v))
			case pwmValue:
				err = servos.SetPWM(n, float64(v))
			}
			if err != nil {
				fmt.Println("Failed to update servo/PWM port ", n, ": ", err)
				return
			}
		}

		if time.Since(lastPowerReadingTime) > 1*time.Second {
			for i, ps := range powerSensors {
				bv, err := ps.ReadBusVoltage()
				if err != nil {
					continue
				}
				bc, err := ps.ReadCurrent()
				if err != nil {
					continue
				}
				bp, err := ps.ReadPower()
				if err != nil {
					continue
				}
				fmt.Printf("Bus %v: %.2fV %.2fA %.2fW\n", i, bv, bc, bp)
				screen.Lock.Lock()
				screen.BusVoltages[i] = bv
				screen.Lock.Unlock()
			}
			lastPowerReadingTime = time.Now()
		}
	}
}
