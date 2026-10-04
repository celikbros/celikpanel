# Historical DNS switch journal bytes

`alpha81-*.json` were emitted by the unmodified journal encoder and validators
at commit `45dfc265bfd7997e9a0e0d39b0ebf60a57f58c00`. The retained `producer.go.txt`
uses that commit's BIND, PowerDNS switch and PowerDNS adoption test inputs,
substituting the standard installed paths before encoding. It does not install a
service or exercise native recovery. `producer.json` binds the export driver,
source files and exact output hashes; source hashes were compared with Git's
recorded commit. The current shared reader must round-trip each file unchanged.

The first export attempt selected a deliberately invalid adoption case at the
end of the historical test and was refused. The corrected exporter stops at the
first valid journal; it did not modify historical production code. These are
historical writer compatibility fixtures, not native workload evidence.