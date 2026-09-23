//go:build linux

package recoveryruntime

import (
	"github.com/alicelik/celikpanel/internal/agentnativecontract"
	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"path/filepath"
)

// MailEnrollmentHelper pins the complete installed kit and its Agent declaration.
// It grants no owner authority and never executes the management Agent as a helper.
type MailEnrollmentHelper struct {
	Path         string
	Generation   string
	agent        *CompatibleMailAgent
	bundle       *flatNativeBundle
	expectedPath string
}

func InspectMailEnrollmentHelper(binaryDirectory, runtimeRoot string) (*MailEnrollmentHelper, error) {
	agent, err := InspectCompatibleMailAgent(binaryDirectory)
	if err != nil {
		return nil, err
	}
	if agent.Contract.MailEnrollmentPolicy != agentnativecontract.MailEnrollmentPolicy || !ValidDigest(agent.Contract.MailRenewalGeneration) {
		agent.Close()
		return nil, fail(ReasonUnsupported)
	}
	generation := agent.Contract.MailRenewalGeneration
	bundle, err := readMailRenewalBundle(filepath.Join(runtimeRoot, generation))
	if err != nil {
		agent.Close()
		return nil, err
	}
	path := filepath.Join(runtimeRoot, generation, mailrenewalkit.BinaryName)
	proof := &MailEnrollmentHelper{path, generation, agent, bundle, path}
	if err = proof.Revalidate(); err != nil {
		proof.Close()
		return nil, err
	}
	return proof, nil
}
func (h *MailEnrollmentHelper) Revalidate() error {
	if h == nil || h.agent == nil || h.bundle == nil || h.Generation != h.agent.Contract.MailRenewalGeneration || h.Generation != h.bundle.generation || h.Path != h.expectedPath {
		return fail(ReasonChanged)
	}
	if err := h.agent.Revalidate(); err != nil {
		return err
	}
	return h.bundle.state.revalidate()
}
func (h *MailEnrollmentHelper) AgentIdentity() (string, string) {
	if h == nil || h.agent == nil {
		return "", ""
	}
	return h.agent.Contract.SourceCommit, h.agent.Contract.AgentSHA256
}
func (h *MailEnrollmentHelper) Close() {
	if h != nil {
		if h.agent != nil {
			h.agent.Close()
			h.agent = nil
		}
		if h.bundle != nil {
			h.bundle.state.close()
			h.bundle = nil
		}
	}
}
