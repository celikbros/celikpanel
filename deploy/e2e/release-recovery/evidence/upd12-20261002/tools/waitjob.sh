#!/bin/bash
L=/var/tmp/cp-upd12-run/logs
name=$1; max=${2:-580}
start=$(date +%s)
while [ ! -e $L/$name.end ]; do
  now=$(date +%s)
  if [ $((now - start)) -ge $max ]; then echo "still running after $max s: $name"; tail -n 3 $L/$name.err | cut -c1-200; exit 0; fi
  sleep 15
done
echo "job $name ended $(cat $L/$name.end) rc=$(cat $L/$name.rc)"; tail -n 5 $L/$name.err | cut -c1-300; tail -n 3 $L/$name.out | cut -c1-300
