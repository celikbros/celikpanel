package transport

// HostPlatformResponse is the verified, non-secret host identity that the
// panel may use when deciding whether a distribution-specific capability is
// certified. Package-manager family alone is deliberately insufficient: a
// dnf binary does not make Fedora, CentOS Stream, CloudLinux and RHEL
// interchangeable product targets.
//
// HostPlatformResponse, panelin dağıtıma özgü bir kabiliyetin sertifikalı olup
// olmadığına karar verirken kullanabileceği doğrulanmış ve gizli olmayan host
// kimliğidir. Yalnız paket-yöneticisi ailesi bilerek yetersizdir: dnf
// çalıştırılabilmesi Fedora, CentOS Stream, CloudLinux ve RHEL'i aynı ürün
// hedefi yapmaz.
type HostPlatformResponse struct {
	DistroFamily   string `json:"distro_family"`
	PackageManager string `json:"package_manager"`
	ServiceManager string `json:"service_manager"`
	DistroID       string `json:"distro_id"`
	VersionID      string `json:"version_id"`
	Architecture   string `json:"architecture"`
}
