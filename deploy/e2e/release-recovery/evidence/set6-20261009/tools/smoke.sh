#!/bin/bash
# set6: a local smoke test of the two guest scripts on the WSL host itself (no guest, no product): the pin script
# in READ mode only (it reads /etc/hosts; with no name mapped the default lookup path is not asked), and the header
# script against a throw-away HTTPS server on 127.0.0.1 with a self-signed certificate. usage: smoke.sh COPY
export PYTHONDONTWRITEBYTECODE=1
R=/var/tmp/cp-set6-run; D=$R/smoke-$1; H=$R/harness-$1/deploy/e2e/release-recovery
rm -rf -- "${D:?}"; mkdir -p $D; cd $D
openssl req -x509 -newkey rsa:2048 -nodes -keyout k.pem -out c.pem -days 1 -subj /CN=set6-smoke > /dev/null 2>&1
cat > server.py <<'PY'
import http.server, ssl, sys
class H(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    def answer(self):
        body = b'<html><script src="/assets/index-abc123.js"></script></html>' if self.path == "/" else b'{"version":"v0","commit":"c"}'
        status = 200
        if "no-such-route" in self.path: status = 404 if self.headers.get("Cookie") else 401
        if self.path == "/api/v1/domains" and not self.headers.get("Cookie"): status = 401
        if self.command == "POST": status = 403
        self.send_response(status)
        self.send_header("Strict-Transport-Security", "max-age=31536000")
        self.send_header("Set-Cookie", "celikpanel_session=should-never-be-kept; Path=/")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers(); self.wfile.write(body)
    do_GET = do_POST = do_OPTIONS = answer
    def log_message(self, *a): pass
s = http.server.ThreadingHTTPServer(("127.0.0.1", int(sys.argv[1])), H)
c = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER); c.load_cert_chain("c.pem", "k.pem")
s.socket = c.wrap_socket(s.socket, server_side=True); s.serve_forever()
PY
python3 server.py 12083 & pid=$!
sleep 1
python3 -I - $H <<'PY' > run.out 2> run.err
import json, subprocess, sys
sys.path.insert(0, sys.argv[1])
import set6_trial as t
script = t.HEADER_SCRIPT.replace("@@COOKIE@@", json.dumps("smoke-cookie-value-0123456789"))
done = subprocess.run(["python3", "-I", "-", t.SESSION_COOKIE, "12083", "panel-smoke.upd1-infra.test"], input=script, capture_output=True, text=True)
print("header script rc", done.returncode, done.stderr[-600:])
reading = json.loads(done.stdout)
print("cookie value in the output:", "smoke-cookie-value" in done.stdout, "| set-cookie value in the output:", "should-never-be-kept" in done.stdout)
for r in reading["requests"]:
    print(r["name"], r.get("status"), r.get("strict_transport_security"), r.get("error"), r.get("answered"), r.get("status_line"))
for name, ok, detail in t.header_judgement(reading, t.HSTS_CANDIDATE):
    print(ok, name[:110])
print(t.headers_text(reading)[:1500])
pin = subprocess.run(["python3", "-I", "-", "read", json.dumps(list(t.CA_NAMES)), t.PIN_MARK, t.ORIGIN_NAME, t.ORIGIN_PIN_LINE, "4"], input=t.PIN_SCRIPT, capture_output=True, text=True)
print("pin script rc", pin.returncode, pin.stderr[-600:])
p = json.loads(pin.stdout)
print({k: p.get(k) for k in ("mode", "files_loopback_only", "default_path", "default_path_not_asked", "nsswitch_hosts", "systemd_resolved", "product_paths_present")})
print(t.pin_verdict(p), t.pin_holds(t.pin_verdict(p)))
PY
kill $pid 2>/dev/null
cat run.out; tail -n 20 run.err
cd /; rm -rf -- "${D:?}"
