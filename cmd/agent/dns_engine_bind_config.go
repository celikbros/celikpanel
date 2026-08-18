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
		end := start + endStart + len(bindZonesMarkerEnd)
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

func managedBINDOptions(config, transferPeer string) (string, error) {
	transferACL := "none"
	if transferPeer != "" {
		peer := net.ParseIP(transferPeer)
		if peer == nil || peer.To4() == nil || peer.To4().String() != transferPeer ||
			!peer.IsGlobalUnicast() {
			return "", errors.New("BIND transfer peer must be a canonical public IPv4 address")
		}
		transferACL = transferPeer + "/32"
	}
	block := "\n\t" + bindOptionsMarkerBegin +
		"\n\trecursion no;" +
		"\n\tallow-recursion { none; };" +
		"\n\tallow-query-cache { none; };" +
		"\n\tallow-transfer { " + transferACL + "; };" +
		"\n\t" + bindOptionsMarkerEnd + "\n"
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
		end := strings.Index(config[start:], bindOptionsMarkerEnd)
		if start < open || end < 0 || start+end+len(bindOptionsMarkerEnd) > close {
			return "", errors.New("managed BIND options markers escape the options block")
		}
		actualEnd := start + end + len(bindOptionsMarkerEnd)
		canonical := strings.TrimSuffix(strings.TrimPrefix(block, "\n\t"), "\n")
		legacy := strings.TrimSuffix(strings.TrimPrefix(legacyBlock, "\n\t"), "\n")
		actual := config[start:actualEnd]
		if actual != canonical && actual != legacy {
			return "", errors.New("existing CelikPanel BIND options were modified")
		}
		outside := config[open+1:start] + config[actualEnd:close]
		body := stripBINDCommentsAndStrings(outside)
		for _, directive := range []string{
			"recursion", "allow-recursion", "allow-query-cache", "allow-transfer",
		} {
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
	for _, directive := range []string{
		"recursion", "allow-recursion", "allow-query-cache", "allow-transfer",
	} {
		if bindContainsDirective(body, directive) {
			return "", fmt.Errorf("BIND options already define %s outside CelikPanel ownership", directive)
		}
	}
	return config[:close] + block + config[close:], nil
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
