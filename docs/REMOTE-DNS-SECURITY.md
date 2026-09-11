# Remote DNS publishing implementation scope

*[Türkçe](REMOTE-DNS-SECURITY.tr.md) · [Implementation status](SERVER-SETUP-STATUS.md)*

**Status:** implementation and local testing explicitly authorized on
11 September 2026. The user answered the separate scoped authorization request
with approval to continue; see [authorization record](REMOTE-DNS-AUTHORIZATION.md).
Earlier automatic-review rejections are historical, not an outstanding permission
request. Implementation remains subject to the requirements below; availability
is recorded in the implementation status document.

This document records the source-code scope already approved in
[SERVER-SETUP-PLAN.md](SERVER-SETUP-PLAN.md), section 3, option 2:

> **Use an existing CelikPanel DNS infrastructure:** establish authorized
> publishing/consuming relationships and verify the remote authority. A peer IP
> alone grants no management permission. A web host using remote DNS need not
> become a DNS secondary or install an authoritative engine.

The user accepted that plan with “ok anlaştık” and then instructed implementation:
“İyi düşündüysen, eminsen yapalım.” This was the implementation scope inferred from that approval. The later explicit authorization now covers the credential-bearing connector
described below, as recorded above. It does not authorize pairing
real servers, sending credentials to any server, publishing a release, or updating
an installed panel. No such action is performed by adding this implementation.
Installed updates remain user-initiated in CelikPanel as required by AGENTS.md.

## Explicit administrative pairing

An authenticated administrator on an already prepared authoritative DNS server
explicitly creates a ten-minute, single-use enrollment code. An administrator on
the consuming host enters that code and the intended HTTPS panel endpoint and
explicitly starts pairing. Reading a page, assessing setup, or selecting a draft
mode does not create credentials or establish a relationship.

Only the selected endpoint receives a code or scoped credential. The client pins
each connection attempt to freshly resolved public addresses, verifies the
endpoint's normal trusted TLS certificate, rejects redirects and proxy use, and
rejects private, loopback, link-local, reserved and literal-IP endpoints. Codes and
credentials are never in URLs, public status responses, audit text or logs.
Pending pairing has an exact persisted identity so a lost response can be
reconciled without creating a second authorization. Cancellation requires an
exact receiver receipt; a generic HTTP authorization error does not prove that
no authorization was granted. An unresolved attempt remains available for
recovery while a separately confirmed new enrollment may proceed.

## Authentication and least privilege

Only three exact machine paths exist: enrollment acceptance, receiver status and
receiver publication. TLS and a verified one-use enrollment code or scoped
credential must be established before treating a request as a machine caller.
Browser cookies, Origin, Referer and fetch metadata are rejected. Administrative
pairing, listing and revocation keep ordinary authenticated admin sessions and
CSRF checks. License enforcement remains independent and applies to the receiver.

The receiver stores credential hashes, never reusable client secrets. A client
may publish only zones permanently owned by its client ID. It may not adopt an
existing local zone, claim another client's zone, change topology, execute shell
commands or administer the receiving panel. Revocation prevents later publication
but preserves already served zones and workloads.

## Durable publication and evidence

The origin persists canonical desired records and an increasing generation before
sending a publication. The receiver claims the exact owner and generation before
using CelikPanel's existing local DNS publication machinery. Retrying an identical
generation reconciles publication; changing its payload, replaying an older
generation or using a different client fails. A lost HTTP response is not success.
An origin also retains generation history after domain deletion. Recreating a
zone through the same connection continues after its applied deletion receipt;
a stale deletion cannot remove that later generation. Ownership never transfers
to another client automatically. Exact already-authorized pending publication
may reconcile during temporary pair unavailability; every new generation still
requires fresh authority proof.

Readiness requires fresh authenticated proof of the authoritative engine and its
independently hosted secondary, not merely a configured peer IP.

## Validation boundary

Tests use temporary databases, isolated HTTP/RPC fixtures and a real local TLS
listener with test-only trust and address routing. No production credentials or
live server pairing is needed. Acceptance tests must cover unauthenticated
and cross-client denial, expiry/revocation, endpoint restrictions, lost-response
recovery, preservation of existing zones and immutable per-domain ownership.
Actual release and installed-panel updates remain separate actions.
