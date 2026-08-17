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

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

func bindPanelCertificateMutation(
	t *testing.T,
	req *IssuePanelCertV2Request,
) {
	t.Helper()
	if req.Email == "" {
		req.Email = "admin@example.test"
	}
	commitment, err := mutationpayload.CanonicalPanelCertificateIssue(
		req.Domain,
		req.Email,
		req.TLSDir,
		req.ExpectedBuildCommit,
	)
	if err != nil {
		t.Fatal(err)
	}
	manager, _ := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	beginMutationTestJobWithIdentity(
		t,
		manager,
		"panel_certificate_issue",
		commitment.Domain,
		commitment.Qualifier,
	)
	req.MutationRequestID = testMutationRequestID
	req.MutationOwnerID = testMutationOwnerID
}

func installPanelCertificateAPTPackageFamily(t *testing.T) {
	t.Helper()
	originalDetect := panelCertDetectPkgFamily
	panelCertDetectPkgFamily = func() string { return `apt` }
	t.Cleanup(func() { panelCertDetectPkgFamily = originalDetect })
}

func bridgePanelCertificateIssueStageToLegacyPublishTest(t *testing.T) {
	t.Helper()
	original := panelCertStageIssue
	originalVerify := panelCertificateIssueVerifyPublished
	panelCertStageIssue = func(
		domain, tlsDir string,
		certificate, privateKey []byte,
		_ panelCertificateIssueReceipt,
	) (*panelCertificateIssueStage, error) {
		return &panelCertificateIssueStage{
			publishAction: func() (bool, error) {
				err := panelCertificateActivationPublishMaterial(
					domain,
					tlsDir,
					certificate,
					privateKey,
				)
				return err == nil, err
			},
			cleanupAction: func(bool) error { return nil },
		}, nil
	}
	panelCertificateIssueVerifyPublished = func(
		string, string, string,
	) (bool, error) {
		return true, nil
	}
	t.Cleanup(func() {
		panelCertStageIssue = original
		panelCertificateIssueVerifyPublished = originalVerify
	})
}

func TestIssuePanelCertificateRequiresDurableMutationBinding(t *testing.T) {
	req := &IssuePanelCertV2Request{
		Domain:              "panel.example.test",
		Email:               "admin@example.test",
		TLSDir:              managedPanelTLSDir,
		ExpectedBuildCommit: strings.TrimSpace(buildCommit),
	}
	var resp IssuePanelCertV2Response
	if err := (&Agent{}).IssuePanelCertificateV2(req, &resp); err != nil {
		t.Fatalf("IssuePanelCertificateV2 returned RPC error: %v", err)
	}
	if !strings.Contains(resp.Error, "durable service mutation lease is required") {
		t.Fatalf("response error = %q, want missing durable lease rejection", resp.Error)
	}
}

func TestIssuePanelCertificateLegacyEndpointIsStableZeroTouchStub(t *testing.T) {
	var resp IssuePanelCertResponse
	if err := (&Agent{}).IssuePanelCertificate(
		&IssuePanelCertRequest{
			MutationRequestID:   testMutationRequestID,
			MutationOwnerID:     testMutationOwnerID,
			Domain:              "panel.example.test",
			Email:               "admin@example.test",
			TLSDir:              managedPanelTLSDir,
			ExpectedBuildCommit: strings.TrimSpace(buildCommit),
		},
		&resp,
	); err != nil {
		t.Fatal(err)
	}
	if resp != (IssuePanelCertResponse{
		Error: issuePanelCertificateLegacyUnsupportedError,
	}) {
		t.Fatalf("legacy response = %#v", resp)
	}
}

func TestIssuePanelCertificateRejectsConcurrentOperation(t *testing.T) {
	if !acquireSiteCertbot() {
		t.Fatal("failed to acquire certificate test slot")
	}
	defer releaseSiteCertbot()

	req := &IssuePanelCertV2Request{
		Domain:              "panel.example.test",
		TLSDir:              managedPanelTLSDir,
		ExpectedBuildCommit: strings.TrimSpace(buildCommit),
	}
	bindPanelCertificateMutation(t, req)
	var resp IssuePanelCertV2Response
	if err := (&Agent{}).IssuePanelCertificateV2(req, &resp); err != nil {
		t.Fatalf("IssuePanelCertificateV2 returned RPC error: %v", err)
	}
	if !strings.Contains(resp.Error, "already in progress") {
		t.Fatalf("response error = %q, want concurrent-operation rejection", resp.Error)
	}
}

func TestIssuePanelCertificateV2RejectsPayloadSubstitutionBeforeHostAccess(
	t *testing.T,
) {
	req := &IssuePanelCertV2Request{
		Domain:              "panel.example.test",
		Email:               "admin@example.test",
		TLSDir:              managedPanelTLSDir,
		ExpectedBuildCommit: strings.TrimSpace(buildCommit),
	}
	bindPanelCertificateMutation(t, req)
	req.Email = "other@example.test"

	originalLookPath := panelCertLookPath
	hostTouched := false
	panelCertLookPath = func(string) (string, error) {
		hostTouched = true
		return "", errors.New("must not run")
	}
	t.Cleanup(func() { panelCertLookPath = originalLookPath })

	var resp IssuePanelCertV2Response
	if err := (&Agent{}).IssuePanelCertificateV2(req, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error == "" ||
		!strings.Contains(resp.Error, "does not authorize") {
		t.Fatalf("payload substitution response = %#v", resp)
	}
	if hostTouched {
		t.Fatal("payload substitution reached host inspection")
	}
}

func TestIssuePanelCertificateV2RejectsUnsupportedDNFBeforeHostAccess(
	t *testing.T,
) {
	req := &IssuePanelCertV2Request{
		Domain:              "panel.example.test",
		Email:               "admin@example.test",
		TLSDir:              managedPanelTLSDir,
		ExpectedBuildCommit: strings.TrimSpace(buildCommit),
	}
	bindPanelCertificateMutation(t, req)

	originalDetect := panelCertDetectPkgFamily
	originalLookPath := panelCertLookPath
	originalInstall := panelCertInstallPackages
	panelCertDetectPkgFamily = func() string { return "dnf" }
	hostTouched := false
	panelCertLookPath = func(string) (string, error) {
		hostTouched = true
		return "", errors.New("must not run")
	}
	panelCertInstallPackages = func(
		context.Context, string, []string,
	) (string, error) {
		hostTouched = true
		return "", errors.New("must not run")
	}
	t.Cleanup(func() {
		panelCertDetectPkgFamily = originalDetect
		panelCertLookPath = originalLookPath
		panelCertInstallPackages = originalInstall
	})

	var resp IssuePanelCertV2Response
	if err := (&Agent{}).IssuePanelCertificateV2(req, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error == "" || hostTouched {
		t.Fatalf("DNF response=%#v hostTouched=%v", resp, hostTouched)
	}
}

func TestIssuePanelCertificateV2UsesExactCataloguedCertbotPackage(
	t *testing.T,
) {
	req := &IssuePanelCertV2Request{
		Domain:              "panel.example.test",
		Email:               "admin@example.test",
		TLSDir:              managedPanelTLSDir,
		ExpectedBuildCommit: strings.TrimSpace(buildCommit),
	}
	bindPanelCertificateMutation(t, req)

	originalDetect := panelCertDetectPkgFamily
	originalLookPath := panelCertLookPath
	originalInstall := panelCertInstallPackages
	panelCertDetectPkgFamily = func() string { return "apt" }
	panelCertLookPath = func(string) (string, error) {
		return "", exec.ErrNotFound
	}
	panelCertInstallPackages = func(
		_ context.Context,
		family string,
		packages []string,
	) (string, error) {
		if family != "apt" ||
			!reflect.DeepEqual(packages, []string{"certbot"}) {
			t.Fatalf("install family=%q packages=%#v", family, packages)
		}
		return "", errors.New("stop after catalog assertion")
	}
	t.Cleanup(func() {
		panelCertDetectPkgFamily = originalDetect
		panelCertLookPath = originalLookPath
		panelCertInstallPackages = originalInstall
	})

	var resp IssuePanelCertV2Response
	if err := (&Agent{}).IssuePanelCertificateV2(req, &resp); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Error, "stop after catalog assertion") {
		t.Fatalf("response=%#v", resp)
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
	installPanelCertificateAPTPackageFamily(t)
	store := installPanelCertificateActivationMemoryStore(t)
	originalLookPath := panelCertLookPath
	originalRun := panelCertRunMutationCommand
	originalHook := panelCertWriteDeployHook
	originalRenewal := panelCertEnsureRenewal
	originalLock := panelCertWithPublishLock
	originalActiveIdentity := panelCertActiveIdentity
	originalReadSource := panelCertificateActivationReadSource
	originalPublish := panelCertificateActivationPublishMaterial
	t.Cleanup(func() {
		panelCertLookPath = originalLookPath
		panelCertRunMutationCommand = originalRun
		panelCertWriteDeployHook = originalHook
		panelCertEnsureRenewal = originalRenewal
		panelCertWithPublishLock = originalLock
		panelCertActiveIdentity = originalActiveIdentity
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
	panelCertActiveIdentity = func(string) (string, bool, error) {
		return "panel.example.test", true, nil
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

	var resp IssuePanelCertV2Response
	req := &IssuePanelCertV2Request{
		Domain:              "panel.example.test",
		TLSDir:              managedPanelTLSDir,
		ExpectedBuildCommit: strings.TrimSpace(buildCommit),
	}
	bindPanelCertificateMutation(t, req)
	err := (&Agent{}).IssuePanelCertificateV2(req, &resp)
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
	if state, present := store.snapshot(); present {
		t.Fatalf("pre-commit activation state was retained: %+v", state)
	}
}

func TestIssuePanelCertificatePublishesExactMaterialAndPersistsRestartIntent(t *testing.T) {
	installPanelCertificateAPTPackageFamily(t)
	store := installPanelCertificateActivationMemoryStore(t)
	bridgePanelCertificateIssueStageToLegacyPublishTest(t)
	originalLookPath := panelCertLookPath
	originalRun := panelCertRunMutationCommand
	originalHook := panelCertWriteDeployHook
	originalRenewal := panelCertEnsureRenewal
	originalLock := panelCertWithPublishLock
	originalActiveIdentity := panelCertActiveIdentity
	originalReadSource := panelCertificateActivationReadSource
	originalPublish := panelCertificateActivationPublishMaterial
	t.Cleanup(func() {
		panelCertLookPath = originalLookPath
		panelCertRunMutationCommand = originalRun
		panelCertWriteDeployHook = originalHook
		panelCertEnsureRenewal = originalRenewal
		panelCertWithPublishLock = originalLock
		panelCertActiveIdentity = originalActiveIdentity
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
		queued, err := enqueueRenewedPanelCertificateActivation(
			panelCertLineageName("panel.example.test"),
		)
		if err != nil {
			return nil, err
		}
		if queued {
			t.Fatal("certbot deploy hook overwrote the exact interactive activation intent")
		}
		return nil, nil
	}
	panelCertActiveIdentity = func(string) (string, bool, error) {
		return "panel.example.test", true, nil
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

	var resp IssuePanelCertV2Response
	req := &IssuePanelCertV2Request{
		Domain:              "panel.example.test",
		TLSDir:              managedPanelTLSDir,
		ExpectedBuildCommit: strings.TrimSpace(buildCommit),
	}
	bindPanelCertificateMutation(t, req)
	if err := (&Agent{}).IssuePanelCertificateV2(req, &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Issued || resp.Error != "" {
		t.Fatalf("response = %+v", resp)
	}
	if !reflect.DeepEqual(order, []string{"renewal", "hook", "publish"}) {
		t.Fatalf("commit order = %#v", order)
	}
	state, present := store.snapshot()
	if !present ||
		state.Origin != panelCertificateActivationOriginInteractive ||
		state.RequestID != testMutationRequestID ||
		state.Phase != panelCertificateActivationPendingPublish {
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

func TestIssuePanelCertificateCleansBoundIntentWhenDeployHookFails(t *testing.T) {
	installPanelCertificateAPTPackageFamily(t)
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

	var resp IssuePanelCertV2Response
	req := &IssuePanelCertV2Request{
		Domain:              "panel.example.test",
		TLSDir:              managedPanelTLSDir,
		ExpectedBuildCommit: strings.TrimSpace(buildCommit),
	}
	bindPanelCertificateMutation(t, req)
	if err := (&Agent{}).IssuePanelCertificateV2(req, &resp); err != nil {
		t.Fatal(err)
	}
	if published {
		t.Fatal("certificate was published after deploy hook validation failed")
	}
	if !strings.Contains(resp.Error, "deploy hook unavailable") {
		t.Fatalf("response error = %q", resp.Error)
	}
	if state, present := store.snapshot(); present {
		t.Fatalf("pre-commit activation state was retained: %+v", state)
	}
}

func TestIssuePanelCertificateCleansUnchangedIntentAfterCertbotFailure(t *testing.T) {
	installPanelCertificateAPTPackageFamily(t)
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

	var resp IssuePanelCertV2Response
	req := &IssuePanelCertV2Request{
		Domain:              "panel.example.test",
		TLSDir:              managedPanelTLSDir,
		ExpectedBuildCommit: strings.TrimSpace(buildCommit),
	}
	bindPanelCertificateMutation(t, req)
	if err := (&Agent{}).IssuePanelCertificateV2(req, &resp); err != nil {
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
