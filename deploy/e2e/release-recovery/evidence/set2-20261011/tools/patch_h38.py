"""set2 H38 (rid-arch run-a): right after the lab wrote its ACME isolation into /etc/hosts, Arch's resolver
(systemd-resolved, which re-reads /etc/hosts at most every two seconds) still answered the public address, so the
isolation was not confirmed and the cell was stopped by hand before its Let's Encrypt section. The helper now waits
until the names resolve to loopback, and the Let's Encrypt section does not send a request unless that is confirmed."""
import sys
d = sys.argv[1]
p = d + "/guest_request_identity_native.py"
s = open(p, encoding="utf-8", newline="").read()
old = '''    return {"action": "lab-isolate-acme", "hosts_lines": [l for l in HOSTS.read_text().splitlines() if "acme" in l],
            "resolves": {host: run(["getent", "hosts", host]).get("stdout", "").strip() for host in ACME_HOSTS}, "at": utc()}'''
new = '''    resolves, waited = {}, 0.0
    started = time.time()
    while True:     # H38: a caching resolver takes the new /etc/hosts lines a moment later
        resolves = {host: run(["getent", "hosts", host]).get("stdout", "").strip() for host in ACME_HOSTS}
        waited = round(time.time() - started, 1)
        if all(loopback_only(answer) for answer in resolves.values()) or waited > 30:
            break
        time.sleep(1)
    return {"action": "lab-isolate-acme", "hosts_lines": [l for l in HOSTS.read_text().splitlines() if "acme" in l],
            "resolves": resolves, "confirmed": all(loopback_only(answer) for answer in resolves.values()),
            "confirmed_after_seconds": waited, "at": utc()}'''
assert s.count(old) == 1
s = s.replace(old, new)
old = '''def cpmove_archive(members: list, public_html_directory_member: bool = True) -> bytes:'''
new = '''def loopback_only(getent_answer: str) -> bool:
    """A ``getent hosts`` answer whose every address is this host's own loopback (and that is not empty)."""
    addresses = [line.split()[0] for line in getent_answer.splitlines() if line.split()]
    return bool(addresses) and all(address in ("127.0.0.1", "::1") for address in addresses)


def cpmove_archive(members: list, public_html_directory_member: bool = True) -> bytes:'''
assert s.count(old) == 1
s = s.replace(old, new)
old = '''    return {"certbot": shutil.which("certbot", path=ENV["PATH"]), "certbot_logs": logs, "certbot_log_count": len(logs),
            "acme_isolated": ACME_MARK in hosts,
            "resolves": {host: run(["getent", "hosts", host]).get("stdout", "").strip() for host in ACME_HOSTS},'''
new = '''    resolves = {host: run(["getent", "hosts", host]).get("stdout", "").strip() for host in ACME_HOSTS}
    return {"certbot": shutil.which("certbot", path=ENV["PATH"]), "certbot_logs": logs, "certbot_log_count": len(logs),
            "acme_isolated": ACME_MARK in hosts and all(loopback_only(answer) for answer in resolves.values()),
            "resolves": resolves,'''
assert s.count(old) == 1
s = s.replace(old, new)
old = '''    found = run(["find", "/var/log/letsencrypt", "/var/lib/celikpanel", "/etc/letsencrypt", "-maxdepth", "6", "-name",'''
new = '''    found = run(["find", "/var/log/letsencrypt", "/var/log/celikpanel", "/var/lib/celikpanel", "/etc/letsencrypt", "-maxdepth", "6", "-name",'''
assert s.count(old) == 1
s = s.replace(old, new)
open(p, "w", encoding="utf-8", newline="").write(s)

p = d + "/request_identity_trial.py"
s = open(p, encoding="utf-8", newline="").read()
old = '''        self.check("lab isolation: the Let's Encrypt directory names resolve to this guest's loopback",
                   all("127.0.0.1" in str(v) or "::1" in str(v) for v in (isolated.get("resolves") or {}).values()),
                   isolated.get("resolves"))'''
new = '''        self.current["acme_isolation"].update(confirmed=isolated.get("confirmed"), after_seconds=isolated.get("confirmed_after_seconds"))
        self.check("lab isolation: the Let's Encrypt directory names resolve to this guest's loopback only",
                   isolated.get("confirmed") is True, isolated.get("resolves"))'''
assert s.count(old) == 1
s = s.replace(old, new)
old = '''        counters = ("certbot_logs", "panel_and_agent_lines", "nginx_reload_lines")'''
new = '''        # H38: no request is sent to this route unless the lab's ACME isolation is confirmed on this guest.
        isolation = self.snap2(route + "-isolation", "read-acme")
        if isolation.get("acme_isolated") is not True:
            self.not_reached(route, ("i", "ii", "iii", "iv"), "the lab's ACME isolation is not confirmed on this guest, so no "
                             "request was sent to the route (a certificate authority could have been contacted)",
                             isolation.get("resolves"))
            return
        counters = ("certbot_logs", "panel_and_agent_lines", "nginx_reload_lines")'''
assert s.count(old) == 1
s = s.replace(old, new)
open(p, "w", encoding="utf-8", newline="").write(s)

p = d + "/test_request_identity_trial.py"
s = open(p, encoding="utf-8", newline="").read()
old = '''    def test_modes(self):'''
new = '''    def test_only_loopback_answers_confirm_the_acme_isolation(self):
        self.assertTrue(native.loopback_only("::1             acme-v02.api.letsencrypt.org"))
        self.assertTrue(native.loopback_only("127.0.0.1 a\n::1 a"))
        self.assertFalse(native.loopback_only(""))
        self.assertFalse(native.loopback_only("2606:4700:60:0:f53d:5624:85c7:3a2c x.pacloudflare.com"))
        self.assertFalse(native.loopback_only("127.0.0.1 a\n203.0.113.9 a"))
        driver = (HERE / "request_identity_trial.py").read_text(encoding="utf-8")
        self.assertLess(driver.index('isolation.get("acme_isolated") is not True'), driver.index("what one entry leaves"))

    def test_modes(self):'''
assert s.count(old) == 1
s = s.replace(old, new)
open(p, "w", encoding="utf-8", newline="").write(s)
print("H38 patched")
