package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/alicelik/celikpanel/internal/licensing"
	"golang.org/x/term"
)

const panelLicensePath = "/api/v1/panel/license"
const pendingLicenseFile = "/var/lib/celikpanel-license/install.json"

func newServerLicense(file string) (*licensing.Manager, error) {
	key, err := hex.DecodeString(licensing.PublicKeyHex)
	if err != nil {
		return nil, errors.New("license verification key is not provisioned")
	}
	machine, err := os.ReadFile("/etc/machine-id")
	if err != nil {
		return nil, err
	}
	id, err := licensing.ServerID(machine)
	if err != nil {
		return nil, err
	}
	return licensing.New(file, key, id)
}

// This command runs before panel database initialization and never emits a key.
// The caller has already verified the immutable release and holds its lock.
func runLicenseCLI(args []string) (bool, error) {
	if len(args) != 1 || args[0] != "--activate-install-license" {
		return false, nil
	}
	if os.Geteuid() != 0 {
		return true, errors.New("license installation requires root")
	}
	installed, err := newServerLicense(filepath.Join(dataDir(), "license.json"))
	if err != nil {
		return true, err
	}
	if installed.Status().CanProvision {
		return true, nil
	}
	targetFile := pendingLicenseFile
	if _, statErr := os.Lstat(filepath.Join(dataDir(), "license.json")); statErr == nil {
		targetFile = filepath.Join(dataDir(), "license.json")
	}
	pending, err := newServerLicense(targetFile)
	if err != nil {
		return true, err
	}
	if pending.Status().CanProvision {
		return true, nil
	}
	// An existing binding can recover without asking for the original key.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_ = installed.Refresh(ctx, true)
	if installed.Status().CanProvision {
		return true, nil
	}
	_ = pending.Refresh(ctx, true)
	if pending.Status().CanProvision {
		return true, nil
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return true, errors.New("rerun installation in a terminal to enter your license from https://celikpanel.net/account/")
	}
	defer tty.Close()
	fmt.Fprintln(tty, "Get your free annual license at https://celikpanel.net/account/ / Yıllık ücretsiz lisansınızı websitesinden alın.")
	fmt.Fprint(tty, "License key (hidden) / Lisans anahtarı (gizli): ")
	terminalState, err := term.GetState(int(tty.Fd()))
	if err != nil {
		return true, err
	}
	interrupted := make(chan os.Signal, 1)
	finished := make(chan struct{})
	signal.Notify(interrupted, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		select {
		case <-interrupted:
			_ = term.Restore(int(tty.Fd()), terminalState)
			fmt.Fprintln(tty, "\nInstallation interrupted; run the same command again / Kurulum kesildi; aynı komutu yeniden çalıştırın.")
			os.Exit(130)
		case <-finished:
		}
	}()
	secret, err := term.ReadPassword(int(tty.Fd()))
	signal.Stop(interrupted)
	close(finished)
	_ = term.Restore(int(tty.Fd()), terminalState)
	fmt.Fprintln(tty)
	if err != nil {
		return true, errors.New("license entry interrupted; run the same installation command again")
	}
	defer func() {
		for i := range secret {
			secret[i] = 0
		}
	}()
	hostname, err := os.Hostname()
	if err != nil {
		return true, err
	}
	activateCtx, activateCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer activateCancel()
	if err = pending.Activate(activateCtx, strings.TrimSpace(string(secret)), hostname); err != nil {
		return true, err
	}
	fmt.Fprintln(tty, "License activated / Lisans etkinleştirildi.")
	return true, nil
}

func (p *Panel) handleLicense(w http.ResponseWriter, r *http.Request) {
	c := currentCaller(r)
	if c == nil || !c.hasAccountRole(roleAdmin) {
		writeCodedError(w, http.StatusForbidden, "admin_only", "administrator access required", "")
		return
	}
	if p.license == nil {
		writeCodedError(w, http.StatusServiceUnavailable, "license_unavailable", "license service is unavailable", "")
		return
	}
	switch r.Method {
	case http.MethodGet:
		_ = p.license.Refresh(r.Context(), false)
	case http.MethodPost:
		var input struct {
			Action string `json:"action"`
			Key    string `json:"key"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		dec.DisallowUnknownFields()
		if dec.Decode(&input) != nil || dec.Decode(new(any)) != io.EOF {
			writeCodedError(w, 400, "invalid_request", "invalid license request", "")
			return
		}
		var err error
		switch input.Action {
		case "activate":
			hostname, e := os.Hostname()
			if e != nil {
				err = e
			} else {
				err = p.license.Activate(r.Context(), input.Key, hostname)
			}
		case "refresh":
			err = p.license.Refresh(r.Context(), true)
		default:
			writeCodedError(w, 400, "invalid_request", "choose activate or refresh", "")
			return
		}
		if err != nil {
			writeCodedError(w, 400, "license_action_failed", err.Error(), "")
			return
		}
	default:
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(405)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(p.license.Status())
}

// Identity recovery, activation and administrator-only signed updates remain available while locked.
// This gate runs on authenticated HTTP requests, never on agents or schedulers.
func licenseRecoveryRequest(r *http.Request) bool {
	// Exact methods and paths only. This exception never grants tenant authority
	// or relaxes the updater's signed-target, host admission and rollback checks.
	if caller := currentCaller(r); caller != nil && caller.Role == roleAdmin {
		switch r.URL.Path {
		case panelUpdateCheckPath, panelUpdateStatusPath, "/api/v1/panel/version", hostMutationReadinessPath:
			return r.Method == http.MethodGet
		case panelUpdateStartPath, panelUpdateAbandonPath:
			return r.Method == http.MethodPost
		}
	}
	switch r.URL.Path {
	case "/api/v1/auth/me", panelLicenseAccessPath:
		return r.Method == http.MethodGet
	case panelLicensePath:
		return r.Method == http.MethodGet || r.Method == http.MethodPost
	case "/api/v1/auth/logout", "/api/v1/auth/password", "/api/v1/auth/unimpersonate":
		return r.Method == http.MethodPost
	}
	return false
}

const panelLicenseAccessPath = "/api/v1/license/access"

// All authenticated roles can read access, but only admins can see or change
// the license itself. No key, binding, or account identity is exposed here.
func (p *Panel) handleLicenseAccess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	allowed := false
	var until int64
	if p.license != nil {
		_ = p.license.Refresh(r.Context(), false)
		status := p.license.Status()
		allowed = status.CanProvision
		until = min(status.ExpiresAt, status.OfflineUntil)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(struct {
		CanUsePanel bool  `json:"can_use_panel"`
		ValidUntil  int64 `json:"valid_until"`
	}{allowed, until})
}

func (p *Panel) allowLicensedPanel(w http.ResponseWriter, r *http.Request) bool {
	if licenseRecoveryRequest(r) {
		return true
	}
	if p.license != nil && p.license.CanProvision(r.Context()) {
		return true
	}
	writeCodedError(w, http.StatusForbidden, "license_required",
		"Panel access requires an active server license. Existing services and scheduled tasks keep running. The server administrator must activate or renew the license.", "")
	return false
}
