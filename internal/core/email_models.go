package core

// PostfixQueueItem represents an email in the queue
type PostfixQueueItem struct {
	ID      string `json:"id"`
	Size    string `json:"size"`
	Sender  string `json:"sender"`
	Arrival string `json:"arrival"`
	Status  string `json:"status"` // active, deferred, hold
}

// PostfixSummary represents queue statistics
type PostfixSummary struct {
	Active   int `json:"active"`
	Deferred int `json:"deferred"`
	Hold     int `json:"hold"`
	Corrupt  int `json:"corrupt"`
}

// DovecotStats represents Dovecot server statistics
type DovecotStats struct {
	Uptime      string `json:"uptime"`
	Connections int    `json:"connections"`
	Logins      int    `json:"logins"`
	AuthSuccess int    `json:"auth_success"`
	AuthFail    int    `json:"auth_fail"`
}

// PostfixActionRequest represents a request to manage queue
type PostfixActionRequest struct {
	Action string `json:"action"` // flush, delete_all, delete_id
	ID     string `json:"id,omitempty"`
}

// PostfixQueueResult is the real, agent-sourced queue state. Installed is
// false when Postfix is not present, so the UI can be honest instead of
// showing numbers for a service that isn't there.
// PostfixQueueResult, agent'tan gelen gerçek kuyruk durumudur. Postfix
// mevcut değilse Installed false olur; böylece arayüz, olmayan bir servis
// için sayı göstermek yerine dürüst olabilir.
type PostfixQueueResult struct {
	Installed bool               `json:"installed"`
	Items     []PostfixQueueItem `json:"items"`
	Summary   PostfixSummary     `json:"summary"`
}

// DovecotStatsResult carries real Dovecot data. Available marks whether the
// counters could actually be read; unmeasurable values stay zero rather
// than being invented.
// DovecotStatsResult gerçek Dovecot verisini taşır. Available, sayaçların
// gerçekten okunup okunamadığını işaretler; ölçülemeyen değerler
// uydurulmak yerine sıfır kalır.
type DovecotStatsResult struct {
	Installed bool         `json:"installed"`
	Stats     DovecotStats `json:"stats"`
}
