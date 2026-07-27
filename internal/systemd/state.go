package systemd

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const unitStateTimeout = 8 * time.Second

type unitState struct {
	Active          string
	Sub             string
	Result          string
	ConditionResult string
}

func readUnitState(serviceName string) (unitState, error) {
	out, err := exec.Command(
		"systemctl", "show", serviceName,
		"--property=ActiveState,SubState,Result,ConditionResult",
		"--no-pager",
	).CombinedOutput()
	if err != nil {
		return unitState{}, fmt.Errorf("systemctl-show-failed:%v:%s", err, strings.TrimSpace(string(out)))
	}
	state := unitState{}
	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "ActiveState":
			state.Active = value
		case "SubState":
			state.Sub = value
		case "Result":
			state.Result = value
		case "ConditionResult":
			state.ConditionResult = value
		}
	}
	return state, nil
}

func (s unitState) summary() string {
	parts := []string{"state=" + s.Active + "/" + s.Sub}
	if s.Result != "" {
		parts = append(parts, "result="+s.Result)
	}
	if s.ConditionResult != "" {
		parts = append(parts, "condition="+s.ConditionResult)
	}
	return strings.Join(parts, ", ")
}

func unitStateReached(state unitState, wantActive bool) (done, success bool) {
	if wantActive {
		if state.Active == "active" {
			return true, true
		}
		if state.Active == "failed" || state.ConditionResult == "no" {
			return true, false
		}
		return false, false
	}
	if state.Active != "active" && state.Active != "activating" {
		return true, true
	}
	return false, false
}

func waitForUnitState(serviceName string, wantActive bool) error {
	deadline := time.Now().Add(unitStateTimeout)
	var last unitState
	for {
		state, err := readUnitState(serviceName)
		if err != nil {
			return err
		}
		last = state
		if done, success := unitStateReached(state, wantActive); done {
			if success {
				return nil
			}
			break
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	target := "inactive"
	if wantActive {
		target = "active"
	}
	return fmt.Errorf("%s did not become %s (%s)", serviceName, target, last.summary())
}

func runUnitChange(serviceName string, wantActive bool, args ...string) error {
	out, err := exec.Command("systemctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl-%s-failed:%v:%s", strings.Join(args, "-"), err, strings.TrimSpace(string(out)))
	}
	return waitForUnitState(serviceName, wantActive)
}

// EnableNow also verifies ActiveState. systemctl can exit zero when a unit is
// skipped by a failed condition; that must never be reported as success.
func (m *Manager) EnableNow(serviceName string) error {
	return runUnitChange(serviceName, true, "enable", "--now", serviceName)
}

func (m *Manager) Enable(serviceName string) error {
	out, err := exec.Command("systemctl", "enable", serviceName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl-enable-failed:%v:%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
