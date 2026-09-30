"""What the Panel shows the owner, reconstructed from the API payloads the UI renders.

D-024 (docs/OPERATION-GUIDANCE.md) requires every waiting or failed state to
name the reason, who must act, the next action and how work resumes. The
driver cannot look at a browser, so it applies the web UI's own selection
rules to the exact API payloads and resolves the result through the shipped
translation catalogues in ``web/src/i18n`` (the product's ``web/src`` when a
run passes ``--product-web-src``; the driver's own checkout otherwise):

* ``api_error_guidance``  - ``apiErrorText`` (web/src/lib/apiError.ts):
  ``err.<code>.<reason>``, then ``err.<code>``, then the server message.
* ``setup_execution_guidance`` - ``setupExecutionGuidance``
  (web/src/lib/serverSetupGuidance.ts), ported rule for rule.
* ``deletion_pending_guidance`` - ``readDomainDeletionOutcome`` +
  ``pendingMessage`` (web/src/lib/domainDeletionPending.ts,
  web/src/components/Domains.tsx).
* ``setup_selection_error`` - the wizard's client-side selection rule
  (web/src/components/ServerSetup.tsx ``dnsSelectionError``).
* ``plan_blocker_guidance`` - the review's server plan blockers through the
  wizard's ``codeKey`` map (``failureText``), read from the shipped source.

* ``setup_plan_failure_guidance`` / ``setup_start_failure_guidance`` - what
  the wizard shows when ``POST /api/v1/setup/plan`` or ``/setup/start`` is not
  OK (``ServerSetup.tsx`` ``review``/``start``), with the ``apiErrorText`` view
  of the same body alongside.

A state is *actionable* only when it carries a stable, specific machine code
and the UI resolves it to a specific, non-generic translated message. A
generic fallback on a blocked, failed or pending state fails the step:
``err.INTERNAL`` (code ``INTERNAL``, "internal server error"),
``setup.planFailed``, ``setup.blocker.unknown``, ``setup.guide.unknown``,
``domains.deletionPending`` (an empty or unreviewed reason), a raw server
string without a code, or a bare HTTP status. "Try again / check the logs"
names no reason, actor or next action (D-024).
"""

from __future__ import annotations

import re
from pathlib import Path
from typing import Any, Iterable

REPO = Path(__file__).resolve().parents[3]
# The driver's own checkout. A run whose product build comes from another
# commit passes --product-web-src (the product's web/src, exported next to the
# dist by scripts/build-dist.sh) so every text below is the PRODUCT's.
WEB_SRC = REPO / "web" / "src"
I18N_ROOT = WEB_SRC / "i18n"
WEB_SRC_FILES = {
    "i18n": "i18n",
    "wizard": "components/ServerSetup.tsx",
    "setup_guidance": "lib/serverSetupGuidance.ts",
}
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
        "setup.blocker.unknown",
        "domains.deletionPending",
        "common.error",
        "dns.recordAddFailed",
        "dns.recordDeleteFailed",
        "dns.zonePublishFailed",
        # apiErrorText for code INTERNAL: "The server hit an internal error. Try
        # again; if it persists, check the panel logs." No reason, actor or step.
        "err.INTERNAL",
        # ServerSetup.tsx review(): any non-OK plan response, whatever its body.
        "setup.planFailed",
    }
)
# Machine codes that name no cause. A state carrying only one of these is
# treated like a state without a code (cmd/panel writeServerError: 500
# {"code":"INTERNAL","error":"internal server error"}).
GENERIC_CODES = frozenset({"INTERNAL"})
GENERIC_MESSAGES = frozenset({"internal server error"})
START_REVIEW_CODES = frozenset({"server_setup_review_required", "server_setup_review_stale",
                                "server_setup_plan_blocked"})
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
        # pair3: the product's own BIND cannot be asked about zone state
        # (no usable rndc key); the owner acts on that server.
        "bind_rndc_unavailable",
        # pair4 P4-1: the secondary's named refused the owner inspector's
        # local (loopback) catalog transfer; the secondary's owner acts.
        "dns_peer_catalog_transfer_refused",
        # pair5 P5-1: the Agent could not run its own proof of the secondary
        # (internal precondition); no owner change was found.
        "dns_peer_proof_internal",
    }
)

# D-024 time bound (pair2 t3): codes that mean "the result is not known yet /
# being reconciled". The wizard's text for them names no actor and no next
# action other than "do not start it again", which is acceptable only for a
# bounded time (--unknown-state-limit-seconds, default 300). Data, not
# wording: add a code here only after reading what the wizard shows for it.
UNKNOWN_STATE_CODES = ("server_setup_reconciling",)
LICENSE_REQUIRED_CODE = "license_required"
# Role guidance that instructs the owner to start or point the peer at this
# server, by text key (pair2 t3: shown while the primary's own DNS step had not
# succeeded). Only keys the product wizard actually uses for that role at run
# time (role_guidance_keys) are checked.
PEER_START_INSTRUCTION_KEYS = {"primary": ("setup.guide.startSecondary",)}

# Reviewed actor for codes whose owner the product text names. Anything not
# listed is recorded as "stated in text" and left to the human reviewer.
ACTOR_BY_CODE = {
    "mail_runtime_cleanup_failed": "this server's owner (mail storage on this server)",
    "license_required": "this server's administrator (license activation)",
    "LICENSE_VERIFICATION_UNAVAILABLE": "this server's administrator (license verification service)",
    "LICENSE_STATUS_UNAVAILABLE": "this server's administrator",
    "pdns_primary_switch_paused": "this server's administrator (choose another engine or topology)",
    "dns_peer_enrollment_required": "primary administrator together with the secondary owner",
    "dns_peer_enrollment_changed": "primary administrator (pinned peer enrollment)",
    "dns_peer_inspection_unknown": "primary administrator and secondary owner",
    "dns_peer_catalog_transfer_refused": "the secondary's owner (allow the catalog transfer from loopback)",
    "dns_peer_proof_internal": "this server's owner (read the Agent log and report it; retry after a fixed Agent)",
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
    elif not code and not raw_message and not keys:
        actionable, why = False, "bare HTTP status: no code, reason or text"
    elif not code:
        actionable, why = False, "blocked/pending state has no stable machine code"
    elif code in GENERIC_CODES:
        actionable, why = False, (f"generic error code {code}: names no reason, no actor and no next action; "
                                  "retrying or reading logs is not guidance")
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
    if not key and not message and status >= 400:
        # apiErrorText's last fallback for an empty or uncoded body.
        messages = [_item("common.error")]
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
    if state not in {"progress", "succeeded"} and not code and not (message or "").strip():
        result["actionable_reason"] = f"bare HTTP status {status}: no code, reason or text"
    elif state not in {"progress", "succeeded"} and not code and (message or "").strip().lower() in GENERIC_MESSAGES:
        result["actionable_reason"] = f"generic server text {message!r} without a code"
    result["http_status"] = status
    result["action_path"] = body.get("action") if isinstance(body.get("action"), str) else None
    return result


def _with_api_view(result: dict[str, Any], api: dict[str, Any], status: int) -> dict[str, Any]:
    result["http_status"] = status
    result["api_error_keys"] = api["message_keys"]
    result["api_error_text"] = api["shown"]
    return result


def setup_plan_failure_guidance(translator: Translator, status: int, body: Any) -> dict[str, Any]:
    """``ServerSetup.tsx`` ``review()``: ``if (!response.ok) throw new Error(t('setup.planFailed'))``.

    The wizard shows ``setup.planFailed`` for every non-OK plan response and
    never reads its body, so the owner sees the same generic sentence for a
    500 ``INTERNAL`` as for anything else. ``api_error_text`` records what
    ``apiErrorText`` would show for that body elsewhere (``err.INTERNAL`` for
    the pair1 case). Neither names the reason, the actor or the next action.
    """

    api = api_error_guidance(translator, status, body)
    state = api["state"] if api["state"] not in {"progress", "succeeded"} else "verified-failure"
    result = _finish(translator, source="setup-plan-review", state=state, code=api["code"], reason=api["reason"],
                     title=None, messages=[_item("setup.planFailed")], details=[], raw_message=api["raw_message"])
    if api["code"] in GENERIC_CODES or not api["code"]:
        result["actionable_reason"] = (f"the wizard shows only setup.planFailed for HTTP {status}; the body carries "
                                       f"{'code ' + api['code'] if api['code'] else 'no code'}: "
                                       "no reason, no actor, no next action")
    return _with_api_view(result, api, status)


def setup_start_failure_guidance(translator: Translator, status: int, body: Any) -> dict[str, Any]:
    """``ServerSetup.tsx`` ``start()`` for a non-OK response.

    409 with a review code returns to the access step with ``setup.conflict``;
    anything else is reconciled by reads, and until an execution is found the
    wizard shows ``setup.reconnecting`` / ``setup.uncertain`` (an unknown
    result). The body's code decides whether a cause exists at all.
    """

    api = api_error_guidance(translator, status, body)
    code = api["code"]
    if status == 409 and code in START_REVIEW_CODES:
        result = _finish(translator, source="setup-start", state="unmet-prerequisite", code=code, reason=api["reason"],
                         title=None, messages=[_item("setup.conflict")], details=[], raw_message=api["raw_message"])
    else:
        result = _finish(translator, source="setup-start", state="unknown", code=code, reason=api["reason"],
                         title="setup.reconnecting", messages=[_item("setup.uncertain")], details=[],
                         raw_message=api["raw_message"])
    return _with_api_view(result, api, status)


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
# Domain deletion (202), the wizard's selection rule and server plan blockers
# ---------------------------------------------------------------------------

# Reviewed verified failures of a non-DNS deletion stage, by stage
# (web/src/lib/domainDeletionPending.ts reviewedStageFailures). pair4 P4-2: the
# mail stage on an Arch primary. Data, not wording: add a stage here only after
# reading the key the Domains screen shows for it.
REVIEWED_DELETION_STAGE_FAILURES = {"mail_runtime_cleanup": "mail_runtime_cleanup_failed"}


def _reviewed_stage_failure(body: dict[str, Any]) -> str:
    stage, reason = body.get("stage"), body.get("reason")
    if isinstance(stage, str) and REVIEWED_DELETION_STAGE_FAILURES.get(stage) == reason:
        return reason
    return ""


def deletion_pending_guidance(translator: Translator, status: int, body: Any, *, saved: bool = False) -> dict[str, Any]:
    body = body if isinstance(body, dict) else {}
    reason = ""
    stage_failure = False
    if saved:
        # readSavedDomainDeletionStatus
        if status == 200 and body.get("status") == "unknown" and body.get("stage") == "unknown":
            reason = ""
        elif status == 200 and body.get("status") == "failed":
            # A recorded, verified stage failure (P4-2).
            reason = _reviewed_stage_failure(body)
            stage_failure = True
        elif status == 200 and body.get("status") == "deletion_pending" and body.get("stage") == "dns_cleanup":
            reason = body.get("reason") if body.get("reason") in REVIEWED_DNS_PEER_REASONS else ""
    elif status == 202 and body.get("status") == "deletion_pending" and body.get("stage") != "dns_cleanup":
        reason = _reviewed_stage_failure(body)
        stage_failure = bool(reason)
    elif status == 202 and body.get("status") == "deletion_pending" and body.get("stage") == "dns_cleanup":
        reason = body.get("reason") if body.get("reason") in REVIEWED_DNS_PEER_REASONS else ""
    if reason in REVIEWED_DELETION_STAGE_FAILURES.values():
        key = f"err.DOMAIN_DELETION_FAILED.{reason}"
    else:
        key = f"err.DNS_PUBLICATION_FAILED.{reason}" if reason else None
    message_key = key if key and translator.has(key) else "domains.deletionPending"
    result = _finish(
        translator, source="domain-deletion", state="verified-failure" if stage_failure else "pending",
        code=reason or (body.get("stage") if isinstance(body.get("stage"), str) else None),
        reason=reason or None, title=None, messages=[_item(message_key)], details=[],
        raw_message=body.get("message") if isinstance(body.get("message"), str) else None,
    )
    result["http_status"] = status
    result["stage"] = body.get("stage")
    return result


WIZARD_SOURCE = WEB_SRC / WEB_SRC_FILES["wizard"]
GUIDANCE_SOURCE = WEB_SRC / WEB_SRC_FILES["setup_guidance"]
ROLE_KEY_RE = re.compile(r"dns_role\s*===\s*'primary'\s*\?\s*'(?P<primary>[A-Za-z0-9_.]+)'"
                         r"\s*:\s*'(?P<secondary>[A-Za-z0-9_.]+)'")
WIZARD_CODE_MAP_RE = re.compile(r"const codeKey: Record<string, TranslationKey> = \{(?P<body>.*?)\n\};", re.S)
WIZARD_CODE_ENTRY_RE = re.compile(r"^\s*(?P<code>[A-Za-z0-9_]+)\s*:\s*'(?P<key>[A-Za-z0-9_.]+)'\s*,?\s*$", re.M)
PDNS_PRIMARY_GATE_CODE = "pdns_primary_switch_paused"


def wizard_code_keys(source: Path = WIZARD_SOURCE) -> dict[str, str]:
    """ServerSetup.tsx ``codeKey``: the text key the wizard shows for a server code.

    Since 6f2fb028 the PowerDNS-primary refusal is the server's plan blocker
    ``pdns_primary_switch_paused``; the wizard only maps it to a text key.
    The map is read from the shipped source at run time, so a build whose
    wizard no longer maps a code falls back to ``setup.blocker.unknown`` exactly
    as ``failureText`` does, and that generic text fails D-024.
    """

    try:
        match = WIZARD_CODE_MAP_RE.search(source.read_text(encoding="utf-8"))
    except OSError:
        return {}
    if match is None:
        return {}
    return {entry.group("code"): entry.group("key") for entry in WIZARD_CODE_ENTRY_RE.finditer(match.group("body"))}


def setup_selection_error(draft: dict[str, Any]) -> str | None:
    """ServerSetup.tsx ``dnsSelectionError`` for the purpose-dns wizard.

    The wizard no longer refuses a PowerDNS primary itself; the server's plan
    carries that blocker (``plan_blocker_guidance``).
    """

    if draft.get("purpose") == "dns" and draft.get("dns_mode") != "local":
        return "setup.components.localDNSRequired"
    return None


def plan_blocker_guidance(translator: Translator, blockers: list[Any], code_keys: dict[str, str]) -> dict[str, Any]:
    """The review's blocker list: ``setup.planBlocked`` over ``failureText(code)`` for each code."""

    codes = [code for code in blockers if isinstance(code, str) and code]
    messages = [_item(code_keys.get(code.split(":")[0], "setup.blocker.unknown")) for code in codes]
    first = next((code for code in codes if code.split(":")[0] == PDNS_PRIMARY_GATE_CODE), codes[0] if codes else None)
    return _finish(translator, source="setup-plan-blockers", state="unmet-prerequisite", code=first, reason=None,
                   title="setup.planBlocked", messages=messages, details=[])


def preview_blocker_guidance(translator: Translator, blockers: list[Any]) -> dict[str, Any]:
    """DNSEngineCard blocker text for a preview's ``blockers[{code}]``."""

    codes = [item.get("code") for item in blockers if isinstance(item, dict) and item.get("code")]
    key_by_code = {"pdns_primary_switch_paused": "dnsEngine.blocker.pdnsPrimarySwitchPaused"}
    messages = [_item(key_by_code.get(code, f"dnsEngine.blocker.{code}")) for code in codes]
    return _finish(translator, source="dns-engine-preview", state="unmet-prerequisite",
                   code=codes[0] if codes else None, reason=None, title=None, messages=messages, details=[])


# ---------------------------------------------------------------------------
# Product web source, unknown states, license and role guidance (pair2)
# ---------------------------------------------------------------------------

def missing_web_src_files(web_src: Path) -> list[str]:
    """Files the driver reads from a product ``web/src``; empty when all exist."""

    return [relative for relative in WEB_SRC_FILES.values() if not (Path(web_src) / relative).exists()]


def _error_code(execution: Any) -> str | None:
    if not isinstance(execution, dict) or not isinstance(execution.get("error"), dict):
        return None
    code = execution["error"].get("code")
    return code.split(":")[0] if isinstance(code, str) and code else None


def unknown_state_code(execution: Any) -> str | None:
    """The listed unknown/reconciling code of a non-terminal setup state, else None."""

    if not isinstance(execution, dict) or execution.get("status") in {"succeeded", "failed"}:
        return None
    code = _error_code(execution)
    return code if code in UNKNOWN_STATE_CODES else None


def license_required_state(execution: Any) -> bool:
    """A setup state that tells the owner a license is required (code or the license phase)."""

    if not isinstance(execution, dict):
        return False
    return _error_code(execution) == LICENSE_REQUIRED_CODE or (
        execution.get("status") == "waiting" and execution.get("phase") == "license")


def dns_step_status(execution: Any) -> str | None:
    if not isinstance(execution, dict):
        return None
    return next((step.get("status") for step in execution.get("steps") or []
                 if isinstance(step, dict) and step.get("kind") == "dns"), None)


DNS_PHASES = frozenset({"dns", "dns_readiness"})
DNS_ROLLED_BACK_CODE = "server_setup_dns_rolled_back"


def dns_blocking_state(execution: Any) -> str | None:
    """Why the primary's own DNS step cannot let a secondary succeed now, or None.

    The product's role text deliberately lets the owner start the secondary
    while the primary's setup is still progressing, so an ordinary ``running``
    or ``pending`` DNS step is not a contradiction. Blocking are: the DNS step
    ``failed``; a listed unknown state (``UNKNOWN_STATE_CODES``); a
    ``waiting`` state with a code at the DNS step (phase ``dns`` /
    ``dns_readiness`` or the current step is the ``dns`` step); a rolled-back
    DNS result (``server_setup_dns_rolled_back``). Never once the DNS step
    succeeded.
    """

    if not isinstance(execution, dict):
        return None
    dns_status = dns_step_status(execution)
    if dns_status == "succeeded":
        return None
    code = _error_code(execution)
    if code == DNS_ROLLED_BACK_CODE:
        return f"dns-rolled-back:{code}"
    if dns_status == "failed":
        return f"dns-step-failed:{code or 'no-code'}"
    unknown = unknown_state_code(execution)
    if unknown:
        return f"unknown:{unknown}"
    if execution.get("status") == "waiting" and code:
        steps = [step for step in execution.get("steps") or [] if isinstance(step, dict)]
        current = next((step for step in steps if step.get("status") == "failed"), None) or next(
            (step for step in steps if step.get("status") in {"running", "waiting"}), None)
        if execution.get("phase") in DNS_PHASES or (current or {}).get("kind") == "dns":
            return f"dns-waiting:{code}"
    return None


def role_guidance_keys(source: Path = GUIDANCE_SOURCE) -> dict[str, list[str]]:
    """Text keys ``setupExecutionGuidance`` picks by ``context.dns_role``, read from the shipped source."""

    try:
        text = Path(source).read_text(encoding="utf-8")
    except OSError:
        return {"primary": [], "secondary": []}
    matches = list(ROLE_KEY_RE.finditer(text))
    return {"primary": [m.group("primary") for m in matches], "secondary": [m.group("secondary") for m in matches]}


def peer_start_keys(source: Path = GUIDANCE_SOURCE, role: str = "primary") -> dict[str, Any]:
    """The instruction keys to check for ``role``: listed AND used by the product wizard for that role."""

    role_keys = role_guidance_keys(source)
    listed = PEER_START_INSTRUCTION_KEYS.get(role, ())
    keys = [key for key in listed if key in role_keys.get(role, [])]
    return {"source": str(source), "role": role, "role_keys": role_keys.get(role, []), "listed": list(listed),
            "keys": keys, "state": "resolved" if keys else "not-used-by-this-build"}
