package main

var

// Managed PowerDNS drop-ins are root-owned in production. Focused tests
// replace this with the current euid because their temporary directories
// cannot be root-owned.
dnsClusterConfigRequiredOwnerUID = uint32(0)
