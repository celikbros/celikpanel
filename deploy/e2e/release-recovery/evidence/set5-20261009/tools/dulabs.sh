#!/bin/bash
du -sh /var/tmp/cp-release-drill-s5-* 2>/dev/null; du -sh /var/tmp/cp-release-drill-s5-d13-good-a/* 2>/dev/null | sort -h | tail -5; du -sh /var/tmp/cp-set5-run; df -B1M /var/tmp | tail -1
