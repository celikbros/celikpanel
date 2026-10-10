package services

import (
	"bytes"
	_ "embed"
	"fmt"
	"sync"
	"text/template"
)

// Frozen vhost templates of the releases that wrote site files without the
// render header (D-031 point 6). They are copies, never edited, of
// `git show <tag>:internal/services/templates/nginx/vhost.conf.tmpl`:
//
//   - v0.1.0-alpha.81: commit a0beb7263d1f4ca72258f6b306f9111ba4e2a334,
//     blob 7f9005207de3672960d586f54a6c3d63c0e32ecc, SHA-256
//     4b4f3bdc4333a505a9749c77dae4cac748193f2d597ec63dd64f98b1eaaadbd7;
//   - v0.1.0-alpha.82: commit 2a0af88660773b43cbe1155c622483e2f0a2bbeb,
//     blob 9e6736416caffe502ee3f21cee19cf58927b5f6d, SHA-256
//     8236662bfb77ed52d65039b7521858be6e7c8d89017ce492c49d52e77a8b1aff.
//
// TestLegacyVhostTemplatesAreTheFrozenReleaseTexts pins the digests. A file
// with no header is adopted only when it is byte for byte what one of these
// renders from the same prepared data; any other file is "unknown origin" and
// is never overwritten.
//
// Başlıksız site dosyası yazan sürümlerin dondurulmuş şablonları; asla
// düzenlenmez. Başlıksız bir dosya yalnız bunlardan birinin aynı veriyle
// ürettiği metne bayt bayt eşitse benimsenir.

//go:embed templates/nginx/legacy/vhost-v0.1.0-alpha.81.conf.tmpl
var legacyVhostTemplateAlpha81 string

//go:embed templates/nginx/legacy/vhost-v0.1.0-alpha.82.conf.tmpl
var legacyVhostTemplateAlpha82 string

// LegacyVhostRender is one frozen release's text for a site.
type LegacyVhostRender struct {
	Release string
	Text    string
}

type legacyVhostTemplate struct {
	release string
	source  *string
}

var legacyVhostTemplates = []legacyVhostTemplate{
	{release: "v0.1.0-alpha.82", source: &legacyVhostTemplateAlpha82},
	{release: "v0.1.0-alpha.81", source: &legacyVhostTemplateAlpha81},
}

var (
	legacyVhostParseOnce sync.Once
	legacyVhostParsed    []*template.Template
	legacyVhostParseErr  error
)

func parsedLegacyVhostTemplates() ([]*template.Template, error) {
	legacyVhostParseOnce.Do(func() {
		for _, legacy := range legacyVhostTemplates {
			parsed, err := template.New(legacy.release).Parse(*legacy.source)
			if err != nil {
				legacyVhostParseErr = fmt.Errorf("parse frozen %s vhost template: %w", legacy.release, err)
				return
			}
			legacyVhostParsed = append(legacyVhostParsed, parsed)
		}
	})
	return legacyVhostParsed, legacyVhostParseErr
}

// LegacyCreationSuffix marks the creation-time text of an earlier release.
const LegacyCreationSuffix = " (creation)"

// LegacyVhostRenders renders data with every frozen template, newest first,
// in the two forms those releases wrote (measured in the eighth native record,
// set8-20261010, on Debian 13, Ubuntu 24.04 and Arch):
//
//   - the start/save text: the data as it is now (the Panel's start, a
//     setting, a certificate, a hosting change re-render with the managed
//     host names: the domain and www.<domain> for a top-level domain, and
//     its aliases);
//   - the creation text: what Agent.CreateSite of those releases wrote before
//     the Panel's first start or save: only the domain as server_name, no
//     alias, no validation-only name, no certificate, no redirection, the
//     same project type, document root and PHP socket.
//
// A site created by alpha.81/82 and never restarted or saved still carries
// the creation text; without it every such site would be "unknown origin".
// Not accepted: a temporary name (`use_temporary`, not offered by the
// interface and not stored, so it cannot be re-derived) and a render whose
// inputs (certificate path, aliases, PHP version) changed after the last
// render of the earlier release: those files are "unknown origin", on the
// safe side. A template that fails for this data contributes nothing.
//
// İki biçim: başlangıç/kaydetme metni ve oluşturma metni (yalnız alan adı).
func LegacyVhostRenders(data VhostData) ([]LegacyVhostRender, error) {
	parsed, err := parsedLegacyVhostTemplates()
	if err != nil {
		return nil, err
	}
	creation := data
	creation.TempDomain = ""
	creation.ServerNames = nil
	creation.ACMEChallengeNames = nil
	creation.RedirectWWW = false
	creation.SSLType = "none"
	creation.SSLCert, creation.SSLKey = "", ""
	creation.ForceHTTPS, creation.SSLAutoRedirect = false, false
	creation.HSTSEnabled, creation.HSTSMaxAge = false, 0

	renders := make([]LegacyVhostRender, 0, 2*len(parsed))
	for _, variant := range []struct {
		data   VhostData
		suffix string
	}{{data, ""}, {creation, LegacyCreationSuffix}} {
		prepared, err := prepareVhostData(variant.data)
		if err != nil {
			continue
		}
		for index, tmpl := range parsed {
			var buf bytes.Buffer
			if err := tmpl.Execute(&buf, prepared); err != nil {
				continue
			}
			renders = append(renders, LegacyVhostRender{
				Release: legacyVhostTemplates[index].release + variant.suffix,
				Text:    buf.String(),
			})
		}
	}
	return renders, nil
}
