// Package binddns renders and stages deterministic, immutable BIND zone
// generations. It deliberately contains no service-management policy; callers
// decide when a verified generation may become active.
package binddns

import "github.com/alicelik/celikpanel/internal/transport"

const (
	manifestSchema = "celikpanel-bind-manifest/v1"
	receiptSchema  = "celikpanel-bind-generation-receipt/v1"
	engineName     = "bind"
)

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
	EngineEpoch    int64         `json:"engine_epoch"`
	Schema         string        `json:"schema"`
	Engine         string        `json:"engine"`
	Generation     string        `json:"generation"`
	ManifestSHA256 string        `json:"manifest_sha256"`
	ConfigSHA256   string        `json:"config_sha256"`
	Zones          []ZoneReceipt `json:"zones"`
}

// Generation is a fully rendered immutable tree before it is written.
type Generation struct {
	ID           string
	Zones        []RenderedZone
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
}

// TreePlan is a path- and runner-independent complete generation plan. It can
// be built from a full Manifest or by applying one delta to a VerifiedTree.
type TreePlan struct {
	engineEpoch int64
	zones       []treeZone
}

type treeZone struct {
	receipt ZoneReceipt
	data    []byte
}

// EngineEpoch reports the engine selection epoch bound into this plan.
func (plan TreePlan) EngineEpoch() int64 { return plan.engineEpoch }

// CurrentReceipt returns a value copy of the verified current receipt.
func (tree VerifiedTree) CurrentReceipt() Receipt { return cloneReceipt(tree.receipt) }
