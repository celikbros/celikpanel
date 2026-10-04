"""Capture-time secret redaction for the DNS pair acceptance driver.

Every request/response pair, guest command output and journal excerpt passes
through one :class:`Redactor` *before* it is handed to the evidence writer, so
no session cookie, CSRF token, password, license key, bearer token or private
key is ever written to disk. The redactor combines three independent rules:

1. structural: JSON object keys and HTTP header names that denote secrets;
2. registered values: exact secret strings learned at run time (the fixture
   admin password, cookie values from ``Set-Cookie``, CSRF tokens), replaced
   wherever they occur, including inside URLs and free text;
3. shape: PEM private-key blocks and ``Bearer``/``Basic`` credentials.

The module is standard-library only and has no side effects.
"""

from __future__ import annotations

import re
from typing import Any, Iterable, Mapping

REDACTED = "[REDACTED]"

# Header names whose values are always secret (compared case-insensitively).
SECRET_HEADERS = frozenset(
    {
        "authorization",
        "proxy-authorization",
        "cookie",
        "set-cookie",
        "x-csrf-token",
        "x-xsrf-token",
        "csrf-token",
        "x-auth-token",
        "x-api-key",
        "x-celikpanel-csrf",
    }
)

# A key is secret when any of these fragments occurs in its lower-cased name,
# unless the whole key is explicitly public below.
SECRET_KEY_FRAGMENTS = (
    "password",
    "passphrase",
    "passwd",
    "secret",
    "token",
    "csrf",
    "xsrf",
    "cookie",
    "session",
    "private",
    "license_key",
    "licence_key",
    "activation_key",
    "activation_code",
    "api_key",
    "apikey",
    "otp",
    "totp",
    "recovery_code",
    "backup_code",
    "credential",
    "authorization",
    "tsig",
    "rndc_key",
    "key_material",
)

# Metadata about a secret (its state, digest, identifier or reason) is public
# evidence the guidance check needs; the secret itself never has these names.
PUBLIC_KEY_SUFFIXES = (
    "_state",
    "_status",
    "_reason",
    "_required",
    "_id",
    "_sha256",
    "_count",
    "_at",
    "_enabled",
    "_configured",
    "_present",
    "_kind",
    "_type",
)

# Keys that contain a fragment above but are public identifiers or digests.
PUBLIC_KEYS = frozenset(
    {
        "public_key",
        "public_key_sha256",
        "client_public_key_sha256",
        "host_key_sha256",
        "credential_id",
        "session_count",
        "sessions",
        "session_expires_at",
        "token_type",
        "otp_enabled",
        "totp_enabled",
        "two_factor_enabled",
        "requires_totp",
        "requires_otp",
        "private_ip",
        "private_network",
    }
)

# Exact key names that are secret although no fragment matches. "code" is
# deliberately absent: error/state codes are the guidance evidence.
SECRET_EXACT_KEYS = frozenset({"license_key", "sid", "jwt", "enrollment_code", "pending_token"})

PEM_PRIVATE_RE = re.compile(
    r"-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----",
    re.DOTALL,
)
AUTH_SCHEME_RE = re.compile(r"\b(Bearer|Basic|Token)\s+[A-Za-z0-9._~+/=-]{6,}", re.IGNORECASE)
# Cookie-like "name=value" pairs whose name denotes a session or CSRF secret.
COOKIE_PAIR_RE = re.compile(
    r"(?i)\b([a-z0-9_.-]*(?:session|csrf|xsrf|token|sid|auth)[a-z0-9_.-]*)=([^;\s,&\"']{4,})"
)
# Query-string or form parameters that carry secrets.
QUERY_SECRET_RE = re.compile(
    r"(?i)([?&](?:password|token|csrf|session|key|secret|code)=)([^&#\s\"']+)"
)
MIN_REGISTERED_LENGTH = 6


def secret_key(name: str) -> bool:
    lowered = name.strip().lower().replace("-", "_")
    if lowered in PUBLIC_KEYS:
        return False
    if lowered in SECRET_EXACT_KEYS:
        return True
    if lowered.endswith(PUBLIC_KEY_SUFFIXES):
        return False
    return any(fragment in lowered for fragment in SECRET_KEY_FRAGMENTS)


class Redactor:
    """Redact secrets from strings, JSON values and HTTP header maps."""

    def __init__(self, secrets: Iterable[str] = ()) -> None:
        self._secrets: set[str] = set()
        for value in secrets:
            self.register(value)

    def register(self, value: str | None) -> None:
        """Remember an exact secret value; short or empty values are refused.

        A too-short value would redact unrelated text and hide evidence, so it
        is rejected loudly instead of being silently ignored.
        """

        if value is None:
            return
        if not isinstance(value, str):
            raise TypeError("registered secrets must be strings")
        if len(value) < MIN_REGISTERED_LENGTH:
            raise ValueError("refusing to register a secret shorter than 6 characters")
        self._secrets.add(value)

    @property
    def registered_count(self) -> int:
        return len(self._secrets)

    def text(self, value: str) -> str:
        result = value
        # Longest first so a secret containing another is removed whole.
        for secret in sorted(self._secrets, key=len, reverse=True):
            if secret in result:
                result = result.replace(secret, REDACTED)
        result = PEM_PRIVATE_RE.sub(REDACTED, result)
        result = AUTH_SCHEME_RE.sub(lambda match: f"{match.group(1)} {REDACTED}", result)
        result = COOKIE_PAIR_RE.sub(lambda match: f"{match.group(1)}={REDACTED}", result)
        result = QUERY_SECRET_RE.sub(lambda match: f"{match.group(1)}{REDACTED}", result)
        return result

    def _secret_item(self, item: Any) -> Any:
        """Value under a secret key: scalars go, structure is kept and walked."""

        if item is None or item == "" or isinstance(item, bool):
            return item
        if isinstance(item, Mapping):
            return self.value(item)
        if isinstance(item, (list, tuple)):
            return [self._secret_item(element) for element in item]
        return REDACTED

    def value(self, value: Any) -> Any:
        if isinstance(value, str):
            return self.text(value)
        if isinstance(value, Mapping):
            redacted: dict[str, Any] = {}
            for key, item in value.items():
                key_text = str(key)
                if secret_key(key_text):
                    redacted[key_text] = self._secret_item(item)
                else:
                    redacted[key_text] = self.value(item)
            return redacted
        if isinstance(value, (list, tuple)):
            return [self.value(item) for item in value]
        return value

    def headers(self, headers: Iterable[tuple[str, str]] | Mapping[str, str]) -> list[list[str]]:
        items = headers.items() if isinstance(headers, Mapping) else headers
        result: list[list[str]] = []
        for name, raw in items:
            if name.strip().lower() in SECRET_HEADERS or secret_key(name):
                result.append([name, REDACTED])
            else:
                result.append([name, self.text(str(raw))])
        return result

    def contains_secret(self, text: str) -> bool:
        """True when a registered secret survives in ``text`` (a test oracle)."""

        return any(secret in text for secret in self._secrets)
