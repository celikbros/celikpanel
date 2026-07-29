package systemsqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	OwnerWorkerInspect       = "inspect"
	OwnerWorkerCheck         = "check"
	OwnerWorkerSnapshot      = "snapshot"
	OwnerWorkerOptimize      = "optimize"
	OwnerWorkerDestinationFD = 3
	OwnerWorkerWorkspaceFD   = OwnerWorkerDestinationFD + 1

	maxOwnerWorkerSnapshotBytes = int64(2 << 30)
)

// OwnerWorkerResponse is the small, path-free protocol returned by the dropped-UID worker.
// OwnerWorkerResponse, yetkisi düşürülmüş UID çalışanının döndürdüğü küçük ve yol içermeyen protokoldür.
type OwnerWorkerResponse struct {
	Success    bool              `json:"success"`
	Inspection MutableInspection `json:"inspection,omitempty"`
	Check      CheckResult       `json:"check,omitempty"`
	Error      string            `json:"error,omitempty"`
}

// PrepareOwnerWorkerProcess enforces the dropped identity and resource ceiling before worker setup continues.
// PrepareOwnerWorkerProcess, çalışan kurulumu sürmeden önce düşürülmüş kimliği ve kaynak tavanını zorunlu kılar.
func PrepareOwnerWorkerProcess() error {
	return validateOwnerWorkerProcess()
}

// RunOwnerWorkerOperation executes one fixed database operation after platform identity checks.
// RunOwnerWorkerOperation, platform kimlik denetimlerinden sonra tek bir sabit veritabanı işlemini yürütür.
func RunOwnerWorkerOperation(
	ctx context.Context,
	definitions []Definition,
	action string,
	databaseID string,
	destination *os.File,
	workspace *os.File,
	limits SnapshotLimits,
) OwnerWorkerResponse {
	if err := PrepareOwnerWorkerProcess(); err != nil {
		return ownerWorkerFailure(err)
	}
	definition, err := ownerWorkerDefinition(definitions, databaseID)
	if err != nil {
		return ownerWorkerFailure(err)
	}
	operations := directMutableOperations{}
	response := OwnerWorkerResponse{}
	switch action {
	case OwnerWorkerInspect:
		response.Inspection, err = operations.Inspect(ctx, definition)
	case OwnerWorkerCheck:
		response.Check, err = operations.Check(ctx, definition)
	case OwnerWorkerSnapshot:
		if err = limits.validate(); err != nil {
			err = errors.New("isolated snapshot capacity limits are invalid")
			break
		}
		if err = validateOwnerWorkerDestination(destination); err == nil {
			err = prepareOwnerWorkerWorkspace(workspace)
		}
		if err == nil {
			operations.workspaceDirectory = workspace
			err = operations.Snapshot(ctx, definition, destination, limits)
		}
	case OwnerWorkerOptimize:
		if !definition.Optimizable {
			err = errors.New("this system SQLite database is read-only")
			break
		}
		err = operations.Optimize(ctx, definition)
	default:
		err = errors.New("unknown isolated SQLite operation")
	}
	if err != nil {
		return ownerWorkerFailure(err)
	}
	response.Success = true
	return response
}

func ownerWorkerDefinition(definitions []Definition, databaseID string) (Definition, error) {
	if !knownDatabaseID(databaseID) || strings.TrimSpace(databaseID) != databaseID {
		return Definition{}, errors.New("unknown system SQLite database")
	}
	for _, definition := range definitions {
		if definition.ID != databaseID {
			continue
		}
		if !definition.Mutable || !filepath.IsAbs(filepath.Clean(definition.Path)) {
			return Definition{}, errors.New("this system SQLite database is read-only")
		}
		if err := validateWriterIdentityDefinition(definition); err != nil {
			return Definition{}, err
		}
		return definition, nil
	}
	return Definition{}, errors.New("unknown system SQLite database")
}

func validateWriterIdentityDefinition(definition Definition) error {
	if definition.WriterIdentitySet {
		if definition.WriterUID == 0 || definition.WriterGID == 0 {
			return errors.New("explicit SQLite writer identity must be non-root")
		}
		return nil
	}
	if definition.WriterUID != 0 || definition.WriterGID != 0 {
		return errors.New("SQLite writer identity is incomplete")
	}
	return nil
}

func ownerWorkerFailure(err error) OwnerWorkerResponse {
	return OwnerWorkerResponse{Error: publicDatabaseError(err).Error()}
}
