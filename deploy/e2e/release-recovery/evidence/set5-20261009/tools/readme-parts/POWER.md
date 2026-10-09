The request was held from 16:58:50Z to 20:02:53Z.

What the host's System log says (`host/sleep-events.txt`, Kernel-Power, read at 20:03Z): the last sleep entry (id 42)
and resume (id 107) are at 15:06:57Z and 15:07:01Z, more than two hours before the first cell. After that the log
has "entering modern standby" (506) at 16:38:46Z, "leaving" (507) at 16:56:45Z, "entering" at 17:10:44Z, "leaving" at
19:15:44Z and "entering" at 19:31:37Z. By that log the first seven cells ran wholly inside a modern-standby interval,
`upd1-debian13-owner-continuation` across its end (19:15:44Z, during its owner step), `upd1-ubuntu-owner-continuation`
across the next entry (19:31:37Z, during its setup), and `upd1-debian13-mgmt-off-reboot` inside the second interval
(`host-clock-gaps.txt`, per cell).

What the run's own clocks say: the host kept executing through all of it. The Windows watcher wrote its line every
30 s from 16:58:49Z to 20:02:49Z (368 lines, no gap over 31 s), the WSL watcher from 17:51:51Z to 20:02:26Z (262
lines, no gap over 31 s), no step of any cell starts more than 1 s after the step before it ended, and no cell has a
gap between two of its own host samples (one every 5 s) larger than 10.5 s: 10.1 to 10.5 s at the VM reset of the
three reset cells, 9.3 s once in `upd1-ubuntu-owner-continuation` while the Panel's port was held, 5.0 to 5.8 s
elsewhere (`sampler-gaps.txt`). So for each of the ten cells: no measured step ran after a wake from a pause, because no pause
of the host is seen inside or between the cells. As in set3, "modern standby" in the log did not stop execution on
this host. What that state changes for a running workload other than pausing it was not measured.
