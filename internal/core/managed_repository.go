package core

import (
	"fmt"
	"regexp"
	"strings"
)

// InstallRequiresManagedRepository reports whether this exact install choice
// depends on the service's managed repository. A repository can be optional
// for the distro default package while still being mandatory for a selected
// vendor version such as postgresql-18.
func InstallRequiresManagedRepository(service *ManagedService, selectedPackage string) (bool, error) {
	if service == nil || service.Repo == nil {
		return false, nil
	}
	if service.Repo.Required {
		return true, nil
	}
	selectedPackage = strings.TrimSpace(selectedPackage)
	pattern := strings.TrimSpace(service.Repo.PackagePattern)
	if selectedPackage == "" || pattern == "" {
		return false, nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false, fmt.Errorf("managed repository %q has an invalid package pattern: %w", service.Repo.ID, err)
	}
	match := re.FindString(selectedPackage)
	return match == selectedPackage, nil
}
