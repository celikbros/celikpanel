# License enforcement review — 10 September 2026

[Türkçe](LICENSE-REVIEW-2026-09-10.tr.md)

## Finding and live evidence

The reported behavior is a server-side authorization defect in Alpha63, not
only an outdated label in the browser. The old manager treats its signed receipt
as permission until `offline_until`. Explicit central refresh rejections return
an error without invalidating that receipt. A successful signature proves who
issued the old receipt; it does not prove that the activation is still current.

Read-only SSH inspection at approximately 18:53 UTC confirmed:

| Target | Observed state |
|---|---|
| Boston, `2.25.80.4` | Alpha63; no installed `license.json`; panel and agent active |
| Frankfurt, `72.62.38.15` | Alpha63; cached receipt still present; panel and agent active |
| `celikpanel.net` | Latest published release Alpha63; Frankfurt's central license has no current server binding |

Both installed panel binaries have SHA-256
`dc38563e34dd49c114f7a941e9c9d351fb606f762c4c04c14b53a4c275b9a482`.
Frankfurt's receipt was issued at `1789055155`, with `refresh_after=1789141555`
and `offline_until=1789659955`: a 24-hour refresh interval and seven-day offline
allowance. Its annual expiry is separate from this cached authorization window.
No production license was rotated, activated, deleted, or manually rewritten
during this review. No production service was restarted.

## Recovered correction

The previous session had already committed the main Alpha64 correction as
`0d64e7c8b4039e662adfb9141547a7f432705106` on `fix/license-revocation-64`.
It is not the version installed on the two servers or published by the portal.

- Every authenticated management request checks shared license state. All four
  roles are covered: administrator, reseller, customer, and additional user.
- Renewal starts after 45 seconds of the signed receipt's lifetime. Access ends
  at 60 seconds unless central verification succeeds. Old seven-day receipts
  are capped locally to the same 60 seconds.
- An explicit rejection immediately denies the request that discovered it and
  persists a rejection marker, so restarting the panel does not restore access.
- Network failure does not grant a new authorization window. Renewal credentials
  remain available for recovery, with shared retry backoff. Idle servers do not
  poll the licensing service.
- The browser removes management pages and presents activation. Direct page
  URLs do not bypass either the browser gate or the server gate.
- The root agent and existing service/scheduler paths do not depend on this
  gate. Websites, databases, mail, and already scheduled workloads keep running.
- Activation, own-account recovery, and the narrowly scoped administrator
  signed-update flow remain available. These exceptions do not grant hosting
  management access or relax update signature checks.

The policy has a bounded detection window, not instant push revocation: a key
changed just after successful verification may remain usable until the next
check, with the existing authorization ending no later than 60 seconds after
its issuance. The annual license term does not extend that window.

## Additional regression coverage

`cmd/panel/license_activity_test.go` previously called `Refresh` directly after
an identity request. That could pass even if the management middleware never
performed verification. The test now sends actual management requests and
checks automatic renewal, central rejection, shared state across all roles,
and denied access after manager reconstruction.

`portal-membership/tests/service.php` now explicitly checks that rotating a key
invalidates the previous activation token on the same server and IP, independently
of a server-release operation or a machine-identity change.

## Validation and publication boundary

Targeted Go licensing/panel tests, the membership integration suite, all 321
frontend tests, and the frontend production build passed during review. Local
Chrome verification covered seven role/language/viewport scenarios, including
Turkish and English at 1440 px and 390 px, rejection locking, preserved form state
after successful verification, and denied deep links. These browser scenarios
use fixture API responses, not live production licenses.

Final verification passed: `make test vet` on the clean tracked-source snapshot,
licensing/panel race tests, 66 membership service checks, 31 membership HTTP
checks, five isolated SMTP tests, and the release-sequence, download-portal,
and signed-manifest contracts. The snapshot included the two final test changes.
The initial whole-workspace run also picked up ignored experimental Go fragments
under `artifacts/`; those local experiments were left untouched.

Production remains Alpha63. This review does not establish that the defect has
already been fixed on live servers. The remaining operational step is a reviewed
Alpha64 release, portal publication, and installation through the panel's signed
update flow, Boston first and Frankfurt after verification. Publication must
preserve membership data, signing secrets, and existing license keys.
