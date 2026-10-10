# set4, harness defect H45 (found while the README was written, from set4-arch/run-a's own records): the reader of
# what is left for a domain looked for the site's home under subscriptions/<s>/domains/<id>; the product's home is
# subscriptions/<s>/sites/<id> (internal/hostingpath SiteHome). The listing was empty before and after, so "the
# site's home is gone" and "no new site directory" were never measured by the run-a cells. Second part: after a
# delete the socket was looked for under the pool's /var/run/... spelling while the listing spells it /run/...
# (Debian and Ubuntu), so "the socket is gone" was not measured there either. usage: patch4.py HARNESS_DIR
import io
import os
import sys

R = sys.argv[1]
p = os.path.join(R, 'guest_set4_native.py')
s = io.open(p, encoding='utf-8', newline='').read()
old = '''    site_dirs = _listing(SITES_ROOT, "*/domains/*")
'''
new = '''    site_dirs = _listing(SITES_ROOT, "*/sites/*")       # internal/hostingpath.SiteHome
'''
assert s.count(old) == 1
s = s.replace(old, new)
old = '''        mine = {"site_home": f"{SITES_ROOT}/{sub}/domains/{dom}", "acme_challenge_root": f"{ACME_ROOT}/subscriptions/{sub}/domains/{dom}"}
'''
new = '''        mine = {"site_home": f"{SITES_ROOT}/{sub}/sites/{dom}", "acme_challenge_root": f"{ACME_ROOT}/subscriptions/{sub}/domains/{dom}"}
'''
assert s.count(old) == 1
s = s.replace(old, new)
old = '''            "sockets": sockets, "site_directories": site_dirs, "acme_challenge_directories": acme_dirs,
'''
new = '''            "sockets": sockets, "site_directories": site_dirs, "acme_challenge_directories": acme_dirs,
            "site_directories_root": rid._stat(SITES_ROOT),
            "subscription_directories": {d: sorted(os.listdir(d)) for d in _listing(SITES_ROOT, "*") if os.path.isdir(d)},
'''
assert s.count(old) == 1
s = s.replace(old, new)
io.open(p, 'w', encoding='utf-8', newline='').write(s)

p = os.path.join(R, 'set4_trial.py')
s = io.open(p, encoding='utf-8', newline='').read()
old = '''                 "socket_left": bool(facts) and any(s.get("path") == facts.get("listen") and s.get("exists") for s in left.get("sockets") or []),
'''
new = '''                 "socket_left": bool(facts) and any(
                     s.get("path") == str(facts.get("listen")).replace("/var/run/", "/run/") and s.get("exists")
                     for s in left.get("sockets") or []),
                 "site_home_existed_with_the_site": mine.get("site_home") in (with_site.get("site_directories") or []),
                 "site_directories_after": left.get("site_directories"),
                 "sockets_after": [s.get("path") for s in left.get("sockets") or []],
'''
assert s.count(old) == 1
s = s.replace(old, new)
old = '''                   deleted["status"] == 200 and gone and not after["domain_rows"] and not after["site_rows"] and after["account"] is None
                   and not after["site_home_left"] and not after["nginx_files_naming_the_domain"]
'''
new = '''                   deleted["status"] == 200 and gone and not after["domain_rows"] and not after["site_rows"] and after["account"] is None
                   and after["site_home_existed_with_the_site"] and not after["site_home_left"]
                   and not after["nginx_files_naming_the_domain"]
'''
assert s.count(old) == 1
s = s.replace(old, new)
old = '''                  "new_site_directories": new_site_dirs, "domain_rows": panel_a.get("domains"), "site_rows": panel_a.get("sites"),
'''
new = '''                  "new_site_directories": new_site_dirs, "site_directories": [before.get("site_directories"), after.get("site_directories")],
                  "subscription_directories_after": after.get("subscription_directories"),
                  "domain_rows": panel_a.get("domains"), "site_rows": panel_a.get("sites"),
'''
assert s.count(old) == 1
s = s.replace(old, new)
old = '''                        "its files": not new_site_dirs,
'''
new = '''                        # H45: only a listing that sees the existing sites can show that no new one was left
                        "its files": not new_site_dirs and bool(before.get("site_directories")),
'''
assert s.count(old) == 1
s = s.replace(old, new)
old = """        removed_true = {"its web server configuration":"""
new = """        homes = re.findall(r"home=(/[^,]+),", chr(10).join(native["product_journal_since"] or []))
        native["home_named_by_useradd_in_the_journal"] = homes
        native["that_home_is_listed_after"] = [h for h in homes if h in (after.get("site_directories") or [])]
        removed_true = {"its web server configuration":"""
assert s.count(old) == 1
s = s.replace(old, new)
old = """                        "its files": not new_site_dirs and bool(before.get("site_directories")),
"""
new = """                        "its files": not new_site_dirs and bool(before.get("site_directories"))
                        and not native["that_home_is_listed_after"],
"""
assert s.count(old) == 1
s = s.replace(old, new)
io.open(p, 'w', encoding='utf-8', newline='').write(s)
print("patched H45")
