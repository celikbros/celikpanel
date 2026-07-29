package main

import "testing"

func TestSafePanelPathRejectsExternalTraversalAndControlPaths(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "", want: true},
		{path: "/services/wireguard", want: true},
		{path: "/domains", want: true},
		{path: "services/wireguard", want: false},
		{path: "//evil.example/path", want: false},
		{path: `\services\wireguard`, want: false},
		{path: "/https://evil.example", want: false},
		{path: "/services/../settings", want: false},
		{path: "/services/./wireguard", want: false},
		{path: "/services/\nwireguard", want: false},
		{path: "/services/%00wireguard", want: false},
		{path: "/services/%0awireguard", want: false},
		{path: "/%2f%2fevil.example", want: false},
		{path: "/services/%2e%2e/settings", want: false},
		{path: "/%252f%252fevil.example", want: false},
	}
	for _, test := range tests {
		if got := safePanelPath(test.path); got != test.want {
			t.Errorf("safePanelPath(%q) = %v, want %v", test.path, got, test.want)
		}
	}
}
