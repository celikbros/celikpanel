//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"reflect"
	"time"

	"github.com/alicelik/celikpanel/internal/dnsunitidentity"
	"github.com/alicelik/celikpanel/internal/hostplatform"
)

type pdnsTopology struct {
	units     []NativeUnitObservation
	processes [3]dnsunitidentity.Processes
}

var pdnsTopologyUnits = []string{"named.service", "bind9.service", "pdns.service"}

// SystemdPDNSRuntimeRunner reads only fixed native unit process properties.
func SystemdPDNSRuntimeRunner(ctx context.Context, name string) ([]byte, error) {
	if ctx == nil || (name != "named.service" && name != "bind9.service" && name != "pdns.service") {
		return nil, errors.New("unsupported PowerDNS runtime query")
	}
	queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(queryCtx, "/usr/bin/systemctl", "show", name,
		"--property=MainPID,ControlPID,SubState,NeedDaemonReload", "--no-pager")
	output := &boundedUnitOutput{}
	cmd.Stdout = output
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("read DNS runtime %s: %w", name, err)
	}
	return output.Bytes(), nil
}

func stoppedBINDUnit(unit NativeUnitObservation) bool {
	if unit.ActiveState != "inactive" {
		return false
	}
	switch {
	case unit.LoadState == "not-found" && unit.UnitFileState == "":
		return true
	case unit.LoadState == "loaded" && unit.UnitFileState == "disabled":
		return true
	case unit.LoadState == "masked" || unit.UnitFileState == "masked" || unit.UnitFileState == "masked-runtime":
		return true
	default:
		return false
	}
}

func probePDNSTopologyOnce(ctx context.Context, units NativeUnitRunner, runtime BINDRuntimeRunner) (pdnsTopology, error) {
	states, err := ProbeNativeUnits(ctx, pdnsTopologyUnits, units)
	if err != nil {
		return pdnsTopology{}, err
	}
	if !stoppedBINDUnit(states[0]) || !stoppedBINDUnit(states[1]) {
		return pdnsTopology{}, errors.New("BIND units are not exactly stopped while PowerDNS is selected")
	}
	if states[2].LoadState != "loaded" || states[2].ActiveState != "active" ||
		states[2].UnitFileState != "enabled" {
		return pdnsTopology{}, errors.New("pdns.service is not exactly loaded, active and enabled")
	}
	var processes [3]dnsunitidentity.Processes
	for index, name := range pdnsTopologyUnits {
		raw, err := runtime(ctx, name)
		if err != nil {
			return pdnsTopology{}, err
		}
		processes[index], err = parseUnitRuntime(raw)
		if err != nil {
			return pdnsTopology{}, fmt.Errorf("verify %s process state: %w", name, err)
		}
		if index < 2 {
			if processes[index].MainPID != 0 || processes[index].ControlPID != 0 || processes[index].SubState != "dead" {
				return pdnsTopology{}, errors.New("stopped BIND unit has a live process")
			}
		} else if processes[index].MainPID == 0 || processes[index].ControlPID != 0 ||
			processes[index].SubState != "running" {
			return pdnsTopology{}, errors.New("pdns.service lacks exact running process state")
		}
	}
	return pdnsTopology{units: states, processes: processes}, nil
}

// ProbePDNSRuntime observes a stable native topology and returns the selected
// MainPID. It does not certify the vendor unit, executable bytes or database.
func ProbePDNSRuntime(ctx context.Context, profile hostplatform.Profile, units NativeUnitRunner, runtime BINDRuntimeRunner) (uint64, error) {
	if ctx == nil || units == nil || runtime == nil ||
		profile.PackageManager != hostplatform.PackageManagerAPT ||
		profile.DistroFamily != hostplatform.DistroFamilyDebian ||
		profile.ServiceManager != hostplatform.ServiceManagerSystemd {
		return 0, errors.New("PowerDNS observation requires a verified APT/systemd host")
	}
	before, err := probePDNSTopologyOnce(ctx, units, runtime)
	if err != nil {
		return 0, err
	}
	after, err := probePDNSTopologyOnce(ctx, units, runtime)
	if err != nil {
		return 0, err
	}
	if !reflect.DeepEqual(before, after) {
		return 0, errors.New("PowerDNS native topology changed during observation")
	}
	return before.processes[2].MainPID, nil
}
