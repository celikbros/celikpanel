#!/bin/bash
pkill -f "scan11.py /mnt/c/CELIKBROS PROJECTS" && echo killed; sleep 1; pgrep -af scan11.py || echo "none left"
