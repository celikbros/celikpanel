package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

// A nil customization preserves the exact legacy preset and serialized plan.
// An explicit empty selection is meaningful and never falls back to a preset.
type serverSetupCustomization struct {
	Components []string `json:"components"`
}

type serverSetupComponentChoice struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Category     string   `json:"category"`
	Dependencies []string `json:"dependencies"`
	Conflicts    []string `json:"conflicts"`
	Supported    bool     `json:"supported"`
	Installed    bool     `json:"installed"`
	Reason       string   `json:"reason,omitempty"`
}

type serverSetupComponentCatalog struct {
	Version            int                          `json:"version"`
	InventoryState     string                       `json:"inventory_state"`
	Presets            map[string][]string          `json:"presets"`
	RequiredComponents []string                     `json:"required_components"`
	Components         []serverSetupComponentChoice `json:"components"`
}

type serverSetupPlanComponent struct {
	ID        string `json:"id"`
	Selected  bool   `json:"selected"`
	Required  bool   `json:"required"`
	Installed bool   `json:"installed"`
}

// The workflow supports these existing audited lifecycle operations. DNS uses
// its dedicated ownership workflow, and required-repository tools stay in
// Components until setup can review their repository prerequisites explicitly.
var serverSetupSelectableComponents = []string{
	"nginx", "php-fpm", "node", "mariadb", "postgresql", "phpmyadmin", "phppgadmin",
	"postfix", "dovecot", "rspamd", "roundcube", "redis", "valkey", "memcached", "fail2ban",
}

var serverSetupRequiredComponents = []string{"nftables", "certbot"}

func serverSetupPresetComponents(draft serverSetupDraft) []string {
	switch draft.Purpose {
	case "web":
		return []string{"nginx", "php-fpm", "mariadb"}
	case "web_mail":
		return []string{"nginx", "php-fpm", "mariadb", "postfix", "dovecot", "roundcube", "rspamd"}
	case "application":
		ids := []string{"nginx", "node"}
		if draft.Database != "" {
			ids = append(ids, draft.Database)
		}
		return ids
	default:
		return []string{}
	}
}

func canonicalServerSetupCustomization(value *serverSetupCustomization) (*serverSetupCustomization, error) {
	if value == nil {
		return nil, nil
	}
	if len(value.Components) > len(serverSetupSelectableComponents) {
		return nil, errors.New("too many setup components")
	}
	ids := append([]string{}, value.Components...)
	for i, id := range ids {
		id = strings.TrimSpace(id)
		if !slices.Contains(serverSetupSelectableComponents, id) {
			return nil, errors.New("unsupported setup component")
		}
		ids[i] = id
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	return &serverSetupCustomization{Components: ids}, nil
}

// Resolve catalogue role dependencies to the supported setup provider. Existing
// competing providers are never replaced: the shared conflict check blocks the
// reviewed plan instead. Mail's complete lifecycle adds the paired mailbox
// services even though the individual package catalogue does not require them.
func serverSetupComponentDependencies(id string) []string {
	dependencies := []string{}
	managed := core.GetManagedServiceByID(id)
	if managed == nil {
		return dependencies
	}
	for _, dependency := range managed.Requires {
		switch dependency {
		case "web-server":
			dependency = "nginx"
		case "smtp-server":
			dependency = "postfix"
		case "imap-server":
			dependency = "dovecot"
		}
		dependencies = append(dependencies, dependency)
	}
	switch id {
	case "postfix":
		dependencies = append(dependencies, "dovecot")
	case "dovecot":
		dependencies = append(dependencies, "postfix")
	}
	slices.Sort(dependencies)
	return slices.Compact(dependencies)
}

func serverSetupResolvedComponents(draft serverSetupDraft) ([]string, error) {
	selected := serverSetupPresetComponents(draft)
	if draft.Customization != nil {
		canonical, err := canonicalServerSetupCustomization(draft.Customization)
		if err != nil {
			return nil, err
		}
		selected = canonical.Components
	}
	result := []string{}
	seen := map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if seen[id] {
			return nil
		}
		if !slices.Contains(serverSetupSelectableComponents, id) && !slices.Contains(serverSetupRequiredComponents, id) {
			return errors.New("unsupported setup dependency")
		}
		seen[id] = true
		for _, dependency := range serverSetupComponentDependencies(id) {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		result = append(result, id)
		return nil
	}
	for _, id := range selected {
		if err := visit(id); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func serverSetupHasComponent(draft serverSetupDraft, id string) bool {
	resolved, err := serverSetupResolvedComponents(draft)
	return err == nil && slices.Contains(resolved, id)
}

func serverSetupMailProfileIDs(draft serverSetupDraft) []string {
	if draft.Customization == nil {
		if draft.Purpose == "web_mail" {
			return []string{core.MailProfileWebmail, core.MailProfileProtected}
		}
		return nil
	}
	resolved, err := serverSetupResolvedComponents(draft)
	if err != nil || !slices.Contains(resolved, "postfix") {
		return nil
	}
	profiles := []string{}
	if slices.Contains(resolved, "roundcube") {
		profiles = append(profiles, core.MailProfileWebmail)
	}
	if slices.Contains(resolved, "rspamd") {
		profiles = append(profiles, core.MailProfileProtected)
	}
	if len(profiles) == 0 {
		profiles = append(profiles, core.MailProfileCore)
	}
	return profiles
}

func serverSetupNeedsDNSPublisher(draft serverSetupDraft) bool {
	if draft.Customization == nil {
		return draft.Purpose != "dns"
	}
	for _, id := range []string{"nginx", "node", "postfix", "roundcube"} {
		if serverSetupHasComponent(draft, id) {
			return true
		}
	}
	return false
}

func (p *Panel) handleServerSetupComponents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if !requireServerSetupAdmin(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	var installedIDs []string
	inventoryErr := p.callAgentContext(ctx, "Agent.InstalledServiceIDsStrict", &transport.Empty{}, &installedIDs)
	var nodeVersions transport.NodeVersionsResponse
	if inventoryErr == nil {
		inventoryErr = p.callAgentContext(ctx, "Agent.ListNodeVersions", &transport.Empty{}, &nodeVersions)
		if inventoryErr == nil && len(nodeVersions.Installed) > 0 {
			installedIDs = append(installedIDs, "node")
		}
	}
	host := p.managedServiceHostProfile()
	catalog := buildServerSetupComponentCatalog(host, installedIDs, inventoryErr == nil)
	_ = json.NewEncoder(w).Encode(catalog)
}

func buildServerSetupComponentCatalog(host core.ManagedServiceHostProfile, installedIDs []string, inventoryReady bool) serverSetupComponentCatalog {
	catalog := serverSetupComponentCatalog{Version: 1, InventoryState: "ready", Presets: map[string][]string{}, RequiredComponents: append([]string{}, serverSetupRequiredComponents...), Components: []serverSetupComponentChoice{}}
	if !inventoryReady {
		catalog.InventoryState = "unknown"
	}
	for _, purpose := range []string{"web", "web_mail", "application", "dns", "custom"} {
		catalog.Presets[purpose] = serverSetupPresetComponents(serverSetupDraft{Purpose: purpose, Database: "mariadb"})
	}
	ids := append(append([]string{}, serverSetupSelectableComponents...), serverSetupRequiredComponents...)
	for _, id := range ids {
		managed := core.GetManagedServiceByID(id)
		if managed == nil {
			continue
		}
		choice := serverSetupComponentChoice{ID: id, Name: managed.Name, Category: managed.Category, Dependencies: serverSetupComponentDependencies(id), Conflicts: []string{}, Installed: inventoryReady && slices.Contains(installedIDs, id), Supported: true}
		for _, other := range core.ManagedServices {
			if managed.ConflictGroup != "" && managed.ConflictGroup == other.ConflictGroup && id != other.ID {
				choice.Conflicts = append(choice.Conflicts, other.ID)
			}
		}
		slices.Sort(choice.Conflicts)
		resolved, _ := serverSetupResolvedComponents(serverSetupDraft{Customization: &serverSetupCustomization{Components: []string{id}}})
		if slices.Contains(serverSetupRequiredComponents, id) {
			resolved = []string{id}
		}
		installed := map[string]bool{}
		if inventoryReady {
			for _, installedID := range installedIDs {
				installed[installedID] = true
			}
		}
		for _, dependency := range resolved {
			managedDependency := core.GetManagedServiceByID(dependency)
			if occupied := core.SeatTakenBy(managedDependency, installed); occupied != "" {
				choice.Supported, choice.Reason = false, "server_setup_service_conflict:"+dependency+":"+occupied
				break
			}
			if _, reason := core.ManagedServiceInstallBlockForHost(managedDependency, host); reason != "" {
				choice.Supported, choice.Reason = false, "server_setup_service_unsupported:"+dependency
				break
			}
		}
		if !inventoryReady {
			choice.Supported, choice.Reason = false, "server_setup_inventory_unavailable"
		}
		catalog.Components = append(catalog.Components, choice)
	}
	return catalog
}
