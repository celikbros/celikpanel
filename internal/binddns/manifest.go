package binddns

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

type manifestDigestZone struct {
	Domain            string `json:"domain"`
	DesiredGeneration int64  `json:"desired_generation"`
	Delete            bool   `json:"delete"`
	Qualifier         string `json:"qualifier"`
	MutationRequestID string `json:"mutation_request_id"`
	MutationOwnerID   string `json:"mutation_owner_id"`
	File              string `json:"file"`
	RecordsSHA256     string `json:"records_sha256"`
	RenderedSHA256    string `json:"rendered_sha256"`
	TotalRecords      int    `json:"total_records"`
	EnabledRecords    int    `json:"enabled_records"`
}

type manifestDigestDocument struct {
	Schema      string               `json:"schema"`
	Engine      string               `json:"engine"`
	EngineEpoch int64                `json:"engine_epoch"`
	Zones       []manifestDigestZone `json:"zones"`
}

// NewTreePlan validates and freezes a complete manifest without binding it to
// a host path or invoking BIND.
func NewTreePlan(manifest Manifest) (TreePlan, error) {
	if manifest.EngineEpoch <= 0 {
		return TreePlan{}, errors.New("BIND engine epoch must be positive")
	}
	zones := make([]treeZone, len(manifest.Zones))
	for index, snapshot := range manifest.Zones {
		zone, err := snapshotToTreeZone(snapshot)
		if err != nil {
			return TreePlan{}, fmt.Errorf("zone %d: %w", index, err)
		}
		zones[index] = zone
	}
	if err := sortAndValidateTreeZones(zones); err != nil {
		return TreePlan{}, err
	}
	return TreePlan{engineEpoch: manifest.EngineEpoch, zones: cloneTreeZones(zones)}, nil
}

// RenderManifest freezes an order-independent manifest into a deterministic
// generation at an absolute POSIX host root.
func RenderManifest(root string, manifest Manifest) (Generation, error) {
	plan, err := NewTreePlan(manifest)
	if err != nil {
		return Generation{}, err
	}
	return RenderTree(root, plan)
}

// RenderTree binds a verified path-independent plan to one host layout.
func RenderTree(root string, plan TreePlan) (Generation, error) {
	if err := validateRoot(root); err != nil {
		return Generation{}, err
	}
	if plan.engineEpoch <= 0 {
		return Generation{}, errors.New("BIND engine epoch must be positive")
	}
	zones := cloneTreeZones(plan.zones)
	if err := sortAndValidateTreeZones(zones); err != nil {
		return Generation{}, err
	}

	generationID, err := manifestGenerationID(plan.engineEpoch, receiptsFromTreeZones(zones))
	if err != nil {
		return Generation{}, err
	}
	var config strings.Builder
	config.WriteString("// Managed by CelikPanel. DO NOT EDIT.\n")
	config.WriteString("// Immutable generation: ")
	config.WriteString(generationID)
	config.WriteString("; engine epoch: ")
	config.WriteString(fmt.Sprintf("%d", plan.engineEpoch))
	config.WriteByte('\n')

	rendered := make([]RenderedZone, 0, len(zones))
	receiptZones := make([]ZoneReceipt, len(zones))
	for index, zone := range zones {
		receiptZones[index] = zone.receipt
		if zone.receipt.Delete {
			continue
		}
		absoluteFile := path.Join(root, "generations", generationID, zone.receipt.File)
		config.WriteString("zone \"")
		config.WriteString(zone.receipt.Domain)
		config.WriteString("\" {\n\ttype master;\n\tfile \"")
		config.WriteString(absoluteFile)
		config.WriteString("\";\n};\n")
		rendered = append(rendered, RenderedZone{
			Domain:         zone.receipt.Domain,
			FileName:       path.Base(zone.receipt.File),
			Data:           append([]byte(nil), zone.data...),
			RecordsSHA256:  zone.receipt.RecordsSHA256,
			RenderedSHA256: zone.receipt.RenderedSHA256,
			TotalRecords:   zone.receipt.TotalRecords,
			EnabledRecords: zone.receipt.EnabledRecords,
		})
	}
	configBytes := []byte(config.String())
	receiptValue := Receipt{
		EngineEpoch:    plan.engineEpoch,
		Schema:         receiptSchema,
		Engine:         engineName,
		Generation:     generationID,
		ManifestSHA256: generationID,
		ConfigSHA256:   sha256Hex(configBytes),
		Zones:          receiptZones,
	}
	if receiptValue.Zones == nil {
		receiptValue.Zones = []ZoneReceipt{}
	}
	receiptBytes, err := encodeReceipt(receiptValue)
	if err != nil {
		return Generation{}, err
	}
	return Generation{
		ID:           generationID,
		Zones:        rendered,
		Config:       append([]byte(nil), configBytes...),
		Receipt:      receiptBytes,
		ReceiptValue: cloneReceipt(receiptValue),
	}, nil
}

func snapshotToTreeZone(snapshot ZoneSnapshot) (treeZone, error) {
	if snapshot.DesiredGeneration < 0 {
		return treeZone{}, errors.New("desired generation must not be negative")
	}
	if err := validateQualifier(snapshot.Qualifier); err != nil {
		return treeZone{}, err
	}
	if err := validateMutationIdentity("mutation request ID", snapshot.MutationRequestID); err != nil {
		return treeZone{}, err
	}
	if err := validateMutationIdentity("mutation owner ID", snapshot.MutationOwnerID); err != nil {
		return treeZone{}, err
	}
	if snapshot.Delete && len(snapshot.Records) != 0 {
		return treeZone{}, errors.New("a deletion tombstone cannot hide records")
	}
	rendered, err := RenderZone(snapshot.Domain, snapshot.Records)
	if err != nil {
		return treeZone{}, err
	}
	receipt := ZoneReceipt{
		Qualifier:         snapshot.Qualifier,
		MutationRequestID: snapshot.MutationRequestID,
		MutationOwnerID:   snapshot.MutationOwnerID,
		Domain:            rendered.Domain,
		DesiredGeneration: snapshot.DesiredGeneration,
		Delete:            snapshot.Delete,
		RecordsSHA256:     rendered.RecordsSHA256,
		TotalRecords:      rendered.TotalRecords,
		EnabledRecords:    rendered.EnabledRecords,
	}
	var data []byte
	if !snapshot.Delete {
		receipt.File = path.Join("zones", rendered.FileName)
		receipt.RenderedSHA256 = rendered.RenderedSHA256
		data = append([]byte(nil), rendered.Data...)
	}
	return treeZone{receipt: receipt, data: data}, nil
}

func manifestGenerationID(engineEpoch int64, zones []ZoneReceipt) (string, error) {
	digestZones := make([]manifestDigestZone, len(zones))
	for index, zone := range zones {
		digestZones[index] = manifestDigestZone{
			Domain:            zone.Domain,
			DesiredGeneration: zone.DesiredGeneration,
			Delete:            zone.Delete,
			Qualifier:         zone.Qualifier,
			MutationRequestID: zone.MutationRequestID,
			MutationOwnerID:   zone.MutationOwnerID,
			File:              zone.File,
			RecordsSHA256:     zone.RecordsSHA256,
			RenderedSHA256:    zone.RenderedSHA256,
			TotalRecords:      zone.TotalRecords,
			EnabledRecords:    zone.EnabledRecords,
		}
	}
	encoded, err := json.Marshal(manifestDigestDocument{
		Schema:      manifestSchema,
		Engine:      engineName,
		EngineEpoch: engineEpoch,
		Zones:       digestZones,
	})
	if err != nil {
		return "", fmt.Errorf("encode canonical BIND manifest: %w", err)
	}
	return sha256Hex(encoded), nil
}

func validateRoot(root string) error {
	if root == "" || root == "/" || !strings.HasPrefix(root, "/") || path.Clean(root) != root {
		return errors.New("BIND generation root must be a canonical absolute POSIX path below /")
	}
	for _, character := range root {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '/' && character != '.' &&
			character != '_' && character != '-' {
			return errors.New("BIND generation root contains an unsafe character")
		}
	}
	return nil
}

func validateQualifier(value string) error {
	if !mutationpayload.ValidDNSZoneSyncV3Qualifier(value) {
		return errors.New("BIND qualifier is not a canonical DNS zone sync v3 commitment")
	}
	return nil
}

func validateMutationIdentity(label, value string) error {
	if len(value) != 32 {
		return fmt.Errorf("BIND %s must be 32 lowercase hexadecimal characters", label)
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return fmt.Errorf("BIND %s must be 32 lowercase hexadecimal characters", label)
		}
	}
	return nil
}

func sortAndValidateTreeZones(zones []treeZone) error {
	sort.Slice(zones, func(left, right int) bool {
		return zones[left].receipt.Domain < zones[right].receipt.Domain
	})
	for index := range zones {
		if err := validateTreeZone(zones[index]); err != nil {
			return fmt.Errorf("zone %d: %w", index, err)
		}
		if index > 0 && zones[index-1].receipt.Domain == zones[index].receipt.Domain {
			return fmt.Errorf("manifest contains duplicate zone %q", zones[index].receipt.Domain)
		}
	}
	return nil
}

func validateTreeZone(zone treeZone) error {
	if err := validateZoneReceipt(zone.receipt); err != nil {
		return err
	}
	if zone.receipt.Delete {
		if len(zone.data) != 0 {
			return errors.New("deletion tree contains hidden zone bytes")
		}
		return nil
	}
	if sha256Hex(zone.data) != zone.receipt.RenderedSHA256 {
		return errors.New("rendered zone bytes do not match the receipt")
	}
	return nil
}

func validateZoneReceipt(zone ZoneReceipt) error {
	domain, err := requireCanonicalDomain(zone.Domain)
	if err != nil {
		return err
	}
	if zone.DesiredGeneration < 0 || !validDigest(zone.RecordsSHA256) || zone.TotalRecords < 0 ||
		zone.EnabledRecords < 0 || zone.EnabledRecords > zone.TotalRecords {
		return errors.New("BIND generation receipt contains invalid zone metadata")
	}
	if err := validateQualifier(zone.Qualifier); err != nil {
		return err
	}
	if err := validateMutationIdentity("mutation request ID", zone.MutationRequestID); err != nil {
		return err
	}
	if err := validateMutationIdentity("mutation owner ID", zone.MutationOwnerID); err != nil {
		return err
	}
	if zone.Delete {
		if zone.File != "" || zone.RenderedSHA256 != "" || zone.TotalRecords != 0 || zone.EnabledRecords != 0 ||
			zone.RecordsSHA256 != emptyRecordsSHA256() {
			return errors.New("BIND generation deletion receipt contains hidden state")
		}
		return nil
	}
	if zone.File != path.Join("zones", zoneFileName(domain)) || !validDigest(zone.RenderedSHA256) {
		return errors.New("BIND generation receipt contains an invalid zone file")
	}
	return nil
}
