"""What the Panel shows the owner, reconstructed from the API payloads the UI renders.

D-024 (docs/OPERATION-GUIDANCE.md) requires every waiting or failed state to
name the reason, who must act, the next action and how work resumes. The
driver cannot look at a browser, so it applies the web UI's own selection
rules to the exact API payloads and resolves the result through the shipped
translation catalogues in ``web/src/i18n``:

* ``api_error_guidance``  - ``apiErrorText`` (web/src/lib/apiError.ts):
  ``err.<code>.<reason>``, then ``err.<code>``, then the server message.
* ``setup_execution_guidance`` - ``setupExecutionGuidance``
  (web/src/lib/serverSetupGuidance.ts), ported rule for rule.
* ``deletion_pending_guidance`` - ``readDomainDeletionOutcome`` +
  ``pendingMessage`` (web/src/lib/domainDeletionPending.ts,
  web/src/components/Domains.tsx).
* ``setup_selection_error`` - the wizard's client-side selection rule
  (web/src/components/ServerSetup.tsx ``dnsSelectionError``).

A state is *actionable* only when it carries a stable machine code and the
UI resolves it to a specific, non-generic translated message. A generic
fallback ("setup.guide.unknown", "domains.deletionPending", a raw server
string without a code) on a blocked or pending state fails the step.
"""

from __future__ import annotations

import re
from pathlib import Path
from typing import Any, Iterable

REPO = Path(__file__).resolve().parents[3]
I18N_ROOT = REPO / "web" / "src" / "i18n"
ENTRY_RE = re.compile(
    r"""^\s*(?P<kq>['"])(?P<key>[A-Za-z0-9_.:-]+)(?P=kq)\s*:\s*"""
    r"""(?P<quote>['"])(?P<text>(?:\\.|(?!(?P=quote)).)*)(?P=quote)\s*,?\s*$"""
)
EXPORT_RE = re.compile(r"^export const (?P<name>[A-Za-z0-9_]+)\b")

# Keys whose text is a deliberate generic fallback, not a diagnosis.
GENERIC_KEYS = frozenset(
    {
        "setup.guide.unknown",
        "setup.guide.checkUnknown",
        "domains.deletionPending",
        "common.error",
        "dns.recordAddFailed",
        "dns.recordDeleteFailed",
        "dns.zonePublishFailed",
    }
)
# Framing sentences that point at another message ("read the error below");
# they never count as the specific guidance on their own.
FRAMING_KEYS = frozenset({"setup.guide.failed", "setup.guide.confirm", "setup.guide.monitoring"})
REVIEWED_DNS_PEER_REASONS = frozenset(
    {
        "dns_peer_enrollment_required",
        "dns_peer_enrollment_changed",
        "dns_peer_inspection_unknown",
        "dns_peer_native_unknown",
        "dns_peer_journal_unknown",
        "dns_peer_owner_edit_unknown",
    }
)

# Reviewed actor for codes whose owner the product text names. Anything not
# listed is recorded as "stated in text" and left to the human reviewer.
ACTOR_BY_CODE = {
    "license_required": "this server's administrator (license activation)",
    "LICENSE_VERIFICATION_UNAVAILABLE": "this server's administrator (license verification service)",
    "LICENSE_STATUS_UNAVAILABLE": "this server's administrator",
    "pdns_primary_switch_paused": "this server's administrator (choose another engine or topology)",
    "dns_peer_enrollment_required": "primary administrator together with the secondary owner",
    "dns_peer_enrollment_changed": "primary administrator (pinned peer enrollment)",
    "dns_peer_inspection_unknown": "primary administrator and secondary owner",
    "server_setup_primary_dns_required": "the primary server's owner",
    "server_setup_dns_readiness_required": "both DNS server owners",
    "server_setup_access_dns_required": "the domain owner (public DNS / registrar)",
}


class CatalogError(RuntimeError):
    pass


def _unescape(text: str) -> str:
    return (
        text.replace("\\'", "'")
        .replace('\\"', '"')
        .replace("\\n", "\n")
        .replace("\\\\", "\\")
    )


def load_catalog(root: Path = I18N_ROOT) -> dict[str, dict[str, str]]:
    """Parse the shipped en/tr catalogues (every entry is a single line)."""

    catalog: dict[str, dict[str, str]] = {"en": {}, "tr": {}}
    files = sorted(path for path in root.rglob("*.ts") if path.name != "index.tsx")
    if not files:
        raise CatalogError(f"no translation catalogues under {root}")
    for path in files:
        language: str | None = None
        for line in path.read_text(encoding="utf-8").splitlines():
            exported = EXPORT_RE.match(line)
            if exported:
                name = exported.group("name")
                language = "tr" if name == "tr" or name.startswith("tr") or name.endswith("Tr") else "en"
                continue
            if language is None:
                continue
            match = ENTRY_RE.match(line)
            if match:
                catalog[language][match.group("key")] = _unescape(match.group("text"))
    if not catalog["en"] or not catalog["tr"]:
        raise CatalogError("translation catalogues are incomplete")
    return catalog


class Translator:
    def __init__(self, catalog: dict[str, dict[str, str]]) -> None:
        self.catalog = catalog

    def has(self, key: str) -> bool:
        return key in self.catalog["en"]

    def text(self, key: str, values: dict[str, Any] | None = None, language: str = "en") -> str:
        text = self.catalog[language].get(key, key)
        for name, value in (values or {}).items():
            text = text.replace("{" + name + "}", str(value))
        return text

    def render(self, items: Iterable[dict[str, Any]]) -> dict[str, list[str]]:
        items = list(items)
        return {
            language: [self.text(item["key"], item.get("values"), language) for item in items]
            for language in ("en", "tr")
        }


def _item(key: str, values: dict[str, Any] | None = None) -> dict[str, Any]:
    return {"key": key, "values": values} if values else {"key": key}


def _finish(
    translator: Translator,
    *,
    source: str,
    state: str,
    code: str | None,
    reason: str | None,
    title: str | None,
    messages: list[dict[str, Any]],
    details: list[dict[str, Any]],
    raw_message: str | None = None,
) -> dict[str, Any]:
    keys = [item["key"] for item in messages]
    specific = [key for key in keys if key not in GENERIC_KEYS | FRAMING_KEYS and translator.has(key)]
    if state in {"progress", "succeeded"}:
        actionable, why = True, "no action required in this state"
    elif not code:
        actionable, why = False, "blocked/pending state has no stable machine code"
    elif not specific:
        actionable, why = False, "UI would show only generic or untranslated text"
    else:
        actionable, why = True, "specific translated guidance for a stable code"
    return {
        "source": source,
        "state": state,
        "code": code,
        "reason": reason,
        "title": translator.text(title) if title else None,
        "title_key": title,
        "message_keys": keys,
        "detail_keys": [item["key"] for item in details],
        "shown": translator.render(messages),
        "details": translator.render(details),
        "raw_message": raw_message,
        "actor": ACTOR_BY_CODE.get(reason or "", ACTOR_BY_CODE.get(code or "", "stated in text" if actionable else "unstated")),
        "actionable": actionable,
        "actionable_reason": why,
    }


# ---------------------------------------------------------------------------
# apiErrorText
# ---------------------------------------------------------------------------

def api_error_guidance(translator: Translator, status: int, body: Any, *, pending: bool = False) -> dict[str, Any]:
    body = body if isinstance(body, dict) else {}
    code = body.get("code") if isinstance(body.get("code"), str) and body.get("code") else None
    reason = body.get("reason") if isinstance(body.get("reason"), str) and body.get("reason") else None
    message = body.get("error") if isinstance(body.get("error"), str) else None
    key = None
    if code and reason and translator.has(f"err.{code}.{reason}"):
        key = f"err.{code}.{reason}"
    elif code and translator.has(f"err.{code}"):
        key = f"err.{code}"
    messages = [_item(key)] if key else []
    if status < 400 and not pending:
        state = "succeeded"
    elif status == 403 and code == "license_required":
        state = "unmet-prerequisite"
    elif status in (502, 503) or (code or "").endswith("UNAVAILABLE"):
        state = "unknown"
    elif pending or body.get("partial_success") is True:
        state = "pending"
    else:
        state = "verified-failure"
    result = _finish(
        translator, source="api-error", state=state, code=code, reason=reason, title=None,
        messages=messages, details=[], raw_message=message,
    )
    if not key and message:
        # apiErrorText falls back to the server's own sentence; record it as shown.
        result["shown"] = {"en": [message], "tr": [message]}
    result["http_status"] = status
    result["action_path"] = body.get("action") if isinstance(body.get("action"), str) else None
    return result


# ---------------------------------------------------------------------------
# setupExecutionGuidance
# ---------------------------------------------------------------------------

def setup_execution_guidance(translator: Translator, execution: dict[str, Any]) -> dict[str, Any]:
    status = execution.get("status")
    error = execution.get("error") if isinstance(execution.get("error"), dict) else None
    code = error.get("code") if error else None
    if status == "succeeded":
        return _finish(translator, source="setup-execution", state="succeeded", code=code, reason=None,
                       title=None, messages=[], details=[])
    context = execution.get("context") if isinstance(execution.get("context"), dict) else None
    steps = [step for step in execution.get("steps") or [] if isinstance(step, dict)]
    current = next((step for step in steps if step.get("status") == "failed"), None) or next(
        (step for step in steps if step.get("status") == "running"), None
    )
    phase_value = execution.get("phase")
    phase = phase_value if phase_value in {
        "license", "verification", "dns_readiness", "dns_publisher", "primary_dns",
        "infrastructure_dns", "access_dns"} else (current.get("kind") if current else None) or phase_value
    confirming = status == "running" and (code or "").split(":")[0] == "server_setup_reconciling"
    title = ("setup.guide.failedTitle" if status == "failed" else "setup.guide.confirmTitle" if confirming
             else "setup.guide.waitTitle" if status == "waiting" else "setup.guide.runningTitle")
    messages: list[dict[str, Any]] = []
    details: list[dict[str, Any]] = []

    def state_of() -> str:
        if status == "failed":
            return "verified-failure"
        if status == "waiting":
            return "unmet-prerequisite"
        if confirming:
            return "unknown"
        return "progress"

    def done() -> dict[str, Any]:
        if not messages:
            messages.append(_item("setup.guide.unknown"))
        if status == "running" or (status == "waiting" and phase in {
                "dns_readiness", "dns_publisher", "access_dns", "primary_dns", "infrastructure_dns"}):
            details.append(_item("setup.guide.monitoring"))
        # A waiting state's machine code is its phase when no error code is set;
        # a failed state must carry the error code the UI tells the owner to read.
        effective = code or (f"phase:{phase}" if status == "waiting" and phase else None)
        return _finish(translator, source="setup-execution", state=state_of(), code=effective,
                       reason=None, title=title, messages=messages, details=details)

    if status == "failed" and code == "server_setup_build_changed":
        title = "setup.guide.buildChangedTitle"
        messages.append(_item("setup.guide.buildChanged"))
        return done()
    if phase == "license":
        title = "setup.licenseWaiting"
        messages.append(_item("setup.guide.license"))
        return done()
    if confirming:
        messages.append(_item("setup.guide.confirm"))
    if status == "failed":
        messages.append(_item("setup.guide.failed"))
    ctx = context or {}
    if phase == "access_dns":
        domain = current.get("target") if current and current.get("kind") == "access_dns" else ctx.get("panel_domain")
        ip = (current.get("qualifier") if current and current.get("kind") == "access_dns" and current.get("qualifier")
              else ctx.get("access_dns_ip") or ctx.get("local_ip"))
        if domain and ip:
            messages.append(_item("setup.guide.accessDNSRecord", {"domain": domain, "ip": ip}))
        if code == "server_setup_access_dns_mismatch":
            messages.append(_item("setup.guide.accessDNSMismatch"))
        elif status == "waiting":
            messages.append(_item("setup.guide.accessDNSUnknown"))
        if context and ctx.get("dns_mode") == "local" and ctx.get("dns_role") == "secondary":
            messages.append(_item("setup.guide.accessDNSSecondary",
                                  {"primary": ctx.get("peer_nameserver"), "primaryIP": ctx.get("peer_ip")}))
            if domain == ctx.get("panel_domain"):
                details.append(_item("setup.guide.accessDNSPeerPanel"))
        elif ctx.get("infrastructure_dns"):
            messages.append(_item("setup.guide.accessDNSPrepared", {
                "zone": ctx["infrastructure_dns"].get("zone"), "primary": ctx.get("local_nameserver"),
                "secondary": ctx.get("peer_nameserver")}))
        else:
            messages.append(_item("setup.guide.accessDNSProvider"))
        details.append(_item("setup.guide.accessDNSChecks" if ctx.get("dns_mode") == "local"
                             else "setup.guide.accessDNSExternalChecks"))
        if status == "waiting":
            messages.append(_item("setup.guide.accessDNSResume"))
    elif phase == "infrastructure_dns":
        if ctx.get("infrastructure_dns"):
            messages.append(_item("setup.guide.infrastructureDNS", {"zone": ctx["infrastructure_dns"].get("zone")}))
        if code == "server_setup_infrastructure_dns_unknown":
            messages.append(_item("setup.infrastructure.unknown"))
        elif status == "waiting":
            messages.append(_item("setup.guide.infrastructureDNSWaiting"))
            if ctx.get("peer_nameserver") and ctx.get("peer_ip"):
                messages.append(_item("setup.guide.infrastructureDNSPeer",
                                      {"peer": ctx.get("peer_nameserver"), "peerIP": ctx.get("peer_ip")}))
    elif phase == "primary_dns" and ctx.get("dns_mode") == "local":
        messages.append(_item("setup.guide.primaryDNSWaiting",
                              {"primary": ctx.get("peer_nameserver"), "primaryIP": ctx.get("peer_ip")}))
        details.append(_item("setup.guide.nativeDNS"))
        if status == "waiting":
            messages.append(_item("setup.guide.accessDNSResume"))
    elif phase in {"dns", "dns_readiness"} and ctx.get("dns_mode") == "local":
        values = {"local": ctx.get("local_nameserver"), "localIP": ctx.get("local_ip"),
                  "peer": ctx.get("peer_nameserver"), "peerIP": ctx.get("peer_ip")}
        primary = ctx.get("dns_role") == "primary"
        messages.append(_item("setup.guide.primary" if primary else "setup.guide.secondary", values))
        messages.append(_item("setup.guide.startSecondary" if primary else "setup.guide.startPrimary", values))
        if phase == "dns_readiness":
            messages.append(_item("setup.guide.pairUnverified"))
        details.extend([_item("setup.guide.pairChecks"), _item("setup.guide.nativeDNS")])
        if ctx.get("dns_role") == "secondary" and ctx.get("dns_hosting_management") in {"manual", "panel"}:
            details.append(_item("setup.guide.manualRecords" if ctx.get("dns_hosting_management") == "manual"
                                 else "setup.guide.automaticRecords"))
    elif phase == "dns_publisher":
        messages.append(_item("setup.guide.publisher"))
    elif phase == "dns" and ctx.get("dns_mode") == "existing":
        messages.append(_item("setup.guide.existingDNS"))
    elif (phase == "dns" and ctx.get("dns_mode") == "external") or phase in {"panel_certificate", "mail_certificate"}:
        domain = ctx.get("mail_hostname") if phase == "mail_certificate" else ctx.get("panel_domain")
        if domain:
            messages.append(_item("setup.guide.certificateDNS", {"domain": domain}))
        details.append(_item("setup.guide.certificateChecks"))
    elif phase == "mail_enrollment":
        key = ("setup.guide.mailEnrollmentUnrecorded" if code == "server_setup_mail_enrollment_not_recorded"
               else "setup.guide.mailEnrollmentRollback" if code == "server_setup_mail_enrollment_rollback"
               else "setup.guide.mailEnrollmentRecorded" if code == "server_setup_mail_enrollment_running"
               else "setup.guide.mailEnrollmentFailed" if status == "failed" else "setup.guide.mailEnrollmentUnknown")
        messages.append(_item(key))
    elif phase in {"service", "runtime", "mail_profile"}:
        messages.append(_item("setup.guide.component"))
    elif phase == "firewall":
        messages.append(_item("setup.guide.firewall"))
    elif phase == "verification":
        messages.append(_item("setup.guide.verification"))
        seen: set[str] = set()
        for check in execution.get("checks") or []:
            if not isinstance(check, dict) or check.get("state") == "ready":
                continue
            ident = check.get("id")
            key = ("setup.guide.pairChecks" if ident == "dns" else "setup.guide.mailIdentity" if ident == "mail_identity"
                   else "setup.guide.mailDelivery" if ident == "mail_delivery"
                   else "setup.guide.certificateChecks" if ident in {"panel_https", "panel_renewal", "mail_tls"}
                   else "setup.guide.firewall" if ident == "firewall" else "setup.guide.checkUnknown")
            actual = ("setup.guide.existingDNS" if ident == "dns" and ctx.get("dns_mode") == "existing"
                      else "setup.guide.externalChecks" if ident == "dns" and ctx.get("dns_mode") != "local" else key)
            if actual not in seen:
                details.append(_item(actual))
            seen.add(actual)
    return done()


# ---------------------------------------------------------------------------
# Domain deletion (202) and the wizard's client-side selection rule
# ---------------------------------------------------------------------------

def deletion_pending_guidance(translator: Translator, status: int, body: Any, *, saved: bool = False) -> dict[str, Any]:
    body = body if isinstance(body, dict) else {}
    reason = ""
    if saved:
        # readSavedDomainDeletionStatus
        if status == 200 and body.get("status") == "unknown" and body.get("stage") == "unknown":
            reason = ""
        elif status == 200 and body.get("status") == "deletion_pending" and body.get("stage") == "dns_cleanup":
            reason = body.get("reason") if body.get("reason") in REVIEWED_DNS_PEER_REASONS else ""
    elif status == 202 and body.get("status") == "deletion_pending" and body.get("stage") == "dns_cleanup":
        reason = body.get("reason") if body.get("reason") in REVIEWED_DNS_PEER_REASONS else ""
    key = f"err.DNS_PUBLICATION_FAILED.{reason}" if reason else None
    message_key = key if key and translator.has(key) else "domains.deletionPending"
    result = _finish(
        translator, source="domain-deletion", state="pending",
        code=reason or (body.get("stage") if isinstance(body.get("stage"), str) else None),
        reason=reason or None, title=None, messages=[_item(message_key)], details=[],
        raw_message=body.get("message") if isinstance(body.get("message"), str) else None,
    )
    result["http_status"] = status
    result["stage"] = body.get("stage")
    return result


WIZARD_SOURCE = REPO / "web" / "src" / "components" / "ServerSetup.tsx"
WIZARD_PDNS_RULE = re.compile(
    r"draft\.dns_mode === 'local' && draft\.dns_role === 'primary' && draft\.dns_engine === 'pdns'"
    r"\s*\?\s*'setup\.pdnsPrimaryPaused'"
)


def wizard_pdns_primary_rule_present(source: Path = WIZARD_SOURCE) -> bool:
    """True while the shipped wizard still refuses a PowerDNS primary client-side.

    ``setup_selection_error`` is a hand port of that rule; if the rule leaves
    ``ServerSetup.tsx`` (for example when the gate opens) the driver must not
    keep applying it, so it checks the exact source text at run time.
    """

    try:
        return WIZARD_PDNS_RULE.search(source.read_text(encoding="utf-8")) is not None
    except OSError:
        return False


def setup_selection_error(draft: dict[str, Any], *, pdns_rule: bool = True) -> str | None:
    """ServerSetup.tsx ``dnsSelectionError`` for the purpose-dns wizard."""

    is_dns = draft.get("purpose") == "dns"
    if is_dns and draft.get("dns_mode") != "local":
        return "setup.components.localDNSRequired"
    if pdns_rule and draft.get("dns_mode") == "local" and draft.get("dns_role") == "primary"             and draft.get("dns_engine") == "pdns":
        return "setup.pdnsPrimaryPaused"
    return None


def gate_refusal_guidance(translator: Translator, key: str, *, code: str) -> dict[str, Any]:
    return _finish(translator, source="ui-selection-rule", state="unmet-prerequisite", code=code, reason=None,
                   title=None, messages=[_item(key)], details=[])


def preview_blocker_guidance(translator: Translator, blockers: list[Any]) -> dict[str, Any]:
    """DNSEngineCard blocker text for a preview's ``blockers[{code}]``."""

    codes = [item.get("code") for item in blockers if isinstance(item, dict) and item.get("code")]
    key_by_code = {"pdns_primary_switch_paused": "dnsEngine.blocker.pdnsPrimarySwitchPaused"}
    messages = [_item(key_by_code.get(code, f"dnsEngine.blocker.{code}")) for code in codes]
    return _finish(translator, source="dns-engine-preview", state="unmet-prerequisite",
                   code=codes[0] if codes else None, reason=None, title=None, messages=messages, details=[])
