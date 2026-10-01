#!/usr/bin/env python3
"""upd4 run copy harness-h12: two mechanical corrections of the mgmt-off-reboot steps (run copy only).

H12: management-return read the owner's Panel state right after login while the Panel still answered
     503 PANEL_STARTING ("Panel management is still starting"); it now waits, bounded (180 s, 3 s steps,
     read-only GET of the recovery status the Panel serves while starting), until the Panel reports
     panel_state=ready, and records the wait.
H13: the verdicts cut for the update-only window used management_off_at taken after
     `systemctl disable --now` had returned, so samples of the Panel being stopped by the owner counted as
     a Panel outage outside the transaction; the instant is now taken before the command
     (the completion instant is kept as management_off_done_at).
"""
import sys
from pathlib import Path

path = Path(sys.argv[1])
text = path.read_text(encoding="utf-8")

old_off = '''        command = "systemctl disable --now celikpanel-panel.service celikpanel-agent.service"
        done = self.guest(command, timeout=180)
        self.state["management_off_at"] = time.time()
'''
new_off = '''        command = "systemctl disable --now celikpanel-panel.service celikpanel-agent.service"
        # H13 (upd4 run copy): the owner's stop starts here; the update-only verdict window ends before it.
        self.state["management_off_at"] = time.time()
        done = self.guest(command, timeout=180)
        self.state["management_off_done_at"] = time.time()
        checks["management_off_window"] = {"requested_at": self.state["management_off_at"],
                                           "done_at": self.state["management_off_done_at"]}
'''
assert text.count(old_off) == 1, "management_off anchor"
text = text.replace(old_off, new_off)

old_ret = '''        if back["login_ok"]:
            after = self.panel_truth("after-management-return")
'''
new_ret = '''        if back["login_ok"]:
            # H12 (upd4 run copy): the Panel answers 503 PANEL_STARTING until its management is ready; the owner
            # would reload until the screens load. Wait (read-only, bounded) for panel_state=ready first.
            ready_deadline = time.monotonic() + 180
            waits = []
            while True:
                probe = self.api("GET", f"/api/v1/recovery/status?request_id={self.state['request_id']}",
                                 purpose="RecoveryStatus (management return readiness)")
                state = ((probe.json() or {}) if probe.status == 200 else {}).get("panel_state")
                waits.append({"at": utc_now(), "http": probe.status, "panel_state": state})
                if state == "ready" or time.monotonic() >= ready_deadline:
                    break
                time.sleep(3)
            back["readiness_wait"] = {"reads": len(waits), "first": waits[0], "last": waits[-1],
                                      "ready": waits[-1].get("panel_state") == "ready"}
            after = self.panel_truth("after-management-return")
'''
assert text.count(old_ret) == 1, "management_return anchor"
text = text.replace(old_ret, new_ret)
path.write_text(text, encoding="utf-8")
print("patched", path)
