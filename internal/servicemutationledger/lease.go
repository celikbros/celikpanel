package servicemutationledger

// These are historical durable lease-expiry values, not a new schema. Recovery
// consumers must not independently invent a different cancelling identity.
const (
	PhaseCancellingExpiredLease = "cancelling_expired_lease"
	ErrorLeaseExpired           = "service_mutation_lease_expired"
	MessageLeaseExpired         = "The panel stopped heartbeating before the service mutation completed."
)
