import time
from machine import Pin, PWM
import rp2

direct = Pin(20, Pin.OUT)

# Construct PWM object, with LED on Pin(25).
pwm = PWM(Pin(25))
pwm2 = PWM(Pin(21))

# Set the PWM frequency.
pwm.freq(1000)
pwm2.freq(10000)
pwm2.duty_u16(0)

# encoder reading
position = 0
enc1 = Pin(18, Pin.IN)
enc2 = Pin(19, Pin.IN)

enc1.irq(lambda pin: print("enc1 falling IRQ with flags:", pin.irq().flags()), Pin.IRQ_FALLING)
enc2.irq(lambda pin: print("enc2 IRQ with flags:", pin.irq().flags()), Pin.IRQ_FALLING)

while 1:
    print("ramping up")
    for i in range(255):
        pwm.duty_u16(i*i)
        pwm2.duty_u16(i*i)
        time.sleep(0.005)
    print("ramping down")
    for i in range(255, 0, -1):
        pwm.duty_u16(i*i)
        pwm2.duty_u16(i*i)
        time.sleep(0.005)
    direct.toggle()
    print("looping")


#



# def pid(i, nextpos, error, last_error, nexttime, newspeed, desired_speed, maxintegral, servoval):
#     nextpos = 0
#     maxintegral = 1000 / Ki
#     reset_motor()
#     nexttime = millidiv + cnt




# PRI pid | i, nextpos, error, last_error, nexttime, newspeed, desired_speed, maxintegral, servoval
#   nextpos := 0
#   maxintegral := 1000 / Ki
#   resetMotors    ' enables the direction ports control from this cog
#   nexttime := millidiv + cnt
#   repeat
#     waitcnt(nexttime)
#     nexttime += millidiv * 5
#     'Here once every 5 milliseconds
#
#     ' Update motor speeds
#     repeat i from 0 to 3          ' loop takes just under 1ms to complete
#       b := i2c.get(speedbase+i)
#       desired_speed := ~b         ' note sneaky '~' that sign extends the byte value
#       nextpos := quad.count(i)
#
#       last_error := desired_speed - actual_speed[i]
#       actual_speed[i] := (nextpos - lastpos[i]) * 3
#       lastpos[i] := nextpos
#       error := desired_speed - actual_speed[i]
#       error_derivative[i] := error - last_error
#       error_integral[i] += error
#       error_integral[i] := -maxintegral #> error_integral[i] <# maxintegral
#       newspeed := Kp * error + Ki * error_integral[i] + Kd * error_derivative[i]
#
#       setMotorSpeed(i, newspeed)
#
#     ' Update servo parameters
#     position1 := ((i2c.get(servo0hi) << 8) + i2c.get(servo0lo)) * 2 + 90_000
#     position2 := (i2c.get(servo1) * 550) + 40_000
#     position3 := (i2c.get(servo2) * 550) + 40_000
#
# PRI setMotorSpeed(motor, speed)
#   pwm.set_duty(motor, speed)
#
#   if speed == 0
#     outa[motorD1[motor]] := %0
#   elseif speed > 0
#     outa[motorD1[motor]] := %0
#   else
#     outa[motorD1[motor]] := %1
