// Package pdnsmanagedconf renders the PowerDNS drop-ins CelikPanel writes in
// the main configuration's include-dir. The Agent writes exactly these bytes;
// the owner's pdns-peer-inspect recognises a CelikPanel-managed secondary by
// rendering them again from the values it reads and comparing directives.
// Keeping one renderer means the inspector cannot drift from the product.
package pdnsmanagedconf

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	// IncludeDir is the directory the main pdns.conf must load exactly once.
	IncludeDir = "/etc/powerdns/pdns.d"
	// ManagedFile holds the backend, database and listener drop-in.
	ManagedFile = "celikpanel.conf"
	// ClusterFile holds the DNS pair drop-in.
	ClusterFile = "celikpanel-cluster.conf"
)

// Standalone is the managed backend drop-in (IncludeDir/ManagedFile) for the
// given SQLite database and canonical public listen addresses.
func Standalone(databasePath string, addresses []string) ([]byte, error) {
	if len(addresses) == 0 {
		return nil, errors.New("managed PowerDNS requires at least one listen address")
	}
	seen := make(map[string]struct{}, len(addresses))
	for _, address := range addresses {
		parsed := net.ParseIP(address)
		if parsed == nil || parsed.String() != address || !parsed.IsGlobalUnicast() ||
			parsed.IsUnspecified() || parsed.IsLoopback() || parsed.IsLinkLocalUnicast() {
			return nil, errors.New("managed PowerDNS listen address is not canonical global unicast")
		}
		if _, duplicate := seen[address]; duplicate {
			return nil, errors.New("managed PowerDNS listen addresses contain a duplicate")
		}
		seen[address] = struct{}{}
	}
	return []byte(fmt.Sprintf(`# Managed by CelikPanel; do not edit by hand.
launch=gsqlite3
gsqlite3-dnssec=yes
gsqlite3-database=%s
local-address=%s
zone-cache-refresh-interval=0
webserver=no
api=no
`, databasePath, strings.Join(addresses, ","))), nil
}

// DirectionalCluster is the directional pair drop-in (IncludeDir/ClusterFile)
// for a primary or secondary pair role, as the Agent writes it today
// (v0.1.0-alpha.39 onward).
func DirectionalCluster(pairRole, localIP, peerIP string) (string, error) {
	return directionalCluster(pairRole, localIP, peerIP, false)
}

// DirectionalClusterAutosecondary is the earlier rendering of the same
// drop-in (v0.1.0-alpha.29 to alpha.38), which also carried
// autosecondary=yes. The Agent never writes it again; it exists only so the
// owner's inspector recognises this product's own earlier output on a
// secondary that CelikPanel has not reconfigured since.
func DirectionalClusterAutosecondary(pairRole, localIP, peerIP string) (string, error) {
	return directionalCluster(pairRole, localIP, peerIP, true)
}

func directionalCluster(pairRole, localIP, peerIP string, autosecondary bool) (string, error) {
	if pairRole != transport.DNSPairRolePrimary &&
		pairRole != transport.DNSPairRoleSecondary {
		return ``, errors.New(`PowerDNS pair role must be primary or secondary`)
	}
	parsedLocal := net.ParseIP(localIP)
	parsedPeer := net.ParseIP(peerIP)
	if parsedLocal == nil || parsedLocal.To4() == nil || parsedLocal.String() != localIP ||
		!parsedLocal.IsGlobalUnicast() || parsedPeer == nil || parsedPeer.To4() == nil ||
		parsedPeer.String() != peerIP || !parsedPeer.IsGlobalUnicast() ||
		parsedLocal.Equal(parsedPeer) {
		return ``, errors.New(`PowerDNS pair addresses must be canonical and distinct`)
	}
	allowAXFR := peerIP
	notify := ``
	if pairRole == transport.DNSPairRolePrimary {
		allowAXFR = localIP + `,` + peerIP
		notify = `also-notify=` + peerIP + string('\n')
	}
	automatic := ``
	if autosecondary {
		automatic = `autosecondary=yes` + string('\n')
	}
	return fmt.Sprintf(`# Managed by CelikPanel - do not edit by hand / elle duzenlemeyin
# Directional DNS pair: AXFR is restricted to the trusted local proof and exact peer.
# Yonlu DNS cifti: AXFR guvenilir yerel kanit ve tam es ile sinirlidir.
primary=yes
secondary=yes
%sallow-axfr-ips=%s
%s`, automatic, allowAXFR, notify), nil
}
