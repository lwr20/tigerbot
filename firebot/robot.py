import serial
from time import sleep

# # All we need, as we don't care which controller we bind to, is the ControllerResource
# from approxeng.input.selectbinder import ControllerResource

serialportname = "/dev/ttyACM0"
serialportspeed = 115200


def set_speeds(power_left, power_right):
    """
    No motor hat - print what we would have sent to it if we'd had one.
    """
    print('Left: {}, Right: {}'.format(power_left, power_right))
    with serial.Serial(serialportname, serialportspeed, timeout=1) as ser:
        command = "\n+sa %s %s %s %s\n" % (power_right,
                                            power_right,
                                            -power_left,
                                            -power_left,
                                            )
        print("sending: %s" % command)
        ser.write(bytes(command, encoding='utf-8'))
    sleep(0.1)

if __name__ == "__main__":
    set_speeds(10, 10)
