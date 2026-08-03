package transport

import "github.com/alicelik/celikpanel/internal/core"

type GetPHPPoolConfigRequest struct {
	Version  string `json:"version"`
	PoolName string `json:"pool_name"`
}

type MigratePHPPoolRequest struct {
	OldVersion string `json:"old_version"`
	NewVersion string `json:"new_version"`
	PoolName   string `json:"pool_name"`
}

// These aliases keep the RPC boundary in transport while preserving the
// canonical PHP domain models already shared by the API and service layer.
type PHPPoolConfig = core.PHPPoolConfig
type PHPPoolConfigRequest = core.PHPPoolConfigRequest
type DeletePHPPoolRequest = core.DeletePHPPoolRequest
type PHPVersionRequest = core.PHPVersionRequest
type ExtendedPHPConfig = core.ExtendedPHPConfig
type ExtendedPHPConfigRequest = core.ExtendedPHPConfigRequest
type PHPExtension = core.PHPExtension
type PHPExtensionRequest = core.PHPExtensionRequest
type PHPConfig = core.PHPConfig
type PHPConfigRequest = core.PHPConfigRequest
type PHPPool = core.PHPPool
type PHPPoolRequest = core.PHPPoolRequest
