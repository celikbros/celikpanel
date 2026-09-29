//go:build !linux

package main

import "context"

// dpkg exists only on the Linux hosts the Agent supports.
var packageStatOverridePreflight = func(context.Context) error { return nil }
