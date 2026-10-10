import io
def patch(p, pairs):
    s = io.open(p, encoding='utf-8').read()
    for a, b in pairs:
        assert s.count(a) == 1, (p, a[:60], s.count(a))
        s = s.replace(a, b)
    io.open(p, 'w', encoding='utf-8', newline=chr(10)).write(s)
patch('README.5.md', [(
"""  29 files of the working tree that differ from HEAD at the end of the run, the 18 under `cmd/`, `internal/` and
  `web/src` (and `web/tests/set3-corrections.test.mjs`, `web/tools/browser-inspect/run.mjs`) are byte-identical to
  the candidate commit.""",
"""  29 files of the working tree that differ from HEAD at the end of the run, 18 are byte-identical to the candidate
  commit: all 16 under `cmd/`, `internal/` and `web/src`, and `web/tests/set3-corrections.test.mjs` and
  `web/tools/browser-inspect/run.mjs`.""")])
patch('README.1.md', [(
"""No installed server was touched or contacted: the drivers reach only disposable QEMU guests on the local `archlinux`
WSL host through loopback ports of that host, and the terms an installed server would be named by do not occur in
this folder outside the tools' own refusal texts.""",
"""No installed server was touched or contacted. What can be shown of that: the drivers open only loopback ports of the
local `archlinux` WSL host (each lab's SSH forward and the tunnel to its guest's Panel; `host/fixture-plan.json` and
`host/wrapper.out.txt` of each cell), and every guest is a new disposable QEMU guest of that host.""")])
patch('README.4.md', [(
"""The Agent's next `systemctl` (its reading of
`postfix.service`, which the reading of `postfix@-.service` follows) started at""",
"""The Agent's next `systemctl` (by the order of the
source, its reading of `postfix.service`, which the reading of `postfix@-.service` follows) started at""")])
patch('readme_fill.py', [("Debian 13.x, kernel", "Debian GNU/Linux 13 (trixie), kernel")])
print('ok')
