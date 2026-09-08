# Alpha54 Release Evidence

*Verified: 8 September 2026 · [Türkçe](RELEASE-EVIDENCE-v0.1.0-alpha.54.tr.md)*

This records the release and public website state recovered when work resumed.
It does not establish the current installation state of Boston or Frankfurt.

| Evidence | Verified result |
|---|---|
| Release | [v0.1.0-alpha.54](https://github.com/celikbros/celikpanel/releases/tag/v0.1.0-alpha.54), published prerelease with six assets |
| Commit | `956d54fdd226483b776bdb84e29c029fab36e043` |
| Sequence | `54` |
| Release CI | [34202049674](https://github.com/celikbros/celikpanel/actions/runs/34202049674), completed successfully at the release commit |
| Manifest timestamp | `2026-09-08T07:58:14Z` |
| GitHub publication timestamp | `2026-09-08T08:09:51Z` |
| Platform | `linux/amd64` |
| Archive size | `23322968` bytes |
| Archive SHA-256 | `62d3a589c316a0f2dee5e5881a1bb4fc81489da7676cb3cecbd9002ae665b7fd` |
| Ed25519 signature | Independently verified against `deploy/release-signing-ed25519.pem` |
| Public portal | `https://celikpanel.net`, Alpha54 confirmed by HTTPS and read-only SSH |

`deploy/verify-download-portal-public.py` passed against the retained Alpha54
publication candidate: 15 requests, one complete archive GET and 23,487,882
downloaded bytes. HTML, CSS, JavaScript, bootstrap, public key, security contact,
release selectors, manifests, signature and archive matched the candidate.
The previous publication transcript records the supported publisher's committed
transaction and backup at `2026-09-08T08:12:37Z`; the backup was independently
observed over SSH. This resumption check did not republish Alpha54.

The navy-and-white design is live. Turkish/English switching, release details,
download links and internal section anchors worked in desktop and mobile checks.
Copy payloads matched the current and pinned commands with a test clipboard
adapter; neither command was executed. Opening the pinned-command disclosure
exposed a grid sizing defect: at a 390px viewport the page expanded to 975px.
Alpha55 addresses this defect by constraining the command and mobile grid tracks.

The August 30 handoff and server records are historical snapshots. The operator
reports that Boston and Frankfurt await installation by their users. Neither
host's present installation state was verified during this website inspection;
the old Alpha52 receipts must not be treated as proof of their current state.
