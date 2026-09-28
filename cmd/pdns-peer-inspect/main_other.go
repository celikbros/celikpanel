//go:build !linux

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "native PowerDNS peer inspection is available only on Linux")
	os.Exit(1)
}
