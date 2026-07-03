package services

import "testing"

func TestValidatePHPVersion(t *testing.T) {
	for _, v := range []string{"8.3", "8.4", "7.4", "10.20"} {
		if err := ValidatePHPVersion(v); err != nil {
			t.Errorf("ValidatePHPVersion(%q) = %v, want nil", v, err)
		}
	}

	// Each of these would escape /etc/php/<version>/... into an
	// arbitrary root-owned path if allowed through.
	// Bunların her biri, izin verilse /etc/php/<version>/... dışına keyfi
	// root'a-ait bir yola kaçardı.
	for _, v := range []string{
		"",
		"8",
		"../../../etc/cron.d/x",
		"8.3/../../..",
		"8.3; rm -rf /",
		"8.3\n",
		"latest",
	} {
		if err := ValidatePHPVersion(v); err == nil {
			t.Errorf("ValidatePHPVersion(%q) = nil, want error", v)
		}
	}
}
