package main

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/alicelik/celikpanel/internal/dnsunitidentity"
	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/pdnsvendor"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	certifiedPDNSUnitPath           = pdnsvendor.UnitPath
	certifiedPDNSExecArgv           = pdnsvendor.ExecArgv
	certifiedDebian13PDNSVendorUnit = pdnsvendor.CertifiedDebian13Unit
	certifiedDebianPDNSAfter        = pdnsvendor.CertifiedDebianAfter
	certifiedUbuntuPDNSAfter        = pdnsvendor.CertifiedUbuntuAfter
)

var certifiedUbuntu2404PDNSVendorUnit = pdnsvendor.CertifiedUbuntu2404Unit

func certifyAPTPDNSCapabilities(profile hostplatform.Profile) error {
	if profile.PackageManager != hostplatform.PackageManagerAPT ||
		profile.DistroFamily != hostplatform.DistroFamilyDebian ||
		profile.ServiceManager != hostplatform.ServiceManagerSystemd {
		return errors.New(
			"PowerDNS authority requires a verified APT package ecosystem and systemd",
		)
	}
	return nil
}

func runCertifiedPDNSTargetMutation(
	profile hostplatform.Profile,
	mutation func() (transport.SwitchDNSEngineV1Response, error),
) (transport.SwitchDNSEngineV1Response, error) {
	if mutation == nil {
		return transport.SwitchDNSEngineV1Response{},
			errors.New("PowerDNS target mutation callback is required")
	}
	if err := certifyAPTPDNSCapabilities(profile); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	return mutation()
}

func validatePDNSVendorUnitIdentity(identity dnsUnitIdentity) error {
	return dnsunitidentity.ValidateAPTPDNSVendorIdentity(identity)
}

type pdnsInactiveTargetSnapshot struct {
	state      bindInstallUnitState
	processes  dnsUnitProcesses
	identity   dnsUnitIdentity
	vendorUnit bindSecureFileIdentity
}

type pdnsSealedTargetOps struct {
	inspectState     func() (bindInstallUnitState, error)
	inspectIdentity  func() (dnsUnitIdentity, error)
	inspectVendor    func() (bindSecureFileIdentity, error)
	inspectProcesses func() (dnsUnitProcesses, error)
	verifyMask       func() error
}

func verifyPDNSTargetSealedBeforeUnmask(
	ctx context.Context,
	profile hostplatform.Profile,
	systemctl string,
) error {
	if ctx == nil || systemctl == "" {
		return errors.New(
			"PowerDNS pre-unmask proof requires a context and systemctl",
		)
	}
	proofCtx, cancel := context.WithTimeout(ctx, dnsRuntimeInspectionTimeout)
	defer cancel()
	guard := dnsSystemdStateGuard(systemctl)
	return verifyPDNSTargetSealedBeforeUnmaskWithOps(
		profile,
		pdnsSealedTargetOps{
			inspectState: func() (bindInstallUnitState, error) {
				return guard.inspect(proofCtx, "pdns.service")
			},
			inspectIdentity: func() (dnsUnitIdentity, error) {
				return inspectDNSUnitIdentity(
					proofCtx, systemctl, "pdns.service",
				)
			},
			inspectVendor: func() (bindSecureFileIdentity, error) {
				return inspectHostPDNSVendorUnit(proofCtx, profile)
			},
			inspectProcesses: func() (dnsUnitProcesses, error) {
				return inspectDNSUnitProcesses(
					proofCtx, systemctl, "pdns.service",
				)
			},
			verifyMask: func() error {
				return verifyExactPersistentServiceMask("pdns.service")
			},
		},
	)
}

func verifyPDNSTargetSealedBeforeUnmaskWithOps(
	profile hostplatform.Profile,
	ops pdnsSealedTargetOps,
) error {
	if err := certifyAPTPDNSCapabilities(profile); err != nil {
		return err
	}
	if ops.inspectState == nil || ops.inspectIdentity == nil ||
		ops.inspectVendor == nil || ops.inspectProcesses == nil ||
		ops.verifyMask == nil {
		return errors.New("invalid PowerDNS pre-unmask proof operations")
	}
	capture := func() (pdnsInactiveTargetSnapshot, error) {
		state, err := ops.inspectState()
		if err != nil {
			return pdnsInactiveTargetSnapshot{}, err
		}
		masked := state.loadState == "masked" &&
			state.activeState == "inactive" &&
			state.unitFileState == "masked"
		unmasked := state.loadState == "loaded" &&
			state.activeState == "inactive" &&
			(state.unitFileState == "disabled" ||
				state.unitFileState == "enabled")
		if !masked && !unmasked {
			return pdnsInactiveTargetSnapshot{},
				errors.New("pdns.service has no exact sealed pre-unmask state")
		}
		vendor, err := ops.inspectVendor()
		if err != nil {
			return pdnsInactiveTargetSnapshot{}, err
		}
		processes, err := ops.inspectProcesses()
		if err != nil {
			return pdnsInactiveTargetSnapshot{}, err
		}
		if err := verifyDNSUnitProcessesStopped(processes); err != nil {
			return pdnsInactiveTargetSnapshot{},
				fmt.Errorf("pdns.service is not stopped before unmask: %w", err)
		}
		identity := dnsUnitIdentity{}
		if masked {
			if err := ops.verifyMask(); err != nil {
				return pdnsInactiveTargetSnapshot{}, err
			}
		} else {
			identity, err = ops.inspectIdentity()
			if err != nil {
				return pdnsInactiveTargetSnapshot{}, err
			}
			if err := validatePDNSVendorUnitIdentity(identity); err != nil {
				return pdnsInactiveTargetSnapshot{}, err
			}
		}
		return pdnsInactiveTargetSnapshot{
			state: state, processes: processes,
			identity: identity, vendorUnit: vendor,
		}, nil
	}
	before, err := capture()
	if err != nil {
		return err
	}
	after, err := capture()
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(after, before) {
		return errors.New(
			"PowerDNS sealed target changed during pre-unmask verification",
		)
	}
	return nil
}

type pdnsInactiveTargetOps struct {
	inspectState     func() (bindInstallUnitState, error)
	inspectIdentity  func() (dnsUnitIdentity, error)
	inspectVendor    func() (bindSecureFileIdentity, error)
	inspectProcesses func() (dnsUnitProcesses, error)
}

func inspectVerifiedPDNSInactiveTarget(
	ctx context.Context,
	profile hostplatform.Profile,
	systemctl string,
	allowedUnitFileStates ...string,
) (pdnsInactiveTargetSnapshot, error) {
	if ctx == nil || systemctl == "" {
		return pdnsInactiveTargetSnapshot{},
			errors.New("PowerDNS inactive target proof requires a context and systemctl")
	}
	proofCtx, cancel := context.WithTimeout(ctx, dnsRuntimeInspectionTimeout)
	defer cancel()
	guard := dnsSystemdStateGuard(systemctl)
	return inspectVerifiedPDNSInactiveTargetWithOps(
		profile, allowedUnitFileStates,
		pdnsInactiveTargetOps{
			inspectState: func() (bindInstallUnitState, error) {
				return guard.inspect(proofCtx, "pdns.service")
			},
			inspectIdentity: func() (dnsUnitIdentity, error) {
				return inspectDNSUnitIdentity(
					proofCtx, systemctl, "pdns.service",
				)
			},
			inspectVendor: func() (bindSecureFileIdentity, error) {
				return inspectHostPDNSVendorUnit(proofCtx, profile)
			},
			inspectProcesses: func() (dnsUnitProcesses, error) {
				return inspectDNSUnitProcesses(
					proofCtx, systemctl, "pdns.service",
				)
			},
		},
	)
}

func inspectVerifiedPDNSInactiveTargetWithOps(
	profile hostplatform.Profile,
	allowedUnitFileStates []string,
	ops pdnsInactiveTargetOps,
) (pdnsInactiveTargetSnapshot, error) {
	if err := certifyAPTPDNSCapabilities(profile); err != nil {
		return pdnsInactiveTargetSnapshot{}, err
	}
	allowed := make(map[string]bool, len(allowedUnitFileStates))
	for _, state := range allowedUnitFileStates {
		if (state != "disabled" && state != "enabled") || allowed[state] {
			return pdnsInactiveTargetSnapshot{},
				errors.New("invalid PowerDNS inactive unit-file state contract")
		}
		allowed[state] = true
	}
	if len(allowed) == 0 || ops.inspectState == nil ||
		ops.inspectIdentity == nil || ops.inspectVendor == nil ||
		ops.inspectProcesses == nil {
		return pdnsInactiveTargetSnapshot{},
			errors.New("invalid PowerDNS inactive target proof operations")
	}
	capture := func() (pdnsInactiveTargetSnapshot, error) {
		state, err := ops.inspectState()
		if err != nil {
			return pdnsInactiveTargetSnapshot{}, err
		}
		if state.loadState != "loaded" || state.activeState != "inactive" ||
			!allowed[state.unitFileState] {
			return pdnsInactiveTargetSnapshot{}, fmt.Errorf(
				"pdns.service is not exactly loaded, inactive, and %v",
				allowedUnitFileStates,
			)
		}
		identity, err := ops.inspectIdentity()
		if err != nil {
			return pdnsInactiveTargetSnapshot{}, err
		}
		if err := validatePDNSVendorUnitIdentity(identity); err != nil {
			return pdnsInactiveTargetSnapshot{}, err
		}
		vendor, err := ops.inspectVendor()
		if err != nil {
			return pdnsInactiveTargetSnapshot{}, err
		}
		processes, err := ops.inspectProcesses()
		if err != nil {
			return pdnsInactiveTargetSnapshot{}, err
		}
		if err := verifyDNSUnitProcessesStopped(processes); err != nil {
			return pdnsInactiveTargetSnapshot{},
				fmt.Errorf("pdns.service is not stopped before activation: %w", err)
		}
		return pdnsInactiveTargetSnapshot{
			state: state, processes: processes,
			identity: identity, vendorUnit: vendor,
		}, nil
	}
	before, err := capture()
	if err != nil {
		return pdnsInactiveTargetSnapshot{}, err
	}
	after, err := capture()
	if err != nil {
		return pdnsInactiveTargetSnapshot{}, err
	}
	if !reflect.DeepEqual(after, before) {
		return pdnsInactiveTargetSnapshot{},
			errors.New("PowerDNS inactive target changed during exact verification")
	}
	return after, nil
}
