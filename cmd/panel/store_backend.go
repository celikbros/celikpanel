package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/alicelik/celikpanel/internal/core"
)

const storeScanFreshness = 15 * time.Minute

type storeLocalizedText struct {
	EN string `json:"en"`
	TR string `json:"tr"`
}

// storeMetadata is presentation data only. Operational recipes, commands,
// SQL and filesystem paths belong in typed, reviewed code and columns.
// storeMetadata yalnız sunum verisidir. İşletim reçeteleri, komutlar, SQL ve
// dosya yolları tipli, incelenen kod ve sütunlarda bulunur.
type storeMetadata struct {
	Name        storeLocalizedText `json:"name"`
	Description storeLocalizedText `json:"description"`
	Icon        string             `json:"icon"`
	Tags        []string           `json:"tags"`
}

type storeOffering struct {
	ID              string
	Kind            string
	Category        string
	Vendor          string
	ReleaseState    string
	EntitlementMode string
	ManagePath      string
	Metadata        storeMetadata
	ComponentIDs    []string
	SortOrder       int
	UpdatedAt       string
}

type storePrimaryAction struct {
	Type    string `json:"type"`
	Path    string `json:"path,omitempty"`
	Enabled bool   `json:"enabled"`
}

type storeItemResponse struct {
	ID               string             `json:"id"`
	Kind             string             `json:"kind"`
	Category         string             `json:"category"`
	Vendor           string             `json:"vendor"`
	Name             string             `json:"name"`
	Description      string             `json:"description"`
	Metadata         storeMetadata      `json:"metadata"`
	ReleaseState     string             `json:"release_state"`
	PlatformState    string             `json:"platform_state"`
	EntitlementState string             `json:"entitlement_state"`
	RuntimeState     string             `json:"runtime_state"`
	PrimaryAction    storePrimaryAction `json:"primary_action"`
	BlockerReason    string             `json:"blocker_reason,omitempty"`
	ComponentIDs     []string           `json:"component_ids"`
	ManagePath       string             `json:"manage_path,omitempty"`
	Included         bool               `json:"included"`
	State            string             `json:"state"`
	StateReason      string             `json:"state_reason,omitempty"`
	Action           string             `json:"action"`
	ActionPath       string             `json:"action_path,omitempty"`
}

type storeRuntimeSnapshot struct {
	Fresh       bool
	Family      string
	ByID        map[string]serviceObservation
	Installed   map[string]bool
	Unavailable string
}

func decodeStoreMetadata(raw string) (storeMetadata, error) {
	var metadata storeMetadata
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&metadata); err != nil {
		return metadata, err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return metadata, errors.New("unexpected JSON value")
		}
		return metadata, err
	}
	if strings.TrimSpace(metadata.Name.EN) == "" || strings.TrimSpace(metadata.Name.TR) == "" ||
		strings.TrimSpace(metadata.Description.EN) == "" || strings.TrimSpace(metadata.Description.TR) == "" {
		return metadata, errors.New("localized name and description are required")
	}
	if metadata.Tags == nil {
		metadata.Tags = []string{}
	}
	return metadata, nil
}

func (p *Panel) loadStoreOfferings(ctx context.Context, onlyID string) ([]storeOffering, error) {
	query := `
		SELECT id, kind, category, vendor, release_state, entitlement_mode,
		       COALESCE(manage_path, ''), metadata_json, sort_order, updated_at
		FROM store_offerings`
	args := []any{}
	if onlyID != "" {
		query += ` WHERE id = ?`
		args = append(args, onlyID)
	}
	query += ` ORDER BY sort_order, id`
	rows, err := p.db.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	offerings := []storeOffering{}
	byID := map[string]int{}
	for rows.Next() {
		var offering storeOffering
		var rawMetadata string
		if err := rows.Scan(&offering.ID, &offering.Kind, &offering.Category, &offering.Vendor,
			&offering.ReleaseState, &offering.EntitlementMode, &offering.ManagePath, &rawMetadata,
			&offering.SortOrder, &offering.UpdatedAt); err != nil {
			return nil, err
		}
		offering.Metadata, err = decodeStoreMetadata(rawMetadata)
		if err != nil {
			return nil, fmt.Errorf("store offering %q metadata: %w", offering.ID, err)
		}
		if !safePanelPath(offering.ManagePath) {
			return nil, fmt.Errorf("store offering %q has an unsafe manage path", offering.ID)
		}
		offering.ComponentIDs = []string{}
		byID[offering.ID] = len(offerings)
		offerings = append(offerings, offering)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(offerings) == 0 {
		return offerings, nil
	}

	componentQuery := `SELECT offering_id, component_id FROM store_offering_components`
	componentArgs := []any{}
	if onlyID != "" {
		componentQuery += ` WHERE offering_id = ?`
		componentArgs = append(componentArgs, onlyID)
	}
	componentQuery += ` ORDER BY offering_id, position, component_id`
	componentRows, err := p.db.GetDB().QueryContext(ctx, componentQuery, componentArgs...)
	if err != nil {
		return nil, err
	}
	defer componentRows.Close()
	for componentRows.Next() {
		var offeringID, componentID string
		if err := componentRows.Scan(&offeringID, &componentID); err != nil {
			return nil, err
		}
		if index, ok := byID[offeringID]; ok {
			offerings[index].ComponentIDs = append(offerings[index].ComponentIDs, componentID)
		}
	}
	if err := componentRows.Err(); err != nil {
		return nil, err
	}
	return offerings, nil
}

func safePanelPath(path string) bool {
	if path == "" {
		return true
	}
	// Store paths are canonical panel routes, never encoded URL payloads.
	// Rejecting every decoding change also removes double-decoding ambiguity.
	// Mağaza yolları kodlanmış URL yükleri değil, kanonik panel rotalarıdır.
	// Kod çözümündeki her değişikliği reddetmek çift çözüm belirsizliğini de kaldırır.
	decoded, err := url.PathUnescape(path)
	if err != nil || decoded != path {
		return false
	}

	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") ||
		strings.Contains(path, `\`) || strings.Contains(path, "://") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	for _, r := range path {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func strictScanObservations(raw string) ([]serviceObservation, error) {
	var doc scanCacheDoc
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("unexpected JSON value")
		}
		return nil, err
	}
	if doc.Observations == nil {
		return nil, errors.New("observations are required")
	}
	seen := map[string]bool{}
	for _, observation := range doc.Observations {
		if observation.ID == "" || seen[observation.ID] {
			return nil, errors.New("invalid or duplicate service observation")
		}
		seen[observation.ID] = true
	}
	return doc.Observations, nil
}

// loadStoreRuntimeSnapshot is deliberately cache-only. Opening the Store and
// acquiring a right must never install software or make an agent RPC.
// loadStoreRuntimeSnapshot bilerek yalnız önbelleği okur. Mağazayı açmak ve bir
// hak edinmek asla yazılım kurmamalı veya agent RPC çağrısı yapmamalıdır.
func (p *Panel) loadStoreRuntimeSnapshot(ctx context.Context, now time.Time) storeRuntimeSnapshot {
	snapshot := storeRuntimeSnapshot{
		ByID:      map[string]serviceObservation{},
		Installed: map[string]bool{},
	}
	var raw, scannedAtRaw string
	if err := p.db.GetDB().QueryRowContext(ctx,
		`SELECT data, scanned_at FROM service_scan_cache WHERE id = 1`,
	).Scan(&raw, &scannedAtRaw); err != nil {
		snapshot.Unavailable = "component_state_unavailable"
		return snapshot
	}
	scannedAt, err := time.Parse(time.RFC3339, scannedAtRaw)
	if err != nil || scannedAt.After(now.Add(time.Minute)) || now.Sub(scannedAt) > storeScanFreshness {
		snapshot.Unavailable = "component_state_stale"
		return snapshot
	}
	observations, err := strictScanObservations(raw)
	if err != nil {
		snapshot.Unavailable = "component_state_invalid"
		return snapshot
	}
	for _, observation := range observations {
		snapshot.ByID[observation.ID] = observation
		if observation.IsInstalled {
			snapshot.Installed[observation.ID] = true
		}
	}
	p.pkgFamilyMu.Lock()
	snapshot.Family = p.pkgFamilyVal
	p.pkgFamilyMu.Unlock()
	snapshot.Fresh = true
	return snapshot
}

func offeringPlatformAndRuntime(offering storeOffering, snapshot storeRuntimeSnapshot) (string, string, string) {
	if len(offering.ComponentIDs) == 0 {
		return "supported", "not_applicable", ""
	}
	if !snapshot.Fresh {
		return "unknown", "unknown", snapshot.Unavailable
	}
	platform := "supported"
	runtime := "running"
	for _, componentID := range offering.ComponentIDs {
		service := core.GetManagedServiceByID(componentID)
		if service == nil {
			return "blocked", "unknown", "unknown_component"
		}
		observation, observed := snapshot.ByID[componentID]
		if !observed {
			if platform == "supported" {
				platform = "unknown"
			}
			runtime = worseRuntime(runtime, "unknown")
			continue
		}
		if !observation.IsInstalled {
			runtime = worseRuntime(runtime, "not_installed")
			if snapshot.Family == "" {
				if platform == "supported" {
					platform = "unknown"
				}
				continue
			}
			if len(service.Packages) > 0 && len(service.Packages[snapshot.Family]) == 0 {
				platform = "unsupported"
				continue
			}
			if core.SeatTakenBy(service, snapshot.Installed) != "" ||
				len(core.RequirementsMissing(service, snapshot.Installed)) > 0 {
				if platform != "unsupported" {
					platform = "blocked"
				}
			}
			continue
		}
		switch strings.ToLower(strings.TrimSpace(observation.Status)) {
		case "active", "running":
		case "failed", "error":
			runtime = worseRuntime(runtime, "error")
		case "inactive", "stopped":
			runtime = worseRuntime(runtime, "stopped")
		default:
			runtime = worseRuntime(runtime, "installed")
		}
	}
	blocker := ""
	switch platform {
	case "unknown":
		blocker = "component_state_unknown"
	case "unsupported":
		blocker = "platform_unsupported"
	case "blocked":
		blocker = "component_install_blocked"
	}
	return platform, runtime, blocker
}

func worseRuntime(current, candidate string) string {
	rank := map[string]int{
		"running": 0, "installed": 1, "not_installed": 2,
		"stopped": 3, "error": 4, "unknown": 5,
	}
	if rank[candidate] > rank[current] {
		return candidate
	}
	return current
}

func localizedStoreText(value storeLocalizedText, locale string) string {
	if locale == "tr" {
		return value.TR
	}
	return value.EN
}

func localizedStoreReason(code, locale string) string {
	reasons := map[string]storeLocalizedText{
		"admin_required":              {EN: "Only the server administrator can change Store entitlements.", TR: "Mağaza haklarını yalnız sunucu yöneticisi değiştirebilir."},
		"subscription_required":       {EN: "Choose a subscription to manage this offering.", TR: "Bu teklifi yönetmek için bir abonelik seçin."},
		"coming_soon":                 {EN: "This offering is not available yet.", TR: "Bu teklif henüz kullanıma açık değil."},
		"retired":                     {EN: "This offering has been retired.", TR: "Bu teklif kullanımdan kaldırıldı."},
		"component_state_unavailable": {EN: "Run a Components rescan before using this offering.", TR: "Bu teklifi kullanmadan önce Bileşenler taramasını çalıştırın."},
		"component_state_stale":       {EN: "Component state is older than 15 minutes; rescan it first.", TR: "Bileşen durumu 15 dakikadan eski; önce yeniden tarayın."},
		"component_state_invalid":     {EN: "The saved component scan is invalid; rescan it first.", TR: "Kaydedilen bileşen taraması geçersiz; önce yeniden tarayın."},
		"component_state_unknown":     {EN: "The required component state is unknown.", TR: "Gerekli bileşen durumu bilinmiyor."},
		"unknown_component":           {EN: "This offering references an unknown component.", TR: "Bu teklif bilinmeyen bir bileşene başvuruyor."},
		"platform_unsupported":        {EN: "A required component is unsupported on this platform.", TR: "Gerekli bir bileşen bu platformda desteklenmiyor."},
		"component_install_blocked":   {EN: "A required component is blocked by dependencies or conflicts.", TR: "Gerekli bir bileşen bağımlılıklar veya çakışmalar nedeniyle engelli."},
		"components_not_installed":    {EN: "Install all required components before granting this entitlement.", TR: "Bu hakkı vermeden önce gerekli tüm bileşenleri kurun."},
		"components_not_running":      {EN: "Start or repair all required components before granting this entitlement.", TR: "Bu hakkı vermeden önce gerekli tüm bileşenleri başlatın veya onarın."},
	}
	if text, ok := reasons[code]; ok {
		return localizedStoreText(text, locale)
	}
	return ""
}

func (p *Panel) entitlementState(ctx context.Context, subscriptionID int, offering storeOffering, now time.Time) (string, error) {
	if offering.EntitlementMode == "included" {
		return "included", nil
	}
	if subscriptionID == 0 {
		return "not_owned", nil
	}
	var status string
	var expiresAt sql.NullString
	err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT status, expires_at
		FROM subscription_entitlements
		WHERE subscription_id = ? AND product_id = ?`,
		subscriptionID, offering.ID,
	).Scan(&status, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "not_owned", nil
	}
	if err != nil {
		return "", err
	}
	if status != "active" {
		return "suspended", nil
	}
	if expiresAt.Valid {
		expiry, err := time.Parse(time.RFC3339, expiresAt.String)
		if err != nil || !expiry.After(now) {
			return "expired", nil
		}
	}
	return "owned", nil
}

func (p *Panel) buildStoreItem(ctx context.Context, offering storeOffering, subscriptionID int,
	locale string, caller *Caller, snapshot storeRuntimeSnapshot, now time.Time,
) (storeItemResponse, error) {
	platformState, runtimeState, blocker := offeringPlatformAndRuntime(offering, snapshot)
	entitlementState, err := p.entitlementState(ctx, subscriptionID, offering, now)
	if err != nil {
		return storeItemResponse{}, err
	}
	if offering.ReleaseState != "available" {
		blocker = offering.ReleaseState
	} else if offering.EntitlementMode == "grant" && subscriptionID == 0 {
		blocker = "subscription_required"
	}
	action := storePrimaryAction{Type: "none", Enabled: false}
	isAdmin := caller != nil && caller.Role == roleAdmin
	if offering.EntitlementMode == "grant" && entitlementState == "owned" {
		if isAdmin {
			action.Type = "remove"
			action.Enabled = true
		} else {
			blocker = "admin_required"
		}
	} else if blocker == "" {
		switch {
		case offering.EntitlementMode == "grant" &&
			(runtimeState == "running" || runtimeState == "installed" || runtimeState == "not_applicable"):
			if isAdmin {
				action.Type = "acquire"
				action.Enabled = true
			} else {
				blocker = "admin_required"
			}
		case offering.EntitlementMode == "grant" && runtimeState == "not_installed":
			if isAdmin {
				action.Type = "open_components"
				action.Path = "/services"
				action.Enabled = true
				blocker = "components_not_installed"
			} else {
				blocker = "admin_required"
			}
		case offering.EntitlementMode == "grant":
			if isAdmin {
				action.Type = "manage_components"
				action.Path = offering.ManagePath
				action.Enabled = true
				blocker = "components_not_running"
			} else {
				blocker = "admin_required"
			}
		case len(offering.ComponentIDs) > 0:
			if isAdmin {
				if runtimeState == "not_installed" {
					action.Type = "open_components"
					action.Path = "/services"
				} else {
					action.Type = "manage_components"
					action.Path = offering.ManagePath
				}
				action.Enabled = true
			} else {
				blocker = "admin_required"
			}
		case offering.ManagePath != "":
			action.Type = "open_domain_apps"
			action.Path = offering.ManagePath
			action.Enabled = true
		}
	}
	state := "available"
	switch {
	case offering.ReleaseState == "coming_soon":
		state = "coming_soon"
	case offering.ReleaseState == "retired":
		state = "unsupported"
	case platformState != "supported":
		state = "unsupported"
	case offering.EntitlementMode == "included":
		state = "included"
	}
	componentIDs := []string{}
	managePath := ""
	if isAdmin {
		componentIDs = append(componentIDs, offering.ComponentIDs...)
		if action.Enabled && action.Path != "" {
			managePath = action.Path
		}
	}
	item := storeItemResponse{
		ID: offering.ID, Kind: offering.Kind, Category: offering.Category, Vendor: offering.Vendor,
		Name:        localizedStoreText(offering.Metadata.Name, locale),
		Description: localizedStoreText(offering.Metadata.Description, locale),
		Metadata:    offering.Metadata, ReleaseState: offering.ReleaseState,
		PlatformState: platformState, EntitlementState: entitlementState, RuntimeState: runtimeState,
		PrimaryAction: action, BlockerReason: blocker, ComponentIDs: componentIDs,
		ManagePath: managePath, Included: offering.EntitlementMode == "included",
		State: state, StateReason: localizedStoreReason(blocker, locale),
		Action: action.Type, ActionPath: action.Path,
	}
	return item, nil
}

func parseStoreQuery(r *http.Request) (int, string, error) {
	allowed := map[string]bool{"subscription_id": true, "locale": true}
	for key, values := range r.URL.Query() {
		if !allowed[key] {
			return 0, "", fmt.Errorf("unknown query parameter %q", key)
		}
		if len(values) != 1 {
			return 0, "", fmt.Errorf("query parameter %q must appear exactly once", key)
		}
	}
	locale := r.URL.Query().Get("locale")
	if locale == "" {
		locale = "en"
	}
	if locale != "en" && locale != "tr" {
		return 0, "", errors.New("locale must be en or tr")
	}
	subscriptionID := 0
	if raw := r.URL.Query().Get("subscription_id"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value <= 0 {
			return 0, "", errors.New("invalid subscription id")
		}
		subscriptionID = value
	}
	return subscriptionID, locale, nil
}

// handleStore serves a DB-backed, cache-only Store projection. It never
// installs a component and never calls the host agent.
// handleStore, veritabanı destekli ve yalnız önbellekten okuyan Mağaza
// görünümünü sunar. Asla bileşen kurmaz ve host agent'ını çağırmaz.
func (p *Panel) handleStore(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Pragma", "no-cache")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	onlyID := ""
	switch {
	case r.URL.Path == "/api/v1/store":
	case strings.HasPrefix(r.URL.Path, "/api/v1/store/"):
		onlyID = strings.TrimPrefix(r.URL.Path, "/api/v1/store/")
		if onlyID == "" || strings.Contains(onlyID, "/") {
			http.NotFound(w, r)
			return
		}
	default:
		http.NotFound(w, r)
		return
	}
	subscriptionID, locale, err := parseStoreQuery(r)
	if err != nil {
		writeClientError(w, http.StatusBadRequest, err.Error())
		return
	}
	caller := currentCaller(r)
	if subscriptionID > 0 {
		if err := p.canAccessSubscription(r.Context(), caller, subscriptionID); err != nil {
			if errors.Is(err, errNotFound) {
				writeClientError(w, http.StatusNotFound, "subscription not found")
			} else {
				writeServerError(w, err)
			}
			return
		}
	}
	offerings, err := p.loadStoreOfferings(r.Context(), onlyID)
	if err != nil {
		writeServerError(w, err)
		return
	}
	if onlyID != "" && len(offerings) == 0 {
		http.NotFound(w, r)
		return
	}
	now := time.Now().UTC()
	snapshot := p.loadStoreRuntimeSnapshot(r.Context(), now)
	items := make([]storeItemResponse, 0, len(offerings))
	for _, offering := range offerings {
		item, err := p.buildStoreItem(r.Context(), offering, subscriptionID, locale, caller, snapshot, now)
		if err != nil {
			writeServerError(w, err)
			return
		}
		items = append(items, item)
	}
	if onlyID != "" {
		json.NewEncoder(w).Encode(map[string]any{"item": items[0]})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"items": items})
}
