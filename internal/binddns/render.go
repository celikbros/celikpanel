package binddns

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	maxRecordContentBytes = 65535
	maxTXTBytes           = 4096
	maxTTL                = 1<<31 - 1
	maxPriority           = 1<<16 - 1
	maxZoneRecords        = 16384
	maxZonePayloadBytes   = 8 << 20
)

type canonicalRecord struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl"`
	Prio     int    `json:"prio"`
	Disabled bool   `json:"disabled"`
}

type recordDigestDocument struct {
	Schema  string            `json:"schema"`
	Records []canonicalRecord `json:"records"`
}

// RenderZone validates a complete zone snapshot and renders deterministic
// BIND master-file bytes. The domain must already be in the panel's canonical
// lower-case, no-trailing-dot form. Record names are canonicalized because old
// ledgers may still contain case or a trailing root dot.
func RenderZone(domain string, records []transport.ZoneRecord) (RenderedZone, error) {
	canonicalDomain, err := requireCanonicalDomain(domain)
	if err != nil {
		return RenderedZone{}, err
	}
	if len(records) > maxZoneRecords {
		return RenderedZone{}, errors.New("BIND zone exceeds the record limit")
	}

	canonical := make([]canonicalRecord, len(records))
	payloadBytes := 0
	for index, record := range records {
		if len(record.Name)+len(record.Type)+len(record.Content)+16 > maxZonePayloadBytes-payloadBytes {
			return RenderedZone{}, errors.New("BIND zone exceeds the payload limit")
		}
		payloadBytes += len(record.Name) + len(record.Type) + len(record.Content) + 16
		canonical[index], err = canonicalizeRecord(canonicalDomain, record)
		if err != nil {
			return RenderedZone{}, fmt.Errorf("record %d: %w", index, err)
		}
	}
	sort.SliceStable(canonical, func(left, right int) bool {
		return lessCanonicalRecord(canonical[left], canonical[right])
	})

	digestRecords := canonical
	if digestRecords == nil {
		digestRecords = []canonicalRecord{}
	}
	digestBytes, err := json.Marshal(recordDigestDocument{
		Schema:  manifestSchema + "/records",
		Records: digestRecords,
	})
	if err != nil {
		return RenderedZone{}, fmt.Errorf("encode canonical record digest: %w", err)
	}

	var output strings.Builder
	output.WriteString("; Managed by CelikPanel. DO NOT EDIT.\n")
	output.WriteString("$ORIGIN ")
	output.WriteString(canonicalDomain)
	output.WriteString(".\n")

	enabled := 0
	for _, record := range canonical {
		if record.Disabled {
			continue
		}
		enabled++
		output.WriteString(record.Name)
		output.WriteString(".\t")
		output.WriteString(strconv.Itoa(record.TTL))
		output.WriteString("\tIN\t")
		output.WriteString(record.Type)
		output.WriteByte('\t')
		output.WriteString(renderRData(record))
		output.WriteByte('\n')
	}
	data := []byte(output.String())

	return RenderedZone{
		Domain:         canonicalDomain,
		FileName:       canonicalDomain + ".zone",
		Data:           data,
		RecordsSHA256:  sha256Hex(digestBytes),
		RenderedSHA256: sha256Hex(data),
		TotalRecords:   len(canonical),
		EnabledRecords: enabled,
	}, nil
}

func requireCanonicalDomain(domain string) (string, error) {
	canonical, err := hostname.CanonicalFQDN(domain)
	if err != nil || canonical != domain {
		return "", errors.New("BIND zone domain must be canonical")
	}
	return canonical, nil
}

func canonicalizeRecord(domain string, record transport.ZoneRecord) (canonicalRecord, error) {
	name, err := canonicalOwnerName(domain, record.Name)
	if err != nil {
		return canonicalRecord{}, err
	}
	recordType := strings.ToUpper(strings.TrimSpace(record.Type))
	switch recordType {
	case "A", "AAAA", "CNAME", "MX", "TXT", "NS", "SRV", "SOA", "CAA", "TLSA":
	default:
		return canonicalRecord{}, fmt.Errorf("unsupported BIND record type %q", recordType)
	}
	if record.TTL < 0 || record.TTL > maxTTL {
		return canonicalRecord{}, errors.New("TTL must be between 0 and 2147483647")
	}
	if record.Prio < 0 || record.Prio > maxPriority {
		return canonicalRecord{}, errors.New("priority must be between 0 and 65535")
	}
	if recordType != "MX" && recordType != "SRV" && record.Prio != 0 {
		return canonicalRecord{}, fmt.Errorf("%s records cannot carry a priority", recordType)
	}
	if len(record.Content) == 0 || len(record.Content) > maxRecordContentBytes ||
		!utf8.ValidString(record.Content) {
		return canonicalRecord{}, errors.New("record content is empty, too large, or invalid UTF-8")
	}
	for _, character := range record.Content {
		if character < 0x20 || character == 0x7f {
			return canonicalRecord{}, errors.New("record content contains a control character")
		}
	}

	content, err := canonicalRData(recordType, record.Content)
	if err != nil {
		return canonicalRecord{}, err
	}
	if recordType == "SOA" && name != domain {
		return canonicalRecord{}, errors.New("SOA owner must be the zone apex")
	}
	return canonicalRecord{
		Name: name, Type: recordType, Content: content,
		TTL: record.TTL, Prio: record.Prio, Disabled: record.Disabled,
	}, nil
}

func canonicalOwnerName(domain, raw string) (string, error) {
	name := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), "."))
	if name == "" || len(name) > 253 || (name != domain && !strings.HasSuffix(name, "."+domain)) {
		return "", errors.New("record owner is outside the zone or invalid")
	}
	for index, label := range strings.Split(name, ".") {
		if label == "" || len(label) > 63 {
			return "", errors.New("record owner has an invalid label")
		}
		if label == "*" {
			if index != 0 {
				return "", errors.New("wildcard is only allowed in the left-most label")
			}
			continue
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("record owner has an invalid label")
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') &&
				(character < '0' || character > '9') &&
				character != '-' && character != '_' {
				return "", errors.New("record owner has an invalid label")
			}
		}
	}
	return name, nil
}

func canonicalRData(recordType, raw string) (string, error) {
	content := strings.TrimSpace(raw)
	switch recordType {
	case "A":
		address, err := netip.ParseAddr(content)
		if err != nil || !address.Is4() {
			return "", errors.New("invalid A address")
		}
		return address.String(), nil
	case "AAAA":
		address, err := netip.ParseAddr(content)
		if err != nil || !address.Is6() {
			return "", errors.New("invalid AAAA address")
		}
		return address.String(), nil
	case "CNAME", "MX", "NS":
		target, err := hostname.CanonicalFQDN(content)
		if err != nil {
			return "", fmt.Errorf("invalid %s target", recordType)
		}
		return target, nil
	case "TXT":
		return canonicalTXT(raw)
	case "SRV":
		return canonicalSRV(content)
	case "SOA":
		return canonicalSOA(content)
	case "CAA":
		return canonicalCAA(content)
	case "TLSA":
		return canonicalTLSA(content)
	default:
		panic("canonicalRData called for unsupported record type")
	}
}

func canonicalTXT(raw string) (string, error) {
	content := strings.TrimSpace(raw)
	if content == "" {
		return "", errors.New("TXT content is required")
	}
	var value strings.Builder
	if !strings.HasPrefix(content, `"`) {
		if strings.ContainsAny(content, `"\\`) {
			return "", errors.New("TXT content contains an unsupported quote or backslash")
		}
		value.WriteString(content)
	} else {
		for rest := content; rest != ""; {
			if rest[0] != '"' {
				return "", errors.New("invalid quoted TXT content")
			}
			rest = rest[1:]
			end := strings.IndexByte(rest, '"')
			if end < 0 || strings.ContainsRune(rest[:end], '\\') || len(rest[:end]) > 255 {
				return "", errors.New("invalid quoted TXT segment")
			}
			value.WriteString(rest[:end])
			rest = strings.TrimSpace(rest[end+1:])
		}
	}
	if value.Len() == 0 || value.Len() > maxTXTBytes {
		return "", errors.New("TXT content must be between 1 and 4096 bytes")
	}
	for _, character := range value.String() {
		if character < 0x20 || character == 0x7f || character == '"' || character == '\\' {
			return "", errors.New("TXT content contains an unsupported character")
		}
	}

	remaining := value.String()
	segments := make([]string, 0, len(remaining)/255+1)
	for len(remaining) > 0 {
		end := 0
		for index, character := range remaining {
			size := utf8.RuneLen(character)
			if size < 0 || index+size > 255 {
				break
			}
			end = index + size
		}
		if end == 0 {
			return "", errors.New("TXT content contains invalid UTF-8")
		}
		segments = append(segments, `"`+remaining[:end]+`"`)
		remaining = remaining[end:]
	}
	return strings.Join(segments, " "), nil
}

func canonicalSRV(content string) (string, error) {
	fields := strings.Fields(content)
	if len(fields) != 3 {
		return "", errors.New("SRV content must be: weight port target")
	}
	weight, err := strconv.ParseUint(fields[0], 10, 16)
	if err != nil {
		return "", errors.New("invalid SRV weight")
	}
	port, err := strconv.ParseUint(fields[1], 10, 16)
	if err != nil {
		return "", errors.New("invalid SRV port")
	}
	target := fields[2]
	if target != "." {
		target, err = hostname.CanonicalFQDN(target)
		if err != nil {
			return "", errors.New("invalid SRV target")
		}
	}
	return fmt.Sprintf("%d %d %s", weight, port, target), nil
}

func canonicalSOA(content string) (string, error) {
	fields := strings.Fields(content)
	if len(fields) != 7 {
		return "", errors.New("SOA content must contain seven fields")
	}
	mname, err := hostname.CanonicalFQDN(fields[0])
	if err != nil {
		return "", errors.New("invalid SOA primary nameserver")
	}
	rname, err := hostname.CanonicalFQDN(fields[1])
	if err != nil {
		return "", errors.New("invalid SOA responsible mailbox")
	}
	numbers := make([]string, 5)
	for index, field := range fields[2:] {
		value, err := strconv.ParseUint(field, 10, 32)
		if err != nil {
			return "", fmt.Errorf("invalid SOA numeric field %d", index+1)
		}
		numbers[index] = strconv.FormatUint(value, 10)
	}
	return strings.Join(append([]string{mname, rname}, numbers...), " "), nil
}

func canonicalCAA(content string) (string, error) {
	fields := strings.Fields(content)
	if len(fields) < 3 {
		return "", errors.New("CAA content must be: flags tag value")
	}
	flags, err := strconv.ParseUint(fields[0], 10, 8)
	if err != nil {
		return "", errors.New("invalid CAA flags")
	}
	tag := strings.ToLower(fields[1])
	if tag == "" || len(tag) > 15 {
		return "", errors.New("invalid CAA tag")
	}
	for _, character := range tag {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
			return "", errors.New("invalid CAA tag")
		}
	}
	value := strings.TrimSpace(strings.Join(fields[2:], " "))
	if strings.HasPrefix(value, `"`) || strings.HasSuffix(value, `"`) {
		if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
			return "", errors.New("invalid quoted CAA value")
		}
		value = value[1 : len(value)-1]
	}
	if value == "" || len(value) > 1024 || strings.ContainsAny(value, `"\\`) {
		return "", errors.New("invalid CAA value")
	}
	return fmt.Sprintf(`%d %s "%s"`, flags, tag, value), nil
}

func canonicalTLSA(content string) (string, error) {
	fields := strings.Fields(content)
	if len(fields) != 4 {
		return "", errors.New("TLSA content must be: usage selector matching-type certificate-data")
	}
	usage, err := strconv.ParseUint(fields[0], 10, 8)
	if err != nil || usage > 3 {
		return "", errors.New("invalid TLSA certificate usage")
	}
	selector, err := strconv.ParseUint(fields[1], 10, 8)
	if err != nil || selector > 1 {
		return "", errors.New("invalid TLSA selector")
	}
	matchingType, err := strconv.ParseUint(fields[2], 10, 8)
	if err != nil || matchingType > 2 {
		return "", errors.New("invalid TLSA matching type")
	}
	data := strings.ToLower(fields[3])
	if len(data) < 2 || len(data) > 8192 || len(data)%2 != 0 {
		return "", errors.New("invalid TLSA certificate data length")
	}
	for _, character := range data {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return "", errors.New("invalid TLSA certificate data")
		}
	}
	return fmt.Sprintf("%d %d %d %s", usage, selector, matchingType, data), nil
}

func renderRData(record canonicalRecord) string {
	switch record.Type {
	case "CNAME", "NS":
		return record.Content + "."
	case "MX":
		return strconv.Itoa(record.Prio) + " " + record.Content + "."
	case "SRV":
		fields := strings.Fields(record.Content)
		target := fields[2]
		if target != "." {
			target += "."
		}
		return fmt.Sprintf("%d %s %s %s", record.Prio, fields[0], fields[1], target)
	case "SOA":
		fields := strings.Fields(record.Content)
		fields[0] += "."
		fields[1] += "."
		return strings.Join(fields, " ")
	default:
		return record.Content
	}
}

func lessCanonicalRecord(left, right canonicalRecord) bool {
	if left.Name != right.Name {
		return left.Name < right.Name
	}
	if left.Type != right.Type {
		return left.Type < right.Type
	}
	if left.Content != right.Content {
		return left.Content < right.Content
	}
	if left.TTL != right.TTL {
		return left.TTL < right.TTL
	}
	if left.Prio != right.Prio {
		return left.Prio < right.Prio
	}
	return !left.Disabled && right.Disabled
}

func sha256Hex(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

func emptyRecordsSHA256() string {
	encoded, _ := json.Marshal(recordDigestDocument{
		Schema:  manifestSchema + "/records",
		Records: []canonicalRecord{},
	})
	return sha256Hex(encoded)
}
