#!/bin/bash
# usage: show.sh FILE [LINES]
tail -n ${2:-60} "$1" | cut -c1-600
