//go:build !linux

package main

import "fmt"

func prepareRecoveryDatabaseMigration(string) (string, error) {
	return "", fmt.Errorf("isolated database migration requires Linux")
}
func publishRecoveryDatabaseMigration(string) error {
	return fmt.Errorf("isolated database migration requires Linux")
}
func restoreRecoveryDatabaseMigration(string) error {
	return fmt.Errorf("isolated database migration requires Linux")
}
func verifyRecoveryDatabaseMigration(string) error {
	return fmt.Errorf("isolated database migration requires Linux")
}
