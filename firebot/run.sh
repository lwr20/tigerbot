#!/bin/bash
rshell 'cp led_rainbow.py /pyboard'
rshell 'repl ~ import led_rainbow ~'
python robot.py
