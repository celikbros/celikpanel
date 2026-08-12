package main

import (
	"context"
	"errors"
	"net/http"

	"github.com/alicelik/celikpanel/internal/core"
)

// teamMemberRouteKey is deliberately based on the dispatcher's canonical
// route kind. Every method that an additional user may reach must appear in
// teamMemberDomainRequirements; absence means deny.
type teamMemberRouteKey struct {
	kind   string
	method string
}

type teamMemberDomainRequirement struct {
	capability core.TeamCapability
	mode       core.TeamPermissionMode
}

var teamMemberDomainRequirements = map[teamMemberRouteKey]teamMemberDomainRequirement{
	{kind: "files", method: http.MethodGet}:              {capability: core.TeamCapabilityFiles, mode: core.TeamPermissionView},
	{kind: "files", method: http.MethodPost}:             {capability: core.TeamCapabilityFiles, mode: core.TeamPermissionManage},
	{kind: "files", method: http.MethodOptions}:          {capability: core.TeamCapabilityFiles, mode: core.TeamPermissionView},
	{kind: "files-download", method: http.MethodGet}:     {capability: core.TeamCapabilityFiles, mode: core.TeamPermissionView},
	{kind: "databases", method: http.MethodGet}:          {capability: core.TeamCapabilityDatabases, mode: core.TeamPermissionView},
	{kind: "databases", method: http.MethodPost}:         {capability: core.TeamCapabilityDatabases, mode: core.TeamPermissionManage},
	{kind: "database-delete", method: http.MethodDelete}: {capability: core.TeamCapabilityDatabases, mode: core.TeamPermissionManage},

	{kind: "mail", method: http.MethodGet}:        {capability: core.TeamCapabilityMail, mode: core.TeamPermissionView},
	{kind: "mail", method: http.MethodPost}:       {capability: core.TeamCapabilityMail, mode: core.TeamPermissionManage},
	{kind: "mail", method: http.MethodPut}:        {capability: core.TeamCapabilityMail, mode: core.TeamPermissionManage},
	{kind: "mail", method: http.MethodDelete}:     {capability: core.TeamCapabilityMail, mode: core.TeamPermissionManage},
	{kind: "mail", method: http.MethodOptions}:    {capability: core.TeamCapabilityMail, mode: core.TeamPermissionView},
	{kind: "mail-health", method: http.MethodGet}: {capability: core.TeamCapabilityMail, mode: core.TeamPermissionView},

	{kind: "dns", method: http.MethodGet}:     {capability: core.TeamCapabilityDNS, mode: core.TeamPermissionView},
	{kind: "dns", method: http.MethodPost}:    {capability: core.TeamCapabilityDNS, mode: core.TeamPermissionManage},
	{kind: "dns", method: http.MethodPut}:     {capability: core.TeamCapabilityDNS, mode: core.TeamPermissionManage},
	{kind: "dns", method: http.MethodDelete}:  {capability: core.TeamCapabilityDNS, mode: core.TeamPermissionManage},
	{kind: "dnssec", method: http.MethodGet}:  {capability: core.TeamCapabilityDNS, mode: core.TeamPermissionView},
	{kind: "dnssec", method: http.MethodPost}: {capability: core.TeamCapabilityDNS, mode: core.TeamPermissionManage},

	{kind: "ssl-mail", method: http.MethodGet}:         {capability: core.TeamCapabilitySSL, mode: core.TeamPermissionView},
	{kind: "ssl-mail", method: http.MethodPut}:         {capability: core.TeamCapabilitySSL, mode: core.TeamPermissionManage},
	{kind: "ssl-retry", method: http.MethodPost}:       {capability: core.TeamCapabilitySSL, mode: core.TeamPermissionManage},
	{kind: "ssl-renewal", method: http.MethodPut}:      {capability: core.TeamCapabilitySSL, mode: core.TeamPermissionManage},
	{kind: "ssl-letsencrypt", method: http.MethodPost}: {capability: core.TeamCapabilitySSL, mode: core.TeamPermissionManage},
	{kind: "ssl-upload", method: http.MethodPost}:      {capability: core.TeamCapabilitySSL, mode: core.TeamPermissionManage},
	{kind: "ssl-settings", method: http.MethodPost}:    {capability: core.TeamCapabilitySSL, mode: core.TeamPermissionManage},
	{kind: "ssl", method: http.MethodGet}:              {capability: core.TeamCapabilitySSL, mode: core.TeamPermissionView},
	{kind: "ssl", method: http.MethodDelete}:           {capability: core.TeamCapabilitySSL, mode: core.TeamPermissionManage},

	{kind: "cron", method: http.MethodGet}:     {capability: core.TeamCapabilityCron, mode: core.TeamPermissionView},
	{kind: "cron", method: http.MethodPost}:    {capability: core.TeamCapabilityCron, mode: core.TeamPermissionManage},
	{kind: "cron", method: http.MethodPut}:     {capability: core.TeamCapabilityCron, mode: core.TeamPermissionManage},
	{kind: "cron", method: http.MethodDelete}:  {capability: core.TeamCapabilityCron, mode: core.TeamPermissionManage},
	{kind: "cron", method: http.MethodOptions}: {capability: core.TeamCapabilityCron, mode: core.TeamPermissionView},

	{kind: "backup-schedule", method: http.MethodGet}:    {capability: core.TeamCapabilityBackups, mode: core.TeamPermissionView},
	{kind: "backup-schedule", method: http.MethodPut}:    {capability: core.TeamCapabilityBackups, mode: core.TeamPermissionManage},
	{kind: "backup-schedule", method: http.MethodDelete}: {capability: core.TeamCapabilityBackups, mode: core.TeamPermissionManage},
	{kind: "backup-restore", method: http.MethodPost}:    {capability: core.TeamCapabilityBackups, mode: core.TeamPermissionManage},
	{kind: "backup-restore", method: http.MethodOptions}: {capability: core.TeamCapabilityBackups, mode: core.TeamPermissionView},
	{kind: "backup-download", method: http.MethodGet}:    {capability: core.TeamCapabilityBackups, mode: core.TeamPermissionView},
	{kind: "backups", method: http.MethodGet}:            {capability: core.TeamCapabilityBackups, mode: core.TeamPermissionView},
	{kind: "backups", method: http.MethodPost}:           {capability: core.TeamCapabilityBackups, mode: core.TeamPermissionManage},
	{kind: "backups", method: http.MethodDelete}:         {capability: core.TeamCapabilityBackups, mode: core.TeamPermissionManage},
	{kind: "backups", method: http.MethodOptions}:        {capability: core.TeamCapabilityBackups, mode: core.TeamPermissionView},

	{kind: "php", method: http.MethodGet}:       {capability: core.TeamCapabilityPHP, mode: core.TeamPermissionView},
	{kind: "php", method: http.MethodPost}:      {capability: core.TeamCapabilityPHP, mode: core.TeamPermissionManage},
	{kind: "php-pool", method: http.MethodGet}:  {capability: core.TeamCapabilityPHP, mode: core.TeamPermissionView},
	{kind: "php-pool", method: http.MethodPost}: {capability: core.TeamCapabilityPHP, mode: core.TeamPermissionManage},

	{kind: "usage", method: http.MethodGet}:   {capability: core.TeamCapabilityStatistics, mode: core.TeamPermissionView},
	{kind: "logs", method: http.MethodGet}:    {capability: core.TeamCapabilityStatistics, mode: core.TeamPermissionView},
	{kind: "logs", method: http.MethodDelete}: {capability: core.TeamCapabilityStatistics, mode: core.TeamPermissionManage},
}

var errTeamMemberCapabilityDenied = errors.New("team member capability denied")

var teamMemberCapabilities = []core.TeamCapability{
	core.TeamCapabilityFiles,
	core.TeamCapabilityDatabases,
	core.TeamCapabilityMail,
	core.TeamCapabilityDNS,
	core.TeamCapabilitySSL,
	core.TeamCapabilityCron,
	core.TeamCapabilityBackups,
	core.TeamCapabilityPHP,
	core.TeamCapabilityStatistics,
}

func teamMemberDomainRequirementFor(kind, method string) (teamMemberDomainRequirement, bool) {
	requirement, ok := teamMemberDomainRequirements[teamMemberRouteKey{kind: kind, method: method}]
	return requirement, ok
}

func teamMemberModeAllows(granted, required core.TeamPermissionMode) bool {
	if granted == core.TeamPermissionManage {
		return required == core.TeamPermissionView || required == core.TeamPermissionManage
	}
	return granted == core.TeamPermissionView && required == core.TeamPermissionView
}

func teamMemberAccessResponse(capabilities map[core.TeamCapability]core.TeamPermissionMode) map[string]string {
	access := make(map[string]string, len(teamMemberCapabilities))
	for _, capability := range teamMemberCapabilities {
		access[string(capability)] = "none"
	}
	for capability, mode := range capabilities {
		if core.ValidTeamCapability(capability) && core.ValidTeamPermissionMode(mode) {
			access[string(capability)] = string(mode)
		}
	}
	return access
}

// teamMemberEffectiveDomainCapabilities returns the additive union of a
// member's subscription and direct-domain grants. A direct view grant never
// downgrades a subscription manage grant. No grant at all is deliberately
// indistinguishable from a missing or foreign domain.
func (p *Panel) teamMemberEffectiveDomainCapabilities(
	ctx context.Context,
	caller *Caller,
	domainID int,
) (map[core.TeamCapability]core.TeamPermissionMode, error) {
	if caller == nil || !caller.isAdditionalUser() || domainID <= 0 {
		return nil, errNotFound
	}

	rows, err := p.db.GetDB().QueryContext(ctx, `
		SELECT permission.capability, permission.mode
		FROM additional_user_domain_permissions permission
		JOIN domains domain ON domain.id = permission.domain_id
		JOIN subscriptions subscription ON subscription.id = domain.subscription_id
		JOIN users member ON member.id = permission.user_id
		JOIN users owner ON owner.id = subscription.owner_id
		WHERE domain.id = ?
		  AND member.id = ?
		  AND member.account_type = 'additional_user'
		  AND member.role = 'customer'
		  AND member.status = 'active'
		  AND member.parent_id = ?
		  AND owner.id = ?
		  AND owner.account_type = 'account'
		  AND owner.role = 'customer'
		  AND owner.status = 'active'
		UNION ALL
		SELECT permission.capability, permission.mode
		FROM additional_user_subscription_permissions permission
		JOIN domains domain ON domain.subscription_id = permission.subscription_id
		JOIN subscriptions subscription ON subscription.id = domain.subscription_id
		JOIN users member ON member.id = permission.user_id
		JOIN users owner ON owner.id = subscription.owner_id
		WHERE domain.id = ?
		  AND member.id = ?
		  AND member.account_type = 'additional_user'
		  AND member.role = 'customer'
		  AND member.status = 'active'
		  AND member.parent_id = ?
		  AND owner.id = ?
		  AND owner.account_type = 'account'
		  AND owner.role = 'customer'
		  AND owner.status = 'active'`,
		domainID, caller.ID, caller.CustomerID, caller.CustomerID,
		domainID, caller.ID, caller.CustomerID, caller.CustomerID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	capabilities := make(map[core.TeamCapability]core.TeamPermissionMode)
	for rows.Next() {
		var capability core.TeamCapability
		var mode core.TeamPermissionMode
		if err := rows.Scan(&capability, &mode); err != nil {
			return nil, err
		}
		if !core.ValidTeamCapability(capability) || !core.ValidTeamPermissionMode(mode) {
			return nil, errTeamMemberCapabilityDenied
		}
		if existing, ok := capabilities[capability]; !ok || mode == core.TeamPermissionManage || existing != core.TeamPermissionManage {
			capabilities[capability] = mode
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(capabilities) == 0 {
		return nil, errNotFound
	}
	return capabilities, nil
}

func (p *Panel) canTeamMemberAccessDomainSubroute(
	ctx context.Context,
	caller *Caller,
	domainID int,
	kind string,
	method string,
) error {
	capabilities, err := p.teamMemberEffectiveDomainCapabilities(ctx, caller, domainID)
	if err != nil {
		return err
	}
	requirement, mapped := teamMemberDomainRequirementFor(kind, method)
	if !mapped {
		return errTeamMemberCapabilityDenied
	}
	granted, ok := capabilities[requirement.capability]
	if !ok || !teamMemberModeAllows(granted, requirement.mode) {
		return errTeamMemberCapabilityDenied
	}
	return nil
}

// authorizeDomainSubroute preserves the legacy owner-tree behavior for full
// accounts and uses capability grants only for additional users.
func (p *Panel) authorizeDomainSubroute(
	w http.ResponseWriter,
	r *http.Request,
	domainID int,
	kind string,
) bool {
	caller := currentCaller(r)
	if caller == nil || !caller.isAdditionalUser() {
		return p.authorizeDomain(w, r, domainID)
	}

	err := p.canTeamMemberAccessDomainSubroute(r.Context(), caller, domainID, kind, r.Method)
	switch {
	case err == nil:
		return true
	case errors.Is(err, errNotFound):
		writeClientError(w, http.StatusNotFound, "domain not found")
	case errors.Is(err, errTeamMemberCapabilityDenied):
		writeClientError(w, http.StatusForbidden, "domain capability required")
	default:
		writeServerError(w, err)
	}
	return false
}

func (p *Panel) teamMemberVisibleDomainIDs(ctx context.Context, caller *Caller) (map[int]bool, error) {
	if caller == nil || !caller.isAdditionalUser() {
		return map[int]bool{}, nil
	}

	rows, err := p.db.GetDB().QueryContext(ctx, `
		SELECT DISTINCT domain.id
		FROM domains domain
		JOIN subscriptions subscription ON subscription.id = domain.subscription_id
		JOIN users owner ON owner.id = subscription.owner_id
		JOIN users member ON member.id = ?
		WHERE member.account_type = 'additional_user'
		  AND member.role = 'customer'
		  AND member.status = 'active'
		  AND member.parent_id = ?
		  AND owner.id = ?
		  AND owner.account_type = 'account'
		  AND owner.role = 'customer'
		  AND owner.status = 'active'
		  AND (
			EXISTS (
				SELECT 1
				FROM additional_user_domain_permissions direct_permission
				WHERE direct_permission.user_id = member.id
				  AND direct_permission.domain_id = domain.id
			)
			OR EXISTS (
				SELECT 1
				FROM additional_user_subscription_permissions subscription_permission
				WHERE subscription_permission.user_id = member.id
				  AND subscription_permission.subscription_id = subscription.id
			)
		  )`, caller.ID, caller.CustomerID, caller.CustomerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	visible := make(map[int]bool)
	for rows.Next() {
		var domainID int
		if err := rows.Scan(&domainID); err != nil {
			return nil, err
		}
		visible[domainID] = true
	}
	return visible, rows.Err()
}
