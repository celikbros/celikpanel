package main

import (
	"errors"
	"strings"
)

const (
	adminCredentialsFileArgument         = "--admin-credentials-file=-"
	validateAdminCredentialsFileArgument = "--validate-admin-credentials-file=-"
)

type inheritedAdminCredentialsFileFlag struct {
	set   bool
	value string
}

func (value *inheritedAdminCredentialsFileFlag) String() string {
	if value == nil {
		return ""
	}
	return value.value
}

func (value *inheritedAdminCredentialsFileFlag) Set(input string) error {
	if value.set {
		return errors.New("admin credentials file flag may be set only once")
	}
	value.set = true
	value.value = input
	return nil
}

func validateAdminCredentialsFileFlags(
	createAdmin bool,
	credentialsFile inheritedAdminCredentialsFileFlag,
	validationFile inheritedAdminCredentialsFileFlag,
) error {
	if credentialsFile.set {
		if credentialsFile.value != "-" {
			return errors.New("admin credentials file must be inherited on stdin")
		}
		if !createAdmin {
			return errors.New("admin credentials file requires create-admin mode")
		}
	}
	if validationFile.set && validationFile.value != "-" {
		return errors.New("admin credentials validation file must be inherited on stdin")
	}
	return nil
}

func validateAdminCredentialsFileArgumentSpellings(arguments []string) error {
	for _, argument := range arguments {
		switch {
		case strings.HasPrefix(argument, "--admin-credentials-file"):
			if argument != adminCredentialsFileArgument {
				return errors.New("admin credentials file flag must use its exact inherited-stdin form")
			}
		case strings.HasPrefix(argument, "-admin-credentials-file"):
			return errors.New("admin credentials file flag must use its exact inherited-stdin form")
		case strings.HasPrefix(argument, "--validate-admin-credentials-file"):
			if argument != validateAdminCredentialsFileArgument {
				return errors.New("admin credentials validation flag must use its exact inherited-stdin form")
			}
		case strings.HasPrefix(argument, "-validate-admin-credentials-file"):
			return errors.New("admin credentials validation flag must use its exact inherited-stdin form")
		}
	}
	return nil
}
