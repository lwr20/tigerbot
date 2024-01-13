#!/bin/bash
docker pull lwr20/firebot:latest
docker run -ti --device=/dev/ttyACM0 --rm lwr20/firebot:latest
