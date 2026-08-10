package core

import "time"

// TeamCapability is a permission that can be granted to an additional user.
// Keep this list closed: unknown values must never silently become permissions.
type TeamCapability string

const (
	TeamCapabilityFiles      TeamCapability = "files"
	TeamCapabilityDatabases  TeamCapability = "databases"
	TeamCapabilityMail       TeamCapability = "mail"
	TeamCapabilityDNS        TeamCapability = "dns"
	TeamCapabilitySSL        TeamCapability = "ssl"
	TeamCapabilityCron       TeamCapability = "cron"
	TeamCapabilityBackups    TeamCapability = "backups"
	TeamCapabilityPHP        TeamCapability = "php"
	TeamCapabilityStatistics TeamCapability = "statistics"
)

// ValidTeamCapability deliberately fails closed for new/unknown values.
func ValidTeamCapability(capability TeamCapability) bool {
	switch capability {
	case TeamCapabilityFiles,
		TeamCapabilityDatabases,
		TeamCapabilityMail,
		TeamCapabilityDNS,
		TeamCapabilitySSL,
		TeamCapabilityCron,
		TeamCapabilityBackups,
		TeamCapabilityPHP,
		TeamCapabilityStatistics:
		return true
	default:
		return false
	}
}

type TeamPermissionMode string

const (
	TeamPermissionView   TeamPermissionMode = "view"
	TeamPermissionManage TeamPermissionMode = "manage"
)

// ValidTeamPermissionMode deliberately fails closed for new/unknown values.
func ValidTeamPermissionMode(mode TeamPermissionMode) bool {
	return mode == TeamPermissionView || mode == TeamPermissionManage
}

type TeamSubscriptionPermission struct {
	SubscriptionID   int                `json:"subscription_id"`
	SubscriptionName string             `json:"subscription_name,omitempty"`
	Capability       TeamCapability     `json:"capability"`
	Mode             TeamPermissionMode `json:"mode"`
}

type TeamDomainPermission struct {
	DomainID   int                `json:"domain_id"`
	DomainName string             `json:"domain_name,omitempty"`
	Capability TeamCapability     `json:"capability"`
	Mode       TeamPermissionMode `json:"mode"`
}

type TeamMemberAccess struct {
	SubscriptionPermissions []TeamSubscriptionPermission `json:"subscription_permissions"`
	DomainPermissions       []TeamDomainPermission       `json:"domain_permissions"`
}

type TeamMember struct {
	ID        int              `json:"id"`
	OwnerID   int              `json:"owner_id"`
	Username  string           `json:"username"`
	Email     string           `json:"email"`
	Status    string           `json:"status"`
	Access    TeamMemberAccess `json:"access"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
}
