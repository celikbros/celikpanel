//go:build linux

package recoveryruntime

import (
	"github.com/alicelik/celikpanel/internal/agentnativecontract"
	"os"
	"path/filepath"
)

// InspectCompatibleMailAgent never executes the inspected Agent. Missing legacy
// declarations and unknown metadata are refusal, not compatible absence.
func InspectCompatibleMailAgent(binaryDirectory string) (result *CompatibleMailAgent, resultErr error) {
	if os.Geteuid() != 0 || !filepath.IsAbs(binaryDirectory) || filepath.Clean(binaryDirectory) != binaryDirectory {
		return nil, fail(ReasonUnsafeMetadata)
	}
	state := &runtimeState{config: resolveConfig{anchor: "/", uid: 0, gid: 0, protectedDirectoryGroups: true}}
	defer func() {
		if resultErr != nil {
			state.close()
		}
	}()
	parent, err := state.openPath(binaryDirectory)
	if err != nil {
		return nil, err
	}
	manifest, err := state.openFile(parent, agentnativecontract.FileName, 0644, agentnativecontract.MaxSize)
	if err != nil {
		return nil, asReadError(err)
	}
	agent, err := state.openFile(parent, "agent", 0755, agentnativecontract.MaxAgentSize)
	if err != nil {
		return nil, asReadError(err)
	}
	for _, file := range []*pinnedFile{manifest, agent} {
		if err = refusePromotionXattrs(file); err != nil {
			return nil, err
		}
	}
	raw, err := manifest.readBounded()
	if err != nil {
		return nil, err
	}
	manifest.digest = Digest(raw)
	binary, err := agent.readBounded()
	if err != nil {
		return nil, err
	}
	agent.digest = Digest(binary)
	contract, err := agentnativecontract.Verify(raw, binary)
	if err != nil {
		return nil, fail(ReasonUnsupported)
	}
	if err = state.revalidate(); err != nil {
		return nil, err
	}
	return &CompatibleMailAgent{Contract: contract, expected: contract, revalidate: state.revalidate, close: func() { _ = state.close() }}, nil
}
