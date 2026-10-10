#!/usr/bin/env python3
"""set9: the cold-load fix of e508af230 measured in a real Chrome against a real Panel, on a disposable QEMU guest.

set7's cell (``set7_trial.Set7BrowserTrial``) with every step unchanged, and two differences only:

  * its run root is /var/tmp/cp-set9-run (handover and the root-only password file live there), not set7's;
  * besides set7's restart request, the browser may ask the harness to STOP the Panel's service and to START it
    again (a file ``restart-request-stopN`` or ``restart-request-startN`` in the handover directory; the driver
    answers ``restart-done-stopN.json`` / ``restart-done-startN.json`` as set7 does for a restart). Used by set9 cell
    4: a cold load with the Panel stopped. Only ``systemctl stop|start celikpanel-panel.service`` on this disposable
    guest; nothing else is changed.

  set9_trial.py plan --cell upd1-debian13-good --artifacts A.json --work-root /var/tmp/cp-release-drill-X --hand NAME [--dry-run]
  set9_trial.py run  --cell upd1-debian13-good --artifacts A.json --work-root /var/tmp/cp-release-drill-X --hand NAME --execute

Every result carries ``native_evidence: false``. No certificate authority and no licence service is contacted by the
driver (set6's name pinning is its first step); the browser opens only the loopback address of the SSH forward.
"""
from __future__ import annotations

from pathlib import Path
import sys
import time

HERE = Path(__file__).resolve().parent
if str(HERE) not in sys.path:
    sys.path.insert(0, str(HERE))
import set7_trial as s7  # noqa: E402

s7.RUN_ROOT = Path("/var/tmp/cp-set9-run")
s7.HAND_ROOT = s7.RUN_ROOT / "hand"
s7.SECRET_ROOT = s7.RUN_ROOT / "secret"
s7.CELL_KIND = "set9-cold-load-and-hold-in-a-real-browser"


class Set9BrowserTrial(s7.Set7BrowserTrial):
    def restart_panel(self, number: str) -> dict:
        for verb in ("stop", "start"):
            if number.startswith(verb) and number[len(verb):].isdigit():
                before = time.time()
                script = (f"date -u +%FT%T.%NZ; systemctl show {s7.PANEL_UNIT} -p MainPID -p ActiveState\n"
                          f"systemctl {verb} {s7.PANEL_UNIT}; echo rc=$?\n"
                          f"date -u +%FT%T.%NZ; systemctl show {s7.PANEL_UNIT} -p MainPID -p ActiveState "
                          f"-p ActiveEnterTimestamp -p InactiveEnterTimestamp\n")
                out = self.guest(script, timeout=180).stdout
                return {"request": number, "what": f"systemctl {verb} {s7.PANEL_UNIT} on the disposable guest, by the harness",
                        "host_before": before, "host_after": time.time(), "guest_output": out.strip()}
        return super().restart_panel(number)


def main(argv: list[str] | None = None) -> int:
    s7.Set7BrowserTrial = Set9BrowserTrial  # main() of set7 constructs the class by this module-level name
    return s7.main(argv)


if __name__ == "__main__":
    raise SystemExit(main())
