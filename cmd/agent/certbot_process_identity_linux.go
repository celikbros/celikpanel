//go:build linux

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// The agent's socket group is deliberately celikpanel. Certbot's private
// source tree, however, belongs to root:root and is later authenticated by the
// protected certificate reader. Set this only on the actual Certbot process;
// the mutation supervisor retains its normal identity and host-lock proof.
func configureCertbotProcessIdentity(cmd *exec.Cmd) error {
	if cmd == nil || filepath.Base(cmd.Path) != "certbot" {
		return nil
	}
	if os.Geteuid() != 0 {
		return errors.New("managed Certbot execution requires root")
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Credential = &syscall.Credential{Uid: 0, Gid: 0, Groups: []uint32{}}
	return nil
}
