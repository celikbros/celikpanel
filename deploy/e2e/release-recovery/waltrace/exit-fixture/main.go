package main

import (
	"os"
	"runtime"
)

func main() {
	runtime.GOMAXPROCS(8)
	ready := make(chan struct{}, 1)
	for i := 0; i < 2; i++ {
		go func() {
			runtime.LockOSThread()
			select {
			case ready <- struct{}{}:
			default:
			}
			select {}
		}()
	}
	<-ready
	os.Exit(0)
}
