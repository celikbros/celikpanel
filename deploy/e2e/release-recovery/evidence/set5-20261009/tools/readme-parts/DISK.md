41.93 after 2.5 GB of this run's own build intermediates were removed (no change); 41.92 before the first cell
(17:13Z). At the gate, before the ten cells in their order: 41.92, 41.94, 41.91, 41.90, 41.78, 41.59, 41.58, 41.56,
41.46, 41.07 (41.09 at its guest start). 41.00 after the last cell was staged (20:02Z); lowest of the 368 readings
taken every 30 s: 41.02 GiB (20:02:49Z, the last one); 40.96 at 20:15Z, after this folder had been written once (`host/c-drive.txt`).
Never under 40 GiB before a guest start or at any reading.

Across the ten cells `C:` fell by 0.92 GiB. What this run wrote per cell is the lab directory without disks and
images (66 MB on the WSL disk) and the staged evidence (24 MB on `C:` for the whole folder). The reading did not
fall evenly: about 0.2 GiB at 18:13Z and about 0.46 GiB between 19:14Z and 19:26Z, with flat stretches between
(`host/c-drive-watch.txt`). Another session was working in the same repository during the run
(`host/working-tree-status.txt`), and this run's own stopped check H47 falls in the second window. What part of the
0.92 GiB is this run's was not established.
