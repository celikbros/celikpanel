# BE: native Agent compatibility and atomic resource inverse

Source `1505a9bcbd43e19962bafac0d24fb721ded38928`, local disposable QEMU
cell `release-recovery__3688d130a0721786`; Debian13 and Arch. Controllers
verify loopback SSH endpoints, registered QEMU processes, pinned keys, protected
guest identity and DMI UUID. No production panel or installed-panel update ran.

The exact-source Makefile built the normal Agent, its native contract and the
independent reader. Their SHA-256 identities and full source declaration are in
[the evidence](AGENT-COMPATIBILITY-BE.json). The reader inspects private application
fixtures without executing their Agent binaries.

On Debian, with the independently enrolled mail hook/helper and enabled active
timer, the compatible Agent passed. An unmarked management binary, edited Agent,
edited declaration and linked Agent each returned refusal with same-operation
recovery guidance. Native hook/unit/helper material, selected certificate, mail
configuration and ledger identities remained unchanged. Management binaries
stayed absent. SMTP 587 and IMAP 993 passed trusted hostname/CA verification with
leaf `2de247fc73a06d582bb53c9cc90a1439d7da22da05f1491a381eeb8b20d6f5f7`.

On Arch, no native mail hook, units or enablement link was installed. Historical
compatibility remained permissible in that absence state, but strict candidate
admission still refused a missing declaration or edited Agent. The existing
shared wants directory, its contents and management absence were unchanged.
This does not claim a running Arch mail workload.

Both native kernels ran the same private filesystem resource tests. Nine actual
SIGKILL boundaries per platform cover declaration addition/replacement, exact
same-operation continuation/inverse, and interrupted inverse after the candidate
directory disappears. A later owner edit blocks inverse publication. These test
operations use private fixture directories and never replace installed binaries.

`verify_agent_compatibility.py` and its negative tests validate the public record.
This establishes read-only native admission and the stated atomic resource cases.
It is not a complete installed update, automatic application rollback, production
mail enrollment, old-writer behavior certification or power-loss acceptance.
