package servicemutationledger

import (
	"github.com/alicelik/celikpanel/internal/hostname"
)

func ServiceMutationCanonicalFQDN(value string) bool {
	canonical, err := hostname.CanonicalFQDN(value)
	return err == nil && canonical == value
}
