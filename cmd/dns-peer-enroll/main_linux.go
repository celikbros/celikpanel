//go:build linux

// dns-peer-enroll is an explicit, local, owner-operated enrollment path for
// optional native BIND secondary deletion proof. Ordinary AXFR needs none of it.
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
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: dns-peer-enroll primary-prepare|primary-activate|primary-status|primary-revoke|secondary-host-key|secondary-install|secondary-resume|secondary-status|secondary-revoke")
	}
	jsonOut := func(v any) error { return json.NewEncoder(out).Encode(v) }
	switch args[0] {
	case "primary-prepare":
		if len(args) != 1 {
			return errors.New("primary-prepare accepts no arguments")
		}
		prepared, err := dnspeerenrollment.PrepareOwner()
		if err != nil {
			return err
		}
		return jsonOut(struct {
			State           string `json:"state"`
			CredentialID    string `json:"credential_id"`
			PublicKey       string `json:"public_key"`
			PublicKeySHA256 string `json:"public_key_sha256"`
			NextAction      string `json:"next_action"`
		}{"prepared", prepared.CredentialID, prepared.PublicKey, prepared.PublicKeySHA256,
			"Give only the displayed public key to the secondary owner; ask them to run secondary-install locally and return their independently reviewed Ed25519 SSH host-key SHA-256."})
	case "primary-activate":
		fs := flag.NewFlagSet("primary-activate", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		credential := fs.String("credential-id", "", "prepared credential ID")
		primary := fs.String("primary-ip", "", "reviewed primary IPv4")
		peer := fs.String("peer-ip", "", "reviewed secondary IPv4")
		catalog := fs.String("catalog", "", "derived catalog name")
		host := fs.String("host-key-sha256", "", "independently reviewed secondary Ed25519 host key digest")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
			return errors.New("invalid primary-activate arguments")
		}
		record, err := dnspeerenrollment.ActivateOwner(dnspeerenrollment.OwnerActivation{
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
		}{"configured", record.EnrollmentID,
			"For an admitted pending DNS deletion, use that exact operation's recovery action; enrollment alone does not enable a blocked DNS topology, and status polling does not retry the mutation."})
	case "primary-status":
		if len(args) != 1 {
			return errors.New("primary-status accepts no arguments")
		}
		snapshot, err := dnspeerenrollment.Read()
		if dnspeerenrollment.IsCode(err, dnspeerenrollment.Disabled) {
			return jsonOut(map[string]string{"state": "disabled", "next_action": "Prepare an owner enrollment only if parentless deletion proof is required."})
		}
		if err != nil {
			return errors.New("peer enrollment is unknown; inspect local owner state before changing it")
		}
		return jsonOut(map[string]string{
			"state": "configured", "enrollment_id": snapshot.Record.EnrollmentID,
			"primary_ip": snapshot.Record.PrimaryIP, "peer_ip": snapshot.Record.PeerIP,
			"catalog_name":             snapshot.Record.CatalogName,
			"host_key_sha256":          snapshot.Record.HostKeySHA256,
			"client_public_key_sha256": snapshot.Record.ClientPublicKeySHA256,
		})
	case "primary-revoke":
		if len(args) != 1 {
			return errors.New("primary-revoke accepts no arguments")
		}
		if err := dnspeerenrollment.RevokeOwner(); err != nil {
			return err
		}
		return jsonOut(map[string]string{"state": "revoked", "next_action": "The secondary owner must also run secondary-revoke. Pending deletion stays pending until the exact request is reverified."})
	case "secondary-install", "secondary-resume":
		fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		primary := fs.String("primary-ip", "", "reviewed primary IPv4")
		peer := fs.String("peer-ip", "", "reviewed local secondary IPv4")
		catalog := fs.String("catalog", "", "derived catalog name")
		public := fs.String("primary-public-key", "", "absolute path to reviewed primary public key")
		inspector := fs.String("inspector", "", "absolute path to reviewed native BIND inspector binary")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
			return errors.New("invalid secondary enrollment arguments")
		}
		apply := dnspeerenrollowner.InstallSecondary
		if args[0] == "secondary-resume" {
			apply = dnspeerenrollowner.ResumeSecondary
		}
		if err := apply(dnspeerenrollowner.SecondaryOptions{
			PrimaryIP: *primary, PeerIP: *peer, CatalogName: *catalog,
			PrimaryPublicKeyPath: *public, InspectorBinaryPath: *inspector,
		}); err != nil {
			return err
		}
		return jsonOut(map[string]string{"state": "configured", "next_action": "Return this server's independently reviewed Ed25519 SSH host-key SHA-256 to the primary owner, then verify a live read-only inspector exchange before relying on deletion proof."})
	case "secondary-host-key":
		if len(args) != 1 {
			return errors.New("secondary-host-key accepts no arguments")
		}
		digest, err := dnspeerenrollowner.SecondaryHostKeySHA256()
		if err != nil {
			return err
		}
		return jsonOut(map[string]string{"host_key_sha256": digest, "next_action": "Compare this digest through a trusted channel before primary-activate; the primary must not learn it from an unauthenticated SSH connection."})
	case "secondary-status":
		if len(args) != 1 {
			return errors.New("secondary-status accepts no arguments")
		}
		status, err := dnspeerenrollowner.SecondaryStatus()
		if err != nil {
			return err
		}
		return jsonOut(status)
	case "secondary-revoke":
		if len(args) != 1 {
			return errors.New("secondary-revoke accepts no arguments")
		}
		if err := dnspeerenrollowner.RevokeSecondary(); err != nil {
			return err
		}
		return jsonOut(map[string]string{"state": "revoked", "next_action": "The primary owner must also run primary-revoke. Standard DNS transfer continues independently."})
	default:
		return errors.New("unknown owner enrollment action")
	}
}
