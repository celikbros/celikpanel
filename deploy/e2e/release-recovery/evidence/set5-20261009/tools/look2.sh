#!/usr/bin/env bash
D=/var/tmp/cp-pair-accept/dist/a8bd6124a99390246c978281992e6eaf48fbb377-acceptance-license
du -sh $D/* | sort -h
du -sh $D/src/* | sort -h | tail -6
du -sh $D/src/web/* 2>/dev/null | sort -h | tail -4
cat $D/dist.json
echo; du -sh /var/tmp/cp-upd1-build/20261009t102025z/* /var/tmp/cp-upd1-build/20261009t102025z/repo/web/* /var/tmp/cp-upd1-build/20261009t102025z/repo/.git 2>/dev/null | sort -h | tail -8
