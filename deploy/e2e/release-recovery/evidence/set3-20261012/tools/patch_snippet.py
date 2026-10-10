# set3: one optional, recorded owner action for a second Arch reading: the nginx snippet the PHP vhost template includes.
ROOT = r'C:\CELIKBROS PROJECTS\celikpanel\deploy\e2e\release-recovery' + '\\'


class Patch:
    def __init__(self, name):
        self.path = ROOT + name
        self.s = open(self.path, encoding='utf-8', newline='').read()
        assert '\r\n' not in self.s, name

    def rep(self, a, b, n=1):
        assert self.s.count(a) == n, (a[:90], self.s.count(a))
        self.s = self.s.replace(a, b)

    def save(self):
        open(self.path, 'w', encoding='utf-8', newline='').write(self.s)


h = Patch('guest_request_identity_native.py')
h.rep('''def owner_php_probe(args: dict) -> dict:''', '''NGINX_PHP_SNIPPET = Path("/etc/nginx/snippets/fastcgi-php.conf")
# The file Debian's and Ubuntu's nginx packages ship under that name (nginx-common), which the product's PHP vhost
# template includes (internal/services/templates/nginx/vhost.conf.tmpl). Arch's nginx package has no snippets directory.
NGINX_PHP_SNIPPET_TEXT = (
    "# regex to split $uri to $fastcgi_script_name and $fastcgi_path\\n"
    "fastcgi_split_path_info ^(.+?\\\\.php)(/.*)$;\\n\\n"
    "# Check that the PHP script exists before passing it\\n"
    "try_files $fastcgi_script_name =404;\\n\\n"
    "# Bypass the fact that try_files resets $fastcgi_path_info\\n"
    "# see: http://trac.nginx.org/nginx/ticket/321\\n"
    "set $path_info $fastcgi_path_info;\\n"
    "fastcgi_param PATH_INFO $path_info;\\n\\n"
    "fastcgi_index index.php;\\n"
    "include fastcgi.conf;\\n")


def owner_nginx_php_snippet(args: dict) -> dict:
    """set3, second Arch reading only: the server owner places by hand the nginx snippet the product's PHP vhost
    includes, with the text Debian's nginx package ships. Nothing is written when the file already exists."""
    existed = NGINX_PHP_SNIPPET.exists()
    made_directory = False
    if not existed:
        if not NGINX_PHP_SNIPPET.parent.exists():
            NGINX_PHP_SNIPPET.parent.mkdir(mode=0o755)
            made_directory = True
        NGINX_PHP_SNIPPET.write_text(NGINX_PHP_SNIPPET_TEXT)
        os.chmod(NGINX_PHP_SNIPPET, 0o644)
    return {"action": "owner-nginx-php-snippet", "path": str(NGINX_PHP_SNIPPET), "existed_before": existed,
            "made_directory": made_directory, "file": _stat(str(NGINX_PHP_SNIPPET)),
            "sha256": hashlib.sha256(NGINX_PHP_SNIPPET.read_bytes()).hexdigest(),
            "fastcgi_conf": _stat("/etc/nginx/fastcgi.conf"), "nginx_test": run(["nginx", "-t"], timeout=30), "at": utc()}


def owner_php_probe(args: dict) -> dict:''')
h.rep('''    "read-http": read_http, "owner-php-probe": owner_php_probe,''',
      '''    "read-http": read_http, "owner-php-probe": owner_php_probe, "owner-nginx-php-snippet": owner_nginx_php_snippet,''')
h.rep('''    return {"units": shown, "pools": pools, "sockets": sockets,''',
      '''    return {"units": shown, "pools": pools, "sockets": sockets,
            "nginx_php_snippet": _stat("/etc/nginx/snippets/fastcgi-php.conf"),''')
h.save()

r = Patch('request_identity_trial.py')
r.rep('''import hashlib
import json
from pathlib import Path
import re
import secrets''', '''import hashlib
import json
import os
from pathlib import Path
import re
import secrets''')
r.rep('''        domain = PHP_SITE_DOMAIN
        self.current["php_before"] = self.snap2("php-before", "read-php")''', '''        domain = PHP_SITE_DOMAIN
        self.current["php_before"] = self.snap2("php-before", "read-php")
        if os.environ.get(OWNER_SNIPPET_ENV) == "1":
            # A second reading only (never the cell's first run): the owner places the one file whose absence stopped
            # the first run, so that what lies behind that stop can be measured. Recorded as an owner action.
            placed = self.owner2("nginx-php-snippet", "owner-nginx-php-snippet")
            self.current["owner_placed_the_nginx_snippet"] = {k: placed.get(k) for k in ("path", "existed_before", "made_directory",
                                                                                         "file", "sha256", "fastcgi_conf")}
            self.note("OWNER ACTION for this reading: the server owner placed /etc/nginx/snippets/fastcgi-php.conf by hand "
                      "(the text Debian's nginx package ships). Every result of C6b, C7 and C7b in this run is a result WITH "
                      "that file; without it the PHP site cannot be created on this platform (the first run).",
                      self.current["owner_placed_the_nginx_snippet"])''')
r.rep('''PHP_SITE_DOMAIN = "set3-php.test"''', '''OWNER_SNIPPET_ENV = "SET3_OWNER_NGINX_PHP_SNIPPET"   # "1": a second reading with one recorded owner action (see C6b)
PHP_SITE_DOMAIN = "set3-php.test"''')
r.rep('''                  "sections": self.sections, "findings": self.state["findings"],
                  "steps": [{k: s.get(k) for k in ("name", "verdict", "reason", "started_at", "finished_at")} for s in self.steps],
                  "overall": base.overall(verdicts),
                  "note": "request-identity: observations for the owner's review; no update is started and no P0 row is judged."}''',
      '''                  "sections": self.sections, "findings": self.state["findings"],
                  "owner_placed_the_nginx_php_snippet": os.environ.get(OWNER_SNIPPET_ENV) == "1",
                  "steps": [{k: s.get(k) for k in ("name", "verdict", "reason", "started_at", "finished_at")} for s in self.steps],
                  "overall": base.overall(verdicts),
                  "note": "request-identity: observations for the owner's review; no update is started and no P0 row is judged."}''')
r.save()

t = Patch('test_request_identity_trial.py')
t.rep('''                         ["lab-isolate-acme", "lab-kill-panel", "owner-change-site", "owner-cpmove-fixture",
                          "owner-php-probe", "owner-restart-panel", "owner-seed-rows", "owner-seed-site"])''',
      '''                         ["lab-isolate-acme", "lab-kill-panel", "owner-change-site", "owner-cpmove-fixture",
                          "owner-nginx-php-snippet", "owner-php-probe", "owner-restart-panel", "owner-seed-rows", "owner-seed-site"])
        # set3: the snippet an owner may place for a second Arch reading is the one the product's vhost template includes
        template = (REPO / "internal" / "services" / "templates" / "nginx" / "vhost.conf.tmpl").read_text(encoding="utf-8")
        self.assertIn("include snippets/" + native.NGINX_PHP_SNIPPET.name + ";", template)
        self.assertIn("include fastcgi.conf;", native.NGINX_PHP_SNIPPET_TEXT)
        self.assertIn("fastcgi_split_path_info ^(.+?\\\\.php)(/.*)$;", native.NGINX_PHP_SNIPPET_TEXT)''')
t.save()
print('ok')
