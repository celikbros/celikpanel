"""set2 H36 (found in the debugging session on the finished rid-ubuntu run-a guest).
`systemctl restart celikpanel-panel` did not cut the restore: the Panel stops gracefully and the request finished
first (the client got its 200, the row is `done`). And for a while after a restart the Panel answers every management
request `503 PANEL_STARTING`, which the driver took for the replay's answer. So arrival (vi) is measured twice: the
owner's `systemctl restart` (whatever comes of it is recorded and held to consistency), and the Panel's process
killed as a crash or the kernel's OOM killer would (the `interrupted` path); after either the driver waits, read-only,
until the Panel serves management requests again."""
import sys
d = sys.argv[1]

p = d + "/guest_request_identity_native.py"
s = open(p, encoding="utf-8", newline="").read()
old = '''def lab_isolate_acme(args: dict) -> dict:'''
new = '''def lab_kill_panel(args: dict) -> dict:
    """A lab fault, not an owner action: the Panel's main process is killed outright (SIGKILL), as a crash, the
    kernel's out-of-memory killer or a power loss would end it. systemd's own restart policy of the unit
    (``Restart=on-failure``) is left to bring it back; this helper only waits for that and reports it."""
    before = read_units({})
    started = time.time()
    done = run(["systemctl", "kill", "--signal=SIGKILL", "--kill-whom=main", "celikpanel-panel.service"], timeout=60)
    after = read_units({})
    for _ in range(120):
        unit = after["units"]["celikpanel-panel.service"]
        if unit.get("ActiveState") == "active" and unit.get("MainPID") not in (None, "0", before["units"]["celikpanel-panel.service"].get("MainPID")):
            break
        time.sleep(0.5)
        after = read_units({})
    policy = run(["systemctl", "show", "celikpanel-panel.service", "-p", "Restart", "-p", "RestartUSec", "-p", "TimeoutStopUSec",
                  "-p", "KillMode", "-p", "NRestarts"])
    return {"action": "lab-kill-panel", "issued_epoch": started, "returned_epoch": time.time(), "result": done,
            "before": before["units"], "after": after["units"], "unit_policy": policy.get("stdout", "").split(), "at": utc()}


def lab_isolate_acme(args: dict) -> dict:'''
assert s.count(old) == 1
s = s.replace(old, new)
old = '''    "lab-isolate-acme": lab_isolate_acme,
}'''
new = '''    "lab-isolate-acme": lab_isolate_acme, "lab-kill-panel": lab_kill_panel,
}'''
assert s.count(old) == 1
s = s.replace(old, new)
old = '''    return {"action": "owner-restart-panel", "issued_epoch": started, "returned_epoch": time.time(), "result": done,
            "before": before["units"], "after": after["units"], "at": utc()}'''
new = '''    policy = run(["systemctl", "show", "celikpanel-panel.service", "-p", "Restart", "-p", "RestartUSec", "-p", "TimeoutStopUSec",
                  "-p", "KillMode", "-p", "NRestarts"])
    return {"action": "owner-restart-panel", "issued_epoch": started, "returned_epoch": time.time(), "result": done,
            "restart_took_seconds": round(time.time() - started, 1), "unit_policy": policy.get("stdout", "").split(),
            "before": before["units"], "after": after["units"], "at": utc()}'''
assert s.count(old) == 1
s = s.replace(old, new)
open(p, "w", encoding="utf-8", newline="").write(s)

p = d + "/request_identity_trial.py"
s = open(p, encoding="utf-8", newline="").read()
a = s.index("        # (vi) the Panel's service is restarted while a restore runs")
b = s.index("    # -- C9: the rows ---")
new = '''        # (vi) the Panel goes away while a restore runs: the owner's restart, then the process killed outright
        for how in ("restart", "kill"):
            self.panel_goes_away(how, route, path, body, reference)
        me = self.call("C8 the owner's session after the Panel came back", "GET", "/api/v1/auth/me")
        self.check("the owner's session is still valid after the Panel came back", me["status"] == 200, me["status"])

    def wait_panel_ready(self, label: str) -> dict:
        """Read-only wait until the Panel serves management requests again (after a start it answers them
        `503 PANEL_STARTING` for a while). The Backups panel's list is asked, as the page does."""
        started, last, seen = time.time(), None, []
        path = f"/api/v1/domains/{self.state['domain_id']}/backups"
        while time.time() - started < 900:
            self.tunnel.ensure()
            try:
                response = self.api("GET", path, purpose=f"Backups panel list while the Panel comes back ({label})")
                parsed = response.json()
                last = {"status": response.status, "code": parsed.get("code") if isinstance(parsed, dict) else None}
            except Exception as exc:  # noqa: BLE001 - the Panel is down or starting; that is what is being waited for
                last = {"no_answer": type(exc).__name__}
            if not seen or seen[-1]["answer"] != last:
                seen.append({"after_seconds": round(time.time() - started, 1), "answer": last})
            if last.get("status") == 200:
                break
            time.sleep(1)
        return {"ready_after_seconds": round(time.time() - started, 1), "answers_in_order": seen}

    def settle(self, label: str, reference: dict, before: dict) -> tuple:
        """What the Agent left: read until two readings in a row agree and nothing partial remains (bounded)."""
        observations, final = [], None
        deadline = time.time() + 900
        while time.time() < deadline:
            now = self.site_state(f"{label}-agent-{len(observations) + 1:02d}")
            observations.append({"at": base.utc_now(), "docroot": now["docroot"], "rows": now["rows"], "archives": len(now["archives"]),
                                 "partial_files": now["partial_files"], "site_home_entries": now["site_home_entries"],
                                 "classification": restore_classification(reference, before, now)})
            if final is not None and self.same_site(final, now) and final["archives"] == now["archives"] and not now["partial_files"] \\
                    and final["site_home_entries"] == now["site_home_entries"]:
                return now, observations
            final = now
            time.sleep(6)
        return final, observations

    def panel_goes_away(self, how: str, route: str, path: str, body: dict, reference: dict) -> None:
        what = {"restart": "the owner restarts the Panel's service (systemctl restart)",
                "kill": "the Panel's process is killed outright (SIGKILL, as a crash or the OOM killer)"}[how]
        before = self.drift("vi-" + how)
        identity = self._own_identity(f"{route} (vi, {how})")
        units_before = self.snap2(f"vi-{how}-units-before", "read-units")
        clock = int(units_before["epoch"])
        tree = self.native2("read-backups", domain_id=self.state["domain_id"])["tree"]
        holder: dict = {}

        def client() -> None:
            holder["answer"] = self.post(f"{route} (vi, {how}) the restore that is running when the Panel goes away", path, body,
                                         identity, 1800)
        thread = threading.Thread(target=client, daemon=True)
        thread.start()
        held = self.hold_until_restore_started(tree)()
        at_fault = self.row(identity)
        if how == "restart":
            fault = self.owner2("restart-panel", "owner-restart-panel")
        else:
            fault = self.snap2("lab-kill-panel", "lab-kill-panel")
            self.current.setdefault("lab_faults", []).append({"what": what, "at": fault.get("at")})
        thread.join(1900)
        seen = holder.get("answer") or {}
        ready = self.wait_panel_ready(how)
        row_after = self.row(identity)
        units_after = self.snap2(f"vi-{how}-units-after", "read-units")
        pids = {name.split(".")[0].split("-")[1]: [units_before["units"][name].get("MainPID"), units_after["units"][name].get("MainPID")]
                for name in ("celikpanel-panel.service", "celikpanel-agent.service")}
        record = {"what": what, "client_saw": brief(seen) or "nothing", "held": held, "row_when_the_panel_went_away": at_fault,
                  "panel_main_pid": pids["panel"], "agent_main_pid": pids["agent"], "agent_kept_its_process": pids["agent"][0] == pids["agent"][1],
                  "systemctl": {k: fault.get(k) for k in ("restart_took_seconds", "unit_policy")}, "returncode": fault["result"].get("returncode"),
                  "panel_ready_again": ready, "row_afterwards": row_after}
        self.current.setdefault("panel_goes_away", {})[how] = record
        self.judge(route, "vi", f"{how}: the restore was running when the Panel went away (row running; the Agent at work; a new Panel process afterwards)",
                   bool(held.get("seen")) and bool(at_fault) and at_fault["status"] == "running" and pids["panel"][0] != pids["panel"][1],
                   {k: record[k] for k in ("held", "row_when_the_panel_went_away", "panel_main_pid", "agent_main_pid", "client_saw")})
        final, observations = self.settle("vi-" + how, reference, before)
        new = one_new(before["archives"], final["archives"])
        record.update(what_the_agent_left=restore_classification(reference, before, final),
                      new_archives=[{"name": e["name"], "origin": e.get("manifest", {}).get("origin"), "bytes": e["bytes"]}
                                    for e in final["entries"] if e["name"] in new],
                      partial_files=final["partial_files"], site_home_entries=final["site_home_entries"],
                      backup_directory_entries_that_are_not_archives=[e["name"] for e in final["entries"]
                                                                      if not e["name"].endswith(".cpbak") or e["name"].startswith(".")],
                      observations=observations)
        journal = self.snap2(f"vi-{how}-journal", "read-journal", units=["celikpanel-agent.service", "celikpanel-panel.service"],
                             since_epoch=clock, lines=600)["journal"].get("stdout", "")
        self.keep_text(f"native-text/journal-panel-{how}.txt", journal or "(no lines)")
        replay = self.post(f"{route} (vi, {how}) the same identity again after the Panel came back", path, body, identity)
        after_replay = self.site_state(f"vi-{how}-after-replay")
        record.update(replay=brief(replay), replay_answer=replay.get("answer"), replay_texts=replay.get("catalogue_texts"))
        status = (row_after or {}).get("status")
        self.judge(route, "vi", f"{how}: what the Agent left was read (recorded; the state itself is the observation)", True,
                   {k: record[k] for k in ("what_the_agent_left", "new_archives", "partial_files", "agent_kept_its_process",
                                           "site_home_entries", "row_afterwards", "client_saw")})
        if status == "interrupted":
            self.judge(route, "vi", f"{how}: the row is `interrupted` and the replay answers 409 REQUEST_OUTCOME_UNKNOWN, starting nothing",
                       replay.get("status") == 409 and answer_code(replay) == "REQUEST_OUTCOME_UNKNOWN"
                       and self.same_site(after_replay, final) and after_replay["archives"] == final["archives"], brief(replay))
        elif status == "done":
            self.judge(route, "vi", f"{how}: the row is `done`: the request finished before the Panel stopped, the site is the "
                       "backup's, and the replay is the stored answer",
                       self.same_site(final, reference) and len(new) == 1 and replay.get("status") == row_after.get("response_status")
                       and replay.get("replayed") == "1" and after_replay["archives"] == final["archives"],
                       {"client_saw": brief(seen), "replay": brief(replay), "site_is_the_backups": self.same_site(final, reference),
                        "new_archives": record["new_archives"]})
        else:
            self.judge(route, "vi", f"{how}: the row is `done` or `interrupted` once the Panel is back", False, row_after)
        if how == "kill":
            self.judge(route, "vi", "kill: the row of a request whose Panel process was killed is `interrupted`", status == "interrupted", row_after)
        self.note(f"(vi, {how}) {what}: the client saw {record['client_saw']}; the row is `{status}`; the Agent left: "
                  f"{record['what_the_agent_left']}; the Panel served management requests again after "
                  f"{ready['ready_after_seconds']} s", record["panel_ready_again"])

'''
s = s[:a] + new + s[b:]
old = '''        self.judge("rows", "vii", "the one interrupted row is the restore the Panel restart cut",
                   [self.identities.get(r["id"]) for r in rows if r["status"] == "interrupted"] == ["restore (vi)"],
                   [self.identities.get(r["id"]) for r in rows if r["status"] != "done"])'''
new = '''        cut = sorted(str(self.identities.get(r["id"])) for r in rows if r["status"] == "interrupted")
        self.judge("rows", "vii", "the interrupted rows are restores of arrival (vi) only, the killed one among them",
                   "restore (vi, kill)" in cut and all(item.startswith("restore (vi") for item in cut),
                   [self.identities.get(r["id"]) for r in rows if r["status"] != "done"])'''
assert s.count(old) == 1
s = s.replace(old, new)
old = '''                         "v": "the connection dropped while the request runs (restore, import)",
                         "vi": "the Panel's service restarted while a restore runs",'''
new = '''                         "v": "the connection dropped while the request runs (restore, import)",
                         "vi": "the Panel goes away while a restore runs: the owner's systemctl restart, and the process killed",'''
assert s.count(old) == 1
s = s.replace(old, new)
open(p, "w", encoding="utf-8", newline="").write(s)

p = d + "/test_request_identity_trial.py"
s = open(p, encoding="utf-8", newline="").read()
old = '''                         ["lab-isolate-acme", "owner-change-site", "owner-cpmove-fixture", "owner-restart-panel",
                          "owner-seed-rows", "owner-seed-site"])'''
new = '''                         ["lab-isolate-acme", "lab-kill-panel", "owner-change-site", "owner-cpmove-fixture",
                          "owner-restart-panel", "owner-seed-rows", "owner-seed-site"])
        unit = (REPO / "deploy" / "systemd" / "celikpanel-panel.service").read_text(encoding="utf-8")
        self.assertIn("Restart=on-failure", unit)     # what brings the Panel back after lab-kill-panel'''
assert s.count(old) == 1
s = s.replace(old, new)
old = '''        self.assertTrue(all(not m.issym() and not m.islnk() for m in regular))
'''
new = '''        self.assertTrue(all(not m.issym() and not m.islnk() for m in regular))
        # H34: as tar writes it, the archive holds the directory member `homedir/public_html/`; the other form leaves
        # exactly that one member out.
        without = native.cpmove_archive(members, public_html_directory_member=False)
        with tarfile.open(fileobj=io.BytesIO(gzip.decompress(without)), mode="r") as archive:
            other = archive.getnames()
        self.assertEqual(sorted(set(listed) - set(other)), ["cpmove-s2impseq/homedir/public_html"])
        self.assertIn("cpmove-s2impseq/homedir/public_html/assets", other)
        raw = gzip.decompress(data)
        self.assertIn(b"cpmove-s2impseq/homedir/public_html/\\0", raw)      # the stored name ends with a slash
        self.assertNotIn(b"cpmove-s2impseq/homedir/public_html/\\0", gzip.decompress(without))
'''
assert s.count(old) == 1
s = s.replace(old, new)
open(p, "w", encoding="utf-8", newline="").write(s)
print("H36 patched")
