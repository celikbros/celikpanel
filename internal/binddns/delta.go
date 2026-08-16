package binddns

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// VerifyTree verifies canonical receipt JSON, the exact config hash, the exact
// set of zone files and every zone hash. It is deliberately filesystem- and
// runner-independent.
func VerifyTree(receiptBytes, config []byte, files map[string][]byte) (VerifiedTree, error) {
	receipt, err := DecodeReceipt(receiptBytes)
	if err != nil {
		return VerifiedTree{}, err
	}
	if sha256Hex(config) != receipt.ConfigSHA256 {
		return VerifiedTree{}, errors.New("BIND generation config hash does not match its receipt")
	}

	expected := make(map[string]struct{}, len(receipt.Zones))
	zones := make([]treeZone, len(receipt.Zones))
	for index, zoneReceipt := range receipt.Zones {
		zone := treeZone{receipt: zoneReceipt}
		if !zoneReceipt.Delete {
			data, ok := files[zoneReceipt.File]
			if !ok {
				return VerifiedTree{}, fmt.Errorf("BIND generation is missing %s", zoneReceipt.File)
			}
			if sha256Hex(data) != zoneReceipt.RenderedSHA256 {
				return VerifiedTree{}, fmt.Errorf("BIND zone file hash mismatch for %s", zoneReceipt.Domain)
			}
			zone.data = append([]byte(nil), data...)
			expected[zoneReceipt.File] = struct{}{}
		}
		zones[index] = zone
	}
	for file := range files {
		if _, ok := expected[file]; !ok {
			return VerifiedTree{}, fmt.Errorf("BIND generation contains unreceipted zone file %q", file)
		}
	}
	return VerifiedTree{receipt: cloneReceipt(receipt), zones: zones}, nil
}

// ApplyDelta replaces exactly one zone in a verified current tree while
// preserving every other zone byte-for-byte. A stale generation is rejected;
// an equal generation is accepted only when every binding and output is exact.
func ApplyDelta(current VerifiedTree, delta ZoneSnapshot) (TreePlan, error) {
	if err := validateReceipt(current.receipt); err != nil {
		return TreePlan{}, errors.New("current BIND tree is not verified")
	}
	if len(current.zones) != len(current.receipt.Zones) {
		return TreePlan{}, errors.New("current BIND tree is incomplete")
	}
	for index := range current.zones {
		if !equalZoneReceipt(current.zones[index].receipt, current.receipt.Zones[index]) ||
			(!current.zones[index].receipt.Delete && sha256Hex(current.zones[index].data) != current.zones[index].receipt.RenderedSHA256) {
			return TreePlan{}, errors.New("current BIND tree changed after verification")
		}
	}

	next, err := snapshotToTreeZone(delta)
	if err != nil {
		return TreePlan{}, err
	}
	zones := cloneTreeZones(current.zones)
	found := -1
	for index := range zones {
		if zones[index].receipt.Domain == next.receipt.Domain {
			found = index
			break
		}
	}
	if found >= 0 {
		previous := zones[found]
		if next.receipt.DesiredGeneration < previous.receipt.DesiredGeneration {
			return TreePlan{}, errors.New("BIND zone delta is older than the current generation")
		}
		if next.receipt.DesiredGeneration == previous.receipt.DesiredGeneration {
			if !equalTreeZone(previous, next) {
				return TreePlan{}, errors.New("BIND zone generation was reused with a different request binding or snapshot")
			}
			return TreePlan{engineEpoch: current.receipt.EngineEpoch, zones: zones}, nil
		}
		zones[found] = next
	} else {
		zones = append(zones, next)
	}
	if err := sortAndValidateTreeZones(zones); err != nil {
		return TreePlan{}, err
	}
	return TreePlan{engineEpoch: current.receipt.EngineEpoch, zones: zones}, nil
}

func encodeReceipt(receipt Receipt) ([]byte, error) {
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return nil, fmt.Errorf("encode BIND generation receipt: %w", err)
	}
	return append(encoded, '\n'), nil
}

// DecodeReceipt accepts only the exact canonical JSON representation emitted
// by this package.
func DecodeReceipt(data []byte) (Receipt, error) {
	if len(data) == 0 || len(data) > 8<<20 {
		return Receipt{}, errors.New("BIND generation receipt has an invalid size")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var receipt Receipt
	if err := decoder.Decode(&receipt); err != nil {
		return Receipt{}, fmt.Errorf("decode BIND generation receipt: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Receipt{}, errors.New("BIND generation receipt contains trailing JSON")
		}
		return Receipt{}, fmt.Errorf("decode trailing BIND generation receipt: %w", err)
	}
	canonical, err := encodeReceipt(receipt)
	if err != nil {
		return Receipt{}, err
	}
	if !bytes.Equal(data, canonical) {
		return Receipt{}, errors.New("BIND generation receipt is not canonical JSON")
	}
	if err := validateReceipt(receipt); err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}

func validateReceipt(receipt Receipt) error {
	if receipt.Schema != receiptSchema || receipt.Engine != engineName || receipt.EngineEpoch <= 0 {
		return errors.New("BIND generation receipt has an unsupported identity")
	}
	if !validDigest(receipt.Generation) || receipt.ManifestSHA256 != receipt.Generation ||
		!validDigest(receipt.ConfigSHA256) || receipt.Zones == nil {
		return errors.New("BIND generation receipt contains an invalid digest")
	}
	previous := ""
	for index, zone := range receipt.Zones {
		if err := validateZoneReceipt(zone); err != nil {
			return err
		}
		if index > 0 && zone.Domain <= previous {
			return errors.New("BIND generation receipt zones are unsorted or duplicated")
		}
		previous = zone.Domain
	}
	expected, err := manifestGenerationID(receipt.EngineEpoch, receipt.Zones)
	if err != nil {
		return err
	}
	if receipt.Generation != expected {
		return errors.New("BIND generation receipt does not match its canonical manifest")
	}
	return nil
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func receiptsFromTreeZones(zones []treeZone) []ZoneReceipt {
	receipts := make([]ZoneReceipt, len(zones))
	for index := range zones {
		receipts[index] = zones[index].receipt
	}
	return receipts
}

func cloneTreeZones(zones []treeZone) []treeZone {
	cloned := make([]treeZone, len(zones))
	for index := range zones {
		cloned[index] = treeZone{
			receipt: zones[index].receipt,
			data:    append([]byte(nil), zones[index].data...),
		}
	}
	return cloned
}

func cloneReceipt(receipt Receipt) Receipt {
	receipt.Zones = append([]ZoneReceipt(nil), receipt.Zones...)
	return receipt
}

func equalZoneReceipt(left, right ZoneReceipt) bool { return left == right }

func equalTreeZone(left, right treeZone) bool {
	return equalZoneReceipt(left.receipt, right.receipt) && bytes.Equal(left.data, right.data)
}
