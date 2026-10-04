//go:build linux

package recoveryruntime

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

const BINDSourceInverseCommand = "check-bind-source-inverse-v1"
const BINDSourceInverseMarker = "celikpanel-bind-source-inverse/v1\n"
const BINDAdoptionInverseCommand = "check-bind-adoption-inverse-v1"
const BINDAdoptionInverseMarker = "celikpanel-bind-adoption-inverse/v1\n"

// CheckBINDSourceInverseSupport asks the already selected, pinned recovery
// executable a closed read-only question. It neither promotes a kit nor starts
// recovery. The caller keeps release exclusion until durable journal creation.
func CheckBINDSourceInverseSupport(ctx context.Context, runtime *Runtime) error {
	return checkBINDInverseSupport(ctx, runtime, BINDSourceInverseCommand, BINDSourceInverseMarker)
}

// CheckBINDAdoptionInverseSupport requires a distinct selected-runtime claim.
func CheckBINDAdoptionInverseSupport(ctx context.Context, runtime *Runtime) error {
	return checkBINDInverseSupport(ctx, runtime, BINDAdoptionInverseCommand, BINDAdoptionInverseMarker)
}

func checkBINDInverseSupport(ctx context.Context, runtime *Runtime, capabilityCommand, marker string) error {
	if ctx == nil {
		return errors.New("BIND recovery capability requires a context")
	}
	if err := runtime.Revalidate(); err != nil {
		return err
	}
	var executable *os.File
	for _, file := range runtime.state.files {
		if file.parent.path == filepath.Join(runtime.Root, "bin") && file.base == "recovery" {
			executable = file.file
			break
		}
	}
	if executable == nil {
		return fail(ReasonMissing)
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	command := exec.CommandContext(probeCtx, "/proc/self/fd/3", capabilityCommand)
	command.ExtraFiles = []*os.File{executable}
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	command.Dir = "/"
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	command.WaitDelay = time.Second
	output := &bindCapabilityOutput{}
	command.Stdout = output
	if err := command.Run(); err != nil {
		if capabilityCommand == BINDAdoptionInverseCommand {
			return errors.New("selected recovery runtime cannot prove running BIND adoption inverse support; the server owner must prepare a compatible recovery runtime before retrying the adoption")
		}
		return errors.New("selected recovery runtime cannot prove BIND source inverse support; the owner must prepare a compatible recovery runtime before this DNS switch")
	}
	if output.String() != marker {
		if capabilityCommand == BINDAdoptionInverseCommand {
			return errors.New("selected recovery runtime returned an unsupported running BIND adoption inverse capability; the server owner must prepare a compatible recovery runtime before retrying")
		}
		return errors.New("selected recovery runtime returned an unsupported BIND source inverse capability")
	}
	return runtime.Revalidate()
}

type bindCapabilityOutput struct{ bytes.Buffer }

func (b *bindCapabilityOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 256 {
		return 0, errors.New("BIND recovery capability output exceeded its bound")
	}
	return b.Buffer.Write(p)
}
