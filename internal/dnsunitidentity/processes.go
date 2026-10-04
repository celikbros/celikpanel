package dnsunitidentity

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Processes is the exact process state reported by systemd for one DNS unit.
// It is an observation, not proof that the process serves a given config.
type Processes struct {
	MainPID    uint64
	ControlPID uint64
	SubState   string
}

func ParseProcesses(output string) (Processes, error) {
	values := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		key, candidate, found := strings.Cut(line, "=")
		if !found || (key != "MainPID" && key != "ControlPID" && key != "SubState") {
			return Processes{}, errors.New("systemctl returned an unexpected DNS unit process row")
		}
		if _, exists := values[key]; exists || candidate == "" {
			return Processes{}, errors.New("systemctl returned an ambiguous DNS unit process")
		}
		values[key] = candidate
	}
	if len(values) != 3 {
		return Processes{}, errors.New("systemctl returned incomplete DNS unit processes")
	}
	parse := func(name string) (uint64, error) {
		value := values[name]
		pid, err := strconv.ParseUint(value, 10, 64)
		if err != nil || strconv.FormatUint(pid, 10) != value {
			return 0, fmt.Errorf("systemctl returned a non-canonical %s", name)
		}
		return pid, nil
	}
	mainPID, err := parse("MainPID")
	if err != nil {
		return Processes{}, err
	}
	controlPID, err := parse("ControlPID")
	if err != nil {
		return Processes{}, err
	}
	return Processes{MainPID: mainPID, ControlPID: controlPID, SubState: values["SubState"]}, nil
}
