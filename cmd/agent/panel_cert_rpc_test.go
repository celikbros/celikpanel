//go:build linux

package main

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

func bindPanelCertificateMutation(
	t *testing.T,
	req *IssuePanelCertRequest,
) {
	t.Helper()
	manager, _ := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	beginMutationTestJob(t, manager)
	req.MutationRequestID = testMutationRequestID
	req.MutationOwnerID = testMutationOwnerID
}

func TestIssuePanelCertificateRequiresDurableMutationBinding(t *testing.T) {
	req := &IssuePanelCertRequest{
		Domain:              "panel.example.test",
		TLSDir:              managedPanelTLSDir,
		ExpectedBuildCommit: strings.TrimSpace(buildCommit),
	}
	var resp IssuePanelCertResponse
	if err := (&Agent{}).IssuePanelCertificate(req, &resp); err != nil {
		t.Fatalf("IssuePanelCertificate returned RPC error: %v", err)
	}
	if !strings.Contains(resp.Error, "durable service mutation lease is required") {
		t.Fatalf("response error = %q, want missing durable lease rejection", resp.Error)
	}
}

func TestIssuePanelCertificateRejectsConcurrentOperation(t *testing.T) {
	if !acquireSiteCertbot() {
		t.Fatal("failed to acquire certificate test slot")
	}
	defer releaseSiteCertbot()

	req := &IssuePanelCertRequest{
		Domain:              "panel.example.test",
		TLSDir:              managedPanelTLSDir,
		ExpectedBuildCommit: strings.TrimSpace(buildCommit),
	}
	bindPanelCertificateMutation(t, req)
	var resp IssuePanelCertResponse
	if err := (&Agent{}).IssuePanelCertificate(req, &resp); err != nil {
		t.Fatalf("IssuePanelCertificate returned RPC error: %v", err)
	}
	if !strings.Contains(resp.Error, "already in progress") {
		t.Fatalf("response error = %q, want concurrent-operation rejection", resp.Error)
	}
}

func TestPanelCertificateChallengeUsesStandaloneWithoutNginx(t *testing.T) {
	originalLookPath := panelCertLookPath
	panelCertLookPath = func(name string) (string, error) {
		if name != "nginx" {
			t.Fatalf("look path name = %q", name)
		}
		return "", exec.ErrNotFound
	}
	t.Cleanup(func() { panelCertLookPath = originalLookPath })

	args, err := (&Agent{}).panelCertificateChallengeArgs(
		context.Background(), "panel.example.test",
	)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"--standalone"}; !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}

func TestPanelCertificateChallengeUsesPersistentNginxWebroot(t *testing.T) {
	originalLookPath := panelCertLookPath
	originalRun := panelCertRunMutationCommand
	originalPrepare := panelCertPrepareChallengeRoot
	originalApply := panelCertApplyVhost
	t.Cleanup(func() {
		panelCertLookPath = originalLookPath
		panelCertRunMutationCommand = originalRun
		panelCertPrepareChallengeRoot = originalPrepare
		panelCertApplyVhost = originalApply
	})

	panelCertLookPath = func(name string) (string, error) {
		if name != "nginx" {
			t.Fatalf("look path name = %q", name)
		}
		return "/usr/sbin/nginx", nil
	}
	panelCertRunMutationCommand = func(
		_ context.Context,
		timeout time.Duration,
		name string,
		args ...string,
	) ([]byte, error) {
		if timeout != panelCertSystemdTimeout || name != "systemctl" ||
			!reflect.DeepEqual(args, []string{"is-active", "--quiet", "nginx"}) {
			t.Fatalf("unexpected active check: %s %#v (%s)", name, args, timeout)
		}
		return nil, nil
	}
	const root = "/var/lib/celikpanel-agent/acme-http-01/panel"
	panelCertPrepareChallengeRoot = func() (string, error) { return root, nil }
	var appliedName, appliedConfig string
	panelCertApplyVhost = func(
		_ context.Context,
		_ *Agent,
		name, config string,
	) error {
		appliedName, appliedConfig = name, config
		return nil
	}

	args, err := (&Agent{}).panelCertificateChallengeArgs(
		context.Background(), "panel.example.test",
	)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"--webroot", "--webroot-path", root}; !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
	if appliedName != panelACMEVhostName("panel.example.test") {
		t.Fatalf("vhost name = %q", appliedName)
	}
	for _, want := range []string{"server_name panel.example.test;", "root " + root + ";", "return 404;"} {
		if !strings.Contains(appliedConfig, want) {
			t.Fatalf("vhost does not contain %q:\n%s", want, appliedConfig)
		}
	}
}

func TestPanelCertificateChallengeNamesAreIndependentPerHostname(t *testing.T) {
	first := panelACMEVhostName("old-panel.example.test")
	second := panelACMEVhostName("new-panel.example.test")
	if first == second {
		t.Fatalf("candidate hostnames share ACME vhost name %q", first)
	}
	for _, name := range []string{first, second} {
		if !strings.HasPrefix(name, panelACMEVhostPrefix) {
			t.Fatalf("vhost name %q does not use managed prefix", name)
		}
	}
}

func TestPanelCertificateChallengeRefusesInactiveNginx(t *testing.T) {
	originalLookPath := panelCertLookPath
	originalRun := panelCertRunMutationCommand
	t.Cleanup(func() {
		panelCertLookPath = originalLookPath
		panelCertRunMutationCommand = originalRun
	})
	panelCertLookPath = func(string) (string, error) { return "/usr/sbin/nginx", nil }
	panelCertRunMutationCommand = func(
		context.Context, time.Duration, string, ...string,
	) ([]byte, error) {
		return []byte("inactive\n"), errors.New("exit status 3")
	}

	_, err := (&Agent{}).panelCertificateChallengeArgs(
		context.Background(), "panel.example.test",
	)
	if err == nil || !strings.Contains(err.Error(), "not active") {
		t.Fatalf("error = %v, want inactive nginx rejection", err)
	}
}

func TestIssuePanelCertificateDoesNotInstallHookOrPublishWithoutAutomaticRenewal(t *testing.T) {
	store := installPanelCertificateActivationMemoryStore(t)
	originalLookPath := panelCertLookPath
	originalRun := panelCertRunMutationCommand
	originalHook := panelCertWriteDeployHook
	originalRenewal := panelCertEnsureRenewal
	originalLock := panelCertWithPublishLock
	originalReadSource := panelCertificateActivationReadSource
	originalPublish := panelCertificateActivationPublishMaterial
	t.Cleanup(func() {
		panelCertLookPath = originalLookPath
		panelCertRunMutationCommand = originalRun
		panelCertWriteDeployHook = originalHook
		panelCertEnsureRenewal = originalRenewal
		panelCertWithPublishLock = originalLock
		panelCertificateActivationReadSource = originalReadSource
		panelCertificateActivationPublishMaterial = originalPublish
	})
	panelCertLookPath = func(name string) (string, error) {
		if name == "certbot" {
			return "/usr/bin/certbot", nil
		}
		return "", exec.ErrNotFound
	}
	panelCertRunMutationCommand = func(
		context.Context, time.Duration, string, ...string,
	) ([]byte, error) {
		return nil, nil
	}
	panelCertWithPublishLock = func(action func() error) error { return action() }
	expiry := time.Now().UTC().Add(24 * time.Hour)
	panelCertificateActivationReadSource = func(string) (
		[]byte, []byte, []byte, time.Time, error,
	) {
		return []byte("certificate"), []byte("private-key"), []byte("leaf-der"), expiry, nil
	}
	hookWritten := false
	panelCertWriteDeployHook = func(string, string) error {
		hookWritten = true
		return nil
	}
	panelCertEnsureRenewal = func(context.Context) error {
		return errors.New("no supported renewal scheduler")
	}
	published := false
	panelCertificateActivationPublishMaterial = func(
		string, string, []byte, []byte,
	) error {
		published = true
		return nil
	}

	var resp IssuePanelCertResponse
	req := &IssuePanelCertRequest{
		Domain:              "panel.example.test",
		TLSDir:              managedPanelTLSDir,
		ExpectedBuildCommit: strings.TrimSpace(buildCommit),
	}
	bindPanelCertificateMutation(t, req)
	err := (&Agent{}).IssuePanelCertificate(req, &resp)
	if err != nil {
		t.Fatal(err)
	}
	if hookWritten {
		t.Fatal("deploy hook was changed before scheduler validation")
	}
	if published {
		t.Fatal("certificate pair was published without automatic renewal")
	}
	if !strings.Contains(resp.Error, "renewal scheduler") {
		t.Fatalf("response error = %q", resp.Error)
	}
	state, present := store.snapshot()
	if !present || state.Phase != panelCertificateActivationPendingPublish {
		t.Fatalf("activation state = %+v present=%v", state, present)
	}
}

func TestIssuePanelCertificatePublishesExactMaterialAndPersistsRestartIntent(t *testing.T) {
	store := installPanelCertificateActivationMemoryStore(t)
	originalLookPath := panelCertLookPath
	originalRun := panelCertRunMutationCommand
	originalHook := panelCertWriteDeployHook
	originalRenewal := panelCertEnsureRenewal
	originalLock := panelCertWithPublishLock
	originalReadSource := panelCertificateActivationReadSource
	originalPublish := panelCertificateActivationPublishMaterial
	t.Cleanup(func() {
		panelCertLookPath = originalLookPath
		panelCertRunMutationCommand = originalRun
		panelCertWriteDeployHook = originalHook
		panelCertEnsureRenewal = originalRenewal
		panelCertWithPublishLock = originalLock
		panelCertificateActivationReadSource = originalReadSource
		panelCertificateActivationPublishMaterial = originalPublish
	})

	panelCertLookPath = func(name string) (string, error) {
		if name == "certbot" {
			return "/usr/bin/certbot", nil
		}
		return "", exec.ErrNotFound
	}
	var certbotArgs []string
	locked := false
	panelCertWithPublishLock = func(action func() error) error {
		if locked {
			t.Fatal("recursive panel certificate publication lock")
		}
		locked = true
		defer func() { locked = false }()
		return action()
	}
	panelCertRunMutationCommand = func(
		_ context.Context,
		_ time.Duration,
		name string,
		args ...string,
	) ([]byte, error) {
		if name != "certbot" {
			t.Fatalf("unexpected command %q", name)
		}
		if locked {
			t.Fatal("certbot was run while the publication lock was held")
		}
		certbotArgs = append([]string(nil), args...)
		return nil, nil
	}
	expiry := time.Now().UTC().Truncate(time.Second).Add(48 * time.Hour)
	leafDER := []byte("exact-leaf-der")
	certificate := []byte("exact-certificate")
	privateKey := []byte("exact-private-key")
	panelCertificateActivationReadSource = func(string) (
		[]byte, []byte, []byte, time.Time, error,
	) {
		return certificate, privateKey, leafDER, expiry, nil
	}
	var order []string
	panelCertEnsureRenewal = func(context.Context) error {
		order = append(order, "renewal")
		return nil
	}
	panelCertWriteDeployHook = func(string, string) error {
		order = append(order, "hook")
		return nil
	}
	panelCertificateActivationPublishMaterial = func(
		domain, tlsDir string,
		gotCertificate, gotPrivateKey []byte,
	) error {
		order = append(order, "publish")
		if domain != "panel.example.test" || tlsDir != managedPanelTLSDir {
			t.Fatalf("publish target = %q %q", domain, tlsDir)
		}
		if string(gotCertificate) != string(certificate) ||
			string(gotPrivateKey) != string(privateKey) {
			t.Fatal("published material differs from the bound source")
		}
		return nil
	}

	var resp IssuePanelCertResponse
	req := &IssuePanelCertRequest{
		Domain:              "panel.example.test",
		TLSDir:              managedPanelTLSDir,
		ExpectedBuildCommit: strings.TrimSpace(buildCommit),
	}
	bindPanelCertificateMutation(t, req)
	if err := (&Agent{}).IssuePanelCertificate(req, &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Issued || resp.Error != "" {
		t.Fatalf("response = %+v", resp)
	}
	if !reflect.DeepEqual(order, []string{"renewal", "hook", "publish"}) {
		t.Fatalf("commit order = %#v", order)
	}
	state, present := store.snapshot()
	if !present || state.Phase != panelCertificateActivationPendingRestart {
		t.Fatalf("activation state = %+v present=%v", state, present)
	}
	if state.LeafSHA256 != panelCertificateLeafSHA256(leafDER) ||
		state.NotAfter == nil || !state.NotAfter.Equal(expiry) {
		t.Fatalf("activation binding = %+v", state)
	}
	joined := strings.Join(certbotArgs, " ")
	for _, want := range []string{
		"--cert-name " + panelCertLineageName("panel.example.test"),
		"-d panel.example.test",
		"--force-renewal",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("certbot args %q do not contain %q", joined, want)
		}
	}
	if strings.Contains(joined, "--keep-until-expiring") {
		t.Fatalf("certbot args retain shared-lineage behavior: %q", joined)
	}
}

func TestIssuePanelCertificateRetainsBoundIntentWhenDeployHookFails(t *testing.T) {
	store := installPanelCertificateActivationMemoryStore(t)
	originalLookPath := panelCertLookPath
	originalRun := panelCertRunMutationCommand
	originalHook := panelCertWriteDeployHook
	originalRenewal := panelCertEnsureRenewal
	originalLock := panelCertWithPublishLock
	originalReadSource := panelCertificateActivationReadSource
	originalPublish := panelCertificateActivationPublishMaterial
	t.Cleanup(func() {
		panelCertLookPath = originalLookPath
		panelCertRunMutationCommand = originalRun
		panelCertWriteDeployHook = originalHook
		panelCertEnsureRenewal = originalRenewal
		panelCertWithPublishLock = originalLock
		panelCertificateActivationReadSource = originalReadSource
		panelCertificateActivationPublishMaterial = originalPublish
	})
	panelCertLookPath = func(name string) (string, error) {
		if name == "certbot" {
			return "/usr/bin/certbot", nil
		}
		return "", exec.ErrNotFound
	}
	panelCertRunMutationCommand = func(
		context.Context, time.Duration, string, ...string,
	) ([]byte, error) {
		return nil, nil
	}
	panelCertEnsureRenewal = func(context.Context) error { return nil }
	panelCertWriteDeployHook = func(string, string) error {
		return errors.New("deploy hook unavailable")
	}
	panelCertWithPublishLock = func(action func() error) error { return action() }
	panelCertificateActivationReadSource = func(string) (
		[]byte, []byte, []byte, time.Time, error,
	) {
		return []byte("certificate"), []byte("private-key"), []byte("leaf-der"),
			time.Now().UTC().Add(24 * time.Hour), nil
	}
	published := false
	panelCertificateActivationPublishMaterial = func(
		string, string, []byte, []byte,
	) error {
		published = true
		return nil
	}

	var resp IssuePanelCertResponse
	req := &IssuePanelCertRequest{
		Domain:              "panel.example.test",
		TLSDir:              managedPanelTLSDir,
		ExpectedBuildCommit: strings.TrimSpace(buildCommit),
	}
	bindPanelCertificateMutation(t, req)
	if err := (&Agent{}).IssuePanelCertificate(req, &resp); err != nil {
		t.Fatal(err)
	}
	if published {
		t.Fatal("certificate was published after deploy hook validation failed")
	}
	if !strings.Contains(resp.Error, "deploy hook unavailable") {
		t.Fatalf("response error = %q", resp.Error)
	}
	state, present := store.snapshot()
	if !present || state.Phase != panelCertificateActivationPendingPublish {
		t.Fatalf("activation state = %+v present=%v", state, present)
	}
}

func TestIssuePanelCertificateCleansUnchangedIntentAfterCertbotFailure(t *testing.T) {
	store := installPanelCertificateActivationMemoryStore(t)
	originalLookPath := panelCertLookPath
	originalRun := panelCertRunMutationCommand
	originalLock := panelCertWithPublishLock
	originalReadSource := panelCertificateActivationReadSource
	t.Cleanup(func() {
		panelCertLookPath = originalLookPath
		panelCertRunMutationCommand = originalRun
		panelCertWithPublishLock = originalLock
		panelCertificateActivationReadSource = originalReadSource
	})

	panelCertLookPath = func(name string) (string, error) {
		if name == "certbot" {
			return "/usr/bin/certbot", nil
		}
		return "", exec.ErrNotFound
	}
	locked := false
	panelCertWithPublishLock = func(action func() error) error {
		if locked {
			t.Fatal("recursive panel certificate publication lock")
		}
		locked = true
		defer func() { locked = false }()
		return action()
	}
	panelCertRunMutationCommand = func(
		context.Context, time.Duration, string, ...string,
	) ([]byte, error) {
		if locked {
			t.Fatal("certbot was run while publication lock was held")
		}
		return []byte("challenge failed"), errors.New("exit status 1")
	}
	sourceRead := false
	panelCertificateActivationReadSource = func(string) (
		[]byte, []byte, []byte, time.Time, error,
	) {
		sourceRead = true
		return nil, nil, nil, time.Time{}, nil
	}

	var resp IssuePanelCertResponse
	req := &IssuePanelCertRequest{
		Domain:              "panel.example.test",
		TLSDir:              managedPanelTLSDir,
		ExpectedBuildCommit: strings.TrimSpace(buildCommit),
	}
	bindPanelCertificateMutation(t, req)
	if err := (&Agent{}).IssuePanelCertificate(req, &resp); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Error, "challenge failed") {
		t.Fatalf("response error = %q", resp.Error)
	}
	if sourceRead {
		t.Fatal("source was read after certbot failure")
	}
	if _, present := store.snapshot(); present {
		t.Fatal("unchanged pending-source intent was not cleaned")
	}
}

func TestCertificateRestartSchedulerNeverRunsDetachedCommand(t *testing.T) {
	originalCommand := panelCertRunMutationCommand
	t.Cleanup(func() { panelCertRunMutationCommand = originalCommand })
	panelCertRunMutationCommand = func(
		context.Context, time.Duration, string, ...string,
	) ([]byte, error) {
		t.Fatal("restart scheduling launched a privileged command")
		return nil, errors.New("unreachable")
	}
	if err := schedulePanelCertificateRestart(context.Background()); err != nil {
		t.Fatal(err)
	}
}
