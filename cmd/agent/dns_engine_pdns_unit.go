package main

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	certifiedPDNSUnitPath = "/usr/lib/systemd/system/pdns.service"
	certifiedPDNSExecArgv = "/usr/sbin/pdns_server --guardian=no --daemon=no --disable-syslog --log-timestamp=no --write-pid=no"
)

const certifiedDebian13PDNSVendorUnit = "[Unit]\n" +
	"Description=PowerDNS Authoritative Server\n" +
	"Documentation=man:pdns_server(1) man:pdns_control(1)\n" +
	"Documentation=https://doc.powerdns.com\n" +
	"Wants=network-online.target\n" +
	"After=network-online.target mysql.service mysqld.service postgresql.service slapd.service mariadb.service time-sync.target\n\n" +
	"[Service]\n" +
	"ExecStart=" + certifiedPDNSExecArgv + "\n" +
	"SyslogIdentifier=pdns_server\n" +
	"User=pdns\nGroup=pdns\nType=notify\nRestart=on-failure\nRestartSec=1\n" +
	"StartLimitInterval=0\nRuntimeDirectory=pdns\n\n" +
	"# Sandboxing\n" +
	"CapabilityBoundingSet=CAP_NET_BIND_SERVICE CAP_CHOWN\n" +
	"AmbientCapabilities=CAP_NET_BIND_SERVICE CAP_CHOWN\n" +
	"LockPersonality=true\nNoNewPrivileges=true\nPrivateDevices=true\nPrivateTmp=true\n" +
	"# Setting PrivateUsers=true prevents us from opening our sockets\n" +
	"ProtectClock=true\nProtectControlGroups=true\nProtectHome=true\nProtectHostname=true\n" +
	"ProtectKernelLogs=true\nProtectKernelModules=true\nProtectKernelTunables=true\n" +
	"# ProtectSystem=full will disallow write access to /etc and /usr, possibly\n" +
	"# not being able to write slaved-zones into sqlite3 or zonefiles.\n" +
	"ProtectSystem=full\nRestrictAddressFamilies=AF_UNIX AF_INET AF_INET6\n" +
	"RestrictNamespaces=true\nRestrictRealtime=true\nRestrictSUIDSGID=true\n" +
	"SystemCallArchitectures=native\n" +
	"SystemCallFilter=~ @clock @debug @module @mount @raw-io @reboot @swap @cpu-emulation @obsolete\n" +
	"ProtectProc=invisible\nPrivateIPC=true\nRemoveIPC=true\nDevicePolicy=closed\n" +
	"# Not enabled by default because it does not play well with LuaJIT\n" +
	"# MemoryDenyWriteExecute=true\n\n" +
	"[Install]\nWantedBy=multi-user.target\n"

const (
	certifiedDebianPDNSAfter = "After=network-online.target mysql.service mysqld.service postgresql.service slapd.service mariadb.service time-sync.target"
	certifiedUbuntuPDNSAfter = "After=network-online.target mysqld.service postgresql.service slapd.service mariadb.service time-sync.target"
)

// Ubuntu 24.04 and Debian 13 ship the same hardened pdns.service contract.
// Ubuntu omits only the obsolete mysql.service ordering alias. Select between
// these reviewed package artifacts by exact bytes, never by os-release name.
var certifiedUbuntu2404PDNSVendorUnit = strings.Replace(
	certifiedDebian13PDNSVendorUnit,
	certifiedDebianPDNSAfter,
	certifiedUbuntuPDNSAfter,
	1,
)

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
	if identity.ID != "pdns.service" ||
		!reflect.DeepEqual(identity.Names, []string{"pdns.service"}) ||
		identity.FragmentPath != certifiedPDNSUnitPath ||
		len(identity.DropInPaths) != 0 || identity.SourcePath != "" ||
		identity.Transient != "no" ||
		identity.ExecStartPath != "/usr/sbin/pdns_server" ||
		identity.ExecStartArgv != certifiedPDNSExecArgv {
		return errors.New(
			"pdns.service does not resolve to the exact certified vendor identity",
		)
	}
	return nil
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
