"""set2 harness debugging session (NOT a cell, its output is not evidence): the sections of the request-identity
driver that run-a never reached, on the finished rid-ubuntu run-a guest, restarted for this purpose only."""
import json
import sys

sys.path.insert(0, "/var/tmp/cp-set2-run/harness-dev/deploy/e2e/release-recovery")
import request_identity_trial as rt  # noqa: E402

sw, base = rt.sw, rt.base
rt.IMPORT_SUFFIX = "x"
document = json.load(open("/var/tmp/cp-upd1-build/20261009t030455z/upd1-artifacts.json"))
base.configure_labels(document)
settings = sw.SettingsCell("rid-debug", "ubuntu", "web_mail", sw.MAIL_PRESET + ("postgresql",), True)
t = rt.RequestIdentityTrial(settings, document, "/var/tmp/cp-release-drill-rid-ub-a", 18460)
print("helpers", sorted(t.upload_helpers()))
t.state["helpers_uploaded"] = True
t.step("owner-login", t.owner_login)
t.state.update(domain_id=1, site_user=sw.site_username(sw.SITE_DOMAIN), mailbox="owner@" + sw.SITE_DOMAIN,
               site_databases={"mariadb": {"name": "set1_owner_test_seq"}}, config_files={})
functions = {"C0-prepare": t.c0_prepare, "C2-admin-account": t.c2_admin_account, "C3-server-databases": t.c3_server_databases, "C7-import": t.c7_import,
             "C4-backup": t.c4_backup, "C8-restore": t.c8_restore, "C9-identities": t.c9_identities}
for key in sys.argv[1:] or list(functions):
    t.section(key, key, functions[key], needs=())
t.tunnel.close()
print(json.dumps(t.matrix, indent=1))
