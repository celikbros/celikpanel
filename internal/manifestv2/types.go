package manifestv2

import (
	"encoding/json"
	"fmt"
	"sort"
)

const (
	// SchemaVersion is the only catalog schema this build accepts.
	// SchemaVersion, bu yapının kabul ettiği tek katalog şemasıdır.
	SchemaVersion = 2

	// AgentSchemaVersion identifies the typed executor capabilities, not a Git release.
	// AgentSchemaVersion, bir Git sürümünü değil türü belirlenmiş yürütücü yeteneklerini tanımlar.
	AgentSchemaVersion = 2

	DefaultCatalogPath   = "/usr/share/celikpanel/manifests/components-v2.db"
	DefaultSignaturePath = DefaultCatalogPath + ".sig"
)

type ItemKind string

const (
	ItemComponent   ItemKind = "component"
	ItemAddon       ItemKind = "addon"
	ItemApplication ItemKind = "application"
)

type SupportState string

const (
	SupportSupported   SupportState = "supported"
	SupportUnsupported SupportState = "unsupported"
	SupportManualOnly  SupportState = "manual_only"
	SupportUnavailable SupportState = "unavailable"
	SupportBlocked     SupportState = "blocked"
)

type CatalogMetadata struct {
	SchemaVersion      int    `json:"schema_version"`
	CatalogVersion     string `json:"catalog_version"`
	CatalogSequence    int64  `json:"catalog_sequence"`
	MinimumAgentSchema int    `json:"minimum_agent_schema"`
	KeyID              string `json:"key_id"`
	CreatedAt          string `json:"created_at"`
}

// OpenPolicy supplies trusted runtime capability and anti-replay floors.
// OpenPolicy, güvenilir çalışma zamanı yeteneği ve yeniden oynatma tabanlarını sağlar.
type OpenPolicy struct {
	AgentSchema            int
	MinimumCatalogSequence int64
	MinimumCatalogDigest   string
}

type CatalogItem struct {
	ID       string          `json:"id"`
	Kind     ItemKind        `json:"kind"`
	Revision int             `json:"revision"`
	Enabled  bool            `json:"enabled"`
	Metadata json.RawMessage `json:"metadata"`
}

type CatalogRecipe struct {
	ID                string           `json:"id"`
	ItemID            string           `json:"item_id"`
	PlatformKey       string           `json:"platform_key"`
	Operation         string           `json:"operation"`
	Revision          int              `json:"revision"`
	Support           SupportState     `json:"support"`
	UnsupportedReason string           `json:"unsupported_reason,omitempty"`
	Selector          PlatformSelector `json:"selector"`
	Spec              RecipeSpec       `json:"spec"`
}

type CatalogDocument struct {
	Metadata CatalogMetadata `json:"metadata"`
	Items    []CatalogItem   `json:"items"`
	Recipes  []CatalogRecipe `json:"recipes"`
}

type HostProfile struct {
	OSFamily       string
	DistroFamily   string
	DistroID       string
	DistroLike     []string
	Version        string
	Architecture   string
	PackageManager string
	ServiceManager string
}

type PlatformSelector struct {
	OSFamily       string   `json:"os_family,omitempty"`
	DistroFamily   string   `json:"distro_family,omitempty"`
	DistroID       string   `json:"distro_id,omitempty"`
	DistroLike     string   `json:"distro_like,omitempty"`
	Version        string   `json:"version,omitempty"`
	Architectures  []string `json:"architectures,omitempty"`
	PackageManager string   `json:"package_manager,omitempty"`
	ServiceManager string   `json:"service_manager,omitempty"`
}

type VariableSpec struct {
	Type     string `json:"type"`
	Pattern  string `json:"pattern,omitempty"`
	Required bool   `json:"required,omitempty"`
	Secret   bool   `json:"secret,omitempty"`
}

type RecipeSpec struct {
	Variables map[string]VariableSpec `json:"variables,omitempty"`
	Steps     []RecipeStep            `json:"steps,omitempty"`
	Verify    []RecipeStep            `json:"verify,omitempty"`
	Rollback  []RecipeStep            `json:"rollback,omitempty"`
}

type RecipeStep struct {
	ID             string   `json:"id"`
	Type           string   `json:"type"`
	Packages       []string `json:"packages,omitempty"`
	Unit           string   `json:"unit,omitempty"`
	Path           string   `json:"path,omitempty"`
	Template       string   `json:"template,omitempty"`
	Mode           string   `json:"mode,omitempty"`
	Host           string   `json:"host,omitempty"`
	Port           int      `json:"port,omitempty"`
	Protocol       string   `json:"protocol,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
	RollbackStepID string   `json:"rollback_step_id,omitempty"`
	presentFields  map[string]struct{}
}

// UnmarshalJSON preserves field presence so even explicit empty irrelevant
// fields are rejected by the semantic step allowlist.
// UnmarshalJSON, açıkça gönderilen boş ve ilgisiz alanların bile anlamsal adım
// izin listesi tarafından reddedilebilmesi için alan varlığını korur.
func (step *RecipeStep) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields == nil {
		return fmt.Errorf("recipe step must be a JSON object")
	}
	known := map[string]struct{}{
		"id":               {},
		"type":             {},
		"packages":         {},
		"unit":             {},
		"path":             {},
		"template":         {},
		"mode":             {},
		"host":             {},
		"port":             {},
		"protocol":         {},
		"timeout_seconds":  {},
		"rollback_step_id": {},
	}
	var unknown []string
	for name := range fields {
		if _, ok := known[name]; !ok {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("unknown field %q", unknown[0])
	}
	type recipeStepWire RecipeStep
	var decoded recipeStepWire
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*step = RecipeStep(decoded)
	step.presentFields = make(map[string]struct{}, len(fields))
	for name := range fields {
		step.presentFields[name] = struct{}{}
	}
	return nil
}

type ResolvedRecipe struct {
	CatalogVersion string
	Digest         string
	Item           CatalogItem
	Recipe         CatalogRecipe
	Specificity    int
}

type SignatureEnvelope struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"key_id"`
	Digest    string `json:"digest"`
	Signature string `json:"signature"`
}
