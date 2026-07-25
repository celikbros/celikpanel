package main

// The agent carries the same two link-time values as the panel, so a
// panel/agent version mismatch is detectable instead of silent. They are
// deployed as a pair; when the pair breaks, the side that ENFORCES a rule may
// be older than the side that believes the rule is in force.
//
// Agent, panelle aynı iki bağlama-anı değerini taşır; böylece panel/agent
// sürüm uyuşmazlığı sessiz kalmaz, saptanabilir olur. İkisi bir çift olarak
// dağıtılır; çift bozulduğunda bir kuralı UYGULAYAN taraf, kuralın yürürlükte
// olduğunu sanan taraftan eski olabilir.
var (
	buildVersion = "dev"
	buildCommit  = "unknown"
)

type AgentVersionResponse struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

func (a *Agent) Version(_ *struct{}, resp *AgentVersionResponse) error {
	resp.Version = buildVersion
	resp.Commit = buildCommit
	return nil
}
