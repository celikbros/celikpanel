//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeServiceStopProofInspector struct {
	states      map[string][]systemdUnitStopState
	stateReads  map[string]int
	cgroupReads map[string]int
	cgroupError error
}

func (fake *fakeServiceStopProofInspector) readUnitState(
	_ context.Context,
	unit string,
) (systemdUnitStopState, error) {
	if fake.stateReads == nil {
		fake.stateReads = make(map[string]int)
	}
	index := fake.stateReads[unit]
	fake.stateReads[unit]++
	states := fake.states[unit]
	if index >= len(states) {
		return systemdUnitStopState{}, errors.New("unexpected state read")
	}
	return states[index], nil
}

func (fake *fakeServiceStopProofInspector) proveControlGroupEmpty(controlGroup string) error {
	if fake.cgroupReads == nil {
		fake.cgroupReads = make(map[string]int)
	}
	fake.cgroupReads[controlGroup]++
	return fake.cgroupError
}

func TestVerifyCelikPanelServicesStoppedWithInspector(t *testing.T) {
	panel := systemdUnitStopState{
		loadState:    "loaded",
		activeState:  "inactive",
		mainPID:      0,
		controlGroup: "/system.slice/celikpanel-panel.service",
	}
	agent := systemdUnitStopState{
		loadState:   "loaded",
		activeState: "failed",
		mainPID:     0,
	}
	fake := &fakeServiceStopProofInspector{states: map[string][]systemdUnitStopState{
		"celikpanel-panel.service": {panel, panel},
		"celikpanel-agent.service": {agent, agent},
	}}
	if err := verifyCelikPanelServicesStoppedWithInspector(context.Background(), fake); err != nil {
		t.Fatalf("stopped services rejected: %v", err)
	}
	if got := fake.cgroupReads[panel.controlGroup]; got != 2 {
		t.Fatalf("cgroup proof count=%d want 2", got)
	}
}

func TestVerifyCelikPanelServicesStoppedRejectsUnsafeOrUnstableState(t *testing.T) {
	validAgent := systemdUnitStopState{loadState: "loaded", activeState: "inactive"}
	tests := []struct {
		name      string
		states    []systemdUnitStopState
		cgroupErr error
		want      string
	}{
		{
			name:   "unit is not loaded",
			states: []systemdUnitStopState{{loadState: "not-found", activeState: "inactive"}},
			want:   "must be loaded",
		},
		{
			name:   "active unit",
			states: []systemdUnitStopState{{loadState: "loaded", activeState: "active"}},
			want:   "must be stopped",
		},
		{
			name:   "nonzero main pid",
			states: []systemdUnitStopState{{loadState: "loaded", activeState: "inactive", mainPID: 42}},
			want:   "MainPID=0",
		},
		{
			name: "path traversal",
			states: []systemdUnitStopState{{
				loadState:    "loaded",
				activeState:  "inactive",
				controlGroup: "/system.slice/../celikpanel-panel.service",
			}},
			want: "unsafe ControlGroup",
		},
		{
			name: "wrong cgroup",
			states: []systemdUnitStopState{{
				loadState:    "loaded",
				activeState:  "inactive",
				controlGroup: "/system.slice/other.service",
			}},
			want: "exact unit name",
		},
		{
			name: "state changes during proof",
			states: []systemdUnitStopState{
				{loadState: "loaded", activeState: "inactive"},
				{loadState: "loaded", activeState: "failed"},
			},
			want: "changed while",
		},
		{
			name: "nonempty cgroup",
			states: []systemdUnitStopState{{
				loadState:    "loaded",
				activeState:  "inactive",
				controlGroup: "/system.slice/celikpanel-panel.service",
			}},
			cgroupErr: errors.New("process remains"),
			want:      "process remains",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeServiceStopProofInspector{
				states: map[string][]systemdUnitStopState{
					"celikpanel-panel.service": test.states,
					"celikpanel-agent.service": {validAgent, validAgent},
				},
				cgroupError: test.cgroupErr,
			}
			err := verifyCelikPanelServicesStoppedWithInspector(context.Background(), fake)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want substring %q", err, test.want)
			}
		})
	}
}

func TestParseSystemdUnitStopState(t *testing.T) {
	state, err := parseSystemdUnitStopState([]byte(
		"MainPID=0\nControlGroup=/system.slice/celikpanel-panel.service\nActiveState=inactive\nLoadState=loaded\n",
	))
	if err != nil {
		t.Fatalf("parse valid properties: %v", err)
	}
	if state.loadState != "loaded" || state.activeState != "inactive" || state.mainPID != 0 ||
		state.controlGroup != "/system.slice/celikpanel-panel.service" {
		t.Fatalf("unexpected state: %#v", state)
	}
	for _, malformed := range []string{
		"LoadState=loaded\nActiveState=inactive\nMainPID=0\n",
		"LoadState=loaded\nActiveState=inactive\nMainPID=00\nControlGroup=\n",
		"LoadState=loaded\nActiveState=inactive\nMainPID=0\nMainPID=0\nControlGroup=\n",
		"LoadState=loaded\nActiveState=inactive\nMainPID=0\nControlGroup=\nUnknown=value\n",
	} {
		if _, err := parseSystemdUnitStopState([]byte(malformed)); err == nil {
			t.Fatalf("malformed systemd output accepted: %q", malformed)
		}
	}
}

func TestProveControlGroupEmptyRecursively(t *testing.T) {
	root := t.TempDir()
	groupPath := filepath.Join(root, "system.slice", "celikpanel-panel.service")
	childPath := filepath.Join(groupPath, "child")
	if err := os.MkdirAll(childPath, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{groupPath, childPath} {
		if err := os.WriteFile(filepath.Join(directory, "cgroup.procs"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	inspector := linuxServiceStopProofInspector{cgroupRoot: root}
	controlGroup := "/system.slice/celikpanel-panel.service"
	if err := inspector.proveControlGroupEmpty(controlGroup); err != nil {
		t.Fatalf("empty recursive cgroup rejected: %v", err)
	}
	if err := os.WriteFile(filepath.Join(childPath, "cgroup.procs"), []byte("123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := inspector.proveControlGroupEmpty(controlGroup); err == nil ||
		!strings.Contains(err.Error(), "still contains process IDs") {
		t.Fatalf("nonempty child cgroup error=%v", err)
	}
}

func TestProveControlGroupEmptyRejectsSymlinksAndMissingProof(t *testing.T) {
	t.Run("symlink child", func(t *testing.T) {
		root := t.TempDir()
		groupPath := filepath.Join(root, "system.slice", "celikpanel-panel.service")
		if err := os.MkdirAll(groupPath, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(groupPath, "cgroup.procs"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(t.TempDir(), filepath.Join(groupPath, "child")); err != nil {
			t.Fatal(err)
		}
		inspector := linuxServiceStopProofInspector{cgroupRoot: root}
		err := inspector.proveControlGroupEmpty("/system.slice/celikpanel-panel.service")
		if err == nil || !strings.Contains(err.Error(), "symbolic link") {
			t.Fatalf("symlink error=%v", err)
		}
	})

	t.Run("missing cgroup procs", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(
			filepath.Join(root, "system.slice", "celikpanel-panel.service"),
			0o700,
		); err != nil {
			t.Fatal(err)
		}
		inspector := linuxServiceStopProofInspector{cgroupRoot: root}
		err := inspector.proveControlGroupEmpty("/system.slice/celikpanel-panel.service")
		if err == nil || !strings.Contains(err.Error(), "cgroup.procs") {
			t.Fatalf("missing proof error=%v", err)
		}
	})

	t.Run("symlink root", func(t *testing.T) {
		parent := t.TempDir()
		realRoot := filepath.Join(parent, "real")
		if err := os.Mkdir(realRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		linkedRoot := filepath.Join(parent, "linked")
		if err := os.Symlink(realRoot, linkedRoot); err != nil {
			t.Fatal(err)
		}
		inspector := linuxServiceStopProofInspector{cgroupRoot: linkedRoot}
		if err := inspector.proveControlGroupEmpty("/system.slice/celikpanel-panel.service"); err == nil {
			t.Fatal("symbolic-link cgroup root accepted")
		}
	})
}
