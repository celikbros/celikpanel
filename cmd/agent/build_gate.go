package main

import (
	"fmt"
	"strings"
)

// requireExpectedBuildCommit binds a privileged mutation to the exact panel
// build that authorized it. Development binaries intentionally use "unknown";
// a production agent never accepts a missing/unknown caller identity.
func requireExpectedBuildCommit(expectedRaw, operation string) error {
	expected := strings.TrimSpace(expectedRaw)
	actual := strings.TrimSpace(buildCommit)
	expectedDevelopment := expected == "" || expected == "unknown"
	actualDevelopment := actual == "" || actual == "unknown"

	if expectedDevelopment && actualDevelopment {
		return nil
	}
	if expectedDevelopment {
		return fmt.Errorf(
			"expected panel build commit is required before %s",
			operation,
		)
	}
	if actualDevelopment || actual != expected {
		return fmt.Errorf(
			"panel/agent build mismatch (panel %s, agent %s); finish the paired upgrade before %s",
			expected,
			actual,
			operation,
		)
	}
	return nil
}
