//go:build !unix

package hostingpath

import "errors"

var errTraversalUnsupported = errors.New("hosting root traversal is only inspected on Unix servers")

// StatDirectory is unavailable off Unix.
func StatDirectory(string) (DirectoryState, error) {
	return DirectoryState{}, errTraversalUnsupported
}

// WebServerAccount is unavailable off Unix.
func WebServerAccount() (Account, bool) {
	return Account{}, false
}

// TraversalAccounts is every site user off Unix.
func TraversalAccounts() []Account {
	return []Account{SiteAccounts()}
}

// NameOwners leaves numeric owners off Unix.
func NameOwners(*TraversalBlock) {}

// ProbeHostingRoot cannot inspect a non-Unix host.
func ProbeHostingRoot() (*TraversalBlock, error) {
	return nil, errTraversalUnsupported
}
