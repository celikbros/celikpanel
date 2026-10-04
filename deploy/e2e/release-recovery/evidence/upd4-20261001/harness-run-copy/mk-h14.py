#!/usr/bin/env python3
"""upd4 run copy harness-h14 (= harness-h12 + H14), one mechanical correction of the mgmt-off-reboot verdict cut.

H14: a guest sample's "t" is taken when the sampler cycle starts; the Panel probe runs last in the cycle
     (after the site, the DNS query, SMTP and cron probes; guest_upd1_workload.py sample()), about 2 s later
     on an isolated external-DNS node. A cycle that started just before the owner's
     `systemctl disable --now` therefore probes the Panel after it was stopped, and the update-only verdict
     window counted that as a Panel outage outside the transaction (upd1-arch-mgmt-off-reboot run-a,
     sample t=01:17:13.716 refused, unit stopped 01:17:15.567 guest time). The cut now ends one sample
     interval (5 s) before the stop request, for the guest and the host series alike.
"""
import sys
from pathlib import Path

path = Path(sys.argv[1])
text = path.read_text(encoding="utf-8")
old = '''            cut = self.state["management_off_at"] + skew
            samples = [s for s in samples if float(s.get("t", 0)) <= cut]
            checks["samples_until_management_off"] = len(samples)
'''
new = '''            # H14 (upd4 run copy): a cycle's "t" is its start; its Panel probe runs about 2 s later. Cycles that
            # started within one interval of the owner's stop request are left out of the update-only window.
            cut = self.state["management_off_at"] + skew - SAMPLE_INTERVAL_S
            samples = [s for s in samples if float(s.get("t", 0)) <= cut]
            checks["samples_until_management_off"] = len(samples)
            checks["management_off_cut"] = {"guest_clock": cut, "margin_seconds": SAMPLE_INTERVAL_S}
'''
assert text.count(old) == 1, "verdict cut anchor"
text = text.replace(old, new)
old_host = '''            host_samples = [s for s in host_samples if float(s.get("t", 0)) <= self.state["management_off_at"]]
'''
new_host = '''            host_samples = [s for s in host_samples
                            if float(s.get("t", 0)) <= self.state["management_off_at"] - SAMPLE_INTERVAL_S]
'''
assert text.count(old_host) == 1, "host cut anchor"
text = text.replace(old_host, new_host)
path.write_text(text, encoding="utf-8")
print("patched", path)
