#!/bin/bash
# usage: trk.sh LABNAME NODE [N]
ev=$(ls -d /var/tmp/cp-release-drill-$1/evidence/$2/upd1/*/ | tail -1)
python3 - "$ev" "${3:-25}" <<'PY'
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
    print(s['utc'][11:], 'U',(s.get('update_status') or {}).get('http'), u.get('status'),
          '| R',(s.get('recovery_api') or {}).get('http'), r.get('phase'), r.get('reason'), r.get('automatic_recovery'), r.get('terminal_proof'), r.get('panel_state'),
          '| CLI', cli.get('observation'), cli.get('phase'), cli.get('reason'), cli.get('automatic_recovery'), cli.get('terminal_proof'),
          '| perr',s.get('panel_error'),'shell',s.get('shell_fetch'),'clierr',s.get('cli_error'),
          '| agree', ag.get('verdict') or ag.get('agree'))
PY
