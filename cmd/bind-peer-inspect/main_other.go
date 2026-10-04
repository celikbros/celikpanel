//go:build !linux

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "native BIND peer inspection is available only on Linux")
	os.Exit(1)
}
