//go:build linux

package dnsenginerecovery

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// NativeUnitObservation is an instantaneous systemd property read. It does not
// establish DNS answers, configuration ownership, or recovery authority.
type NativeUnitObservation struct {
	Name          string
	LoadState     string
	ActiveState   string
	UnitFileState string
}

type NativeUnitRunner func(context.Context, string) ([]byte, error)

func allowedDNSUnit(name string) bool {
	switch name {
	case "named.service", "bind9.service", "pdns.service":
		return true
	default:
		return false
	}
}

// SystemdUnitRunner queries only fixed, read-only properties of a validated
// native DNS unit. The executable path and arguments cannot come from a journal.
func SystemdUnitRunner(ctx context.Context, name string) ([]byte, error) {
	if ctx == nil {
		return nil, errors.New("native DNS unit query has no context")
	}
	if !allowedDNSUnit(name) {
		return nil, errors.New("unsupported native DNS unit")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/systemctl", "show", name,
		"--no-pager", "-p", "Id", "-p", "Names", "-p", "LoadState",
		"-p", "ActiveState", "-p", "UnitFileState")
	output := &boundedUnitOutput{}
	cmd.Stdout = output
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("read native DNS unit %s: %w", name, err)
	}
	return output.Bytes(), nil
}

type boundedUnitOutput struct{ bytes.Buffer }

func (output *boundedUnitOutput) Write(data []byte) (int, error) {
	if output.Len()+len(data) > 4096 {
		return 0, errors.New("native DNS unit output exceeds its bound")
	}
	return output.Buffer.Write(data)
}

func safeUnitProperty(value string, allowEmpty bool) bool {
	if len(value) > 128 || (!allowEmpty && value == "") {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

func parseNativeUnitObservation(name string, output []byte) (NativeUnitObservation, error) {
	if !allowedDNSUnit(name) || len(output) > 4096 {
		return NativeUnitObservation{}, errors.New("untrusted native DNS unit observation")
	}
	properties := make(map[string]string, 5)
	for _, line := range strings.Split(strings.TrimSuffix(string(output), "\n"), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok || (key != "Id" && key != "Names" && key != "LoadState" && key != "ActiveState" && key != "UnitFileState") {
			return NativeUnitObservation{}, errors.New("unexpected native DNS unit property")
		}
		if _, seen := properties[key]; seen {
			return NativeUnitObservation{}, errors.New("duplicate native DNS unit property")
		}
		properties[key] = value
	}
	if len(properties) != 5 || !allowedDNSUnit(properties["Id"]) ||
		!safeUnitProperty(properties["LoadState"], false) ||
		!safeUnitProperty(properties["ActiveState"], false) ||
		!safeUnitProperty(properties["UnitFileState"], true) {
		return NativeUnitObservation{}, errors.New("incomplete native DNS unit properties")
	}
	found := false
	for _, alias := range strings.Fields(properties["Names"]) {
		if alias == name {
			found = true
		}
	}
	if !found {
		return NativeUnitObservation{}, errors.New("native DNS unit identity differs from requested alias")
	}
	return NativeUnitObservation{
		Name: name, LoadState: properties["LoadState"],
		ActiveState: properties["ActiveState"], UnitFileState: properties["UnitFileState"],
	}, nil
}

// ProbeNativeUnits refuses partial success. Only names extracted from an
// already validated journal may be passed; this function enforces the fixed
// allowlist again before making any systemd call.
func ProbeNativeUnits(ctx context.Context, names []string, runner NativeUnitRunner) ([]NativeUnitObservation, error) {
	if ctx == nil || runner == nil || len(names) == 0 || len(names) > 3 {
		return nil, errors.New("invalid native DNS unit observation request")
	}
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if !allowedDNSUnit(name) || seen[name] {
			return nil, errors.New("unsupported or duplicate native DNS unit")
		}
		seen[name] = true
	}
	observations := make([]NativeUnitObservation, 0, len(names))
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		output, err := runner(ctx, name)
		if err != nil {
			return nil, err
		}
		observed, err := parseNativeUnitObservation(name, output)
		if err != nil {
			return nil, fmt.Errorf("verify native DNS unit %s: %w", name, err)
		}
		observations = append(observations, observed)
	}
	return observations, nil
}
