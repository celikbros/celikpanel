"""set2: additive changes to guest_settings_native.py (working tree, harness only)."""
import sys

p = sys.argv[1]
s = open(p, encoding="utf-8", newline="").read()
crlf = "\r\n" in s
s = s.replace("\r\n", "\n")


def rep(old, new, count=1):
    global s
    assert s.count(old) == count, (old[:60], s.count(old))
    s = s.replace(old, new)


rep('UNITS = re.compile(r"(?:postfix(?:@-)?|postgresql(?:@[0-9]+(?:\\.[0-9]+)?-[A-Za-z0-9_.-]+)?|mariadb|mysql|cron|cronie)\\.service\\Z")',
    'UNITS = re.compile(r"(?:postfix(?:@-)?|postgresql(?:@[0-9]+(?:\\.[0-9]+)?-[A-Za-z0-9_.-]+)?|mariadb|mysql|cron|cronie|dovecot|nginx)"\n'
    '                   r"\\.service\\Z")')

rep('''              "master_pid": _pid_file("/var/spool/postfix/pid/master.pid"),
              "units": unit_facts(("postfix.service", "postfix@-.service")),
              "mail_version"''',
    '''              "master_pid": _pid_file("/var/spool/postfix/pid/master.pid"),
              "status": run(["postfix", "status"]),
              "units": unit_facts(("postfix.service", "postfix@-.service")),
              "mail_version"''')

rep('''    with tempfile.TemporaryDirectory(prefix="set1-mariadb-check-") as private:
        done = run([program, "--defaults-file=" + path, "--datadir=" + private, "--help", "--verbose"], timeout=60)
''',
    '''    with tempfile.TemporaryDirectory(prefix="set1-mariadb-check-") as private:
        read_from = path
        if args.get("content_b64") is not None:
            # set2: a text that is NOT on disk (the Panel refused it), read by the same program from a private copy.
            read_from = os.path.join(private, "candidate.cnf")
            Path(read_from).write_bytes(base64.b64decode(args["content_b64"], validate=True))
            os.mkdir(os.path.join(private, "data"))
            private = os.path.join(private, "data")
        done = run([program, "--defaults-file=" + read_from, "--datadir=" + private, "--help", "--verbose"], timeout=60)
''')

rep('''def read_clock(args: dict) -> dict:
    return {"epoch": time.time(), "at": utc()}
''',
    '''def read_clock(args: dict) -> dict:
    return {"epoch": time.time(), "at": utc()}


# -- set2: service units as systemd and the daemons themselves report them -----------

SERVICE_PROPERTIES = ("LoadState", "ActiveState", "SubState", "MainPID", "Result", "Type", "RemainAfterExit",
                      "ExecStart", "ExecReload", "ReloadResult", "ConsistsOf", "PartOf", "PropagatesReloadTo",
                      "ReloadPropagatedFrom", "Wants", "WantedBy", "ActiveEnterTimestamp", "InactiveEnterTimestamp",
                      "ExecMainStartTimestamp", "ExecMainPID", "NRestarts", "FragmentPath", "DropInPaths")


def _alive(pid) -> bool:
    return isinstance(pid, int) and pid > 1 and os.path.isdir("/proc/%d" % pid)


def read_service(args: dict) -> dict:
    """Read-only: what systemd says about each unit (the properties the Agent reads, with every ExecReload line
    kept), and what the daemons say themselves (``postfix status``, the master's PID file, PostgreSQL's
    postmaster.pid). With ``cat`` the unit texts as ``systemctl cat`` prints them."""
    units = [unit_name(u) for u in args.get("units", [])]
    shown = {}
    for unit in units:
        argv = ["systemctl", "show", unit] + ["--property=" + name for name in SERVICE_PROPERTIES]
        answer = run(argv)
        properties: dict = {}
        for line in answer.get("stdout", "").splitlines():
            if "=" in line:
                name, value = line.split("=", 1)
                properties.setdefault(name, []).append(value)
        shown[unit] = {"returncode": answer.get("returncode"),
                       "properties": {k: (v[0] if len(v) == 1 else v) for k, v in properties.items()},
                       "is_active": run(["systemctl", "is-active", unit]).get("stdout", "").strip()}
        if args.get("cat"):
            shown[unit]["cat"] = run(["systemctl", "cat", unit]).get("stdout", "")
    result = {"units": shown, "at": utc(), "epoch": time.time()}
    if args.get("postfix"):
        master = _pid_file("/var/spool/postfix/pid/master.pid")
        result["postfix"] = {"status": run(["postfix", "status"]), "master_pid": master, "master_alive": _alive(master)}
    if args.get("postgres"):
        files = psql("SELECT current_setting('data_directory'), current_setting('server_version');")
        parts = (files.get("stdout", "").strip().split("\\t") + ["", ""])[:2]
        postmaster = _pid_file(os.path.join(parts[0], "postmaster.pid")) if parts[0] else None
        result["postgres"] = {"answers": files.get("returncode") == 0, "server_version": parts[1],
                              "postmaster_pid": postmaster, "postmaster_alive": _alive(postmaster),
                              "stderr": files.get("stderr", "")[-300:]}
    if args.get("mariadb"):
        client = shutil.which("mariadb", path=ENV["PATH"]) or shutil.which("mysql", path=ENV["PATH"])
        answer = run([client, "-N", "-B", "-e", "SELECT VERSION(), @@pid_file"]) if client else {}
        pid_file = (answer.get("stdout", "").strip().split("\\t") + ["", ""])[1]
        pid = _pid_file(pid_file) if pid_file else None
        result["mariadb"] = {"answers": answer.get("returncode") == 0, "server_pid": pid, "server_alive": _alive(pid),
                             "version": (answer.get("stdout", "").strip().split("\\t") + [""])[0],
                             "stderr": answer.get("stderr", "")[-300:]}
    return result


AGENT_REREAD_BATCH = (
    "SELECT 'file=' || current_setting('config_file');\\n"
    "SELECT 'before=' || extract(epoch from clock_timestamp());\\n"
    "SELECT 'signal=' || pg_reload_conf();\\n"
    "SELECT 'waited=' || count(*) FROM (SELECT pg_sleep(1)) AS waited;\\n"
    "SELECT 'loaded=' || extract(epoch from pg_conf_load_time());\\n"
    "SELECT 'error=' || sourceline || ':' || coalesce(name, '') || ':' || error FROM pg_file_settings "
    "WHERE error IS NOT NULL "
    "AND coalesce(name, '') NOT IN (SELECT name FROM pg_settings WHERE pending_restart) ORDER BY seqno LIMIT 1;\\n")


def owner_pg_reread(args: dict) -> dict:
    """The exact statement batch of cmd/agent/db_config.go dbConfigPostgreSQLRereadVerified (postgresql.conf form),
    run the way the Agent runs it (``sudo -u postgres psql`` over the local socket), with its raw output. It sends
    PostgreSQL the reload signal, which an owner may do at any time; it changes no file."""
    answer = run(["sudo", "-u", "postgres", "psql", "--no-psqlrc", "--set", "ON_ERROR_STOP=on", "--no-align",
                  "--tuples-only", "--quiet"], input_bytes=AGENT_REREAD_BATCH.encode())
    version = psql("SELECT version();")
    return {"action": "owner-pg-reread", "batch": AGENT_REREAD_BATCH, "answer": answer,
            "version": version.get("stdout", "").strip(), "at": utc()}
''')

rep('''    "read-clock": read_clock,
''', '''    "read-clock": read_clock, "read-service": read_service, "owner-pg-reread": owner_pg_reread,
''')
open(p, "w", encoding="utf-8", newline="").write(s.replace("\n", "\r\n") if crlf else s)
print("patched; crlf", crlf)
