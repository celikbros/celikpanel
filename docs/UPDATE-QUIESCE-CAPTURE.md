# Update coordinator capture

[Turkish](UPDATE-QUIESCE-CAPTURE.tr.md)

## Incident and evidence

On September 13, 2026, two Frankfurt attempts to install alpha.76 stopped with
`state=unchanged` and `coordinator cgroup changed during quiesce capture:
celikpanel-agent.service`. Later systemd and raw `cgroup.procs` observations
showed only MainPID 856154. The worker journal did not record the failing process
list. These observations do not establish which process or read failure caused
the original rejection. No installed-server update or repair was performed by
the assistant.

The capture previously rejected the first nonmatching process list. The agent's
update-status path itself launches a `systemctl show` helper. A helper present at
capture is a plausible race, not a confirmed diagnosis of the Frankfurt event.

## Source change

Only initial active-coordinator capture, before publishing the quiesce marker,
now permits up to 20 observations with 100 ms between nonmatching observations.
Each observation must retain the original ActiveState, MainPID and process start
time, with the process neither frozen nor missing. Only an exact cgroup match
can succeed. Read failures abort immediately. The observation count and sleep
budget are bounded; this is not a wall-clock deadline for systemd responses.

No process is killed, no command name is exempted, and no update request is
restarted. Persistent extra processes still block the update. The later frozen
identity, mutation-ledger, lock, and recovery proofs retain their strict checks.
The failure detail now includes the expected PID and a bounded observed PID
list (or an unavailable indication), without command arguments or credentials.
This improves capture tolerance and diagnostics; it cannot guarantee that every
later quiesce check succeeds or that Frankfurt's original cause is resolved.

## Validation and operator path

`deploy/test-update-quiesce-capture.sh` exercises the actual capture functions
with isolated process/systemd fixtures: clean and transient-helper success;
persistent helpers, missing or unreadable lists, identity changes, frozen or
missing processes, and unchanged inactive-service behavior. It also checks that
the strict matcher still rejects extra processes. The existing bootstrap-update
contract runs this regression suite.

This source change requires a newly tested and signed release before it is
available to an installed panel. Do not patch a signed installed updater or
bypass the cgroup proof. The owner initiates any later update from the panel UI.
A repeated failure must retain its exact operation and diagnostics for review.
