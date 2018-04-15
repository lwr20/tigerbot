package rainbowmode

import (
	"context"
	"math"
	"sync"

	"fmt"
	"sync/atomic"
	"time"

	"sort"

	"github.com/tigerbot-team/tigerbot/go-controller/pkg/joystick"
	"github.com/tigerbot-team/tigerbot/go-controller/pkg/mux"
	"github.com/tigerbot-team/tigerbot/go-controller/pkg/propeller"
	"github.com/tigerbot-team/tigerbot/go-controller/pkg/rainbow"
	"github.com/tigerbot-team/tigerbot/go-controller/pkg/tofsensor"
	"gocv.io/x/gocv"
)

type Colour int

const (
	Red Colour = iota
	Blue
	Yellow
	Green
)

type Phase int

const (
	Rotating Phase = iota
	Advancing
	Reversing
)

func (c Colour) String() string {
	switch c {
	case Red:
		return "red"
	case Blue:
		return "blue"
	case Yellow:
		return "yellow"
	case Green:
		return "green"
	}
	return fmt.Sprintf("Colour(%d)", int(c))
}

var targetSequence = []Colour{Red, Blue, Yellow, Green}

type RainbowMode struct {
	Propeller      propeller.Interface
	cancel         context.CancelFunc
	stopWG         sync.WaitGroup
	joystickEvents chan *joystick.Event

	running        bool
	targetBallIdx  int
	cancelSequence context.CancelFunc
	sequenceDone   chan struct{}

	paused int32

	// State of balls searching.
	phase                   Phase
	targetColour            Colour
	perceivedSize           int
	ballX                   int
	roughDirectionCount     int
	ballFixed               bool
	advanceReverseStartTime time.Time
	advanceDuration         time.Duration
	savePicture             int32
	pictureIndex            int
	ballInView              bool
}

func New(propeller propeller.Interface) *RainbowMode {
	return &RainbowMode{
		Propeller:      propeller,
		joystickEvents: make(chan *joystick.Event),
		phase:          Rotating,
	}
}

func (m *RainbowMode) Name() string {
	return "Rainbow mode"
}

func (m *RainbowMode) Start(ctx context.Context) {
	m.stopWG.Add(1)
	var loopCtx context.Context
	loopCtx, m.cancel = context.WithCancel(ctx)
	go m.loop(loopCtx)
}

func (m *RainbowMode) Stop() {
	m.cancel()
	m.stopWG.Wait()
}

// loop processes input and starts/stops the goroutine that does the actual CV work.
func (m *RainbowMode) loop(ctx context.Context) {
	defer m.stopWG.Done()
	defer m.stopSequence()

	for {
		select {
		case <-m.sequenceDone:
			m.running = false
			m.sequenceDone = nil
		case <-ctx.Done():
			return
		case event := <-m.joystickEvents:
			if event.Type == joystick.EventTypeButton && event.Value == 1 {
				switch event.Number {
				case joystick.ButtonR1:
					m.startSequence()
				case joystick.ButtonSquare:
					m.stopSequence()
				case joystick.ButtonTriangle:
					m.pauseOrResumeSequence()
				case joystick.ButtonCircle:
					atomic.StoreInt32(&m.savePicture, 1)
				}
			}
		}
	}
}

func (m *RainbowMode) startSequence() {
	if m.running {
		fmt.Println("Already running")
		return
	}

	fmt.Println("Starting sequence...")
	m.targetBallIdx = 0
	m.running = true
	atomic.StoreInt32(&m.paused, 0)

	seqCtx, cancel := context.WithCancel(context.Background())
	m.cancelSequence = cancel
	m.sequenceDone = make(chan struct{})
	go m.runSequence(seqCtx)
}

func clamp(v float64, limit float64) int8 {
	if v < -limit {
		v = -limit
	}
	if v > limit {
		v = limit
	}
	if v <= math.MinInt8 {
		return math.MinInt8
	}
	if v >= math.MaxInt8 {
		return math.MaxInt8
	}
	return int8(v)
}

const (
	FPS                     = 15
	ROTATE_SPEED            = 8
	SLOW_ROTATE_SPEED       = 5
	LIMIT_SPEED             = 80
	FORWARD_SPEED           = 5
	X_STRAIGHT_AHEAD        = 320
	X_PLUS_OR_MINUS         = 80
	DIRECTION_ADJUST_FACTOR = 0.08
)

func (m *RainbowMode) setSpeeds(forwards, sideways, rotation float64) {
	fl := clamp(forwards-sideways-rotation, LIMIT_SPEED)
	fr := clamp(forwards+sideways+rotation, LIMIT_SPEED)
	bl := clamp(forwards+sideways-rotation, LIMIT_SPEED)
	br := clamp(forwards-sideways+rotation, LIMIT_SPEED)

	fmt.Printf("Speeds: FL=%d FR=%d BL=%d BR=%d\n", fl, fr, bl, br)
	m.Propeller.SetMotorSpeeds(fl, fr, bl, br)
}

// runSequence is a goroutine that reads form the camera and controls the motors.
func (m *RainbowMode) runSequence(ctx context.Context) {
	defer close(m.sequenceDone)

	mx, err := mux.New("/dev/i2c-1")
	if err != nil {
		fmt.Println("Failed to open mux", err)
		return
	}

	var tofs []tofsensor.Interface
	defer func() {
		for _, tof := range tofs {
			tof.Close()
		}
	}()
	for _, port := range []int{
		mux.BusTOFSideLeft,
		mux.BusTOFFrontLeft,
		mux.BusTOFForward,
		mux.BusTOFFrontRight,
		mux.BusTOFSideRight,
	} {
		tof, err := tofsensor.NewMuxed("/dev/i2c-1", 0x29, mx, port)
		if err != nil {
			fmt.Println("Failed to open sensor", err)
			return
		}
		err = tof.StartContinuousMeasurements()
		if err != nil {
			fmt.Println("Failed to start continuous measurements", err)
			return
		}
		tofs = append(tofs, tof)
	}

	var filters []*Filter
	for i := 0; i < 5; i++ {
		filters = append(filters, &Filter{})
	}

	readSensors := func() {
		// Read the sensors
		for j, tof := range tofs {
			reading := "-"
			readingInMM, err := tof.GetNextContinuousMeasurement()
			filters[j].Accumulate(readingInMM)
			if err == tofsensor.ErrMeasurementInvalid {
				reading = "<invalid>"
			} else if err != nil {
				reading = "<failed>"
			} else {
				reading = fmt.Sprintf("%dmm", readingInMM)
			}
			fmt.Printf("%s:=%5s/%5dmm ", filters[j].Name, reading, filters[j].BestGuess())
		}
		fmt.Println()
	}

	sideLeft := filters[0]
	sideLeft.Name = "L"
	forwardLeft := filters[1]
	forwardLeft.Name = "FL"
	forward := filters[2]
	forward.Name = "F"
	forwardRight := filters[3]
	forwardRight.Name = "FR"
	sideRight := filters[4]
	sideRight.Name = "R"

	for i := 0; i < 5; i++ {
		readSensors()
	}

	webcam, err := gocv.VideoCaptureDevice(0)
	if err != nil {
		fmt.Printf("error opening video capture device: %v\n", 0)
		return
	}
	defer webcam.Close()

	// Capture 15 frames per second.
	webcam.Set(gocv.VideoCaptureFPS, float64(FPS))

	img := gocv.NewMat()
	defer img.Close()

	startTime := time.Now()

	m.reset()
	fmt.Println("Next target ball: ", targetSequence[m.targetBallIdx])

	defer fmt.Println("Exiting sequence loop")

	var (
		lastFrameTime time.Time
		numFramesRead int
	)

	for m.targetBallIdx < len(targetSequence) && ctx.Err() == nil {
		for atomic.LoadInt32(&m.paused) == 1 && ctx.Err() == nil {
			m.Propeller.SetMotorSpeeds(0, 0, 0, 0)
			time.Sleep(100 * time.Millisecond)
		}

		if numFramesRead > 0 {
			timeSinceLastFrame := time.Since(lastFrameTime)
			skipFrames := int(timeSinceLastFrame / (time.Second / FPS))
			if skipFrames > 0 {
				fmt.Printf("Skipping %v frames\n", skipFrames)
				webcam.Grab(skipFrames)
			}
		}

		// This blocks until the next frame is ready.
		if ok := webcam.Read(img); !ok {
			fmt.Printf("cannot read device\n")
			time.Sleep(1 * time.Millisecond)
			continue
		}
		thisFrameTime := time.Now()
		codeTime := time.Since(lastFrameTime)
		if codeTime < 1*time.Second/(FPS+1) {
			fmt.Printf("Code running too fast: %v\n", codeTime)
		}
		lastFrameTime = thisFrameTime
		numFramesRead++

		if img.Empty() {
			fmt.Printf("no image on device\n")
			time.Sleep(1 * time.Millisecond)
			continue
		}

		m.targetColour = targetSequence[m.targetBallIdx]
		m.processImage(img)

		// Don't do anything for the first second, to allow the code to synchronize with the
		// camera frame rate.
		if numFramesRead <= FPS {
			continue
		}

		readSensors()

		if m.phase == Reversing {
			fmt.Println("Target:", m.targetColour, "Reversing")
			if time.Since(m.advanceReverseStartTime) < m.advanceDuration {
				continue
			}
			m.reset()
			m.phase = Rotating
			// Fall through.
		}

		if m.phase == Rotating {
			fmt.Println("Target:", m.targetColour, "Rotating")
			if !m.roughDirectionKnown() {
				// Continue rotating.
				if m.ballInView {
					m.setSpeeds(0, 0, SLOW_ROTATE_SPEED)
				} else {
					m.setSpeeds(0, 0, ROTATE_SPEED)
				}
				continue
			}
			m.phase = Advancing
			m.advanceReverseStartTime = time.Now()
			// Fall through.
		}

		if m.phase == Advancing {
			fmt.Println("Target:", m.targetColour, "Advancing")

			closeEnough := !forward.IsFar() && forward.BestGuess() <= 110 &&
				!forwardRight.IsFar() && forwardRight.BestGuess() <= 70 &&
				!forwardLeft.IsFar() && forwardLeft.BestGuess() <= 70

			if m.ballFixed && !closeEnough {
				// We're approaching the ball but not yet close enough.
				sideways := m.getTOFDifference()
				rotation := m.getDirectionAdjust()
				m.setSpeeds(FORWARD_SPEED, sideways, rotation)
				continue
			} else {
				// Either we've lost the ball, or we're close enough, so we should
				// switch to reversing.
				m.phase = Reversing
				m.advanceDuration = time.Since(m.advanceReverseStartTime)
				m.advanceReverseStartTime = time.Now()
				m.setSpeeds(-FORWARD_SPEED, 0, 0)
				if !m.ballFixed {
					// We haven't found the current ball yet.
					continue
				}
				// Fall through (=> we're close enough).
			}
		}

		fmt.Println("Reached target ball:", m.targetColour, "in", time.Since(startTime))
		m.targetBallIdx++
		if m.targetBallIdx < len(targetSequence) {
			fmt.Println("Next target ball: ", targetSequence[m.targetBallIdx])
		} else {
			fmt.Println("Done!!")
		}
	}

	m.Propeller.SetMotorSpeeds(0, 0, 0, 0)
}

func (m *RainbowMode) reset() {
	m.perceivedSize = 0
	m.ballX = 0
	m.roughDirectionCount = 0
	m.phase = Rotating
	m.ballFixed = false
}

func (m *RainbowMode) processImage(img gocv.Mat) {
	w := img.Cols()
	h := img.Rows()
	if w != 640 || h != 480 {
		fmt.Printf("Read image %v x %v\n", img.Cols(), img.Rows())
	}

	if atomic.LoadInt32(&m.savePicture) == 1 {
		m.pictureIndex++
		saveFile := fmt.Sprintf("/tmp/image-%v.jpg", m.pictureIndex)
		success := gocv.IMWrite(saveFile, img)
		fmt.Printf("TestMode: wrote %v? %v\n", saveFile, success)
		atomic.StoreInt32(&m.savePicture, 0)
	}

	hsv := gocv.NewMat()
	defer hsv.Close()
	gocv.CvtColor(img, hsv, gocv.ColorBGRToHSV)
	pos, err := rainbow.FindBallPosition(
		hsv,
		rainbow.Balls[m.targetColour.String()],
	)
	if err == nil {
		fmt.Printf("Found at %#v\n", pos)
		m.ballX = pos.X
		m.perceivedSize = pos.Radius
		if m.ballX >= (X_STRAIGHT_AHEAD-X_PLUS_OR_MINUS) && m.ballX <= (X_STRAIGHT_AHEAD+X_PLUS_OR_MINUS) {
			fmt.Println("Ball seen roughly ahead")
			m.roughDirectionCount++
			m.ballFixed = true
		}
		m.ballInView = true
	} else {
		fmt.Printf("Not found: %v\n", err)
		m.roughDirectionCount = 0
		m.ballFixed = false
		m.ballInView = false
	}
}

func (m *RainbowMode) roughDirectionKnown() bool {
	return m.roughDirectionCount >= 2
}

func (m *RainbowMode) nowCloseEnough() bool {
	return m.perceivedSize > 120
}

func (m *RainbowMode) getDirectionAdjust() float64 {
	return DIRECTION_ADJUST_FACTOR * float64(X_STRAIGHT_AHEAD-m.ballX)
}

func (m *RainbowMode) getTOFDifference() float64 {
	return float64(0)
}

func (m *RainbowMode) stopSequence() {
	if !m.running {
		fmt.Println("Not running")
		return
	}
	fmt.Println("Stopping sequence...")

	m.cancelSequence()
	m.cancelSequence = nil
	<-m.sequenceDone
	m.running = false
	m.sequenceDone = nil
	atomic.StoreInt32(&m.paused, 0)
}

func (m *RainbowMode) pauseOrResumeSequence() {
	if atomic.LoadInt32(&m.paused) == 1 {
		fmt.Println("Resuming sequence...")
		atomic.StoreInt32(&m.paused, 0)
	} else {
		fmt.Println("Pausing sequence...")
		atomic.StoreInt32(&m.paused, 1)
	}
}

func (m *RainbowMode) OnJoystickEvent(event *joystick.Event) {
	m.joystickEvents <- event
}

type Filter struct {
	Name    string
	samples []int
}

func (f *Filter) Accumulate(sample int) {
	f.samples = append(f.samples, sample)
	if len(f.samples) > 3 {
		f.samples = f.samples[1:]
	}
}

func (f *Filter) IsFar() bool {
	// Look backwards in the samples
	for i := len(f.samples) - 1; i >= 0; i-- {
		if f.samples[i] > 400 {
			// 400mm is far by definition.
			return true
		}
		if f.samples[i] > 0 {
			// any recent non-far sample means we're not far.
			return false
		}
	}
	return true
}

func (f *Filter) IsGood() bool {
	return f.BestGuess() > 0 && f.BestGuess() < 200
}

func (f *Filter) BestGuess() int {
	var goodSamples []int
	for _, s := range f.samples {
		if s != 0 {
			goodSamples = append(goodSamples, s)
		}
	}
	if len(goodSamples) == 0 {
		return 0
	}
	sort.Ints(goodSamples)
	return goodSamples[len(goodSamples)/2]
}