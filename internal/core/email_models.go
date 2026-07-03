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
