#!/bin/bash
B=/var/tmp/cp-upd1-build/20261009t030455z
ls -la $B | head -30
python3 -c "import json,sys;d=json.load(open(sys.argv[1]));print(sorted(d));print({k:d[k] for k in d if not isinstance(d[k],dict)});print(sorted(d['baseline']))" $B/upd1-artifacts.json
du -sh $B
