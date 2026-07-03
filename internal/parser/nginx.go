package parser

import (
	"fmt"
	"regexp"
	"strings"
)

type NginxParser struct{}

func NewNginxParser() *NginxParser {
	return &NginxParser{}
}

// Parse is a simplified parser for MVP. 
// It mainly looks for "server { ... }" blocks and extracts basic directives.
// For a production-ready parser, we would need a proper lexer/parser (e.g., using 'text/scanner').
func (p *NginxParser) Parse(content string) (interface{}, error) {
	blocks := []Block{}
	
	// Very basic regex to find server blocks
	// This is NOT robust for nested braces but sufficient for MVP "managed" blocks
	// We assume standard formatting for managed files.
	serverRegex := regexp.MustCompile(`(?s)server\s*\{(.*?)\}`)
	matches := serverRegex.FindAllStringSubmatch(content, -1)

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		blockBody := match[1]
		
		b := Block{
			Name:       "server",
			Directives: make(map[string]string),
		}

		// Extract server_name
		if nameMatch := regexp.MustCompile(`server_name\s+(.*?);`).FindStringSubmatch(blockBody); len(nameMatch) > 1 {
			b.Directives["server_name"] = strings.TrimSpace(nameMatch[1])
		}

		// Extract listen
		if listenMatch := regexp.MustCompile(`listen\s+(.*?);`).FindStringSubmatch(blockBody); len(listenMatch) > 1 {
			b.Directives["listen"] = strings.TrimSpace(listenMatch[1])
		}

		// Extract root
		if rootMatch := regexp.MustCompile(`root\s+(.*?);`).FindStringSubmatch(blockBody); len(rootMatch) > 1 {
			b.Directives["root"] = strings.TrimSpace(rootMatch[1])
		}

		blocks = append(blocks, b)
	}

	return blocks, nil
}

// Generate creates a valid Nginx config from the structured data
func (p *NginxParser) Generate(data interface{}) (string, error) {
	blocks, ok := data.([]Block)
	if !ok {
		return "", fmt.Errorf("invalid data type for Nginx generator")
	}

	var sb strings.Builder
	sb.WriteString("# @managed-by: celikpanel\n\n")

	for _, b := range blocks {
		if b.Name == "server" {
			sb.WriteString("server {\n")
			
			if val, ok := b.Directives["listen"]; ok {
				sb.WriteString(fmt.Sprintf("    listen %s;\n", val))
			}
			if val, ok := b.Directives["server_name"]; ok {
				sb.WriteString(fmt.Sprintf("    server_name %s;\n", val))
			}
			if val, ok := b.Directives["root"]; ok {
				sb.WriteString(fmt.Sprintf("    root %s;\n", val))
			}
			
			// Default standard settings
			sb.WriteString("    index index.php index.html;\n")
			sb.WriteString("    location / {\n")
			sb.WriteString("        try_files $uri $uri/ =404;\n")
			sb.WriteString("    }\n")

			sb.WriteString("}\n\n")
		}
	}

	return sb.String(), nil
}

func (p *NginxParser) Validate(content string) error {
	// In a real scenario, we would run `nginx -t` here.
	// For now, just check if it's not empty.
	if len(strings.TrimSpace(content)) == 0 {
		return fmt.Errorf("config content is empty")
	}
	return nil
}
