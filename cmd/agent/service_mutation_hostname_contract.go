package main

import "github.com/alicelik/celikpanel/internal/servicemutationledger"

func serviceMutationCanonicalFQDN(value string) bool {
	return servicemutationledger.ServiceMutationCanonicalFQDN(value)
}
