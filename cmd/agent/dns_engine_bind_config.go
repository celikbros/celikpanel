package main

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

const (
	bindZonesMarkerBegin   = "// BEGIN CELIKPANEL MANAGED BIND ZONES"
	bindZonesMarkerEnd     = "// END CELIKPANEL MANAGED BIND ZONES"
	bindOptionsMarkerBegin = "// BEGIN CELIKPANEL MANAGED BIND OPTIONS"
	bindOptionsMarkerEnd   = "// END CELIKPANEL MANAGED BIND OPTIONS"
)

func managedBINDZoneInclude(config, includePath string) (string, error) {
	if includePath == "" || !strings.HasPrefix(includePath, "/") ||
		strings.ContainsAny(includePath, "\x00\n\"\\") {
		return "", errors.New("invalid managed BIND zone include path")
	}
	block := bindZonesMarkerBegin + "\ninclude \"" + includePath + "\";\n" +
		bindZonesMarkerEnd + "\n"
	beginCount := strings.Count(config, bindZonesMarkerBegin)
	endCount := strings.Count(config, bindZonesMarkerEnd)
	if beginCount != endCount || beginCount > 1 {
		return "", errors.New("BIND zone include markers are incomplete or duplicated")
	}
	if beginCount == 1 {
		start := strings.Index(config, bindZonesMarkerBegin)
		endStart := strings.Index(config[start:], bindZonesMarkerEnd)
		if endStart < 0 {
			return "", errors.New("BIND zone include marker is incomplete")
		}
		endStart += start
		if !bindMarkerStartsActiveComment(config, start) ||
			!bindMarkerStartsActiveComment(config, endStart) {
			return "", errors.New("BIND zone include markers are not active configuration comments")
		}
		end := endStart + len(bindZonesMarkerEnd)
		if end < len(config) && config[end] == '\r' {
			end++
		}
		if end < len(config) && config[end] == '\n' {
			end++
		}
		if config[start:end] != block {
			return "", errors.New("existing CelikPanel BIND zone include was modified")
		}
		return config, nil
	}
	if strings.Contains(config, includePath) {
		return "", errors.New("managed BIND zone include exists outside its ownership markers")
	}
	if config != "" && !strings.HasSuffix(config, "\n") {
		config += "\n"
	}
	if config != "" {
		config += "\n"
	}
	return config + block, nil
}

// canonicalBINDTransferACL is the one place a transfer peer becomes an address
// match list. managedBINDOptions writes it into the file and
// managedBINDOptionAssignments shows it to the operator; both read it here so
// the screen and the file cannot say different things.
//
// canonicalBINDTransferACL, bir aktarım eşinin adres eşleşme listesine
// dönüştüğü tek yerdir. managedBINDOptions onu dosyaya yazar,
// managedBINDOptionAssignments onu operatöre gösterir; ikisi de buradan okur,
// böylece ekran ile dosya farklı şeyler söyleyemez.
func canonicalBINDTransferACL(transferPeer string) (string, error) {
	peer := net.ParseIP(transferPeer)
	if peer == nil || peer.To4() == nil || peer.To4().String() != transferPeer ||
		!peer.IsGlobalUnicast() {
		return "", errors.New("BIND transfer peer must be a canonical public IPv4 address")
	}
	return transferPeer + "/32", nil
}

func managedBINDOptions(config, transferPeer string) (string, error) {
	assignments, err := managedBINDOptionAssignments(transferPeer)
	if err != nil {
		return "", err
	}
	block := "\n\t" + bindOptionsMarkerBegin
	for _, assignment := range assignments {
		block += "\n\t" + assignment[0] + " " + assignment[1] + ";"
	}
	block += "\n\t" + bindOptionsMarkerEnd + "\n"
	legacyBlock := "\n\t" + bindOptionsMarkerBegin +
		"\n\trecursion no;" +
		"\n\tallow-recursion { none; };" +
		"\n\tallow-query-cache { none; };" +
		"\n\t" + bindOptionsMarkerEnd + "\n"
	beginCount := strings.Count(config, bindOptionsMarkerBegin)
	endCount := strings.Count(config, bindOptionsMarkerEnd)
	if beginCount != endCount || beginCount > 1 {
		return "", errors.New("BIND options markers are incomplete or duplicated")
	}
	open, close, err := bindOptionsBlock(config)
	if err != nil {
		return "", err
	}
	if beginCount == 1 {
		start := strings.Index(config, bindOptionsMarkerBegin)
		endOffset := strings.Index(config[start:], bindOptionsMarkerEnd)
		endStart := start + endOffset
		if start < open || endOffset < 0 ||
			!bindMarkerStartsActiveComment(config, start) ||
			!bindMarkerStartsActiveComment(config, endStart) ||
			endStart+len(bindOptionsMarkerEnd) > close {
			return "", errors.New("managed BIND options markers escape the options block")
		}
		actualEnd := endStart + len(bindOptionsMarkerEnd)
		canonical := strings.TrimSuffix(strings.TrimPrefix(block, "\n\t"), "\n")
		legacy := strings.TrimSuffix(strings.TrimPrefix(legacyBlock, "\n\t"), "\n")
		actual := config[start:actualEnd]
		if actual != canonical && actual != legacy {
			return "", errors.New("existing CelikPanel BIND options were modified")
		}
		body := bindOptionsBodyWithoutManagedSpan(
			config, open, close, start, actualEnd,
		)
		for _, directive := range bindManagedOptionDirectives {
			if bindContainsDirective(body, directive) {
				return "", fmt.Errorf(
					"BIND options already define %s outside CelikPanel ownership", directive,
				)
			}
		}
		if actual == canonical {
			return config, nil
		}
		return config[:start] + canonical + config[actualEnd:], nil
	}
	body := stripBINDCommentsAndStrings(config[open+1 : close])
	for _, directive := range bindManagedOptionDirectives {
		if bindContainsDirective(body, directive) {
			return "", fmt.Errorf("BIND options already define %s outside CelikPanel ownership", directive)
		}
	}
	return config[:close] + block + config[close:], nil
}

func exactLegacyManagedBINDOptions(config string) bool {
	if strings.Count(config, bindOptionsMarkerBegin) != 1 ||
		strings.Count(config, bindOptionsMarkerEnd) != 1 {
		return false
	}
	open, close, err := bindOptionsBlock(config)
	if err != nil {
		return false
	}
	start := strings.Index(config, bindOptionsMarkerBegin)
	endOffset := strings.Index(config[start:], bindOptionsMarkerEnd)
	if start < open || endOffset < 0 {
		return false
	}
	endStart := start + endOffset
	if !bindMarkerStartsActiveComment(config, start) ||
		!bindMarkerStartsActiveComment(config, endStart) {
		return false
	}
	end := endStart + len(bindOptionsMarkerEnd)
	if end > close {
		return false
	}
	legacy := bindOptionsMarkerBegin +
		"\n\trecursion no;" +
		"\n\tallow-recursion { none; };" +
		"\n\tallow-query-cache { none; };" +
		"\n\t" + bindOptionsMarkerEnd
	if config[start:end] != legacy {
		return false
	}
	outside := bindOptionsBodyWithoutManagedSpan(config, open, close, start, end)
	for _, directive := range bindManagedOptionDirectives {
		if bindContainsDirective(outside, directive) {
			return false
		}
	}
	return true
}

func managedBINDLegacyOptions(config string) (string, error) {
	if exactLegacyManagedBINDOptions(config) {
		return config, nil
	}
	canonical, err := managedBINDOptions(config, "")
	if err != nil || canonical != config {
		if err == nil {
			err = errors.New("BIND options are not the exact managed directional policy")
		}
		return "", err
	}
	start := strings.Index(config, bindOptionsMarkerBegin)
	endOffset := strings.Index(config[start:], bindOptionsMarkerEnd)
	if start < 0 || endOffset < 0 {
		return "", errors.New("managed BIND options markers are unavailable")
	}
	end := start + endOffset + len(bindOptionsMarkerEnd)
	legacy := bindOptionsMarkerBegin +
		"\n\trecursion no;" +
		"\n\tallow-recursion { none; };" +
		"\n\tallow-query-cache { none; };" +
		"\n\t" + bindOptionsMarkerEnd
	return config[:start] + legacy + config[end:], nil
}

func bindContainsDirective(body, directive string) bool {
	for index := 0; index < len(body); {
		for index < len(body) && !bindIdentifierStart(body[index]) {
			index++
		}
		start := index
		for index < len(body) && bindIdentifierPart(body[index]) {
			index++
		}
		if start < index && body[start:index] == directive {
			return true
		}
	}
	return false
}

func bindOptionsBodyWithoutManagedSpan(
	config string,
	open, close, managedStart, managedEnd int,
) string {
	clean := []byte(stripBINDCommentsAndStrings(config))
	for index := managedStart; index < managedEnd && index < len(clean); index++ {
		clean[index] = ' '
	}
	return string(clean[open+1 : close])
}

// bindMarkerStartsActiveComment proves that a CelikPanel marker begins a real
// line comment in configuration syntax. Raw substring matching is insufficient:
// an exact marker wrapped in a block comment, line comment, or quoted string is
// inert even though its bytes are otherwise unchanged.
func bindMarkerStartsActiveComment(config string, markerStart int) bool {
	if markerStart < 0 || markerStart+1 >= len(config) ||
		config[markerStart] != '/' || config[markerStart+1] != '/' {
		return false
	}
	const (
		bindLexCode = iota
		bindLexString
		bindLexLineComment
		bindLexBlockComment
	)
	state := bindLexCode
	for index := 0; index < markerStart; {
		switch state {
		case bindLexCode:
			switch {
			case config[index] == '"':
				state = bindLexString
				index++
			case config[index] == '#':
				state = bindLexLineComment
				index++
			case config[index] == '/' && index+1 < markerStart && config[index+1] == '/':
				state = bindLexLineComment
				index += 2
			case config[index] == '/' && index+1 < markerStart && config[index+1] == '*':
				state = bindLexBlockComment
				index += 2
			default:
				index++
			}
		case bindLexString:
			if config[index] == '\\' && index+1 < markerStart {
				index += 2
				continue
			}
			if config[index] == '"' {
				state = bindLexCode
			}
			index++
		case bindLexLineComment:
			if config[index] == '\n' {
				state = bindLexCode
			}
			index++
		case bindLexBlockComment:
			if config[index] == '*' && index+1 < markerStart && config[index+1] == '/' {
				state = bindLexCode
				index += 2
				continue
			}
			index++
		}
	}
	return state == bindLexCode
}

func bindOptionsBlock(config string) (int, int, error) {
	clean := stripBINDCommentsAndStrings(config)
	foundOpen, foundClose := -1, -1
	depth := 0
	for index := 0; index < len(clean); {
		if clean[index] == '{' {
			depth++
			index++
			continue
		}
		if clean[index] == '}' {
			if depth == 0 {
				return 0, 0, errors.New("BIND configuration has an unmatched closing brace")
			}
			depth--
			if foundOpen >= 0 && depth == 0 && foundClose < 0 {
				foundClose = index
			}
			index++
			continue
		}
		if !bindIdentifierStart(clean[index]) {
			index++
			continue
		}
		start := index
		for index < len(clean) && bindIdentifierPart(clean[index]) {
			index++
		}
		if clean[start:index] != "options" || depth != 0 {
			continue
		}
		cursor := index
		for cursor < len(clean) && (clean[cursor] == ' ' || clean[cursor] == '\t' || clean[cursor] == '\r' || clean[cursor] == '\n') {
			cursor++
		}
		if cursor >= len(clean) || clean[cursor] != '{' {
			return 0, 0, errors.New("BIND options statement is malformed")
		}
		if foundOpen >= 0 {
			return 0, 0, errors.New("BIND configuration contains multiple options blocks")
		}
		foundOpen = cursor
		depth = 1
		index = cursor + 1
	}
	if depth != 0 {
		return 0, 0, errors.New("BIND configuration has an unclosed brace")
	}
	if foundOpen < 0 || foundClose < 0 {
		return 0, 0, errors.New("BIND configuration has no complete top-level options block")
	}
	return foundOpen, foundClose, nil
}

func stripBINDCommentsAndStrings(config string) string {
	clean := []byte(config)
	for index := 0; index < len(clean); {
		switch {
		case clean[index] == '"':
			clean[index] = ' '
			index++
			for index < len(clean) {
				character := clean[index]
				clean[index] = ' '
				index++
				if character == '\\' && index < len(clean) {
					clean[index] = ' '
					index++
					continue
				}
				if character == '"' {
					break
				}
			}
		case clean[index] == '/' && index+1 < len(clean) && clean[index+1] == '/':
			for index < len(clean) && clean[index] != '\n' {
				clean[index] = ' '
				index++
			}
		case clean[index] == '/' && index+1 < len(clean) && clean[index+1] == '*':
			clean[index], clean[index+1] = ' ', ' '
			index += 2
			for index < len(clean) {
				if index+1 < len(clean) && clean[index] == '*' && clean[index+1] == '/' {
					clean[index], clean[index+1] = ' ', ' '
					index += 2
					break
				}
				if clean[index] != '\n' && clean[index] != '\r' {
					clean[index] = ' '
				}
				index++
			}
		case clean[index] == '#':
			for index < len(clean) && clean[index] != '\n' {
				clean[index] = ' '
				index++
			}
		default:
			index++
		}
	}
	return string(clean)
}

func bindIdentifierStart(character byte) bool {
	return (character >= 'a' && character <= 'z') ||
		(character >= 'A' && character <= 'Z') || character == '_'
}

func bindIdentifierPart(character byte) bool {
	return bindIdentifierStart(character) ||
		(character >= '0' && character <= '9') || character == '-'
}
