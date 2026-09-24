//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"time"

	"github.com/alicelik/celikpanel/internal/dnslistener"
)

type BINDListenerRunner func(context.Context) ([]byte, error)

type boundedListenerOutput struct {
	data []byte
}

func (output *boundedListenerOutput) Write(data []byte) (int, error) {
	if len(output.data)+len(data) > 64<<10 {
		return 0, errors.New("DNS listener output exceeds its bound")
	}
	output.data = append(output.data, data...)
	return len(data), nil
}

// SSListenerRunner reads only the local kernel's port-53 socket inventory.
// It does not change a service or trust an executable selected from PATH.
func SSListenerRunner(ctx context.Context) ([]byte, error) {
	if ctx == nil {
		return nil, errors.New("DNS listener observation requires a context")
	}
	path := "/usr/bin/ss"
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("locate native ss: %w", err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(queryCtx, path, "-H", "-lntup", "sport = :53")
	output := &boundedListenerOutput{}
	cmd.Stdout = output
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("inspect local DNS listeners: %w", err)
	}
	return output.data, nil
}

// ProbeBINDListeners compares two strict socket inventories against the
// already-verified systemd MainPID. It is a point-in-time observation only.
func ProbeBINDListeners(ctx context.Context, mainPID uint64, primaryIP string, runner BINDListenerRunner) error {
	return ProbeAuthorityListeners(ctx, "named", mainPID, primaryIP, runner)
}

// ProbeAuthorityListeners binds a strictly parsed public socket inventory to
// one previously verified systemd process. It is not mutation admission.
func ProbeAuthorityListeners(ctx context.Context, process string, mainPID uint64, primaryIP string, runner BINDListenerRunner) error {
	if ctx == nil || runner == nil || mainPID == 0 {
		return errors.New("invalid BIND listener observation")
	}
	var first []string
	for attempt := range 2 {
		if err := ctx.Err(); err != nil {
			return err
		}
		raw, err := runner(ctx)
		if err != nil {
			return err
		}
		if len(raw) > 64<<10 {
			return errors.New("DNS listener output exceeds its bound")
		}
		identities, err := dnslistener.CanonicalPublicListeners(string(raw), process, mainPID)
		if err != nil {
			return err
		}
		if primaryIP != "" && !dnslistener.HasIPv4Listener(identities, primaryIP, mainPID) {
			return errors.New("BIND does not own TCP and UDP port 53 on the primary IPv4 address")
		}
		if attempt == 0 {
			first = identities
		} else if !reflect.DeepEqual(first, identities) {
			return errors.New("BIND listener inventory changed during observation")
		}
	}
	return nil
}
