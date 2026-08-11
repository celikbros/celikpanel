package manifestv2

import (
	"encoding/json"
	"fmt"
	"net"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	catalogIDPattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{1,127}$`)
	operationPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)
	packagePattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9+._:@{}-]{0,255}$`)
	unitPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@:%\\-]{0,255}$`)
	envNamePattern   = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,127}$`)
	modePattern      = regexp.MustCompile(`^[0-7]{3,4}$`)
	digestPattern    = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

var allowedStepTypes = map[string]struct{}{
	"package_install": {},
	"package_remove":  {},
	"service_enable":  {},
	"service_disable": {},
	"service_start":   {},
	"service_stop":    {},
	"service_restart": {},
	"file_write":      {},
	"firewall_open":   {},
	"firewall_close":  {},
	"service_active":  {},
	"tcp_probe":       {},
}

var allowedDistroFamilies = map[string]struct{}{
	"arch":   {},
	"debian": {},
	"rhel":   {},
}

func validateDocument(doc CatalogDocument) error {
	if err := validateMetadata(doc.Metadata); err != nil {
		return err
	}
	items := make(map[string]CatalogItem, len(doc.Items))
	for _, item := range doc.Items {
		if err := validateItem(item); err != nil {
			return err
		}
		if _, exists := items[item.ID]; exists {
			return fmt.Errorf("duplicate catalog item %q", item.ID)
		}
		items[item.ID] = item
	}
	recipes := make(map[string]struct{}, len(doc.Recipes))
	keys := make(map[string]struct{}, len(doc.Recipes))
	for _, recipe := range doc.Recipes {
		if _, exists := items[recipe.ItemID]; !exists {
			return fmt.Errorf("recipe %q references unknown item %q", recipe.ID, recipe.ItemID)
		}
		if err := validateRecipe(recipe); err != nil {
			return err
		}
		if normalizeToken(recipe.Selector.DistroFamily) != "" && doc.Metadata.MinimumAgentSchema < 2 {
			return fmt.Errorf(
				"recipe %q uses distro_family but minimum_agent_schema is %d; version 2 or newer is required",
				recipe.ID,
				doc.Metadata.MinimumAgentSchema,
			)
		}
		if _, exists := recipes[recipe.ID]; exists {
			return fmt.Errorf("duplicate recipe %q", recipe.ID)
		}
		recipes[recipe.ID] = struct{}{}
		key := recipe.ItemID + "\x00" + recipe.PlatformKey + "\x00" + recipe.Operation
		if _, exists := keys[key]; exists {
			return fmt.Errorf(
				"duplicate recipe selection for item %q, platform %q and operation %q",
				recipe.ItemID,
				recipe.PlatformKey,
				recipe.Operation,
			)
		}
		keys[key] = struct{}{}
	}
	return nil
}

func validateMetadata(meta CatalogMetadata) error {
	if meta.SchemaVersion != SchemaVersion {
		return fmt.Errorf("catalog schema version %d is unsupported", meta.SchemaVersion)
	}
	if strings.TrimSpace(meta.CatalogVersion) == "" {
		return fmt.Errorf("catalog version is required")
	}
	if meta.CatalogSequence < 1 {
		return fmt.Errorf("catalog sequence must be positive")
	}
	if meta.MinimumAgentSchema < 1 {
		return fmt.Errorf("minimum agent schema must be positive")
	}
	if !catalogIDPattern.MatchString(meta.KeyID) {
		return fmt.Errorf("invalid catalog key id %q", meta.KeyID)
	}
	if _, err := time.Parse(time.RFC3339, strings.TrimSpace(meta.CreatedAt)); err != nil {
		return fmt.Errorf("catalog created_at must be RFC3339: %w", err)
	}
	return nil
}

func validateItem(item CatalogItem) error {
	if !catalogIDPattern.MatchString(item.ID) {
		return fmt.Errorf("invalid catalog item id %q", item.ID)
	}
	switch item.Kind {
	case ItemComponent, ItemAddon, ItemApplication:
	default:
		return fmt.Errorf("catalog item %q has invalid kind %q", item.ID, item.Kind)
	}
	if item.Revision < 1 {
		return fmt.Errorf("catalog item %q has invalid revision", item.ID)
	}
	if len(item.Metadata) == 0 {
		item.Metadata = json.RawMessage(`{}`)
	}
	if err := strictJSONObjectExtension(item.Metadata); err != nil {
		return fmt.Errorf("catalog item %q has invalid metadata JSON: %w", item.ID, err)
	}
	return nil
}

func validateRecipe(recipe CatalogRecipe) error {
	if !catalogIDPattern.MatchString(recipe.ID) {
		return fmt.Errorf("invalid recipe id %q", recipe.ID)
	}
	if !catalogIDPattern.MatchString(recipe.ItemID) {
		return fmt.Errorf("recipe %q has invalid item id", recipe.ID)
	}
	if !catalogIDPattern.MatchString(recipe.PlatformKey) {
		return fmt.Errorf("recipe %q has invalid platform key %q", recipe.ID, recipe.PlatformKey)
	}
	if !operationPattern.MatchString(recipe.Operation) {
		return fmt.Errorf("recipe %q has invalid operation %q", recipe.ID, recipe.Operation)
	}
	if recipe.Revision < 1 {
		return fmt.Errorf("recipe %q has invalid revision", recipe.ID)
	}
	switch recipe.Support {
	case SupportSupported:
		if len(recipe.Spec.Steps) == 0 {
			return fmt.Errorf("supported recipe %q has no steps", recipe.ID)
		}
	case SupportUnsupported, SupportManualOnly, SupportUnavailable, SupportBlocked:
		if strings.TrimSpace(recipe.UnsupportedReason) == "" {
			return fmt.Errorf("recipe %q requires an unsupported reason", recipe.ID)
		}
	default:
		return fmt.Errorf("recipe %q has invalid support state %q", recipe.ID, recipe.Support)
	}
	if err := validateSelector(recipe.ID, recipe.Selector); err != nil {
		return err
	}
	return validateSpec(recipe.ID, recipe.Selector, recipe.Spec)
}

func validateSelector(recipeID string, selector PlatformSelector) error {
	selector.OSFamily = normalizeToken(selector.OSFamily)
	selector.DistroFamily = normalizeToken(selector.DistroFamily)
	selector.DistroID = normalizeToken(selector.DistroID)
	selector.DistroLike = normalizeToken(selector.DistroLike)
	selector.PackageManager = normalizeToken(selector.PackageManager)
	selector.ServiceManager = normalizeToken(selector.ServiceManager)

	switch selector.OSFamily {
	case "linux":
	case "":
		return fmt.Errorf("recipe %q selector requires an explicit audited os_family", recipeID)
	default:
		return fmt.Errorf(
			"recipe %q selector uses unaudited os_family %q",
			recipeID,
			selector.OSFamily,
		)
	}
	if selector.DistroFamily != "" {
		if _, ok := allowedDistroFamilies[selector.DistroFamily]; !ok {
			return fmt.Errorf(
				"recipe %q selector uses unsupported distro_family %q",
				recipeID,
				selector.DistroFamily,
			)
		}
	}
	if selector.DistroLike != "" && (selector.DistroID != "" || selector.DistroFamily != "") {
		return fmt.Errorf("recipe %q selector cannot combine distro_like with distro_id or distro_family", recipeID)
	}
	if (selector.PackageManager == "") != (selector.ServiceManager == "") {
		return fmt.Errorf("recipe %q selector must pair package_manager and service_manager", recipeID)
	}
	if selector.Version != "" {
		if _, err := versionConstraintMatches("0", selector.Version); err != nil {
			return fmt.Errorf("recipe %q has invalid version constraint: %w", recipeID, err)
		}
	}
	seen := map[string]struct{}{}
	for _, architecture := range selector.Architectures {
		architecture = normalizeToken(architecture)
		if architecture == "" {
			return fmt.Errorf("recipe %q has an empty architecture", recipeID)
		}
		if _, exists := seen[architecture]; exists {
			return fmt.Errorf("recipe %q repeats architecture %q", recipeID, architecture)
		}
		seen[architecture] = struct{}{}
	}
	return nil
}

func validateSpec(recipeID string, selector PlatformSelector, spec RecipeSpec) error {
	for name, variable := range spec.Variables {
		if !envNamePattern.MatchString(name) {
			return fmt.Errorf("recipe %q has invalid variable %q", recipeID, name)
		}
		switch variable.Type {
		case "string", "package", "version", "path", "port", "boolean":
		default:
			return fmt.Errorf("recipe %q variable %q has invalid type %q", recipeID, name, variable.Type)
		}
		if variable.Pattern != "" {
			if _, err := regexp.Compile(variable.Pattern); err != nil {
				return fmt.Errorf("recipe %q variable %q has invalid pattern: %w", recipeID, name, err)
			}
		}
	}

	ids := map[string]struct{}{}
	rollbackIDs := map[string]struct{}{}
	for _, step := range spec.Rollback {
		if err := validateStep(recipeID, selector, step, stepSectionRollback); err != nil {
			return err
		}
		if _, exists := ids[step.ID]; exists {
			return fmt.Errorf("recipe %q repeats step id %q", recipeID, step.ID)
		}
		ids[step.ID] = struct{}{}
		rollbackIDs[step.ID] = struct{}{}
	}
	for _, step := range spec.Steps {
		if err := validateStep(recipeID, selector, step, stepSectionMain); err != nil {
			return err
		}
		if _, exists := ids[step.ID]; exists {
			return fmt.Errorf("recipe %q repeats step id %q", recipeID, step.ID)
		}
		ids[step.ID] = struct{}{}
		if step.RollbackStepID != "" {
			if _, exists := rollbackIDs[step.RollbackStepID]; !exists {
				return fmt.Errorf(
					"recipe %q step %q references missing rollback step %q",
					recipeID,
					step.ID,
					step.RollbackStepID,
				)
			}
		}
	}
	for _, step := range spec.Verify {
		if err := validateStep(recipeID, selector, step, stepSectionVerify); err != nil {
			return err
		}
		if _, exists := ids[step.ID]; exists {
			return fmt.Errorf("recipe %q repeats step id %q", recipeID, step.ID)
		}
		ids[step.ID] = struct{}{}
	}
	return nil
}

type stepSection uint8

const (
	stepSectionMain stepSection = iota
	stepSectionVerify
	stepSectionRollback
)

func validateStep(recipeID string, selector PlatformSelector, step RecipeStep, section stepSection) error {
	if !catalogIDPattern.MatchString(step.ID) {
		return fmt.Errorf("recipe %q has invalid step id %q", recipeID, step.ID)
	}
	if _, ok := allowedStepTypes[step.Type]; !ok {
		return fmt.Errorf("recipe %q step %q has invalid type %q", recipeID, step.ID, step.Type)
	}
	if step.TimeoutSeconds < 0 || step.TimeoutSeconds > 3600 {
		return fmt.Errorf("recipe %q step %q has invalid timeout", recipeID, step.ID)
	}
	if section != stepSectionMain &&
		stepFieldPresent(step, "rollback_step_id", step.RollbackStepID != "") {
		return fmt.Errorf(
			"recipe %q step %q may reference rollback only from the main steps list",
			recipeID,
			step.ID,
		)
	}
	if err := validateStepShape(recipeID, step); err != nil {
		return err
	}

	switch step.Type {
	case "package_install", "package_remove":
		if len(step.Packages) == 0 {
			return fmt.Errorf("recipe %q step %q has no packages", recipeID, step.ID)
		}
		for _, name := range step.Packages {
			if !packagePattern.MatchString(name) {
				return fmt.Errorf("recipe %q step %q has invalid package %q", recipeID, step.ID, name)
			}
		}
	case "service_enable", "service_disable", "service_start", "service_stop", "service_restart", "service_active":
		if !unitPattern.MatchString(step.Unit) {
			return fmt.Errorf("recipe %q step %q has invalid service unit %q", recipeID, step.ID, step.Unit)
		}
	case "file_write":
		if err := validateRecipePath(selector, step.Path); err != nil {
			return fmt.Errorf("recipe %q step %q has unsafe target path %q", recipeID, step.ID, step.Path)
		}
		if step.Template == "" {
			return fmt.Errorf("recipe %q step %q has no template", recipeID, step.ID)
		}
		if step.Mode != "" && !modePattern.MatchString(step.Mode) {
			return fmt.Errorf("recipe %q step %q has invalid mode %q", recipeID, step.ID, step.Mode)
		}
	case "firewall_open", "firewall_close":
		if step.Port < 1 || step.Port > 65535 || (step.Protocol != "tcp" && step.Protocol != "udp") {
			return fmt.Errorf("recipe %q step %q has invalid firewall endpoint", recipeID, step.ID)
		}
	case "tcp_probe":
		if step.Port < 1 || step.Port > 65535 {
			return fmt.Errorf("recipe %q step %q has invalid TCP port", recipeID, step.ID)
		}
		host := net.ParseIP(strings.TrimSpace(step.Host))
		if host == nil || !host.IsLoopback() {
			return fmt.Errorf(
				"recipe %q step %q TCP probe host must be a loopback IP address",
				recipeID,
				step.ID,
			)
		}
	}
	return nil
}

func validateStepShape(recipeID string, step RecipeStep) error {
	allowed := map[string]bool{}
	switch step.Type {
	case "package_install", "package_remove":
		allowed["packages"] = true
	case "service_enable", "service_disable", "service_start", "service_stop", "service_restart", "service_active":
		allowed["unit"] = true
	case "file_write":
		allowed["path"] = true
		allowed["template"] = true
		allowed["mode"] = true
	case "firewall_open", "firewall_close":
		allowed["port"] = true
		allowed["protocol"] = true
	case "tcp_probe":
		allowed["host"] = true
		allowed["port"] = true
	}

	fields := []struct {
		name    string
		present bool
	}{
		{name: "packages", present: stepFieldPresent(step, "packages", len(step.Packages) > 0)},
		{name: "unit", present: stepFieldPresent(step, "unit", step.Unit != "")},
		{name: "path", present: stepFieldPresent(step, "path", step.Path != "")},
		{name: "template", present: stepFieldPresent(step, "template", step.Template != "")},
		{name: "mode", present: stepFieldPresent(step, "mode", step.Mode != "")},
		{name: "host", present: stepFieldPresent(step, "host", step.Host != "")},
		{name: "port", present: stepFieldPresent(step, "port", step.Port != 0)},
		{name: "protocol", present: stepFieldPresent(step, "protocol", step.Protocol != "")},
	}
	for _, field := range fields {
		if field.present && !allowed[field.name] {
			return fmt.Errorf(
				"recipe %q step %q field %q is not allowed for type %q",
				recipeID,
				step.ID,
				field.name,
				step.Type,
			)
		}
	}
	return nil
}

func stepFieldPresent(step RecipeStep, name string, nonZero bool) bool {
	if nonZero {
		return true
	}
	_, present := step.presentFields[name]
	return present
}

func validateRecipePath(selector PlatformSelector, value string) error {
	if value == "" || strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("path is empty or contains NUL")
	}
	switch normalizeToken(selector.OSFamily) {
	case "linux":
		return validatePOSIXRecipePath(value)
	default:
		return fmt.Errorf("path requires an explicit audited os_family")
	}
}

func validatePOSIXRecipePath(value string) error {
	if !path.IsAbs(value) || strings.Contains(value, `\`) {
		return fmt.Errorf("path is not an absolute POSIX path")
	}
	if containsParentTraversal(value) {
		return fmt.Errorf("path contains parent traversal")
	}
	return nil
}

func containsParentTraversal(value string) bool {
	normalized := strings.ReplaceAll(value, `\`, "/")
	for _, part := range strings.Split(normalized, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

func normalizeToken(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func versionConstraintMatches(version, constraint string) (bool, error) {
	versionParts, err := parseNumericVersion(version)
	if err != nil {
		return false, err
	}
	for _, raw := range strings.Split(constraint, ",") {
		part := strings.TrimSpace(raw)
		if part == "" {
			return false, fmt.Errorf("empty version expression")
		}
		operator := "="
		for _, candidate := range []string{">=", "<=", ">", "<", "="} {
			if strings.HasPrefix(part, candidate) {
				operator = candidate
				part = strings.TrimSpace(strings.TrimPrefix(part, candidate))
				break
			}
		}
		want, err := parseNumericVersion(part)
		if err != nil {
			return false, err
		}
		cmp := compareVersionParts(versionParts, want)
		matched := map[string]bool{
			"=":  cmp == 0,
			">":  cmp > 0,
			">=": cmp >= 0,
			"<":  cmp < 0,
			"<=": cmp <= 0,
		}[operator]
		if !matched {
			return false, nil
		}
	}
	return true, nil
}

func parseNumericVersion(value string) ([]int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("empty version")
	}
	parts := strings.Split(value, ".")
	result := make([]int, len(parts))
	for i, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("invalid version %q", value)
		}
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return nil, fmt.Errorf("version %q is not numeric", value)
		}
		result[i] = number
	}
	return result, nil
}

func compareVersionParts(left, right []int) int {
	length := len(left)
	if len(right) > length {
		length = len(right)
	}
	for i := 0; i < length; i++ {
		l, r := 0, 0
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		if l < r {
			return -1
		}
		if l > r {
			return 1
		}
	}
	return 0
}
