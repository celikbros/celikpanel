- Anything on a screen: no browser was used. The update card, the recovery screen and the catalogue sentences quoted
  in the step files are rendered or looked up by the driver from the installed build's own rules and catalogues.
- The candidate installed fresh (its own installer, its own first setup): every cell installs alpha.81 and updates.
- The Postfix Stop on a fresh install of `67b62cc0f`, on Arch (no Postfix there), more than once per platform, and
  with the kernel trace of set4b. What the Agent's `postconf` reading returned.
- The other paths `67b62cc0f` changes: the snapshot before a mail TLS change, the restore, the read-back, with a
  `main.cf` that `postconf` warns about. No mail certificate is selected in these labs; no certificate authority is
  asked. set4c holds the readings of the real `postconf`; nothing here adds to them.
- Item 9 on Arch; a site with a certificate; an owner-edited snippet.
- set4's other items (1 to 8, 11, 12) and set3's Part 1 under this commit: not run again.
- A guest disk that is slow, full, or lost at a VM reset: the guests' disks were in RAM.
- The live ledger between the publication at 43 and the return in the start-check cell (as in set3: it follows from
  the updater's order and the recorded failure code, not from a reading).
- The guests' traffic: not captured. That no certificate authority and no licence service was contacted rests on the
  pinned names, on the Panel's own answer at the licence step, and on the absence of certificate routes among the
  recorded requests, not on a capture.
- The signed release path: the archives are test-licence builds signed by a per-lab fixture key and served by a
  fixture origin on the guest's loopback; no production key, no real origin.
- Whether the host slept inside a cell is answered from the host's event log and the watcher's readings ("Host
  power"), not from a hardware power record.
- More than one run per cell: each cell ran once.
