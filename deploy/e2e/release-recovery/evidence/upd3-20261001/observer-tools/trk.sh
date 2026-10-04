#!/bin/bash
# usage: trk.sh LABNAME NODE [N]  -- read-only view of the latest track samples of a running cell
ev=$(ls -d /var/tmp/cp-release-drill-$1/evidence/$2/upd1/*/ | tail -1)
python3 - "$ev" "${3:-12}" <<'PY'
import json,sys,glob
ev,n=sys.argv[1],int(sys.argv[2])
files=sorted(glob.glob(ev+'steps/*-track*/samples/*.json'))
print('samples',len(files))
for f in files[-n:]:
    s=json.load(open(f))
    u=(s.get('update_status') or {}).get('body') or {}
    r=(s.get('recovery_api') or {}).get('body') or {}
    cli=None
    try: cli=json.loads(s['cli']['json']['stdout'] or 'null')
    except Exception: pass
    cli=cli or {}
    ag=s.get('agreement') or {}
    print(s['utc'][11:22], 'U',(s.get('update_status') or {}).get('http'), u.get('status'), (u.get('summary') or '')[:100],
          '| R',(s.get('recovery_api') or {}).get('http'), r.get('phase'), r.get('terminal_proof'), r.get('automatic_recovery'), r.get('waiting_for'),
          '| CLI', cli.get('phase'), cli.get('terminal_proof'), cli.get('automatic_recovery'), cli.get('waiting_for'), cli.get('failure_code'),
          '| perr',str(s.get('panel_error'))[:60],'clierr',str(s.get('cli_error'))[:60], '|', ag.get('verdict'))
PY
