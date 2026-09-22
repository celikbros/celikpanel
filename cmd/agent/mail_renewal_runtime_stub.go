//go:build !linux

package main

import "context"

func prepareIndependentMailRuntime() error { return mailHostLinuxOnly() }

func recoverIndependentSelectedMailRenewal(mailHostRenewal, string) error { return mailHostLinuxOnly() }

func recoverIndependentPendingMailRenewal(mailHostRenewal) error { return mailHostLinuxOnly() }

func resumeIndependentMailEnrollment(context.Context, string) error { return mailHostLinuxOnly() }
