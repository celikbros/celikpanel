package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

type ReconcileSiteCertLineagesRequest struct {
	ExpectedBuildCommit string   `json:"expected_build_commit"`
	ReferencedLineages  []string `json:"referenced_lineages"`
	// ActiveLineages is the pre-ledger-expansion wire field. Accepting it keeps
	// reconciliation safe while panel and agent binaries are rolling between
	// versions; both fields have identical "retain this lineage" semantics.
	ActiveLineages []string `json:"active_lineages"`
}

type ReconcileSiteCertLineagesResponse struct {
	Deleted int    `json:"deleted"`
	Error   string `json:"error,omitempty"`
}

// ReconcileSiteCertLineages removes only agent-generated staging lineages
// that are not referenced anywhere in the panel's certificate ledger. It never
// considers canonical domain lineages, so operator/panel certificates remain
// outside this cleanup authority even in the legacy global certbot store.
func (a *Agent) ReconcileSiteCertLineages(
	req *ReconcileSiteCertLineagesRequest,
	resp *ReconcileSiteCertLineagesResponse,
) error {
	if req == nil {
		resp.Error = "certificate lineage reconciliation request is required"
		return nil
	}
	if err := requireExpectedBuildCommit(req.ExpectedBuildCommit, "reconciling certificate lineages"); err != nil {
		resp.Error = err.Error()
		return nil
	}

	if len(req.ReferencedLineages) > 10000 || len(req.ActiveLineages) > 10000 {
		resp.Error = "too many referenced lineages"
		return nil
	}
	allReferenced := make(
		[]string,
		0,
		len(req.ReferencedLineages)+len(req.ActiveLineages),
	)
	allReferenced = append(allReferenced, req.ReferencedLineages...)
	allReferenced = append(allReferenced, req.ActiveLineages...)
	referenced := make(map[string]struct{}, len(allReferenced))
	for _, raw := range allReferenced {
		lineage := strings.ToLower(strings.TrimSpace(raw))
		if !validStagedSiteLineage.MatchString(lineage) {
			resp.Error = "invalid referenced staged lineage name"
			return nil
		}
		referenced[lineage] = struct{}{}
	}

	if !acquireSiteCertbot() {
		resp.Error = "another site certificate operation is already running; retry shortly"
		return nil
	}
	defer releaseSiteCertbot()

	isolated := certificateCleanupIsolatedStorage()
	legacy := isolated
	legacy.ConfigDir = certificateCleanupLegacyConfigDir()
	for _, storage := range []certbotStorage{isolated, legacy} {
		configs, err := filepath.Glob(
			filepath.Join(storage.ConfigDir, "renewal", "cp-site-*.conf"),
		)
		if err != nil {
			resp.Error = fmt.Sprintf("list staged certbot lineages: %v", err)
			return nil
		}
		for _, config := range configs {
			lineage := strings.TrimSuffix(filepath.Base(config), ".conf")
			if !validStagedSiteLineage.MatchString(lineage) {
				continue
			}
			if _, keep := referenced[lineage]; keep {
				continue
			}
			if _, err := certificateCleanupLookPath("certbot"); err != nil {
				resp.Error = "staged certbot lineage exists but certbot is missing"
				return nil
			}
			args := []string{"delete"}
			args = append(args, storage.commandArgs()...)
			args = append(args, "--cert-name", lineage, "--non-interactive")
			out, err := certificateCleanupRunCertbot(args...)
			if err != nil {
				resp.Error = panelCertCommandError("certbot delete", out, err).Error()
				return nil
			}
			resp.Deleted++
		}
	}
	return nil
}
