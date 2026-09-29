//go:build linux

// dns-peer-enroll is an explicit, local, owner-operated enrollment path for
// optional native BIND or PowerDNS secondary deletion proof. Ordinary AXFR
// needs none of it. Every subcommand accepts --engine bind|pdns (default bind).
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/alicelik/celikpanel/internal/dnspeerenrollment"
	"github.com/alicelik/celikpanel/internal/dnspeerenrollowner"
	"github.com/alicelik/celikpanel/internal/pdnspeerenrollment"
)

const usage = "usage: dns-peer-enroll primary-prepare|primary-activate|primary-status|primary-revoke|secondary-host-key|secondary-install|secondary-resume|secondary-status|secondary-revoke [--engine bind|pdns]"

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type prepared struct{ CredentialID, PublicKey, PublicKeySHA256 string }

type activation struct {
	CredentialID, PrimaryIP, PeerIP, CatalogName, SSHUsername, HostKeySHA256 string
}

type primaryRecord struct {
	EnrollmentID, PrimaryIP, PeerIP, CatalogName, HostKeySHA256, ClientPublicKeySHA256 string
}

// primaryBackend is one engine's primary-side owner writer and reader.
type primaryBackend struct {
	prepare  func() (prepared, error)
	activate func(activation) (string, error)
	// status returns disabled=true only when the enrollment file is absent.
	status func() (primaryRecord, bool, error)
	revoke func() error
}

type secondaryBackend struct {
	install func(dnspeerenrollowner.Engine, dnspeerenrollowner.SecondaryOptions) error
	resume  func(dnspeerenrollowner.Engine, dnspeerenrollowner.SecondaryOptions) error
	status  func(dnspeerenrollowner.Engine) (dnspeerenrollowner.Status, error)
	revoke  func(dnspeerenrollowner.Engine) (dnspeerenrollowner.RevokeResult, error)
	hostKey func() (string, error)
}

type backends struct {
	primary   map[dnspeerenrollowner.Engine]primaryBackend
	secondary secondaryBackend
}

func nativeBackends() backends {
	bind := primaryBackend{
		prepare: func() (prepared, error) {
			p, err := dnspeerenrollment.PrepareOwner()
			return prepared{p.CredentialID, p.PublicKey, p.PublicKeySHA256}, err
		},
		activate: func(a activation) (string, error) {
			r, err := dnspeerenrollment.ActivateOwner(dnspeerenrollment.OwnerActivation{
				CredentialID: a.CredentialID, PrimaryIP: a.PrimaryIP, PeerIP: a.PeerIP,
				CatalogName: a.CatalogName, SSHUsername: a.SSHUsername, HostKeySHA256: a.HostKeySHA256})
			return r.EnrollmentID, err
		},
		status: func() (primaryRecord, bool, error) {
			s, err := dnspeerenrollment.Read()
			if dnspeerenrollment.IsCode(err, dnspeerenrollment.Disabled) {
				return primaryRecord{}, true, nil
			}
			if err != nil {
				return primaryRecord{}, false, err
			}
			r := s.Record
			return primaryRecord{r.EnrollmentID, r.PrimaryIP, r.PeerIP, r.CatalogName, r.HostKeySHA256, r.ClientPublicKeySHA256}, false, nil
		},
		revoke: dnspeerenrollment.RevokeOwner,
	}
	pdns := primaryBackend{
		prepare: func() (prepared, error) {
			p, err := pdnspeerenrollment.PrepareOwner()
			return prepared{p.CredentialID, p.PublicKey, p.PublicKeySHA256}, err
		},
		activate: func(a activation) (string, error) {
			r, err := pdnspeerenrollment.ActivateOwner(pdnspeerenrollment.OwnerActivation{
				CredentialID: a.CredentialID, PrimaryIP: a.PrimaryIP, PeerIP: a.PeerIP,
				CatalogName: a.CatalogName, SSHUsername: a.SSHUsername, HostKeySHA256: a.HostKeySHA256})
			return r.EnrollmentID, err
		},
		status: func() (primaryRecord, bool, error) {
			s, err := pdnspeerenrollment.Read()
			if pdnspeerenrollment.IsCode(err, pdnspeerenrollment.Disabled) {
				return primaryRecord{}, true, nil
			}
			if err != nil {
				return primaryRecord{}, false, err
			}
			r := s.Record
			return primaryRecord{r.EnrollmentID, r.PrimaryIP, r.PeerIP, r.CatalogName, r.HostKeySHA256, r.ClientPublicKeySHA256}, false, nil
		},
		revoke: pdnspeerenrollment.RevokeOwner,
	}
	return backends{
		primary: map[dnspeerenrollowner.Engine]primaryBackend{dnspeerenrollowner.EngineBIND: bind, dnspeerenrollowner.EnginePDNS: pdns},
		secondary: secondaryBackend{
			install: dnspeerenrollowner.InstallSecondaryEngine,
			resume:  dnspeerenrollowner.ResumeSecondaryEngine,
			status:  dnspeerenrollowner.SecondaryStatusEngine,
			revoke:  dnspeerenrollowner.RevokeSecondaryEngine,
			hostKey: dnspeerenrollowner.SecondaryHostKeySHA256,
		},
	}
}

// engineValue accepts --engine once; a repeated selector is ambiguous.
type engineValue struct {
	engine dnspeerenrollowner.Engine
	set    bool
}

func (v *engineValue) String() string { return string(v.engine) }
func (v *engineValue) Set(s string) error {
	if v.set {
		return errors.New("--engine given more than once")
	}
	engine, err := dnspeerenrollowner.ParseEngine(s)
	if err != nil {
		return err
	}
	v.engine, v.set = engine, true
	return nil
}

func newFlagSet(name string) (*flag.FlagSet, *engineValue) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	engine := &engineValue{engine: dnspeerenrollowner.EngineBIND}
	fs.Var(engine, "engine", "native secondary DNS engine: bind (default) or pdns")
	return fs, engine
}

// engineArg is appended to commands named in guidance. It is empty for BIND
// so historical BIND output stays unchanged.
func engineArg(engine dnspeerenrollowner.Engine) string {
	if engine == dnspeerenrollowner.EnginePDNS {
		return " --engine pdns"
	}
	return ""
}

func engineLabel(engine dnspeerenrollowner.Engine) string {
	if engine == dnspeerenrollowner.EnginePDNS {
		return "PowerDNS"
	}
	return "BIND"
}

func otherEngine(engine dnspeerenrollowner.Engine) dnspeerenrollowner.Engine {
	if engine == dnspeerenrollowner.EnginePDNS {
		return dnspeerenrollowner.EngineBIND
	}
	return dnspeerenrollowner.EnginePDNS
}

func run(args []string, out io.Writer) error { return runWith(args, out, nativeBackends()) }

func runWith(args []string, out io.Writer, b backends) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	jsonOut := func(v any) error { return json.NewEncoder(out).Encode(v) }
	noArgs := func() (dnspeerenrollowner.Engine, error) {
		fs, engine := newFlagSet(args[0])
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
			return "", errors.New(args[0] + " accepts no arguments other than --engine bind|pdns")
		}
		return engine.engine, nil
	}
	switch args[0] {
	case "primary-prepare":
		engine, err := noArgs()
		if err != nil {
			return err
		}
		p, err := b.primary[engine].prepare()
		if err != nil {
			return err
		}
		return jsonOut(struct {
			State           string `json:"state"`
			CredentialID    string `json:"credential_id"`
			PublicKey       string `json:"public_key"`
			PublicKeySHA256 string `json:"public_key_sha256"`
			NextAction      string `json:"next_action"`
		}{"prepared", p.CredentialID, p.PublicKey, p.PublicKeySHA256,
			"Give only the displayed public key to the secondary owner; ask them to run secondary-install" + engineArg(engine) + " locally and return their independently reviewed Ed25519 SSH host-key SHA-256."})
	case "primary-activate":
		fs, engine := newFlagSet("primary-activate")
		credential := fs.String("credential-id", "", "prepared credential ID")
		primary := fs.String("primary-ip", "", "reviewed primary IPv4")
		peer := fs.String("peer-ip", "", "reviewed secondary IPv4")
		catalog := fs.String("catalog", "", "derived catalog name")
		host := fs.String("host-key-sha256", "", "independently reviewed secondary Ed25519 host key digest")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
			return errors.New("invalid primary-activate arguments")
		}
		id, err := b.primary[engine.engine].activate(activation{
			CredentialID: *credential, PrimaryIP: *primary, PeerIP: *peer,
			CatalogName: *catalog, SSHUsername: dnspeerenrollowner.Account,
			HostKeySHA256: *host,
		})
		if err != nil {
			return err
		}
		return jsonOut(struct {
			State        string `json:"state"`
			EnrollmentID string `json:"enrollment_id"`
			NextAction   string `json:"next_action"`
		}{"configured", id,
			"For an admitted pending DNS deletion, use that exact operation's recovery action; enrollment alone does not enable a blocked DNS topology, and status polling does not retry the mutation."})
	case "primary-status":
		engine, err := noArgs()
		if err != nil {
			return err
		}
		record, disabled, err := b.primary[engine].status()
		if err == nil && disabled {
			next := "Prepare an owner enrollment only if parentless deletion proof is required."
			other := otherEngine(engine)
			if _, otherDisabled, otherErr := b.primary[other].status(); otherErr != nil || !otherDisabled {
				next += " A " + engineLabel(other) + " enrollment file exists; only one engine can be enrolled, so run primary-status --engine " + string(other) + " first."
			}
			return jsonOut(map[string]string{"state": "disabled", "next_action": next})
		}
		if err != nil {
			return errors.New("peer enrollment is unknown; inspect local owner state before changing it")
		}
		return jsonOut(map[string]string{
			"state": "configured", "enrollment_id": record.EnrollmentID,
			"primary_ip": record.PrimaryIP, "peer_ip": record.PeerIP,
			"catalog_name":             record.CatalogName,
			"host_key_sha256":          record.HostKeySHA256,
			"client_public_key_sha256": record.ClientPublicKeySHA256,
		})
	case "primary-revoke":
		engine, err := noArgs()
		if err != nil {
			return err
		}
		if err := b.primary[engine].revoke(); err != nil {
			return err
		}
		return jsonOut(map[string]string{"state": "revoked", "next_action": "The secondary owner must also run secondary-revoke" + engineArg(engine) + ". Pending deletion stays pending until the exact request is reverified."})
	case "secondary-install", "secondary-resume":
		fs, engine := newFlagSet(args[0])
		primary := fs.String("primary-ip", "", "reviewed primary IPv4")
		peer := fs.String("peer-ip", "", "reviewed local secondary IPv4")
		catalog := fs.String("catalog", "", "derived catalog name")
		account := fs.String("catalog-account", "", "PowerDNS only: account of the catalog CONSUMER zone")
		public := fs.String("primary-public-key", "", "absolute path to reviewed primary public key")
		inspector := fs.String("inspector", "", "absolute path to the reviewed native inspector binary for the selected engine")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
			return errors.New("invalid secondary enrollment arguments")
		}
		accountSet := false
		fs.Visit(func(f *flag.Flag) { accountSet = accountSet || f.Name == "catalog-account" })
		if accountSet && engine.engine != dnspeerenrollowner.EnginePDNS {
			return errors.New("invalid secondary enrollment arguments")
		}
		apply := b.secondary.install
		if args[0] == "secondary-resume" {
			apply = b.secondary.resume
		}
		if err := apply(engine.engine, dnspeerenrollowner.SecondaryOptions{
			PrimaryIP: *primary, PeerIP: *peer, CatalogName: *catalog, CatalogAccount: *account,
			PrimaryPublicKeyPath: *public, InspectorBinaryPath: *inspector,
		}); err != nil {
			return err
		}
		// No subcommand performs a live exchange, so none is claimed here.
		return jsonOut(map[string]string{"state": "configured", "next_action": "Run secondary-host-key and give its Ed25519 SSH host-key SHA-256 to the primary owner through a trusted channel for primary-activate" + engineArg(engine.engine) + ". No owner command tests live authentication; the primary Agent makes the first authenticated read-only inspector exchange only while reconciling an admitted pending deletion, and reports its result on that operation."})
	case "secondary-host-key":
		if _, err := noArgs(); err != nil {
			return err
		}
		digest, err := b.secondary.hostKey()
		if err != nil {
			return err
		}
		return jsonOut(map[string]string{"host_key_sha256": digest, "next_action": "Compare this digest through a trusted channel before primary-activate; the primary must not learn it from an unauthenticated SSH connection."})
	case "secondary-status":
		engine, err := noArgs()
		if err != nil {
			return err
		}
		status, err := b.secondary.status(engine)
		if err != nil {
			return err
		}
		return jsonOut(status)
	case "secondary-revoke":
		engine, err := noArgs()
		if err != nil {
			return err
		}
		result, err := b.secondary.revoke(engine)
		if err != nil {
			return err
		}
		if engine == dnspeerenrollowner.EngineBIND {
			return jsonOut(map[string]string{"state": "revoked", "next_action": "The primary owner must also run primary-revoke. Standard DNS transfer continues independently."})
		}
		return jsonOut(map[string]string{"state": "revoked", "ssh_config": result.SSHConfig,
			"next_action": "The primary owner must also run primary-revoke" + engineArg(engine) + ". Standard DNS transfer continues independently. " + sshGuidance(result)})
	default:
		return errors.New("unknown owner enrollment action")
	}
}

// sshGuidance states what happened to the owner SSH configuration after the
// key was removed and who acts next. The channel is disabled in every case.
func sshGuidance(r dnspeerenrollowner.RevokeResult) string {
	include := "Include " + dnspeerenrollowner.PDNSSSHDConfigPath
	switch r.SSHConfig {
	case "restored":
		return "The recorded original sshd_config was restored and OpenSSH reloaded; the original copy, receipt and inspector files remain for your review."
	case "restored_not_reloaded":
		return r.Reason + "; run sshd -t, then reload the OpenSSH service yourself."
	case "original":
		return "sshd_config already matched the recorded original; nothing else changed."
	case "preserved_owner_edit":
		return r.Reason + ". If you no longer want it, remove the line '" + include + "' yourself, run sshd -t and reload OpenSSH."
	default:
		return r.Reason + ". The key is removed, so the channel is disabled. Compare " + dnspeerenrollowner.PDNSSSHDBackupPath + " with /etc/ssh/sshd_config and remove the line '" + include + "' yourself if wanted."
	}
}
