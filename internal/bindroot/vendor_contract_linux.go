//go:build linux

package bindroot

import (
	"errors"

	"github.com/alicelik/celikpanel/internal/hostplatform"
)

type VendorContract struct {
	UnitPath         string
	EnvironmentPath  string
	UnitBytes        []byte
	EnvironmentBytes []byte
}

const CertifiedAPTBINDVendorUnit = "[Unit]\n" +
	"Description=BIND Domain Name Server\n" +
	"Documentation=man:named(8)\n" +
	"After=network.target\nWants=nss-lookup.target\nBefore=nss-lookup.target\n\n" +
	"[Service]\nType=notify\nEnvironmentFile=-/etc/default/named\n" +
	"ExecStart=/usr/sbin/named -f $OPTIONS\n" +
	"ExecReload=/usr/sbin/rndc reload\nExecStop=/usr/sbin/rndc stop\n" +
	"Restart=on-failure\n\n[Install]\nWantedBy=multi-user.target\nAlias=bind9.service\n"

const CertifiedAPTBINDVendorEnvironment = "#\n# run resolvconf?\n" +
	"RESOLVCONF=no\n\n# startup options for the server\nOPTIONS=\"-u bind\"\n"

const CertifiedPacmanBINDVendorUnit = "[Unit]\n" +
	"Description=Internet domain name server\nAfter=network.target\n\n" +
	"[Service]\nExecStart=/usr/bin/named -f -u named\n" +
	"ExecReload=/usr/bin/kill -HUP $MAINPID\n\n" +
	"[Install]\nWantedBy=multi-user.target\n"

func CertifiedVendorContract(profile hostplatform.Profile) (VendorContract, error) {
	if profile.ServiceManager != hostplatform.ServiceManagerSystemd {
		return VendorContract{}, errors.New("BIND vendor proof requires systemd")
	}
	switch {
	case profile.PackageManager == hostplatform.PackageManagerAPT &&
		profile.DistroFamily == hostplatform.DistroFamilyDebian:
		return VendorContract{
			UnitPath:         "/usr/lib/systemd/system/named.service",
			EnvironmentPath:  "/etc/default/named",
			UnitBytes:        []byte(CertifiedAPTBINDVendorUnit),
			EnvironmentBytes: []byte(CertifiedAPTBINDVendorEnvironment),
		}, nil
	case profile.PackageManager == hostplatform.PackageManagerPacman &&
		profile.DistroFamily == hostplatform.DistroFamilyArch:
		return VendorContract{
			UnitPath:  "/usr/lib/systemd/system/named.service",
			UnitBytes: []byte(CertifiedPacmanBINDVendorUnit),
		}, nil
	default:
		return VendorContract{}, errors.New("BIND vendor unit proof is unsupported on this host profile")
	}
}
