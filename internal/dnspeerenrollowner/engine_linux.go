//go:build linux

package dnspeerenrollowner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/bindpeerinspector"
	"github.com/alicelik/celikpanel/internal/dnspeerproof"
	"github.com/alicelik/celikpanel/internal/dnspeertransport"
	"github.com/alicelik/celikpanel/internal/pdnspeerinspector"
	"golang.org/x/crypto/ssh"
)

// Engine selects the native DNS server whose optional read-only inspector
// channel is installed on this secondary. BIND is the historical default.
type Engine string

const (
	EngineBIND Engine = "bind"
	EnginePDNS Engine = "pdns"
)

// ParseEngine accepts only the two reviewed native engines.
func ParseEngine(value string) (Engine, error) {
	switch Engine(value) {
	case EngineBIND, EnginePDNS:
		return Engine(value), nil
	}
	return "", errors.New("engine must be bind or pdns")
}

// PowerDNS secondary layout. Only the policy path is dictated by
// pdnspeerinspector. The remaining files carry engine-specific content (the
// inspector binary, fixed command, sudoers command and forced command), so
// they have their own paths: each engine's install, status and revoke then
// recognise only their own files and can detect the other engine's leftovers.
// The locked SSH account name is shared: one secondary hosts one engine
// channel (enforced below), and the Match block, pinned key and sudoers entry
// confine that account to the enrolled engine's inspector.
const (
	PDNSHomePath           = "/var/lib/pdns-peer-inspector"
	PDNSInspectorPath      = "/usr/local/libexec/celikpanel-pdns-peer-inspect"
	PDNSWrapperPath        = "/usr/local/libexec/celikpanel-pdns-peer-inspect-wrapper"
	PDNSPolicyPath         = pdnspeerinspector.OwnerPolicyPath
	PDNSAuthorizedKeysPath = "/etc/ssh/pdns-peer-inspector/authorized_keys"
	PDNSSSHDConfigPath     = "/etc/pdns-peer-inspector/sshd-match.conf"
	PDNSSSHDBackupPath     = "/etc/pdns-peer-inspector/sshd-config-original"
	PDNSSSHDReceiptPath    = "/etc/pdns-peer-inspector/sshd-config-receipt.json"
	PDNSSudoersPath        = "/etc/sudoers.d/celikpanel-pdns-peer-inspector"
	pdnsSSHDReceiptSchema  = "celikpanel-pdns-peer-sshd-include/v1"
)

// ownerPolicy is the engine-neutral view of a reviewed inspector policy.
type ownerPolicy struct {
	PrimaryIP, PeerIP, CatalogName, CatalogAccount string
	viewOK                                         bool
}

type profile struct {
	engine        Engine
	label         string
	cliArg        string // appended to owner commands in guidance; empty for BIND
	inputs        string // resume inputs named in guidance
	installInputs string // install inputs named in guidance
	account       string
	home          string
	inspector     string
	wrapper       string
	policyPath    string
	authorized    string
	sshdConfig    string
	sshdBackup    string
	sshdReceipt   string
	sudoers       string
	ownerDir      string
	keyDir        string
	fixedCommand  string
	serviceUnit   string
	receiptSchema string
	// restoreSSHDOnRevoke is false for BIND: its published revoke contract
	// keeps the owner SSH configuration and only disables the key.
	restoreSSHDOnRevoke bool
	renderPolicy        func(SecondaryOptions) []byte
	readPolicy          func() (ownerPolicy, error)
	validateExtra       func(SecondaryOptions) error
}

var bindProfile = &profile{
	engine: EngineBIND, label: "BIND", cliArg: "",
	inputs: "reviewed pair, key and inspector", installInputs: "reviewed pair and key",
	account: Account, home: HomePath, inspector: InspectorPath, wrapper: WrapperPath,
	policyPath: PolicyPath, authorized: AuthorizedKeysPath, sshdConfig: SSHDConfigPath,
	sshdBackup: SSHDBackupPath, sshdReceipt: SSHDReceiptPath, sudoers: SudoersPath,
	ownerDir: "/etc/bind-peer-inspector", keyDir: "/etc/ssh/bind-peer-inspector",
	fixedCommand: dnspeertransport.FixedCommand, serviceUnit: "named.service",
	receiptSchema: sshdReceiptSchema,
	renderPolicy: func(o SecondaryOptions) []byte {
		raw, _ := json.Marshal(bindpeerinspector.OwnerPolicyV1{Schema: bindpeerinspector.PolicySchemaV1,
			PrimaryIP: o.PrimaryIP, PeerIP: o.PeerIP, CatalogName: o.CatalogName, View: dnspeerproof.DefaultView})
		return raw
	},
	readPolicy: func() (ownerPolicy, error) {
		p, _, err := (bindpeerinspector.OwnerPolicyReader{}).Read(context.Background())
		if err != nil {
			return ownerPolicy{}, err
		}
		return ownerPolicy{PrimaryIP: p.PrimaryIP, PeerIP: p.PeerIP, CatalogName: p.CatalogName,
			viewOK: p.View == dnspeerproof.DefaultView}, nil
	},
	validateExtra: func(o SecondaryOptions) error {
		if o.CatalogAccount != "" {
			return errors.New("a catalog account applies only to a PowerDNS secondary")
		}
		return nil
	},
}

var pdnsProfile = &profile{
	engine: EnginePDNS, label: "PowerDNS", cliArg: " --engine pdns",
	inputs:        "reviewed pair, catalog account, key and inspector",
	installInputs: "reviewed pair, catalog account and key",
	account:       Account, home: PDNSHomePath, inspector: PDNSInspectorPath, wrapper: PDNSWrapperPath,
	policyPath: PDNSPolicyPath, authorized: PDNSAuthorizedKeysPath, sshdConfig: PDNSSSHDConfigPath,
	sshdBackup: PDNSSSHDBackupPath, sshdReceipt: PDNSSSHDReceiptPath, sudoers: PDNSSudoersPath,
	ownerDir: "/etc/pdns-peer-inspector", keyDir: "/etc/ssh/pdns-peer-inspector",
	fixedCommand: dnspeertransport.FixedPowerDNSCommand, serviceUnit: "pdns.service",
	receiptSchema: pdnsSSHDReceiptSchema, restoreSSHDOnRevoke: true,
	renderPolicy: func(o SecondaryOptions) []byte {
		raw, _ := json.Marshal(pdnspeerinspector.OwnerPolicyV1{Schema: pdnspeerinspector.PolicySchemaV1,
			PrimaryIP: o.PrimaryIP, PeerIP: o.PeerIP, CatalogName: o.CatalogName, CatalogAccount: o.CatalogAccount})
		return raw
	},
	readPolicy: func() (ownerPolicy, error) {
		p, _, err := (pdnspeerinspector.OwnerPolicyReader{}).Read(context.Background())
		if err != nil {
			return ownerPolicy{}, err
		}
		return ownerPolicy{PrimaryIP: p.PrimaryIP, PeerIP: p.PeerIP, CatalogName: p.CatalogName,
			CatalogAccount: p.CatalogAccount,
			viewOK:         p.Schema == pdnspeerinspector.PolicySchemaV1 && validCatalogAccount(p.CatalogAccount)}, nil
	},
	validateExtra: func(o SecondaryOptions) error {
		if !validCatalogAccount(o.CatalogAccount) {
			return errors.New("the PowerDNS catalog CONSUMER zone account is required: 1-128 characters of a-z, 0-9, '-' or '_', exactly as stored for the catalog zone")
		}
		return nil
	},
}

// validCatalogAccount mirrors pdnspeerinspector.OwnerPolicyV1.ValidateRequest.
func validCatalogAccount(account string) bool {
	if account == "" || len(account) > 128 {
		return false
	}
	for _, c := range account {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '_' {
			return false
		}
	}
	return true
}

func profileFor(engine Engine) (*profile, error) {
	switch engine {
	case EngineBIND:
		return bindProfile, nil
	case EnginePDNS:
		return pdnsProfile, nil
	}
	return nil, errors.New("engine must be bind or pdns")
}

func (p *profile) other() *profile {
	if p.engine == EngineBIND {
		return pdnsProfile
	}
	return bindProfile
}

func (p *profile) sshdInclude() string { return "\nInclude " + p.sshdConfig + "\n" }

// otherEngineArtifactAt names the first file of the other engine's channel
// or retained evidence below root. Revocation leaves evidence in place, so
// its presence still means an owner decision is outstanding.
func (p *profile) otherEngineArtifactAt(root string) (string, error) {
	o := p.other()
	for _, name := range []string{o.authorized, o.policyPath, o.sshdReceipt, o.sshdBackup, o.sshdConfig, o.sudoers, o.wrapper, o.inspector} {
		if _, err := os.Lstat(filepath.Join(root, name)); err == nil {
			return name, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return name, err
		}
	}
	return "", nil
}

func (p *profile) refuseOtherEngine() error {
	o := p.other()
	name, err := p.otherEngineArtifactAt("/")
	if err != nil {
		return fmt.Errorf("%s peer inspector file %s cannot be inspected; review it locally before enrolling %s: %w", o.label, name, p.label, err)
	}
	if name != "" {
		return fmt.Errorf("a %s peer inspector channel or its retained evidence exists at %s; one secondary hosts one native engine channel. Run secondary-status --engine %s, run secondary-revoke --engine %s if it is still authorized, and remove that engine's retained files after owner review before enrolling %s",
			o.label, name, o.engine, o.engine, p.label)
	}
	return nil
}

func (p *profile) validateOptions(o SecondaryOptions) (ssh.PublicKey, []byte, error) {
	key, bin, err := validateOptions(o)
	if err != nil {
		return nil, nil, err
	}
	if err := p.validateExtra(o); err != nil {
		return nil, nil, err
	}
	return key, bin, nil
}

func (p *profile) validPolicyAuthority(policy ownerPolicy) bool {
	derived, err := binddns.CatalogDomain(policy.PrimaryIP)
	peer := net.ParseIP(policy.PeerIP)
	return err == nil && derived == policy.CatalogName && peer != nil && peer.To4() != nil &&
		peer.String() == policy.PeerIP && policy.PeerIP != policy.PrimaryIP && peer.IsGlobalUnicast() && policy.viewOK
}

// InstallSecondaryEngine is InstallSecondary for an explicit native engine.
func InstallSecondaryEngine(engine Engine, o SecondaryOptions) error {
	p, err := profileFor(engine)
	if err != nil {
		return err
	}
	return p.enrollSecondary(o, false)
}

// ResumeSecondaryEngine is ResumeSecondary for an explicit native engine.
func ResumeSecondaryEngine(engine Engine, o SecondaryOptions) error {
	p, err := profileFor(engine)
	if err != nil {
		return err
	}
	return p.enrollSecondary(o, true)
}

// SecondaryStatusEngine is SecondaryStatus for an explicit native engine.
func SecondaryStatusEngine(engine Engine) (Status, error) {
	p, err := profileFor(engine)
	if err != nil {
		return Status{}, err
	}
	return p.secondaryStatus()
}

// RevokeResult reports what happened to the owner SSH configuration after
// the authorized key was removed. The key removal itself is the revocation.
type RevokeResult struct {
	// SSHConfig is retained (BIND contract), restored, restored_not_reloaded,
	// original (already the recorded original), preserved_owner_edit or
	// restore_failed.
	SSHConfig string
	Reason    string
}

// RevokeSecondaryEngine disables the engine's fixed authorized key. For
// PowerDNS it then returns the recorded owner sshd_config only when the live
// file is exactly the recorded publication; an owner edit is preserved.
func RevokeSecondaryEngine(engine Engine) (RevokeResult, error) {
	p, err := profileFor(engine)
	if err != nil {
		return RevokeResult{}, err
	}
	return p.revokeSecondary()
}
