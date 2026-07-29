package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/alicelik/celikpanel/internal/core"
)

const storeCatalogAdminPath = "/api/v1/admin/store-catalog"

var (
	storeOfferingPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_:-]{0,79}$`)
	storeCategoryPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,39}$`)
	storeIconPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,47}$`)
	storeTagPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9+._-]{0,39}$`)
)

type storeAdminOfferingResponse struct {
	ID                 string        `json:"id"`
	Kind               string        `json:"kind"`
	Category           string        `json:"category"`
	Vendor             string        `json:"vendor"`
	ReleaseState       string        `json:"release_state"`
	EntitlementMode    string        `json:"entitlement_mode"`
	ManagePath         string        `json:"manage_path,omitempty"`
	Metadata           storeMetadata `json:"metadata"`
	ComponentIDs       []string      `json:"component_ids"`
	SortOrder          int           `json:"sort_order"`
	UpdatedAt          string        `json:"updated_at"`
	ActiveEntitlements int           `json:"active_entitlements"`
}

type storeAdminComponentResponse struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Description         string   `json:"description"`
	Category            string   `json:"category"`
	Kind                string   `json:"kind"`
	LifecycleOperations []string `json:"lifecycle_operations"`
	PolicySource        string   `json:"policy_source"`
	Editable            bool     `json:"editable"`
}

type storeAdminOperationPolicy struct {
	Mode                  string `json:"mode"`
	Management            string `json:"management"`
	CatalogFormat         string `json:"catalog_format"`
	Verification          string `json:"verification"`
	RuntimeActivation     string `json:"runtime_activation"`
	BrowserEditable       bool   `json:"browser_editable"`
	DatabasePathHint      string `json:"database_path_hint"`
	DetachedSignatureHint string `json:"detached_signature_hint"`
}

type storeAdminUpdateRequest struct {
	Category                     *string        `json:"category"`
	Vendor                       *string        `json:"vendor"`
	ReleaseState                 *string        `json:"release_state"`
	Metadata                     *storeMetadata `json:"metadata"`
	ComponentIDs                 *[]string      `json:"component_ids"`
	SortOrder                    *int           `json:"sort_order"`
	ExpectedUpdatedAt            *string        `json:"expected_updated_at"`
	AcknowledgeEntitlementImpact bool           `json:"acknowledge_entitlement_impact"`
}

// handleStoreCatalogAdmin exposes only bounded Store presentation and binding
// updates. Host commands, SQL, paths and executable recipes are never accepted.
// handleStoreCatalogAdmin yalnız sınırlı Mağaza sunumu ve bağ güncellemelerini
// açar. Host komutları, SQL, yollar ve çalıştırılabilir reçeteler asla kabul edilmez.
func (p *Panel) handleStoreCatalogAdmin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Pragma", "no-cache")
	caller := currentCaller(r)
	if caller == nil || caller.Role != roleAdmin {
		writeClientError(w, http.StatusForbidden, "administrator access required")
		return
	}
	if len(r.URL.Query()) != 0 {
		writeClientError(w, http.StatusBadRequest, "query parameters are not supported")
		return
	}

	switch {
	case r.URL.Path == storeCatalogAdminPath:
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeClientError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		p.listStoreCatalogAdmin(w, r)
	case strings.HasPrefix(r.URL.Path, storeCatalogAdminPath+"/"):
		offeringID := strings.TrimPrefix(r.URL.Path, storeCatalogAdminPath+"/")
		if offeringID == "" || strings.Contains(offeringID, "/") {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPatch {
			w.Header().Set("Allow", http.MethodPatch)
			writeClientError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		p.updateStoreCatalogOffering(w, r, offeringID)
	default:
		http.NotFound(w, r)
	}
}

func (p *Panel) listStoreCatalogAdmin(w http.ResponseWriter, r *http.Request) {
	offerings, err := p.loadStoreOfferings(r.Context(), "")
	if err != nil {
		writeServerError(w, err)
		return
	}
	responseOfferings := make([]storeAdminOfferingResponse, 0, len(offerings))
	entitlementCounts, err := p.loadStoreActiveEntitlementCounts(r.Context())
	if err != nil {
		writeServerError(w, err)
		return
	}
	for _, offering := range offerings {
		item := adminOfferingResponse(offering)
		item.ActiveEntitlements = entitlementCounts[offering.ID]
		responseOfferings = append(responseOfferings, item)
	}

	components := make([]storeAdminComponentResponse, 0, len(core.ManagedServices))
	for i := range core.ManagedServices {
		service := &core.ManagedServices[i]
		components = append(components, storeAdminComponentResponse{
			ID:                  service.ID,
			Name:                service.Name,
			Description:         service.Description,
			Category:            service.Category,
			Kind:                string(service.Kind),
			LifecycleOperations: storeLifecycleOperations(service.Kind),
			PolicySource:        "release_managed",
			Editable:            false,
		})
	}
	slices.SortFunc(components, func(a, b storeAdminComponentResponse) int {
		return strings.Compare(a.ID, b.ID)
	})

	_ = json.NewEncoder(w).Encode(map[string]any{
		"offerings":  responseOfferings,
		"components": components,
		"operation_policy": storeAdminOperationPolicy{
			Mode:                  "read_only",
			Management:            "release_managed",
			CatalogFormat:         "manifest_v2_signed_sqlite",
			Verification:          "implemented",
			RuntimeActivation:     "pending",
			BrowserEditable:       false,
			DatabasePathHint:      "component-catalog / components-v2.db",
			DetachedSignatureHint: "components-v2.db.sig",
		},
	})
}

func (p *Panel) loadStoreActiveEntitlementCounts(ctx context.Context) (map[string]int, error) {
	rows, err := p.db.GetDB().QueryContext(ctx, `
		SELECT product_id, COUNT(*)
		FROM subscription_entitlements
		WHERE status = 'active'
		  AND (expires_at IS NULL OR julianday(expires_at) > julianday('now'))
		GROUP BY product_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var offeringID string
		var count int
		if err := rows.Scan(&offeringID, &count); err != nil {
			return nil, err
		}
		counts[offeringID] = count
	}
	return counts, rows.Err()
}

func storeLifecycleOperations(kind core.ServiceKind) []string {
	switch kind {
	case core.KindService:
		return []string{"install", "start", "restart", "stop", "remove"}
	case core.KindRuntime:
		return []string{"install_version", "remove_version"}
	case core.KindTool:
		return []string{"install", "remove"}
	default:
		return []string{}
	}
}

func adminOfferingResponse(offering storeOffering) storeAdminOfferingResponse {
	return storeAdminOfferingResponse{
		ID:              offering.ID,
		Kind:            offering.Kind,
		Category:        offering.Category,
		Vendor:          offering.Vendor,
		ReleaseState:    offering.ReleaseState,
		EntitlementMode: offering.EntitlementMode,
		ManagePath:      offering.ManagePath,
		Metadata:        offering.Metadata,
		ComponentIDs:    offering.ComponentIDs,
		SortOrder:       offering.SortOrder,
		UpdatedAt:       offering.UpdatedAt,
	}
}

func (p *Panel) updateStoreCatalogOffering(w http.ResponseWriter, r *http.Request, offeringID string) {
	if !storeOfferingPattern.MatchString(offeringID) {
		http.NotFound(w, r)
		return
	}
	contentType := strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0])
	if contentType != "application/json" {
		writeClientError(w, http.StatusUnsupportedMediaType, "content type must be application/json")
		return
	}

	var request storeAdminUpdateRequest
	if err := decodeStrictJSON(w, r, &request); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if request.Category == nil || request.Vendor == nil || request.ReleaseState == nil ||
		request.Metadata == nil || request.ComponentIDs == nil || request.SortOrder == nil ||
		request.ExpectedUpdatedAt == nil {
		writeClientError(w, http.StatusBadRequest, "all editable catalog fields are required")
		return
	}

	category := strings.TrimSpace(*request.Category)
	vendor := strings.TrimSpace(*request.Vendor)
	releaseState := strings.TrimSpace(*request.ReleaseState)
	metadata := *request.Metadata
	componentIDs := append([]string(nil), (*request.ComponentIDs)...)
	expectedUpdatedAt := strings.TrimSpace(*request.ExpectedUpdatedAt)
	if err := validateStoreAdminUpdate(
		category, vendor, releaseState, &metadata, componentIDs, *request.SortOrder, expectedUpdatedAt,
	); err != nil {
		writeClientError(w, http.StatusBadRequest, "catalog fields are invalid")
		return
	}

	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		writeServerError(w, err)
		return
	}
	tx, err := p.db.GetDB().BeginTx(r.Context(), nil)
	if err != nil {
		writeServerError(w, err)
		return
	}
	defer tx.Rollback()

	current := storeOffering{ID: offeringID}
	var rawCurrentMetadata string
	if err := tx.QueryRowContext(r.Context(),
		`SELECT kind, category, vendor, release_state, entitlement_mode,
		        COALESCE(manage_path, ''), metadata_json, sort_order, updated_at
		 FROM store_offerings WHERE id = ?`, offeringID,
	).Scan(&current.Kind, &current.Category, &current.Vendor, &current.ReleaseState,
		&current.EntitlementMode, &current.ManagePath, &rawCurrentMetadata,
		&current.SortOrder, &current.UpdatedAt); errors.Is(err, sql.ErrNoRows) {
		writeClientError(w, http.StatusNotFound, "store offering not found")
		return
	} else if err != nil {
		writeServerError(w, err)
		return
	}
	current.Metadata, err = decodeStoreMetadata(rawCurrentMetadata)
	if err != nil {
		writeServerError(w, err)
		return
	}
	componentRows, err := tx.QueryContext(r.Context(), `
		SELECT component_id FROM store_offering_components
		WHERE offering_id = ? ORDER BY position, component_id`, offeringID)
	if err != nil {
		writeServerError(w, err)
		return
	}
	for componentRows.Next() {
		var componentID string
		if err := componentRows.Scan(&componentID); err != nil {
			componentRows.Close()
			writeServerError(w, err)
			return
		}
		current.ComponentIDs = append(current.ComponentIDs, componentID)
	}
	if err := componentRows.Err(); err != nil {
		componentRows.Close()
		writeServerError(w, err)
		return
	}
	if err := componentRows.Close(); err != nil {
		writeServerError(w, err)
		return
	}
	var activeEntitlements int
	if err := tx.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM subscription_entitlements
		WHERE product_id = ? AND status = 'active'
		  AND (expires_at IS NULL OR julianday(expires_at) > julianday('now'))`,
		offeringID,
	).Scan(&activeEntitlements); err != nil {
		writeServerError(w, err)
		return
	}

	canonicalNoop := current.Category == category &&
		current.Vendor == vendor &&
		current.ReleaseState == releaseState &&
		current.SortOrder == *request.SortOrder &&
		storeMetadataEqual(current.Metadata, metadata) &&
		slices.Equal(current.ComponentIDs, componentIDs)
	if canonicalNoop {
		if err := tx.Rollback(); err != nil {
			writeServerError(w, err)
			return
		}
		// An exact retry after a failed host sync must not wait for the periodic
		// reconciler. The catalog is already closed, so retry the fail-closed VPN
		// peer removal before reporting the no-op as successful.
		// Başarısız bir makine senkronundan sonraki birebir tekrar, dönemsel
		// uzlaştırıcıyı beklememelidir. Katalog zaten kapalıdır; no-op sonucunu
		// başarılı bildirmeden önce güvenli-kapalı VPN peer kaldırmayı yeniden dene.
		if err := p.reconcileClosedStoreOffering(r.Context(), offeringID, releaseState); err != nil {
			writeAgentError(w, err, "synchronize closed Store offering")
			return
		}
		response := adminOfferingResponse(current)
		response.ActiveEntitlements = activeEntitlements
		_ = json.NewEncoder(w).Encode(map[string]any{"offering": response, "unchanged": true})
		return
	}
	if current.UpdatedAt != expectedUpdatedAt {
		writeClientError(w, http.StatusConflict, "store offering changed; reload and try again")
		return
	}
	if current.ReleaseState != "available" && releaseState == "available" {
		writeClientError(w, http.StatusConflict, "publishing an offering is managed by a signed release")
		return
	}
	if current.ReleaseState == "available" && releaseState != "available" &&
		activeEntitlements > 0 && !request.AcknowledgeEntitlementImpact {
		writeClientError(w, http.StatusConflict, "active entitlements are affected; explicit acknowledgement is required")
		return
	}
	changedFields := storeAdminChangedFields(current, category, vendor, releaseState, metadata, componentIDs, *request.SortOrder)
	beforeDigest, err := storeAdminCanonicalDigest(
		current.Category, current.Vendor, current.ReleaseState, current.Metadata,
		current.ComponentIDs, current.SortOrder,
	)
	if err != nil {
		writeServerError(w, err)
		return
	}
	afterDigest, err := storeAdminCanonicalDigest(
		category, vendor, releaseState, metadata, componentIDs, *request.SortOrder,
	)
	if err != nil {
		writeServerError(w, err)
		return
	}

	updatedAt := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := tx.ExecContext(r.Context(), `
		UPDATE store_offerings
		SET category = ?, vendor = ?, release_state = ?, metadata_json = ?,
		    sort_order = ?, updated_at = ?
		WHERE id = ? AND updated_at = ?`,
		category, vendor, releaseState, string(metadataJSON), *request.SortOrder,
		updatedAt, offeringID, expectedUpdatedAt,
	)
	if err != nil {
		writeServerError(w, err)
		return
	}
	affected, err := result.RowsAffected()
	if err != nil {
		writeServerError(w, err)
		return
	}
	if affected != 1 {
		writeClientError(w, http.StatusConflict, "store offering changed; reload and try again")
		return
	}
	if _, err := tx.ExecContext(r.Context(),
		`DELETE FROM store_offering_components WHERE offering_id = ?`, offeringID,
	); err != nil {
		writeServerError(w, err)
		return
	}
	for index, componentID := range componentIDs {
		if _, err := tx.ExecContext(r.Context(), `
			INSERT INTO store_offering_components (offering_id, component_id, position)
			VALUES (?, ?, ?)`,
			offeringID, componentID, (index+1)*10,
		); err != nil {
			writeServerError(w, err)
			return
		}
	}
	impactAcknowledged := current.ReleaseState == "available" && releaseState != "available" &&
		activeEntitlements > 0 && request.AcknowledgeEntitlementImpact
	if err := insertStoreCatalogAudit(
		r, tx, offeringID, current.ReleaseState, releaseState, changedFields,
		impactAcknowledged, activeEntitlements, beforeDigest, afterDigest,
	); err != nil {
		writeServerError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeServerError(w, err)
		return
	}
	// Catalog availability is part of the VPN authorization decision. Apply a
	// closing transition synchronously so an acknowledged admin action cannot
	// leave previously authorized peers live until the one-minute reconciler.
	// Katalog kullanılabilirliği VPN yetkilendirme kararının bir parçasıdır.
	// Onaylanan yönetici işlemi daha önce yetkilendirilmiş peerları bir dakikalık
	// uzlaştırıcıya kadar canlı bırakmasın diye kapanış geçişini eşzamanlı uygula.
	if err := p.reconcileClosedStoreOffering(r.Context(), offeringID, releaseState); err != nil {
		writeAgentError(w, err, "synchronize closed Store offering")
		return
	}

	updated, err := p.loadStoreOfferings(r.Context(), offeringID)
	if err != nil || len(updated) != 1 {
		if err == nil {
			err = fmt.Errorf("updated store offering %q disappeared", offeringID)
		}
		writeServerError(w, err)
		return
	}
	response := adminOfferingResponse(updated[0])
	response.ActiveEntitlements = activeEntitlements
	_ = json.NewEncoder(w).Encode(map[string]any{"offering": response})
}

// reconcileClosedStoreOffering performs the immediate security side effects
// of a release-managed catalog closure. Entitlements remain recorded so a
// future signed release can make them effective again, but VPN peers are
// revoked and removed from the live WireGuard configuration now.
//
// reconcileClosedStoreOffering, yayın-yönetimli bir katalog kapanışının acil
// güvenlik yan etkilerini uygular. Haklar, gelecekteki imzalı bir yayının
// yeniden etkinleştirebilmesi için kayıtlı kalır; ancak VPN peerları şimdi
// iptal edilip canlı WireGuard yapılandırmasından kaldırılır.
func (p *Panel) reconcileClosedStoreOffering(
	ctx context.Context,
	offeringID, releaseState string,
) error {
	if offeringID != vpnProductID || releaseState == "available" {
		return nil
	}
	return p.reconcileVPNEntitlements(ctx)
}

type storeAdminCanonicalState struct {
	Category     string        `json:"category"`
	Vendor       string        `json:"vendor"`
	ReleaseState string        `json:"release_state"`
	Metadata     storeMetadata `json:"metadata"`
	ComponentIDs []string      `json:"component_ids"`
	SortOrder    int           `json:"sort_order"`
}

func storeAdminCanonicalDigest(
	category string,
	vendor string,
	releaseState string,
	metadata storeMetadata,
	componentIDs []string,
	sortOrder int,
) (string, error) {
	if metadata.Tags == nil {
		metadata.Tags = []string{}
	}
	encoded, err := json.Marshal(storeAdminCanonicalState{
		Category:     category,
		Vendor:       vendor,
		ReleaseState: releaseState,
		Metadata:     metadata,
		ComponentIDs: append([]string{}, componentIDs...),
		SortOrder:    sortOrder,
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", digest), nil
}

func storeMetadataEqual(a, b storeMetadata) bool {
	return a.Name == b.Name &&
		a.Description == b.Description &&
		a.Icon == b.Icon &&
		slices.Equal(a.Tags, b.Tags)
}

func storeAdminChangedFields(
	current storeOffering,
	category string,
	vendor string,
	releaseState string,
	metadata storeMetadata,
	componentIDs []string,
	sortOrder int,
) []string {
	fields := make([]string, 0, 6)
	if current.Category != category {
		fields = append(fields, "category")
	}
	if current.Vendor != vendor {
		fields = append(fields, "vendor")
	}
	if current.ReleaseState != releaseState {
		fields = append(fields, "release_state")
	}
	if !storeMetadataEqual(current.Metadata, metadata) {
		fields = append(fields, "metadata")
	}
	if !slices.Equal(current.ComponentIDs, componentIDs) {
		fields = append(fields, "component_bindings")
	}
	if current.SortOrder != sortOrder {
		fields = append(fields, "sort_order")
	}
	return fields
}

func validateStoreAdminUpdate(
	category string,
	vendor string,
	releaseState string,
	metadata *storeMetadata,
	componentIDs []string,
	sortOrder int,
	expectedUpdatedAt string,
) error {
	if !storeCategoryPattern.MatchString(category) ||
		!boundedStoreText(vendor, 1, 100) ||
		(releaseState != "available" && releaseState != "coming_soon" && releaseState != "retired") ||
		sortOrder < 0 || sortOrder > 1_000_000 ||
		!boundedStoreText(expectedUpdatedAt, 1, 64) ||
		metadata == nil {
		return errors.New("invalid typed field")
	}

	metadata.Name.EN = strings.TrimSpace(metadata.Name.EN)
	metadata.Name.TR = strings.TrimSpace(metadata.Name.TR)
	metadata.Description.EN = strings.TrimSpace(metadata.Description.EN)
	metadata.Description.TR = strings.TrimSpace(metadata.Description.TR)
	metadata.Icon = strings.TrimSpace(metadata.Icon)
	if !boundedStoreText(metadata.Name.EN, 1, 120) ||
		!boundedStoreText(metadata.Name.TR, 1, 120) ||
		!boundedStoreText(metadata.Description.EN, 1, 600) ||
		!boundedStoreText(metadata.Description.TR, 1, 600) ||
		!storeIconPattern.MatchString(metadata.Icon) ||
		len(metadata.Tags) > 12 ||
		len(componentIDs) > 32 {
		return errors.New("invalid metadata")
	}

	seenTags := map[string]bool{}
	for index := range metadata.Tags {
		metadata.Tags[index] = strings.TrimSpace(metadata.Tags[index])
		if !storeTagPattern.MatchString(metadata.Tags[index]) || seenTags[metadata.Tags[index]] {
			return errors.New("invalid or duplicate tag")
		}
		seenTags[metadata.Tags[index]] = true
	}
	if metadata.Tags == nil {
		metadata.Tags = []string{}
	}

	seenComponents := map[string]bool{}
	for _, componentID := range componentIDs {
		if componentID == "" || seenComponents[componentID] || core.GetManagedServiceByID(componentID) == nil {
			return errors.New("unknown or duplicate component")
		}
		seenComponents[componentID] = true
	}
	return nil
}

func boundedStoreText(value string, minimum, maximum int) bool {
	length := utf8.RuneCountInString(value)
	if length < minimum || length > maximum {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// insertStoreCatalogAudit shares the catalog transaction. A catalog mutation
// cannot commit unless its immutable offering identity is also audited.
// insertStoreCatalogAudit katalog işlemiyle aynı transaction'ı paylaşır.
// Değişmez teklif kimliği denetlenmeden katalog değişikliği commit edilemez.
func insertStoreCatalogAudit(
	r *http.Request,
	tx *sql.Tx,
	offeringID string,
	oldReleaseState string,
	newReleaseState string,
	changedFields []string,
	impactAcknowledged bool,
	activeEntitlements int,
	beforeDigest string,
	afterDigest string,
) error {
	caller := currentCaller(r)
	if caller == nil || caller.Role != roleAdmin {
		return errors.New("administrator caller required")
	}
	userAgent := r.UserAgent()
	if len(userAgent) > 300 {
		userAgent = userAgent[:300]
	}
	_, err := tx.ExecContext(r.Context(), `
		INSERT INTO audit_logs
			(user_id, action, resource_type, resource_id, ip_address, user_agent)
		VALUES (?, ?, 'store_offering', NULL, ?, ?)`,
		caller.ID, fmt.Sprintf(
			"store.catalog.update:%s:fields=%s:state=%s->%s:impact_ack=%t,count=%d:before=%s:after=%s",
			offeringID, strings.Join(changedFields, ","), oldReleaseState, newReleaseState,
			impactAcknowledged, activeEntitlements, beforeDigest, afterDigest,
		),
		clientIP(r), userAgent,
	)
	return err
}
