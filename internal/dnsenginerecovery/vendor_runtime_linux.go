//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"reflect"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/bindroot"
	"github.com/alicelik/celikpanel/internal/dnsunitidentity"
	"github.com/alicelik/celikpanel/internal/hostplatform"
)

type BINDRuntimeRunner func(context.Context, string) ([]byte, error)

// SystemdBINDRuntimeRunner observes fixed native service properties only.
func SystemdBINDRuntimeRunner(ctx context.Context, name string) ([]byte, error) {
	if ctx == nil || (name != "named.service" && name != "bind9.service") {
		return nil, errors.New("unsupported BIND runtime query")
	}
	queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(queryCtx, "/usr/bin/systemctl", "show", name,
		"--property=MainPID,ControlPID,SubState,NeedDaemonReload", "--no-pager")
	output := &boundedUnitOutput{}
	cmd.Stdout = output
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("read BIND runtime %s: %w", name, err)
	}
	return output.Bytes(), nil
}

func parseUnitRuntime(raw []byte) (dnsunitidentity.Processes, error) {
	if len(raw) > 4096 {
		return dnsunitidentity.Processes{}, errors.New("DNS runtime output exceeds its bound")
	}
	lines := strings.Split(string(raw), "\n")
	processLines := make([]string, 0, 3)
	reloadSeen := false
	for _, line := range lines {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "NeedDaemonReload=") {
			if reloadSeen || line != "NeedDaemonReload=no" {
				return dnsunitidentity.Processes{}, errors.New("DNS unit has an unknown or pending daemon reload")
			}
			reloadSeen = true
			continue
		}
		processLines = append(processLines, line)
	}
	if !reloadSeen {
		return dnsunitidentity.Processes{}, errors.New("DNS unit daemon reload state is absent")
	}
	return dnsunitidentity.ParseProcesses(strings.Join(processLines, "\n"))
}

func parseBINDRuntime(raw []byte) (dnsunitidentity.Processes, error) {
	processes, err := parseUnitRuntime(raw)
	if err != nil {
		return dnsunitidentity.Processes{}, err
	}
	if processes.MainPID == 0 || processes.ControlPID != 0 || processes.SubState != "running" {
		return dnsunitidentity.Processes{}, errors.New("BIND unit lacks exact running systemd process state")
	}
	return processes, nil
}

func probeBINDRuntimeOnce(ctx context.Context, profile hostplatform.Profile, runner BINDRuntimeRunner) (dnsunitidentity.Processes, error) {
	raw, err := runner(ctx, "named.service")
	if err != nil {
		return dnsunitidentity.Processes{}, err
	}
	named, err := parseBINDRuntime(raw)
	if err != nil {
		return dnsunitidentity.Processes{}, fmt.Errorf("verify named.service runtime: %w", err)
	}
	if profile.PackageManager == hostplatform.PackageManagerAPT {
		rawAlias, err := runner(ctx, "bind9.service")
		if err != nil {
			return dnsunitidentity.Processes{}, err
		}
		alias, err := parseBINDRuntime(rawAlias)
		if err != nil || alias != named {
			return dnsunitidentity.Processes{}, errors.New("BIND alias runtime differs from named.service")
		}
	}
	return named, nil
}

// ProbeBINDVendorRuntime compares two systemd process/reload observations.
// A running PID is not proof of its executable bytes, loaded config or answers.
func ProbeBINDVendorRuntime(ctx context.Context, profile hostplatform.Profile, runner BINDRuntimeRunner) (dnsunitidentity.Processes, error) {
	if ctx == nil || runner == nil {
		return dnsunitidentity.Processes{}, errors.New("invalid BIND runtime observation")
	}
	if _, err := bindroot.CertifiedVendorContract(profile); err != nil {
		return dnsunitidentity.Processes{}, err
	}
	first, err := probeBINDRuntimeOnce(ctx, profile, runner)
	if err != nil {
		return dnsunitidentity.Processes{}, err
	}
	second, err := probeBINDRuntimeOnce(ctx, profile, runner)
	if err != nil {
		return dnsunitidentity.Processes{}, err
	}
	if !reflect.DeepEqual(first, second) {
		return dnsunitidentity.Processes{}, errors.New("BIND runtime changed during observation")
	}
	return second, nil
}

// ProbeBINDAdoptionRuntime admits the Debian owner server whose bind9.service
// alias was absent before adoption. It reads only named's running process and
// re-proves the exact absent alias around both observations. Managed BIND and
// switch paths continue to require ProbeBINDVendorRuntime's active alias.
func ProbeBINDAdoptionRuntime(ctx context.Context, profile hostplatform.Profile, aliasAbsent bool, runtime BINDRuntimeRunner, unit NativeUnitRunner) (dnsunitidentity.Processes, error) {
	if !aliasAbsent {
		return ProbeBINDVendorRuntime(ctx, profile, runtime)
	}
	if ctx == nil || runtime == nil || unit == nil ||
		profile.PackageManager != hostplatform.PackageManagerAPT ||
		profile.DistroFamily != hostplatform.DistroFamilyDebian ||
		profile.ServiceManager != hostplatform.ServiceManagerSystemd {
		return dnsunitidentity.Processes{}, errors.New("absent-alias BIND adoption runtime requires a verified Debian systemd host")
	}
	if _, err := bindroot.CertifiedVendorContract(profile); err != nil {
		return dnsunitidentity.Processes{}, err
	}
	checkAlias := func() error {
		states, err := ProbeNativeUnits(ctx, []string{"bind9.service"}, unit)
		if err != nil {
			return err
		}
		if len(states) != 1 || states[0].Name != "bind9.service" ||
			states[0].LoadState != "not-found" || states[0].ActiveState != "inactive" || states[0].UnitFileState != "" {
			return errors.New("BIND adoption alias appeared or changed during runtime proof")
		}
		return nil
	}
	var first dnsunitidentity.Processes
	for attempt := 0; attempt < 2; attempt++ {
		if err := checkAlias(); err != nil {
			return dnsunitidentity.Processes{}, err
		}
		raw, err := runtime(ctx, "named.service")
		if err != nil {
			return dnsunitidentity.Processes{}, err
		}
		current, err := parseBINDRuntime(raw)
		if err != nil {
			return dnsunitidentity.Processes{}, err
		}
		if err := checkAlias(); err != nil {
			return dnsunitidentity.Processes{}, err
		}
		if attempt == 0 {
			first = current
		} else if current != first {
			return dnsunitidentity.Processes{}, errors.New("owner BIND named process changed during absent-alias observation")
		}
	}
	return first, ctx.Err()
}
