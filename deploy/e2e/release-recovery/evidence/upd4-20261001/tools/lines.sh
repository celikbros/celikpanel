#!/bin/bash
# usage: lines.sh CELL RELFILE FROM TO -> print a line range of a staged file (RUN env selects run-a/run-b)
S=/var/tmp/cp-upd4-run/stage/upd4-20261001
sed -n "$3,$4p" "$S/$1/${RUN:-run-a}/$2" | cut -c1-${WIDTH:-400}
