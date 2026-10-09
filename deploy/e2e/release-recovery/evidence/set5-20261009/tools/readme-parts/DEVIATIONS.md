1. **The Postfix Stop ran in the two good-update cells, not in fresh-install cells.** A fresh install of the candidate
   needs a second build (the candidate source labelled as the installed version: five more dist directories, about
   6 GB written to the WSL disk while it builds) and two more guests; the host's free-disk rule left 1.9 GiB. The
   brief allows the update cell. Consequences: the candidate under the Stop was installed by the update, not by its
   own installer, and Postfix had been configured by alpha.81's setup; set4b's fresh-install record of the same
   section on `1f182a483` is not repeated on `67b62cc0f`. The section runs after `verdicts`, so the SMTP outage of the
   Stop is not in the cell's sample series; its own journal reading is in the step.
2. **The guests' disks were RAM-backed, and the base images were hard links** ("Disk" above). set3 and set4 kept
   overlays and image copies on the WSL disk. No duration of this run is comparable with theirs as a duration.
3. **Run copies leave out the retained evidence of earlier runs** (except `evidence/upd1-20261001`, which four offline
   tests read). set3 and set4 used the whole `git archive`. Each copy's `runcopy-against-commit.txt` shows that
   nothing else is missing or different.
4. **More was removed than the overlay disks**, all of it created by this run and each removal listed
   (`host/removals.txt`, `host/removals-build.txt`): the `src` directories of the three dist directories this build
   made (after the proof; 0.83 GB each), each lab's hard links to the cached base images, each lab's tmpfs
   mounts, the prepared overlay files no guest ever opened, and one bytecode file of this session. Nothing of an earlier run was removed, and nothing was removed to get above the disk floor: the removals
   on the WSL disk do not change the `C:` reading on this host.
5. **Four certificate-authority names were pinned, not two**: the two Let's Encrypt names of set3 and set4 and the two
   other directories the product's source names. `celikpanel.net` is pinned by the `origin` step as before, which is
   before the baseline is installed but after the first step of the cell.
6. **Added to every cell**: the first step and the last measuring step (name pinning and its reading at the end); in the eight cells
   of copy `e` the last measuring step also reads the kernel and package versions. These steps are in `result.json` and count
   in `overall`; the cell table judges set3's steps separately.
7. **set4's item 12** (the update check after an automatic return) was not run again; the brief asks for item 9 only.
8. **The two good cells of Debian and Ubuntu ran from copy `d`, the other eight from copy `e`** (the difference is the
   package reading and the name of one report field, H46).
9. **Evidence of the cells is under `update-alpha81/`** (set4's name), not `part2-alpha81/` (set3's).
10. **One thing is not as the brief words it**: "40 GB" was applied as 40 GiB (the stricter reading, the floor of
    set3 and set4).
