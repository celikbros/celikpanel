These v1 bytes were emitted by the actual Alpha81 Agent producer at source
`45dfc265bfd7997e9a0e0d39b0ebf60a57f58c00`, compiled with Go 1.26.5 on isolated
ext4. Production source was unmodified. The retained `export_test.go.txt` driver
was added only to that archived source's test package; `producer.json` binds its
bytes, the original producer file and all emitted fixtures.

BIND ownership and zone-add use the original `publishedBINDOwnershipFixture`,
including the production delta renderer, `bindStateForPublishedReceipt`, state
writer and acquisition writer. Edit and delete use the same original production
delta/state producers. Standalone BIND/PowerDNS and adopted PowerDNS use original
valid historical state fixtures and the actual canonical producer.

This is historical producer/reader byte compatibility and role separation, not
native daemon operation, installed migration or independent DNS recovery proof.
The initial export-only driver had a Go integer type mismatch and did not run;
its corrected driver emitted the retained bytes. No production source was fixed
or altered in the historical archive.

The four `v2-*` files are current-format cross-language goldens, projected from
those retained Alpha81 semantic inputs. They are not historical producer output.
The Go canonical writers must emit these exact bytes; the Python native-lab
observer independently verifies the role, acquisition digest and canonical layout.
