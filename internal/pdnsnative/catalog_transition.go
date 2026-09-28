package pdnsnative

import (
	"encoding/base64"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// Snapshot is a read-only, WAL-aware SQLite observation. Rows are in table
// column order, sorted by their complete canonical values. The caller must
// prove the database file identity and capture a consistent transaction.
type Snapshot struct {
	Schema [][4]string
	Tables map[string][][]any
}

type CatalogTransition struct {
	SourceSerial uint32
	NativeSerial uint32
	CatalogHash  string
}

// VerifyFreshPrimaryCatalogTransition admits only the native SQL changes
// measured for a fresh PowerDNS producer with MASTER members. It does not
// authorize a filesystem effect: inode, WAL, daemon version and owner proofs
// are separate requirements of the V3 journal and independent recovery.
func VerifyFreshPrimaryCatalogTransition(staged, live Snapshot, catalog string, sourceSerial uint32) (CatalogTransition, error) {
	if catalog == "" || sourceSerial != 1 || !reflect.DeepEqual(staged.Schema, live.Schema) ||
		len(staged.Tables) != len(live.Tables) {
		return CatalogTransition{}, errors.New("unsupported PowerDNS native catalog snapshot")
	}
	for table, before := range staged.Tables {
		after, ok := live.Tables[table]
		if !ok {
			return CatalogTransition{}, errors.New("PowerDNS native snapshot table changed")
		}
		switch table {
		case "domains", "records", "domainmetadata":
		default:
			if !reflect.DeepEqual(before, after) {
				return CatalogTransition{}, fmt.Errorf("PowerDNS native snapshot changed %s", table)
			}
		}
	}
	beforeDomains, afterDomains := staged.Tables["domains"], live.Tables["domains"]
	if len(beforeDomains) == 0 || len(beforeDomains) != len(afterDomains) {
		return CatalogTransition{}, errors.New("PowerDNS native domains changed")
	}
	producerID := int64(0)
	memberSerials := make(map[int64]uint32)
	for _, row := range staged.Tables["records"] {
		if len(row) != 10 || row[4] == nil || row[3] != "SOA" {
			continue
		}
		id, ok := integer(row[1])
		if !ok {
			return CatalogTransition{}, errors.New("PowerDNS native record domain ID is invalid")
		}
		if row[2] == catalog {
			continue
		}
		serial, err := soaSerial(row[4])
		if err != nil {
			return CatalogTransition{}, err
		}
		memberSerials[id] = serial
	}
	for i, before := range beforeDomains {
		after := afterDomains[i]
		if len(before) != 9 || len(after) != 9 || before[3] != nil ||
			!reflect.DeepEqual(before[:3], after[:3]) ||
			!reflect.DeepEqual(before[4:5], after[4:5]) ||
			!reflect.DeepEqual(before[6:], after[6:]) {
			return CatalogTransition{}, errors.New("PowerDNS native domain changed outside last_check")
		}
		id, ok := integer(before[0])
		if !ok || before[5] != nil {
			return CatalogTransition{}, errors.New("PowerDNS staged domain identity is invalid")
		}
		want := int64(0)
		if before[1] == catalog && before[4] == "PRODUCER" {
			producerID = id
			want = 1
		} else if before[4] == "MASTER" && before[8] == catalog {
			want = int64(memberSerials[id])
		} else {
			return CatalogTransition{}, errors.New("PowerDNS native domain is not a transferable catalog member")
		}
		actual, valid := integer(after[5])
		if !valid || want == 0 || actual != want {
			return CatalogTransition{}, errors.New("PowerDNS native last_check differs from staged member serial")
		}
	}
	if producerID == 0 {
		return CatalogTransition{}, errors.New("PowerDNS native producer is absent")
	}
	beforeRecords, afterRecords := staged.Tables["records"], live.Tables["records"]
	if len(beforeRecords) != len(afterRecords) {
		return CatalogTransition{}, errors.New("PowerDNS native record count changed")
	}
	var stagedSOA, nativeSOA []any
	withoutSOA := func(rows [][]any, native bool) ([][]any, error) {
		kept := make([][]any, 0, len(rows)-1)
		for _, row := range rows {
			if len(row) != 10 {
				return nil, errors.New("PowerDNS native record shape changed")
			}
			id, ok := integer(row[1])
			if !ok {
				return nil, errors.New("PowerDNS native record domain ID is invalid")
			}
			if id == producerID && row[2] == catalog && row[3] == "SOA" {
				if native {
					if nativeSOA != nil {
						return nil, errors.New("duplicate native producer SOA")
					}
					nativeSOA = row
				} else {
					if stagedSOA != nil {
						return nil, errors.New("duplicate staged producer SOA")
					}
					stagedSOA = row
				}
				continue
			}
			kept = append(kept, row)
		}
		return kept, nil
	}
	beforeKept, err := withoutSOA(beforeRecords, false)
	if err != nil {
		return CatalogTransition{}, err
	}
	afterKept, err := withoutSOA(afterRecords, true)
	if err != nil {
		return CatalogTransition{}, err
	}
	if stagedSOA == nil || nativeSOA == nil || !reflect.DeepEqual(beforeKept, afterKept) ||
		!reflect.DeepEqual(stagedSOA[1:4], nativeSOA[1:4]) ||
		!reflect.DeepEqual(stagedSOA[5:], nativeSOA[5:]) ||
		stagedSOA[4] != "invalid. invalid. 1 60 30 3600 30" {
		return CatalogTransition{}, errors.New("PowerDNS native record rewrite differs from staged catalog")
	}
	nativeSerial, err := soaSerial(nativeSOA[4])
	if err != nil || nativeSerial <= sourceSerial ||
		nativeSOA[4] != fmt.Sprintf("invalid invalid %d 60 30 3600 30", nativeSerial) {
		return CatalogTransition{}, errors.New("PowerDNS native catalog SOA rewrite is unsupported")
	}
	beforeMetadata, afterMetadata := staged.Tables["domainmetadata"], live.Tables["domainmetadata"]
	if len(beforeMetadata) != 0 || len(afterMetadata) != 1 || len(afterMetadata[0]) != 4 {
		return CatalogTransition{}, errors.New("PowerDNS native metadata rewrite is unsupported")
	}
	metadata := afterMetadata[0]
	metadataDomain, valid := integer(metadata[1])
	hash, hashOK := metadata[3].(string)
	decoded, decodeErr := base64.StdEncoding.DecodeString(hash)
	if _, idOK := integer(metadata[0]); !idOK || !valid || metadataDomain != producerID ||
		metadata[2] != "CATALOG-HASH" || !hashOK || decodeErr != nil || len(decoded) != 32 {
		return CatalogTransition{}, errors.New("PowerDNS native catalog metadata is invalid")
	}
	return CatalogTransition{SourceSerial: sourceSerial, NativeSerial: nativeSerial, CatalogHash: hash}, nil
}

func integer(value any) (int64, bool) {
	switch v := value.(type) {
	case int64:
		return v, true
	case float64:
		if v >= 0 && v <= 1<<53 && v == float64(int64(v)) {
			return int64(v), true
		}
	case int:
		return int64(v), true
	}
	return 0, false
}

func soaSerial(value any) (uint32, error) {
	content, ok := value.(string)
	if !ok {
		return 0, errors.New("PowerDNS SOA content is invalid")
	}
	fields := strings.Fields(content)
	if len(fields) != 7 {
		return 0, errors.New("PowerDNS SOA field count is invalid")
	}
	n, err := strconv.ParseUint(fields[2], 10, 32)
	if err != nil || n == 0 {
		return 0, errors.New("PowerDNS SOA serial is invalid")
	}
	return uint32(n), nil
}
