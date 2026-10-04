package dnsengineartifact

import (
	"errors"
	"github.com/alicelik/celikpanel/internal/bindconfig"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"path"
	"strings"
)

type BINDAdoptionSourceProofV1 struct {
	Kind  string                     `json:"kind"`
	Files []BINDAdoptionSourceFileV1 `json:"files"`
	Zones []BINDAdoptionSourceZoneV1 `json:"zones"`
}
type BINDAdoptionSourceFileV1 struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Mode   uint32 `json:"mode"`
	UID    uint32 `json:"uid"`
	GID    uint32 `json:"gid"`
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}
type BINDAdoptionSourceZoneV1 struct {
	Name      string `json:"name"`
	Class     string `json:"class"`
	Type      string `json:"type"`
	File      string `json:"file"`
	SOASerial uint32 `json:"soa_serial"`
}

func validateBINDAdoptionSourceProofV1(j SwitchJournalV1) error {
	p := j.InversePlan
	if p == nil || p.SourceBIND == nil || p.SourceBIND.Kind != BINDAdoptionSourceProofKindV1 || p.HostLayout != "apt" || j.SourceEngine != "" || j.TargetEngine != "bind" || j.Mode != "switch" || j.Topology != "standalone" || j.PairRole != "" || j.LocalIP != "" || j.LocalNS != "" || j.PeerIP != "" || j.PeerNS != "" || j.SourceEpoch != 0 || j.TargetEpoch != 1 || j.StateBefore.Exists || len(j.SourceUnitsBefore) != 0 {
		return errors.New("BIND adoption source lacks exact initial standalone envelope")
	}
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(j.Mode, j.SourceEngine, j.TargetEngine, j.SourceEpoch, j.TargetEpoch, j.SourceRevision, j.Topology, j.PairRole, j.LocalIP, j.LocalNS, j.PeerIP, j.PeerNS, j.Zones)
	if err != nil {
		return err
	}
	running, err := RunningBINDAdoptionJournal(manifest, j)
	if err != nil || !running {
		return errors.New("BIND adoption source lacks active owner unit preimage")
	}
	if len(p.BINDUnchangedConfig) != 2 || len(j.ConfigBefore) != 2 {
		return errors.New("BIND adoption source lacks frozen config")
	}
	parsed, err := bindconfig.ParseAdoptionStaticZones(string(j.ConfigBefore[0].Data), string(j.ConfigBefore[1].Data), string(p.BINDUnchangedConfig[1].Data))
	if err != nil {
		return err
	}
	proof := p.SourceBIND
	if len(proof.Files) == 0 || len(proof.Files) > 64 || len(proof.Zones) != len(parsed) {
		return errors.New("BIND adoption source has incomplete bounded files or zones")
	}
	fileSet := map[string]bool{}
	prev := ""
	for _, f := range proof.Files {
		if f.Path <= prev || !strings.HasPrefix(f.Path, "/") || path.Clean(f.Path) != f.Path || f.Path == "/" || !ValidGeneration(f.SHA256) || f.Size <= 0 || f.Size > 16<<20 || f.Mode == 0 || f.Mode&^0o777 != 0 || f.Mode&0o022 != 0 || f.UID != 0 || f.GID > 1<<31-1 || f.Device == 0 || f.Inode == 0 {
			return errors.New("BIND adoption source file identity is unsafe")
		}
		prev = f.Path
		fileSet[f.Path] = true
	}
	primary := 0
	ownerPrimary := 0
	packagedCount := 4
	if p.BINDUnchangedConfig[1].Path == "/etc/bind/named.conf.root-hints" {
		packagedCount = 1
	}
	ownerCount := len(parsed) - packagedCount
	for i, z := range proof.Zones {
		expected := parsed[i]
		if z.Name != expected.Name || z.Class != expected.Class || z.Type != expected.Type || z.File != expected.File || !fileSet[z.File] {
			return errors.New("BIND adoption source zone differs from static config")
		}
		if z.Type == "master" || z.Type == "primary" {
			primary++
			if i < ownerCount {
				ownerPrimary++
			}
		} else if z.SOASerial != 0 {
			return errors.New("BIND hint contains SOA serial")
		}
	}
	if primary == 0 || ownerPrimary == 0 {
		return errors.New("BIND adoption has no authoritative owner primary")
	}
	if len(fileSet) != len(proof.Files) {
		return errors.New("BIND adoption has duplicate source file")
	}
	for _, f := range proof.Files {
		used := false
		for _, z := range proof.Zones {
			if z.File == f.Path {
				used = true
				break
			}
		}
		if !used {
			return errors.New("BIND adoption has unrelated source file")
		}
	}
	return nil
}
