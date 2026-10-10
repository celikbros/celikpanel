- **Removed by this run, each listed** (`host/removals.txt`, `host/removals-build.txt`): the 17 overlay disks of this
  run's ten labs after each cell's checksums were verified on the staged copy (19.0 GB in all, on the labs' tmpfs
  mounts); the 17 tmpfs mounts; the labs' 17 hard links to the cached base images (the cached files stay, with the
  link counts they had: `host/host-leftovers.txt`); the 17 prepared overlay files no guest ever opened (197 KB each,
  they lay under the mounts) and the run's 17 staging copies of them; the `src` directories of the three dist
  directories this build made (0.83 GB each). One bytecode file this session's first smoke test wrote into the
  working tree's ignored `__pycache__` (`set5_redact.cpython-313.pyc`) was removed as well. Nothing else was removed.
  No directory or file of an earlier run was removed or changed; the alpha.81 dist directory of set3 and the image
  cache were read only. No shared directory was chmod'ed.
- **Left on the WSL host, with sizes** (`host/host-leftovers.txt`): the run directory `/var/tmp/cp-set5-run` (367 MB:
  five run copies, logs, staging copies); the ten lab directories `/var/tmp/cp-release-drill-s5-*` without disks or
  images (66 MB each: the lab's key, plan, evidence and its copy of the fixture origin's files); the builder's
  clone `/var/tmp/cp-upd1-build/20261009t170058z` (1.1 GB); the three dist directories this build made under
  `/var/tmp/cp-pair-accept/dist/` (`8c2250b0...`, `ecccd8d7...`, `49143bd1...`, 67 MB each). Two `__pycache__`
  directories are in run copies `d` and `e` (written when the trial scans imported `set5_redact`). No QEMU process,
  no job, no tmpfs mount and no listening port of this run is left.
- **Left in the repository's working tree**: this folder; the four new harness files and the change to `lab.py`.
  Nothing was committed or pushed; the branch head is where it was (`host/working-tree-status.txt`: `2ab7bf20c`). That
  file also shows `docs/RESILIENCE-CONTRACT*.md` modified: not by this run (the session's first `git status` listed
  only `ROADMAP*` as modified); no build and no cell read the working tree's product or docs files.
- **No installed server.** `host/installed-server-names-search.txt`: the terms set4's README lists (`celikhost`,
  `boston`, `frankfurt`, `2.25.80.4`, `72.62.38.15`, `185.95.0.123`) searched in every file of this folder outside the
  README and its parts; `frankfurt` occurs only in repository script names inside the run copies' file listings, the
  others nowhere. An absence of names, not a capture of traffic.
- **Secrets.** Redaction is at collection time: the pair redactor, set3's shape rules, and set5's token-digest rules
  in front of them, with one more pass over every file of a run before its result is written and one over the staged
  copy with the lab's raw values known (per run `set5-redaction-sweep.json`, `host/token-digest-sweep-at-staging.json`:
  counts only). In the four return cells the digest of the update-transaction token (`transaction_token_sha256`, and
  the same value as a directory name under `.release-db-migrations/` in a sudo journal line) is replaced by
  `[REDACTED-SHA256]` when the file is written. In the three return cells with a VM reset one host-side record
  (`recovery-fault-collection-*.json`, written by the lab's reset tool, not through the driver's redactor) held it;
  its plain-text places were redacted on the staged copy, 3 in each, before any checksum of this folder was written; the
  same value also lay twice inside the record's `events_base64` text, which that pass and `secret-scan.txt` did not look
  into, and was replaced at intake ("Corrections after intake"); the lab's raw copy stays on the WSL host. `secret-scan.txt` (its last line is
  the result) lists the classes: PEM private keys; the body lines of every lab key file; licence keys; WireGuard
  keys and configuration words; secret-named JSON fields; hash-shaped credential values; password assignments; token
  digests by shape and by value (3 values are read from the ten labs' raw records on the host and searched for in
  every retained file: 0 occurrences; the Arch return cell's value is in no raw record of the host, only in files the
  driver wrote redacted, so it is covered by shape only); HTTP credential headers; and, as information, local
  user-profile paths (6 occurrences, all of them the search pattern itself in `tools/secretscan.py` and
  `tools/stagecommon.sh`). The lab nonce (the
  random identity of a destroyed lab guest, passed to every guest helper) is in the records as in every earlier
  set and is not treated as a secret.
