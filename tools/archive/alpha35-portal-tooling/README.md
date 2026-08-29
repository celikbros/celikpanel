# Alpha35 portal tooling (archived)

These files are exact recovery copies of historical Alpha35 portal build,
promotion, publication, and verification helpers found in a local handoff
worktree on 2026-08-29.

They are preserved for engineering archaeology only. They are **not** the
current CelikPanel build, release, signing, publication, or deployment path.
Do not execute, apply, or merge them as-is. A future owner must first review
their trust assumptions, external dependencies, release layout, and tests
against the then-current codebase.

Recovered files and SHA-256 digests:

| File | SHA-256 |
| --- | --- |
| `build-alpha35-portal.sh` | `ade27d8d87102b38e997094b84bd1d8637e637e2fc39f8e2259d852ca8a4ad53` |
| `promote-alpha35-portal.py` | `087be6fb989e8ee933703f960d521820f30b95c07cce38cb386ae09d33f535ee` |
| `publish-alpha35-portal.ps1` | `4c13ff54085b5c6db29f66189a3da73acc69cdf29db2d7d9f5850944bc0d2154` |
| `test-alpha35-promote-offline.py` | `36c15629bf72379be9e87772fc35c4a42e4b17075183e6d3df68b8ff66786266` |
| `verify-alpha35-release.py` | `5fc5650df4e8bd1336ee6bbc6033ed30622b56ceb5ee32dc96be100b7a5c18ce` |

Line-ending note: the recovered `publish-alpha35-portal.ps1` source used CRLF.
The repository's enforced `*.ps1 text eol=lf` rule normalizes the archived
copy to LF. Its normalized-content SHA-256 is
`f5fd5020383e0ce4ae524b27885cd0ddee6871687c4c7d9ceda57b51f0ad7602`;
the source byte SHA-256 remains recorded in the table above.
