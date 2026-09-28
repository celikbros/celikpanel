//go:build !linux

package main

import "errors"

func retireExactRunningBINDAdoptionInstall(dnsEngineSwitchJournal, dnsEngineInstallOwnershipReceipt) error {
	return errors.New("independent BIND adoption receipt retirement requires Linux")
}

func publishNewRunningBINDAdoptionInstall(dnsEngineInstallOwnershipReceipt) error {
	return errors.New("independent BIND adoption receipt publication requires Linux")
}

func verifyExactRunningBINDAdoptionInstall(dnsEngineInstallOwnershipReceipt) error {
	return errors.New("independent BIND adoption receipt proof requires Linux")
}
