# CelikPanel v0.1.0-alpha.77

[Turkish](RELEASE-NOTES-v0.1.0-alpha.77.tr.md)

The initial update coordinator check now waits briefly for temporary helper
processes to exit, instead of failing on the first nonmatching process list.
Every observation must retain the original service state, PID and process start
time. Persistent helpers and unreadable or changed identities still stop the
update before quiesce publication. Later frozen-process and mutation checks
remain strict.

Capture failures now retain the expected and observed process IDs, without
command arguments. This improves diagnosis of the Frankfurt alpha.76 update
failure; the original journal did not identify its exact cause.

Validation: thirteen isolated capture/matcher scenarios and the existing
bootstrap-update contract pass. See [capture behavior and limits](UPDATE-QUIESCE-CAPTURE.md).

Installed panels are updated by their owner through the panel update interface.
This release does not perform an installed-server update or change DNS/PTR data.
