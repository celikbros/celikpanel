package main

import (
	"os/user"
	"strconv"
)

// lookupGroupID resolves a system group's numeric gid.
// lookupGroupID, bir sistem grubunun sayısal gid'ini çözer.
func lookupGroupID(name string) (int, bool) {
	g, err := user.LookupGroup(name)
	if err != nil {
		return 0, false
	}
	gid, err := strconv.Atoi(g.Gid)
	return gid, err == nil
}
