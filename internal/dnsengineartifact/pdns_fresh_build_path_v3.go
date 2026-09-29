package dnsengineartifact

import (
	"errors"
	"path/filepath"
	"strings"
)

// A fresh paired PowerDNS primary builds its candidate SQLite database under a
// temporary name in the same root-private directory, fsyncs it, and renames it
// to the candidate name only once it is complete and verified. A crash while
// the database is being written therefore leaves at most this operation's own
// temporary file, never a partial file under the candidate name recovery
// reads. The temporary name is derived from the candidate name, which the V3
// journal policy derives from the request id, so the Agent's pre-start inverse
// and the owner command compute exactly the same path. No journal field
// records it; the V3 schema is unchanged.
//
// Eşli PowerDNS birincilinin aday veritabanı aynı kök-özel dizinde geçici bir
// adla kurulur, fsync edilir ve ancak tam ve doğrulanmışken aday adına
// taşınır. Böylece yazım sırasında bir çökme, kurtarmanın okuduğu adda asla
// yarım bir dosya bırakmaz; en fazla bu işlemin kendi geçici dosyası kalır.
// Geçici ad istek kimliğinden türeyen aday adından türetilir; V3 şeması
// değişmez.

const (
	pdnsFreshCandidatePrefixV3 = ".celikpanel-switch-"
	pdnsFreshCandidateSuffixV3 = ".sqlite3"
	pdnsFreshBuildSuffixV3     = ".building.sqlite3"
)

// PDNSFreshCandidateBuildPathsV3 returns the temporary build file for the
// candidate at candidatePath and the SQLite sidecar names that file can have.
func PDNSFreshCandidateBuildPathsV3(candidatePath string) (string, []string, error) {
	clean := filepath.Clean(candidatePath)
	base := filepath.Base(clean)
	if clean != candidatePath || !filepath.IsAbs(clean) ||
		!strings.HasPrefix(base, pdnsFreshCandidatePrefixV3) ||
		!strings.HasSuffix(base, pdnsFreshCandidateSuffixV3) ||
		strings.HasSuffix(base, pdnsFreshBuildSuffixV3) {
		return "", nil, errors.New("PowerDNS fresh candidate path is not a request candidate name")
	}
	id := strings.TrimSuffix(strings.TrimPrefix(base, pdnsFreshCandidatePrefixV3), pdnsFreshCandidateSuffixV3)
	if len(id) != 32 || strings.Trim(id, "0123456789abcdef") != "" {
		return "", nil, errors.New("PowerDNS fresh candidate path does not carry a request id")
	}
	build := filepath.Join(filepath.Dir(clean), pdnsFreshCandidatePrefixV3+id+pdnsFreshBuildSuffixV3)
	return build, []string{build + "-journal", build + "-wal", build + "-shm"}, nil
}
