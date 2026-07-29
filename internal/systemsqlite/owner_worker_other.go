//go:build !linux

package systemsqlite

import (
	"errors"
	"os"
)

// NewOwnerProcessMutableOperations fails closed outside Linux.
// NewOwnerProcessMutableOperations, Linux dışında güvenli biçimde işlemi reddeder.
func NewOwnerProcessMutableOperations([]Definition, string) (MutableOperations, error) {
	return unavailableMutableOperations{}, errors.New("owner-isolated SQLite operations require Linux")
}

func validateOwnerWorkerProcess() error {
	return errors.New("owner-isolated SQLite operations require Linux")
}

func verifyOwnerWorkerSource(*managedSource, Definition) error {
	return errors.New("owner-isolated SQLite operations require Linux")
}

func validateOwnerWorkerDestination(*os.File) error {
	return errors.New("owner-isolated SQLite operations require Linux")
}

func prepareOwnerWorkerWorkspace(*os.File) error {
	return errors.New("owner-isolated SQLite operations require Linux")
}
