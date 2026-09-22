//go:build !linux

package main

func prepareIndependentMailRuntime() error { return mailHostLinuxOnly() }

func recoverIndependentSelectedMailRenewal(mailHostRenewal, string) error { return mailHostLinuxOnly() }

func recoverIndependentPendingMailRenewal(mailHostRenewal) error { return mailHostLinuxOnly() }
