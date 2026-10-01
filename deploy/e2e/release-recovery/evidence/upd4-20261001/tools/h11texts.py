#!/usr/bin/env python3
"""H11 (harness model lag, recorded): what the product build's recovery screen and update card show for the
retry_scheduled and paused observations, rendered from the build's own catalogues in the order of
RecoveryAccess.tsx:49-60 and systemUpdateOutcome.ts:140-160 at a6dd5b1e. Host only, read-only.
usage: h11texts.py OUT"""
import importlib.util, json, sys
from pathlib import Path
R = "/var/tmp/cp-upd4-run"
art = json.load(open(open(R + "/ART").read().strip()))
spec = importlib.util.spec_from_file_location("guidance", R + "/harness/deploy/e2e/dns-pair-acceptance/guidance.py")
g = importlib.util.module_from_spec(spec); spec.loader.exec_module(g)
src = Path(art["good"]["product_web_src"])
tr = g.Translator(g.load_catalog(src / "i18n"))
CMD = "sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50"
views = {
    "screen retry_scheduled (first_failure_code=panel_start_unverified, previous_failure=recovery_failed)": [
        "recovery.automatic.retryTitle", "recovery.failure.panel_start_unverified.pending", "recovery.automatic.retryHelp",
        ("recovery.previousFailure", "recovery.reason.recovery_failed")],
    "card retry_scheduled (same observation)": [
        "recovery.automatic.retryTitle", "recovery.failure.panel_start_unverified.pending", "recovery.automatic.retryHelp",
        "panelUpdate.outcome.followsRecovery"],
    "screen paused_retry_limit (first_failure_code=panel_start_unverified)": [
        "recovery.automatic.pausedTitle", "recovery.automatic.cause.panel_start_unverified", "recovery.automatic.pausedHelp",
        "recovery.automatic.renewal", "recovery.automatic.inspect", CMD, "recovery.automatic.resume",
        ("recovery.previousFailure", "recovery.reason.recovery_failed")],
    "card paused_retry_limit (same observation)": [
        "recovery.automatic.pausedTitle", "recovery.automatic.cause.panel_start_unverified", "recovery.automatic.pausedHelp",
        "recovery.automatic.renewal", "recovery.automatic.inspect", CMD, "recovery.automatic.resume",
        "panelUpdate.outcome.followsRecovery"],
}
out = [f"# product-rendered texts (catalogues of {src}); source order RecoveryAccess.tsx:49-60, systemUpdateOutcome.ts:140-160"]
missing = []
for title, keys in views.items():
    out.append(f"## {title}")
    for lang in ("en", "tr"):
        parts = []
        for k in keys:
            if isinstance(k, tuple):
                parts.append(tr.text(k[0], language=lang) + ": " + tr.text(k[1], language=lang))
                missing += [x for x in k if not tr.has(x)]
            elif k == CMD:
                parts.append(CMD)
            else:
                parts.append(tr.text(k, language=lang))
                if not tr.has(k):
                    missing.append(k)
        out.append(f"[{lang}] " + " || ".join(parts))
out.append("missing keys: " + json.dumps(sorted(set(missing))))
Path(sys.argv[1]).write_text("\n".join(out) + "\n", encoding="utf-8")
print("\n".join(out))
