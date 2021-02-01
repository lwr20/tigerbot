from time import sleep
import ZeroBorg
from approxeng.input.selectbinder import ControllerResource


class Motor:
    def __init__(self):
        self.motorone = 0
        self.motortwo = 0
        self.motorthree = 0
        self.motorfour = 0
        self.ZB = ZeroBorg.ZeroBorg()
        self.ZB.Init()
        self.ZB.ResetEpo()
        self.ZB.SetCommsFailsafe(false)

    def one(self, power):
        self.motorone = power
        self.ZB.SetMotor1(power)

    def two(self, power):
        self.motortwo = power
        self.ZB.SetMotor2(power)

    def three(self, power):
        self.motorthree = power
        self.ZB.SetMotor3(power)

    def four(self, power):
        self.motorfour = power
        self.ZB.SetMotor4(power)

    def stop(self):
        self.ZB.MotorsOff()


motor = Motor()


def mixer(in_yaw, in_throttle):
    left = in_throttle + in_yaw
    right = in_throttle - in_yaw
    scale_left = abs(left / 125.0)
    scale_right = abs(right / 125.0)
    scale_max = max(scale_left, scale_right)
    scale_max = max(1, scale_max)
    out_left = int(self.constrain(left / scale_max, -125, 125))
    out_right = int(self.constrain(right / scale_max, -125, 125))
    results = [out_right, out_left]
    return results


def constrain(val, min_val, max_val):
    return min(max_val, max(min_val, val))


def update_motor():
    mixer_results = self.mixer(self.x_axis, self.y_axis)
    # print (mixer_results)
    power_left = int((mixer_results[0] / 125.0) * 100)
    power_right = int((mixer_results[1] / 125.0) * 100)
    print("left: " + str(power_left) + " right: " + str(power_right))

    if not self.disable_motor:
        self.motor.one((-power_right * self.max_power))
        self.motor.two((power_left * self.max_power))


def axis_handler(signal, number, value, init):
    # Axis 0 = left stick horizontal.  -ve = left
    # Axis 1 = left stick vertical.    -ve = up
    # Axis 5 = right stick vertical.   -ve = up
    # Axis 2 = right stick horizontal. -ve left
    if number == 5:
        if value > 130:
            print("Backwards")
        elif value < 125:
            print("Forward")
        self.y_axis = value
        if self.y_axis > 130:
            self.y_axis = -(self.y_axis - 130)
        elif self.y_axis < 125:
            self.y_axis = ((-self.y_axis) + 125)
        else:
            self.y_axis = 0.0
    elif number == 2:
        if value > 130:
            print("Right")
        elif value < 125:
            print("Left")
        self.x_axis = value
        if self.x_axis > 130:
            self.x_axis = (self.x_axis - 130)
        elif self.x_axis < 125:
            self.x_axis = -((-self.x_axis) + 125)
        else:
            self.x_axis = 0.0
        print("X: " + str(self.x_axis))
    self.update_motor()


def set_speeds(power_left, power_right):
    """
    As we have an motor hat, we can use the motors

    :param power_left:
        Power to send to left motor
    :param power_right:
        Power to send to right motor, will be inverted to reflect chassis layout
    """
    motor.one.speed(-power_right)
    motor.two.speed(power_left)


# Outer try / except catches the RobotStopException we just defined, which we'll raise when we want to
# bail out of the loop cleanly, shutting the motors down. We can raise this in response to a button press
try:
    while True:
        # Inner try / except is used to wait for a controller to become available, at which point we
        # bind to it and enter a loop where we read axis values and send commands to the motors.
        try:
            # Bind to any available joystick, this will use whatever's connected as long as the library
            # supports it.
            with ControllerResource(dead_zone=0.1, hot_zone=0.2) as joystick:
                print('Controller found, press HOME button to exit, use left stick to drive.')
                print(joystick.controls)
                # Loop until the joystick disconnects, or we deliberately stop by raising a
                # RobotStopException
                while joystick.connected:
                    # Get joystick values from the left analogue stick
                    x_axis, y_axis = joystick['lx', 'ly']
                    # Get power from mixer function
                    power_left, power_right = mixer(in_yaw=x_axis, in_throttle=y_axis)
                    # Set motor speeds
                    set_speeds(power_left, power_right)
                    # Get a ButtonPresses object containing everything that was pressed since the last
                    # time around this loop.
                    joystick.check_presses()
                    # Print out any buttons that were pressed, if we had any
                    if joystick.has_presses:
                        print(joystick.presses)
                    # If home was pressed, raise a RobotStopException to bail out of the loop
                    # Home is generally the PS button for playstation controllers, XBox for XBox etc
                    if 'home' in joystick.presses:
                        raise RobotStopException()
        except IOError:
            # We get an IOError when using the ControllerResource if we don't have a controller yet,
            # so in this case we just wait a second and try again after printing a message.
            print('No controller found yet')
            sleep(1)
except RobotStopException:
    # This exception will be raised when the home button is pressed, at which point we should
    # stop the motors.
    stop_motors()
