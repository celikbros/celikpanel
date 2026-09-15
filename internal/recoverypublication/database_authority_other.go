//go:build !linux

package recoverypublication

func OpenDatabaseAuthority(string) (*DatabaseAuthority, error) { return nil, ErrUnavailable }
func VerifyDatabasePolicy(string) error                        { return ErrUnavailable }

func ProbeDatabaseMigration() error { return ErrUnavailable }
