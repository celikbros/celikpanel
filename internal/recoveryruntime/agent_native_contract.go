package recoveryruntime

import "github.com/alicelik/celikpanel/internal/agentnativecontract"

// CompatibleMailAgent is a pinned content/metadata proof, not authority. The
// caller must supply authenticated release/snapshot provenance and the operation
// exclusion before relying on it. A declaration does not certify behavior of an
// arbitrary historical executable or grant permission to install or roll back.
type CompatibleMailAgent struct {
	Contract   agentnativecontract.Contract
	revalidate func() error
	close      func()
	expected   agentnativecontract.Contract
}

func (p *CompatibleMailAgent) Revalidate() error {
	if p == nil || p.revalidate == nil || p.Contract != p.expected {
		return fail(ReasonChanged)
	}
	return p.revalidate()
}
func (p *CompatibleMailAgent) Close() {
	if p != nil && p.close != nil {
		p.close()
		p.close = nil
		p.revalidate = nil
	}
}

// VerifyAgentNativeContract verifies a candidate declaration even before any
// independent hook exists. Installed historical baselines remain a separate
// compatibility decision; they must not receive fabricated declarations.
func VerifyAgentNativeContract(binaryDirectory string) error {
	proof, err := InspectCompatibleMailAgent(binaryDirectory)
	if err != nil {
		return err
	}
	defer proof.Close()
	return proof.Revalidate()
}
