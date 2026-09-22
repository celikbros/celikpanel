// Package servicemutationledger owns the historical v1 service mutation ledger
// and operation-specific phase codecs shared by Agent writers and recovery readers.
// Validation proves internal consistency only: it grants no authority, checks no
// filesystem trust, and neither establishes host idleness nor resumes work. Callers
// must hold the host lease, read trusted current evidence and inspect other journals
// and native package-manager activity before authorizing a mutation.
package servicemutationledger
