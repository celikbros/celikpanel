// Package binddns renders and stages deterministic, immutable BIND zone
// generations. It deliberately contains no service-management policy; callers
// decide when a verified generation may become active.
package binddns

import "github.com/alicelik/celikpanel/internal/transport"

const (
	manifestSchema   = "celikpanel-bind-manifest/v1"
	manifestSchemaV1 = "celikpanel-bind-manifest/v1"
	manifestSchemaV2 = "celikpanel-bind-manifest/v2"
	receiptSchemaV1  = "celikpanel-bind-generation-receipt/v1"
	receiptSchemaV2  = "celikpanel-bind-generation-receipt/v2"
	engineName       = "bind"
)

const (
	PairRolePrimary   = "primary"
	PairRoleSecondary = "secondary"
)

// Pairing is the immutable directional identity for a BIND pair. The server
// owning NS1 is the primary and publishes a catalog zone; the server owning
// NS2 is the secondary and subscribes to that catalog. Keeping this identity
// inside each generation prevents mutable panel settings from changing AXFR
// authority during recovery.
type Pairing struct {
	Role    string
	LocalIP string
	LocalNS string
	PeerIP  string
	PeerNS  string
}

// ZoneSnapshot is one complete desired zone image. Delete is a tombstone and
// therefore requires Records to be empty. DesiredGeneration is the panel
// ledger generation that produced the snapshot; it is bound into the receipt.
type ZoneSnapshot struct {
	DesiredGeneration int64
	Domain            string
	Delete            bool
	Qualifier         string
	MutationRequestID string
	MutationOwnerID   string
	Records           []transport.ZoneRecord
}

// Manifest is the complete set of zone changes staged as one immutable BIND
// generation. Zone order is intentionally insignificant.
type Manifest struct {
	EngineEpoch int64
	Pairing     *Pairing
	Zones       []ZoneSnapshot
}

// RenderedZone contains a safe BIND master file and the hashes recorded in the
// generation receipt. Disabled records are omitted from Data but are retained
// in RecordsSHA256 and TotalRecords.
type RenderedZone struct {
	Domain         string
	FileName       string
	Data           []byte
	RecordsSHA256  string
	RenderedSHA256 string
	TotalRecords   int
	EnabledRecords int
}

// ZoneReceipt is the canonical durable description of one staged zone.
type ZoneReceipt struct {
	Qualifier         string `json:"qualifier"`
	MutationRequestID string `json:"mutation_request_id"`
	MutationOwnerID   string `json:"mutation_owner_id"`
	Domain            string `json:"domain"`
	DesiredGeneration int64  `json:"desired_generation"`
	Delete            bool   `json:"delete"`
	File              string `json:"file"`
	RecordsSHA256     string `json:"records_sha256"`
	RenderedSHA256    string `json:"rendered_sha256"`
	TotalRecords      int    `json:"total_records"`
	EnabledRecords    int    `json:"enabled_records"`
}

// Receipt is written as canonical JSON inside every immutable generation.
type Receipt struct {
	EngineEpoch    int64           `json:"engine_epoch"`
	Schema         string          `json:"schema"`
	Engine         string          `json:"engine"`
	Generation     string          `json:"generation"`
	ManifestSHA256 string          `json:"manifest_sha256"`
	ConfigSHA256   string          `json:"config_sha256"`
	Pairing        *PairingReceipt `json:"pairing,omitempty"`
	Zones          []ZoneReceipt   `json:"zones"`
}

// PairingReceipt binds both sides of the AXFR relationship and the exact
// catalog bytes. Secondary generations have no local catalog file: they bind
// the expected peer catalog name and memory-only transfer policy instead.
type PairingReceipt struct {
	Role          string `json:"role"`
	LocalIP       string `json:"local_ip"`
	LocalNS       string `json:"local_ns"`
	PeerIP        string `json:"peer_ip"`
	PeerNS        string `json:"peer_ns"`
	LocalCatalog  string `json:"local_catalog"`
	PeerCatalog   string `json:"peer_catalog"`
	CatalogSerial uint32 `json:"catalog_serial"`
	CatalogFile   string `json:"catalog_file,omitempty"`
	CatalogSHA256 string `json:"catalog_sha256,omitempty"`
	InMemory      bool   `json:"in_memory,omitempty"`
}

// Generation is a fully rendered immutable tree before it is written.
type Generation struct {
	ID           string
	Zones        []RenderedZone
	Catalog      *RenderedZone
	Config       []byte
	Receipt      []byte
	ReceiptValue Receipt
}

// VerifiedTree is a receipt and its exact zone/config bytes after all hashes,
// paths and canonical JSON have been verified. Its fields are intentionally
// private so ApplyDelta cannot be fed an unverified or partially reconstructed
// current generation.
type VerifiedTree struct {
	receipt Receipt
	zones   []treeZone
	catalog []byte
}

// TreePlan is a path- and runner-independent complete generation plan. It can
// be built from a full Manifest or by applying one delta to a VerifiedTree.
type TreePlan struct {
	engineEpoch   int64
	pairing       *Pairing
	catalogSerial uint32
	zones         []treeZone
}

type treeZone struct {
	receipt ZoneReceipt
	data    []byte
}

// EngineEpoch reports the engine selection epoch bound into this plan.
func (plan TreePlan) EngineEpoch() int64 { return plan.engineEpoch }

// Pairing returns a defensive copy of the directional BIND pair identity.
func (plan TreePlan) Pairing() *Pairing { return clonePairing(plan.pairing) }

// CurrentReceipt returns a value copy of the verified current receipt.
func (tree VerifiedTree) CurrentReceipt() Receipt { return cloneReceipt(tree.receipt) }

// Zone returns one zone from a tree that has already passed VerifyTree or a
// Publisher LoadCurrent verification. The domain must be in canonical panel
// form. Both the receipt and bytes are defensive copies; deletion tombstones
// deliberately return nil bytes.
func (tree VerifiedTree) Zone(domain string) (ZoneReceipt, []byte, bool) {
	if _, err := requireCanonicalDomain(domain); err != nil {
		return ZoneReceipt{}, nil, false
	}
	for _, zone := range tree.zones {
		if zone.receipt.Domain != domain {
			continue
		}
		if err := validateTreeZone(zone); err != nil {
			return ZoneReceipt{}, nil, false
		}
		receipt := zone.receipt
		if receipt.Delete {
			return receipt, nil, true
		}
		return receipt, append([]byte(nil), zone.data...), true
	}
	return ZoneReceipt{}, nil, false
}
