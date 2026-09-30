package main

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

func TestServiceFailureComponentAndStepFromPhase(t *testing.T) {
	for _, tc := range []struct {
		service, phase, component, step string
	}{
		{"nginx", "installing", "nginx", serviceFailureStepPackageInstall},
		{"nginx", "queued", "nginx", serviceFailureStepPackageInstall},
		{"nginx", "configuring", "nginx", serviceFailureStepConfigure},
		{"nginx", "scanning", "nginx", serviceFailureStepVerify},
		{"webmail", "profile/webmail/preflight", "webmail", serviceFailureStepPreflight},
		{"webmail", "profile/webmail/postfix/installing", "postfix", serviceFailureStepPackageInstall},
		{"webmail", "profile/webmail/dovecot/starting", "dovecot", serviceFailureStepUnitStart},
		{"webmail", "profile/webmail/roundcube/configuring", "roundcube", serviceFailureStepConfigure},
		{"webmail", "profile/webmail/mail-stack", "webmail", serviceFailureStepConfigure},
		{"webmail", "profile/webmail/verifying", "webmail", serviceFailureStepVerify},
		{"webmail", "profile/webmail/unknown-phase", "webmail", ""},
		{"Bad ID", "installing", "", serviceFailureStepPackageInstall},
	} {
		component, step := serviceFailureComponentAndStep(tc.service, tc.phase)
		if component != tc.component || step != tc.step {
			t.Errorf("%s %s = %q/%q, want %q/%q", tc.service, tc.phase, component, step, tc.component, tc.step)
		}
	}
}

func TestServiceFailureDetailIsTheBoundedRedactedHostLine(t *testing.T) {
	pacman := errors.New("install profile service postfix: service install: package install failed: exit status 1: " +
		":: Synchronizing package databases...\n core downloading...\n" +
		"error: failed retrieving file 'core.db' from https://user:tok3n@mirror.example/arch/core/os/x86_64/core.db?sig=abc : Could not resolve host\n" +
		"error: failed to synchronize all databases (unexpected error)")
	detail := serviceFailureDetail(serviceFailureStepPackageInstall, pacman)
	if !strings.HasPrefix(detail, "error: failed retrieving file 'core.db' from https://mirror.example/...") ||
		strings.Contains(detail, "tok3n") || strings.Contains(detail, "sig=") {
		t.Fatalf("pacman detail=%q", detail)
	}
	apt := errors.New("service install: package install failed: exit status 100: Reading package lists...\n" +
		"E: Unable to locate package dovecot-lmtpd\nE: Sub-process /usr/bin/dpkg returned an error code (1)")
	if got := serviceFailureDetail(serviceFailureStepPackageInstall, apt); got != "E: Unable to locate package dovecot-lmtpd" {
		t.Fatalf("apt detail=%q", got)
	}
	secret := errors.New("start configured mail service: token=abcdef password=hunter2 {SSHA512}c2FsdA== $6$salt$hash\nsecond line")
	got := serviceFailureDetail(serviceFailureStepUnitStart, secret)
	for _, leaked := range []string{"abcdef", "hunter2", "c2FsdA", "$6$", "second line"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("detail leaked %q: %q", leaked, got)
		}
	}
	long := errors.New("start configured mail service: " + strings.Repeat("x", 400))
	if got := serviceFailureDetail(serviceFailureStepUnitStart, long); len([]rune(got)) > serviceFailureDetailLimit {
		t.Fatalf("detail is not bounded: %d runes", len([]rune(got)))
	}
	if got := serviceFailureDetail(serviceFailureStepVerify, nil); got != "" {
		t.Fatalf("nil cause detail=%q", got)
	}
}

func TestServiceFailureGuidanceOnlyForInstallFailures(t *testing.T) {
	lease := operationFailure(errCodeServiceOperationLeaseLost, "lease", errors.New("error: raw"))
	annotateServiceOperationFailure("nginx", "installing", lease)
	if lease.Component != "" || lease.Step != "" || lease.Detail != "" {
		t.Fatalf("non-install failure was annotated: %+v", lease)
	}
	if result := withServiceFailureGuidance(serviceOperationResult{"success": false}, lease); result["failure"] != nil {
		t.Fatalf("non-install failure stored guidance: %v", result)
	}
}

func TestServiceFailureGuidanceIsDurableOnTheFailedRowOnly(t *testing.T) {
	f := newServiceOperationTestFixture(t)
	ctx := context.Background()
	op, err := f.panel.createServiceOperation(ctx, serviceOperationKindMailProfileInstall, "webmail", "", serviceOperationActor{UserID: f.userID})
	if err != nil {
		t.Fatal(err)
	}
	phase := mailProfilePhase("webmail", "dovecot", "starting")
	if err := f.panel.markServiceOperationRunning(ctx, op.ID, phase); err != nil {
		t.Fatal(err)
	}
	failure := serviceInstallFailure(errors.New("install profile service dovecot: start configured mail service: Job for dovecot.service failed because the control process exited with error code."))
	annotateServiceOperationFailure(op.ServiceID, phase, failure)
	result := withServiceFailureGuidance(newMailProfileResult(mustMailProfile(t, "webmail")), failure)
	if err := f.panel.finishServiceOperationFailed(ctx, op.ID, phase, result, failure); err != nil {
		t.Fatal(err)
	}
	loaded, err := f.panel.serviceOperationByID(ctx, op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Error == nil || loaded.Error.Code != "service_install_failed" ||
		loaded.Error.Component != "dovecot" || loaded.Error.Step != serviceFailureStepUnitStart ||
		loaded.Error.Detail != "start configured mail service: Job for dovecot.service failed because the control process exited with error code." {
		t.Fatalf("durable guidance=%+v", loaded.Error)
	}

	// A malformed or foreign "failure" value never changes the code/message.
	if _, err := f.database.GetDB().Exec(`UPDATE service_operations SET result_json=? WHERE id=?`,
		`{"success":false,"failure":{"component":"../etc","step":"rm","detail":"x"}}`, op.ID); err != nil {
		t.Fatal(err)
	}
	loaded, err = f.panel.serviceOperationByID(ctx, op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Error == nil || loaded.Error.Component != "" || loaded.Error.Step != "" || loaded.Error.Detail != "" ||
		loaded.Error.Code != "service_install_failed" {
		t.Fatalf("malformed guidance was accepted: %+v", loaded.Error)
	}
}

func TestMailProfilePackageFailureReportsComponentStepAndPackageManagerLine(t *testing.T) {
	fixture, agent := newMailProfileTestFixture(t)
	agent.serviceOperationTestAgent.mu.Lock()
	agent.serviceOperationTestAgent.installError = "package install failed: exit status 1: resolving dependencies...\n" +
		"error: target not found: dovecot-pigeonhole\nerror: could not prepare transaction"
	agent.serviceOperationTestAgent.mu.Unlock()
	recorder, queued := postMailProfile(t, fixture, "webmail", mustServiceOperationRequestID(t))
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("profile status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	failed, body := waitForServiceOperation(t, fixture.panel, fixture.userID, queued.ID, serviceOperationFailed)
	if failed.Error == nil || failed.Error.Code != "service_install_failed" ||
		failed.Error.Message != "The service could not be installed and verified." ||
		failed.Error.Component != "postfix" || failed.Error.Step != serviceFailureStepPackageInstall ||
		failed.Error.Detail != "error: target not found: dovecot-pigeonhole" {
		t.Fatalf("failed profile error=%+v", failed.Error)
	}
	if strings.Contains(body, "resolving dependencies") || strings.Contains(body, "could not prepare") {
		t.Fatalf("more than the reason line reached the API: %s", body)
	}
}

func TestServerSetupChildFailureCarriesInstallGuidance(t *testing.T) {
	step := serverSetupExecutionStep{serverSetupPlanStep: serverSetupPlanStep{ID: "05-mail_profile", Kind: "mail_profile", Target: "webmail"}}
	got := serverSetupFailureForStep(step, &serverSetupChildFailure{
		Code: "service_install_failed", Message: "The service could not be installed and verified.",
		Component: "dovecot", Step: serviceFailureStepUnitStart, Detail: "Job for dovecot.service failed",
	})
	if got.Code != "service_install_failed" || got.Component != "dovecot" ||
		got.Step != serviceFailureStepUnitStart || got.Detail != "Job for dovecot.service failed" {
		t.Fatalf("setup failure=%+v", got)
	}
}

func mustMailProfile(t *testing.T, id string) mailProfileDefinition {
	t.Helper()
	profile, ok := mailProfileByID(id)
	if !ok {
		t.Fatalf("unknown profile %s", id)
	}
	return profile
}

// upd1 finding P2: on Arch the web_mail plan was admitted and then stopped at
// the Dovecot configure step. The reviewed plan now refuses mail there, before
// any mutation, with a typed blocker; web hosting stays admissible.
func TestServerSetupRefusesMailOnArchBeforeAnyMutation(t *testing.T) {
	for _, purpose := range []string{"web_mail", "web"} {
		t.Run(purpose, func(t *testing.T) {
			f, state := setupOperationFixture(t)
			f.panel.pkgFamilyMu.Lock()
			f.panel.pkgFamilyVal = "pacman"
			f.panel.pkgFamilyMu.Unlock()
			caps := []string{transport.AgentCapabilityMailHostCertificateV1, transport.AgentCapabilityMailTLSSyncV2, transport.AgentCapabilityFirewallApplyV2, transport.AgentCapabilityPanelCertificateIssueV2}
			f.agent.versionCapabilities = &caps
			state.Draft.Purpose = purpose
			if purpose == "web_mail" {
				state.Draft.MailHostname = "mail.example.test"
			}
			plan, err := f.panel.buildServerSetupPlan(context.Background(), state, serviceOperationActor{UserID: f.userID})
			if err != nil {
				t.Fatal(err)
			}
			if got := f.panel.managedServiceHostProfile().PackageFamily; got != "pacman" {
				t.Fatalf("fixture host family=%q", got)
			}
			refused := slices.Contains(plan.Blockers, "server_setup_service_unsupported:dovecot")
			if purpose == "web_mail" && (!refused || plan.CanStart) {
				t.Fatalf("Arch web_mail plan was admitted: can_start=%v blockers=%v", plan.CanStart, plan.Blockers)
			}
			if purpose == "web" && refused {
				t.Fatalf("Arch web plan was refused for mail: %v", plan.Blockers)
			}
			if f.agent.installCalls.Load() != 0 {
				t.Fatal("plan review installed a package")
			}
		})
	}
}

func TestDovecotInstallIsClosedOnPacmanWithAnActionableReason(t *testing.T) {
	dovecot := core.GetManagedServiceByID("dovecot")
	if reason := core.ManagedServiceInstallDisabledReason(dovecot, "apt"); reason != "" {
		t.Fatalf("apt dovecot is blocked: %s", reason)
	}
	kind, reason := core.ManagedServiceInstallBlockForHost(dovecot, core.ManagedServiceHostProfile{PackageFamily: "pacman"})
	if kind != core.ManagedServiceInstallBlockDistribution || reason != core.DovecotPacmanLayoutReason {
		t.Fatalf("pacman dovecot block=%v %q", kind, reason)
	}
	for _, part := range []string{"Arch Linux", "/etc/dovecot/dovecot.conf", "server administrator", "Web hosting", "operating system's own tools"} {
		if !strings.Contains(reason, part) {
			t.Fatalf("reason lacks %q: %s", part, reason)
		}
	}
	// Observation is unchanged: an existing Arch Dovecot stays recognisable.
	if pkgs := dovecot.Packages["pacman"]; !slices.Equal(pkgs, []string{"dovecot"}) {
		t.Fatalf("pacman observation map changed: %v", pkgs)
	}
	for _, id := range []string{"postfix", "nginx", "php-fpm", "mariadb", "certbot", "rspamd"} {
		if reason := core.ManagedServiceInstallDisabledReason(core.GetManagedServiceByID(id), "pacman"); reason != "" {
			t.Fatalf("%s is newly blocked on pacman: %s", id, reason)
		}
	}
}
