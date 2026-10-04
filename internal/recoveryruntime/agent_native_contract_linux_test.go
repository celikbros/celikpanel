//go:build linux

package recoveryruntime

import (
	"github.com/alicelik/celikpanel/internal/agentnativecontract"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompatibleMailAgentPinsExactMetadataAndBytes(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root descriptor fixture")
	}
	for _, kind := range []string{"valid", "missing", "edited-agent", "edited-contract", "agent-link", "contract-link", "agent-hardlink", "contract-mode", "foreign-owner", "late-agent-replaced", "late-contract-replaced", "late-parent-replaced", "late-proof-edited", "closed"} {
		t.Run(kind, func(t *testing.T) {
			root, e := os.MkdirTemp("/run", "celikpanel-agent-contract-")
			if e != nil {
				t.Fatal(e)
			}
			defer os.RemoveAll(root)
			dir := filepath.Join(root, "bin")
			if e = os.Mkdir(dir, 0755); e != nil {
				t.Fatal(e)
			}
			agent := []byte("non-executable fixture bytes are never run")
			c, _ := agentnativecontract.New(agent, strings.Repeat("a", 40))
			raw, _ := agentnativecontract.Encode(c)
			a, m := filepath.Join(dir, "agent"), filepath.Join(dir, agentnativecontract.FileName)
			put := func(p string, raw []byte, mode os.FileMode) {
				t.Helper()
				if e := os.WriteFile(p, raw, mode); e != nil {
					t.Fatal(e)
				}
				if e := os.Chmod(p, mode); e != nil {
					t.Fatal(e)
				}
			}
			put(a, agent, 0755)
			put(m, raw, 0644)
			switch kind {
			case "missing":
				os.Remove(m)
			case "edited-agent":
				put(a, []byte("other Agent"), 0755)
			case "edited-contract":
				put(m, []byte("{}\n"), 0644)
			case "agent-link":
				os.Rename(a, a+".saved")
				os.Symlink(a+".saved", a)
			case "contract-link":
				os.Rename(m, m+".saved")
				os.Symlink(m+".saved", m)
			case "agent-hardlink":
				os.Link(a, a+".other")
			case "contract-mode":
				os.Chmod(m, 0664)
			case "foreign-owner":
				os.Chown(m, 65534, 0)
			}
			proof, e := InspectCompatibleMailAgent(dir)
			bad := map[string]bool{"missing": true, "edited-agent": true, "edited-contract": true, "agent-link": true, "contract-link": true, "agent-hardlink": true, "contract-mode": true, "foreign-owner": true}[kind]
			if bad {
				if e == nil {
					proof.Close()
					t.Fatal("unverified Agent accepted")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			defer proof.Close()
			switch kind {
			case "late-agent-replaced":
				os.Rename(a, a+".saved")
				put(a, agent, 0755)
			case "late-contract-replaced":
				os.Rename(m, m+".saved")
				put(m, raw, 0644)
			case "late-parent-replaced":
				os.Rename(dir, dir+".saved")
				os.Mkdir(dir, 0755)
				put(a, agent, 0755)
				put(m, raw, 0644)
			case "late-proof-edited":
				proof.Contract.SourceCommit = strings.Repeat("b", 40)
			case "closed":
				proof.Close()
			}
			e = proof.Revalidate()
			if kind == "valid" {
				if e != nil {
					t.Fatal(e)
				}
			} else if e == nil {
				t.Fatal("changed proof accepted")
			}
		})
	}
}
