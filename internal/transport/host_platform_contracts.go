package transport

// HostPlatformResponse is the verified, non-secret host capability profile the
// panel uses for package- and service-manager routing. DistroID and VersionID
// are audit metadata only; authorization comes from the consistent verified
// family, package manager, service manager, and architecture tuple.
type HostPlatformResponse struct {
	DistroFamily   string `json:"distro_family"`
	PackageManager string `json:"package_manager"`
	ServiceManager string `json:"service_manager"`
	DistroID       string `json:"distro_id"`
	VersionID      string `json:"version_id"`
	Architecture   string `json:"architecture"`
}
