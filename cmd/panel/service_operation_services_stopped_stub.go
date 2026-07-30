//go:build !linux

package main

import "fmt"

func verifyCelikPanelServicesStoppedPlatform() error {
	return fmt.Errorf("secure CelikPanel service stop proof requires Linux")
}
