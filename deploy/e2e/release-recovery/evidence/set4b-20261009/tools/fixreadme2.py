import io
p = 'README.2.md'
s = io.open(p, encoding='utf-8').read()
a = "moments quoted from that cell are differences on the guest's own CLOCK_MONOTONIC within one Stop.\n"
assert s.count(a) == 1
b = a + """
The same record shows three earlier spells of modern standby for an idle screen (13:57:28Z to 14:07:45Z, 14:17:15Z
to 14:21:38Z, 14:32:46Z to 14:37:54Z). The WSL host went on executing through them: the steps of the cells and the
build that ran in those spells carry consecutive times of their own (`diagnostic/set4b-diag-ubuntu/run-b/result.json`:
D10 from 14:17:04Z to 14:19:26Z; `build/build-job.out.txt`). Whether the processor was slowed in those spells was not
measured; the five Stops of D10 gave the same order of events and the same second between the stop and the master's
end as the Stops of the re-measurement.
"""
io.open(p, 'w', encoding='utf-8', newline=chr(10)).write(s.replace(a, b))
print('ok')
