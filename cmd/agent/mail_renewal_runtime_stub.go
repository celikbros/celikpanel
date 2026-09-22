//go:build !linux

package main

func prepareIndependentMailRuntime() error { return mailHostLinuxOnly() }

func recoverIndependentSelectedMailRenewal(mailHostRenewal) error { return mailHostLinuxOnly() }
