//go:build linux && celikpanel_mail_renewal

package main

// This compile-time entry excludes ordinary Agent startup and CLI dispatch.
const mailRenewalOnlyBuild = true
