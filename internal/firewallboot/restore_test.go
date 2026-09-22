package firewallboot

import (
	"context"
	"errors"
	"github.com/alicelik/celikpanel/internal/firewallpolicy"
	"strings"
	"testing"
)

type fakeHost struct {
	saves                            []snapshot
	ports                            [][]int
	loads, sshCalls, applies, checks int
	commandErr                       bool
	tables                           string
	batches                          []string
}

func (h *fakeHost) load() (snapshot, error) {
	i := h.loads
	h.loads++
	if i >= len(h.saves) {
		i = len(h.saves) - 1
	}
	return h.saves[i], nil
}
func (h *fakeHost) ssh(context.Context) ([]int, error) {
	i := h.sshCalls
	h.sshCalls++
	if i >= len(h.ports) {
		i = len(h.ports) - 1
	}
	if h.ports[i] == nil {
		return nil, errors.New("SSH unknown")
	}
	return h.ports[i], nil
}
func (h *fakeHost) command(_ context.Context, _ string, args []string, input string) ([]byte, error) {
	if h.commandErr {
		return nil, errors.New("nft unavailable")
	}
	switch args[0] {
	case "list":
		return []byte(h.tables), nil
	case "--check":
		h.checks++
	case "-f":
		h.applies++
	}
	h.batches = append(h.batches, input)
	return nil, nil
}
func goodHost() *fakeHost {
	return &fakeHost{saves: []snapshot{{exists: true, identity: "one", data: firewallpolicy.Encode([]int{80}, []int{53}, []int{22})}}, ports: [][]int{{2222}}, tables: "table inet other_owner\ntable inet celikpanel_fw\n"}
}
func TestAbsentPolicyDoesNotProbeOrApply(t *testing.T) {
	h := &fakeHost{saves: []snapshot{{}}}
	r, e := restore(context.Background(), h, false)
	if e != nil || r.Present || r.Applied || h.sshCalls != 0 || h.applies != 0 {
		t.Fatalf("%+v %v", r, e)
	}
}
func TestCheckDoesNotApplyAndRestoreKeepsSSHAndOtherTables(t *testing.T) {
	for _, check := range []bool{true, false} {
		h := goodHost()
		r, e := restore(context.Background(), h, check)
		if e != nil || !r.Present || r.Applied == check || h.checks != 1 || h.applies != map[bool]int{true: 0, false: 1}[check] {
			t.Fatalf("%+v %v %+v", r, e, h)
		}
		if !strings.Contains(h.batches[0], "tcp dport { 22, 80, 2222 } accept") || strings.Contains(h.batches[0], "other_owner") || strings.Contains(h.batches[0], "flush") {
			t.Fatal(h.batches)
		}
	}
}
func TestChangedOwnerFileOrSSHRefusesApply(t *testing.T) {
	for _, change := range []string{"identity", "bytes", "absent", "ssh"} {
		t.Run(change, func(t *testing.T) {
			h := goodHost()
			s := h.saves[0]
			switch change {
			case "identity":
				s.identity = "replacement"
			case "bytes":
				s.data = firewallpolicy.Encode([]int{443}, nil, []int{22})
			case "absent":
				s.exists = false
			case "ssh":
				h.ports = append(h.ports, []int{2200})
			}
			h.saves = append(h.saves, s)
			if _, e := restore(context.Background(), h, false); e == nil || h.applies != 0 {
				t.Fatalf("owner change applied: %v", e)
			}
		})
	}
}
func TestUnknownPolicySSHOrKernelNeverApplies(t *testing.T) {
	for _, change := range []string{"policy", "ssh", "kernel", "tables"} {
		t.Run(change, func(t *testing.T) {
			h := goodHost()
			switch change {
			case "policy":
				h.saves[0].data = []byte("flush ruleset\n")
			case "ssh":
				h.ports = [][]int{nil}
			case "kernel":
				h.commandErr = true
			case "tables":
				h.tables = "unexpected output"
			}
			if _, e := restore(context.Background(), h, false); e == nil || h.applies != 0 {
				t.Fatalf("unknown state applied: %v", e)
			}
		})
	}
}
