package main

// The defer guards make every legacy/internal error branch carry a stable
// code, even when a newly added return path forgets to assign a more specific
// one. The detailed string stays inside the panel/agent trust boundary.
//
// Defer korumaları, yeni bir return dalı daha özel kod atamayı unutsa bile her
// eski/iç hata dalının sabit kod taşımasını sağlar. Ayrıntılı metin panel/agent
// güven sınırı içinde kalır.
func ensureRepoStatusErrorCode(resp *RepoStatusResponse, fallback string) {
	if resp == nil || resp.Error == "" || resp.ErrorCode != "" {
		return
	}
	resp.ErrorCode = fallback
}

func ensureRepoPackagesErrorCode(resp *RepoPackagesResponse, fallback string) {
	if resp == nil || resp.Error == "" || resp.ErrorCode != "" {
		return
	}
	resp.ErrorCode = fallback
}
