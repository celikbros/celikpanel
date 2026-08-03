package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/services"
)

// cPanel importer (roadmap 3B). A cpmove/backup archive is inspected in a
// single streaming pass — nothing is extracted or applied until the operator
// confirms the preview. Only the well-known control files are read
// (cp/<user>, homedir/etc/*/shadow|quota, va/*, dnszones/*.db,
// mysql/*.create); site payload is only measured, never buffered.
//
// cPanel içe aktarıcı (yol haritası 3B). Bir cpmove/backup arşivi tek akış
// geçişinde incelenir — operatör önizlemeyi onaylayana dek hiçbir şey
// çıkarılmaz ya da uygulanmaz. Yalnızca bilinen kontrol dosyaları okunur;
// site yükü yalnızca ölçülür, asla belleğe alınmaz.

const cpmoveControlFileLimit = 2 << 20 // 2 MB: control files are tiny; refuse absurd ones

type CpmoveMailAccount struct {
	Domain    string `json:"domain"`
	User      string `json:"user"`
	CryptHash string `json:"crypt_hash"`
	QuotaMB   int    `json:"quota_mb"`
}

type CpmoveForwarder struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

type CpmoveDNSRecord struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Prio    int    `json:"prio"`
}

type CpmoveDatabase struct {
	Name      string `json:"name"`
	DumpBytes int64  `json:"dump_bytes"`
}

type CpmoveInspectRequest struct {
	Path string `json:"path"`
}

type CpmoveInspectResponse struct {
	Username     string                       `json:"username"`
	MainDomain   string                       `json:"main_domain"`
	Domains      []string                     `json:"domains"`
	PublicHTML   bool                         `json:"public_html"`
	SiteBytes    int64                        `json:"site_bytes"`
	MailAccounts []CpmoveMailAccount          `json:"mail_accounts"`
	Forwarders   []CpmoveForwarder            `json:"forwarders"`
	DNSZones     map[string][]CpmoveDNSRecord `json:"dns_zones"`
	Databases    []CpmoveDatabase             `json:"databases"`
	Error        string                       `json:"error,omitempty"`
}

// cpmoveRel normalises an archive member path: the leading cpmove-USER/ or
// backup-.../ directory is stripped so lookups see homedir/, cp/, va/…
// cpmoveRel, arşiv üyesi yolunu normalleştirir: baştaki cpmove-USER/ ya da
// backup-.../ dizini atılır; aramalar homedir/, cp/, va/… görür.
func cpmoveRel(name string) string {
	name = strings.TrimPrefix(path.Clean(name), "./")
	parts := strings.SplitN(name, "/", 2)
	if len(parts) == 2 {
		first := parts[0]
		if strings.HasPrefix(first, "cpmove-") || strings.HasPrefix(first, "backup-") {
			return parts[1]
		}
	}
	return name
}

func openCpmove(archivePath string) (*tar.Reader, func(), error) {
	if !strings.HasSuffix(archivePath, ".tar.gz") && !strings.HasSuffix(archivePath, ".tgz") {
		return nil, nil, fmt.Errorf("expected a .tar.gz cpmove/backup archive")
	}
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, nil, err
	}
	gz, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return nil, nil, fmt.Errorf("not a gzip archive: %v", err)
	}
	closer := func() { gz.Close(); f.Close() }
	return tar.NewReader(gz), closer, nil
}

func readSmall(tr *tar.Reader, size int64) (string, error) {
	if size > cpmoveControlFileLimit {
		return "", fmt.Errorf("control file too large")
	}
	b, err := io.ReadAll(io.LimitReader(tr, cpmoveControlFileLimit))
	return string(b), err
}

func (a *Agent) InspectCpmove(req *CpmoveInspectRequest, resp *CpmoveInspectResponse) error {
	tr, done, err := openCpmove(req.Path)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	defer done()

	resp.Domains = []string{}
	resp.MailAccounts = []CpmoveMailAccount{}
	resp.Forwarders = []CpmoveForwarder{}
	resp.DNSZones = map[string][]CpmoveDNSRecord{}
	resp.Databases = []CpmoveDatabase{}

	quotas := map[string]int{} // "domain/user" → MB
	dumps := map[string]int64{}
	creates := map[string]bool{}

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			resp.Error = fmt.Sprintf("archive read failed: %v", err)
			return nil
		}
		rel := cpmoveRel(hdr.Name)

		switch {
		case strings.HasPrefix(rel, "cp/") && hdr.Typeflag == tar.TypeReg:
			resp.Username = path.Base(rel)
			content, err := readSmall(tr, hdr.Size)
			if err != nil {
				continue
			}
			for _, line := range strings.Split(content, "\n") {
				if k, v, ok := strings.Cut(line, "="); ok {
					if k == "DNS" {
						resp.MainDomain = strings.TrimSpace(v)
					}
					// DNS, DNS1, DNS2… list main + addon/parked domains.
					// DNS, DNS1, DNS2… ana + addon/park domain'leri listeler.
					if strings.HasPrefix(k, "DNS") {
						if d := strings.TrimSpace(v); d != "" {
							resp.Domains = append(resp.Domains, d)
						}
					}
				}
			}

		case rel == "homedir/public_html" || strings.HasPrefix(rel, "homedir/public_html/"):
			resp.PublicHTML = true
			if hdr.Typeflag == tar.TypeReg {
				resp.SiteBytes += hdr.Size
			}

		case strings.HasPrefix(rel, "homedir/etc/") && strings.HasSuffix(rel, "/quota") && hdr.Typeflag == tar.TypeReg:
			domain := path.Base(path.Dir(rel))
			content, err := readSmall(tr, hdr.Size)
			if err != nil {
				continue
			}
			for _, line := range strings.Split(content, "\n") {
				if user, bytesStr, ok := strings.Cut(strings.TrimSpace(line), ":"); ok {
					if b, err := strconv.ParseInt(strings.TrimSpace(bytesStr), 10, 64); err == nil && b > 0 {
						quotas[domain+"/"+user] = int(b / (1024 * 1024))
					}
				}
			}

		case strings.HasPrefix(rel, "homedir/etc/") && strings.HasSuffix(rel, "/shadow") && hdr.Typeflag == tar.TypeReg:
			domain := path.Base(path.Dir(rel))
			content, err := readSmall(tr, hdr.Size)
			if err != nil {
				continue
			}
			for _, line := range strings.Split(content, "\n") {
				fields := strings.Split(strings.TrimSpace(line), ":")
				if len(fields) >= 2 && fields[0] != "" && strings.HasPrefix(fields[1], "$") {
					resp.MailAccounts = append(resp.MailAccounts, CpmoveMailAccount{
						Domain:    domain,
						User:      fields[0],
						CryptHash: fields[1],
					})
				}
			}

		case strings.HasPrefix(rel, "va/") && hdr.Typeflag == tar.TypeReg:
			content, err := readSmall(tr, hdr.Size)
			if err != nil {
				continue
			}
			for _, line := range strings.Split(content, "\n") {
				if src, dst, ok := strings.Cut(strings.TrimSpace(line), ":"); ok {
					src, dst = strings.TrimSpace(src), strings.TrimSpace(dst)
					// Only plain address forwarders; pipes/scripts are not imported.
					// Yalnız düz adres yönlendirmeleri; pipe/betikler içe alınmaz.
					if src != "" && strings.Contains(dst, "@") && !strings.ContainsAny(dst, "|/") {
						resp.Forwarders = append(resp.Forwarders, CpmoveForwarder{Source: src, Destination: dst})
					}
				}
			}

		case strings.HasPrefix(rel, "dnszones/") && strings.HasSuffix(rel, ".db") && hdr.Typeflag == tar.TypeReg:
			zone := strings.TrimSuffix(path.Base(rel), ".db")
			content, err := readSmall(tr, hdr.Size)
			if err != nil {
				continue
			}
			resp.DNSZones[zone] = parseBindZone(zone, content)

		case strings.HasPrefix(rel, "mysql/") && hdr.Typeflag == tar.TypeReg:
			base := path.Base(rel)
			switch {
			case strings.HasSuffix(base, ".create"):
				creates[strings.TrimSuffix(base, ".create")] = true
			case strings.HasSuffix(base, ".sql") && base != "mysql.sql":
				dumps[strings.TrimSuffix(base, ".sql")] = hdr.Size
			}
		}
	}

	for name, size := range dumps {
		resp.Databases = append(resp.Databases, CpmoveDatabase{Name: name, DumpBytes: size})
	}
	// A .create without a dump still counts as a database (empty one).
	// Dump'ı olmayan .create de (boş) bir veritabanı sayılır.
	for name := range creates {
		if _, ok := dumps[name]; !ok {
			resp.Databases = append(resp.Databases, CpmoveDatabase{Name: name})
		}
	}

	// Attach quotas to accounts now that both maps are complete.
	// İki harita da tamamlandığına göre kotaları hesaplara bağla.
	for i := range resp.MailAccounts {
		acc := &resp.MailAccounts[i]
		if mb, ok := quotas[acc.Domain+"/"+acc.User]; ok {
			acc.QuotaMB = mb
		}
	}

	if resp.MainDomain == "" && len(resp.DNSZones) > 0 {
		for z := range resp.DNSZones {
			resp.MainDomain = z
			break
		}
	}
	return nil
}

// parseBindZone extracts records from a BIND zone file, tolerantly: comments
// and $-directives are skipped, the multi-line SOA is consumed by bracket
// balance, sticky names are honoured and quoted TXT segments are joined.
// parseBindZone, bir BIND zone dosyasından kayıtları hoşgörüyle çıkarır:
// yorumlar ve $-yönergeleri atlanır, çok satırlı SOA parantez dengesiyle
// tüketilir, yapışkan adlar korunur ve tırnaklı TXT parçaları birleştirilir.
func parseBindZone(zone, content string) []CpmoveDNSRecord {
	records := []CpmoveDNSRecord{}
	knownTypes := map[string]bool{"A": true, "AAAA": true, "CNAME": true, "MX": true, "TXT": true, "SRV": true, "NS": true, "CAA": true}

	lastName := zone + "."
	inSOA := false

	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := stripZoneComment(scanner.Text())
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if inSOA {
			if strings.Contains(line, ")") {
				inSOA = false
			}
			continue
		}
		if strings.HasPrefix(line, "$") {
			continue
		}

		startsWithSpace := line[0] == ' ' || line[0] == '\t'
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		name := lastName
		idx := 0
		if !startsWithSpace {
			name = fields[0]
			lastName = name
			idx = 1
		}

		// Optional TTL and class before the type.
		// Türden önce isteğe bağlı TTL ve sınıf.
		ttl := 3600
		for idx < len(fields) {
			f := strings.ToUpper(fields[idx])
			if n, err := strconv.Atoi(fields[idx]); err == nil {
				ttl = n
				idx++
				continue
			}
			if f == "IN" {
				idx++
				continue
			}
			break
		}
		if idx >= len(fields) {
			continue
		}
		rtype := strings.ToUpper(fields[idx])
		idx++
		if rtype == "SOA" {
			if !strings.Contains(line, ")") {
				inSOA = true
			}
			continue
		}
		if !knownTypes[rtype] || idx >= len(fields) {
			continue
		}

		rec := CpmoveDNSRecord{Name: qualifyZoneName(name, zone), Type: rtype, TTL: ttl}
		rdata := fields[idx:]

		switch rtype {
		case "MX":
			if len(rdata) >= 2 {
				rec.Prio, _ = strconv.Atoi(rdata[0])
				rec.Content = strings.TrimSuffix(rdata[1], ".")
			}
		case "SRV":
			if len(rdata) >= 4 {
				rec.Prio, _ = strconv.Atoi(rdata[0])
				rec.Content = strings.Join(rdata[1:], " ")
				rec.Content = strings.TrimSuffix(rec.Content, ".")
			}
		case "TXT":
			joined := strings.Join(rdata, " ")
			var parts []string
			for {
				start := strings.IndexByte(joined, '"')
				if start < 0 {
					break
				}
				end := strings.IndexByte(joined[start+1:], '"')
				if end < 0 {
					break
				}
				parts = append(parts, joined[start+1:start+1+end])
				joined = joined[start+1+end+1:]
			}
			if len(parts) == 0 {
				rec.Content = strings.Join(rdata, " ")
			} else {
				rec.Content = strings.Join(parts, "")
			}
		case "CNAME", "NS":
			rec.Content = qualifyZoneName(rdata[0], zone)
		default: // A, AAAA, CAA
			rec.Content = strings.Join(rdata, " ")
		}

		if rec.Content != "" {
			records = append(records, rec)
		}
	}
	return records
}

// stripZoneComment cuts a BIND line at the first semicolon OUTSIDE quotes —
// a ';' inside a TXT string is data, not a comment.
// stripZoneComment, BIND satırını tırnak DIŞINDAKİ ilk noktalı virgülde
// keser — TXT dizesi içindeki ';' yorum değil veridir.
func stripZoneComment(line string) string {
	inQuote := false
	for i, r := range line {
		switch r {
		case '"':
			inQuote = !inQuote
		case ';':
			if !inQuote {
				return line[:i]
			}
		}
	}
	return line
}

// qualifyZoneName turns zone-file names into plain FQDNs: "@"→zone,
// "www"→www.zone, trailing dots stripped.
// qualifyZoneName, zone dosyası adlarını düz FQDN'e çevirir.
func qualifyZoneName(name, zone string) string {
	name = strings.TrimSpace(name)
	if name == "@" || name == "" {
		return zone
	}
	if strings.HasSuffix(name, ".") {
		return strings.TrimSuffix(name, ".")
	}
	return name + "." + zone
}

type CpmoveExtractRequest struct {
	Path           string `json:"path"`
	SubscriptionID int    `json:"subscription_id"`
	DomainID       int    `json:"domain_id"`
}

type CpmoveExtractResponse struct {
	Files int    `json:"files"`
	Bytes int64  `json:"bytes"`
	Error string `json:"error,omitempty"`
}

// ExtractCpmoveFiles streams homedir/public_html out of the archive into the
// site's document root. Entries are sanitised (no "..", no absolute paths)
// and symlinks/hardlinks are skipped — an archive cannot write outside the
// target directory.
// ExtractCpmoveFiles, arşivden homedir/public_html'i sitenin belge köküne
// akıtır. Girişler temizlenir (".." yok, mutlak yol yok) ve symlink/hardlink
// atlanır — bir arşiv hedef dizinin dışına yazamaz.
func (a *Agent) ExtractCpmoveFiles(req *CpmoveExtractRequest, resp *CpmoveExtractResponse) error {
	targetDir, err := hostingpath.DocumentRoot(req.SubscriptionID, req.DomainID)
	if err != nil {
		resp.Error = "invalid site filesystem identity"
		return nil
	}
	tr, done, err := openCpmove(req.Path)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	defer done()

	if err := secureExtractCpmoveFiles(tr, targetDir, resp); err != nil {
		resp.Error = err.Error()
	}
	return nil
}

type CpmoveImportDBRequest struct {
	Path     string `json:"path"`
	DumpName string `json:"dump_name"` // database name inside mysql/ (e.g. user_wp)
	TargetDB string `json:"target_db"` // database to import into (already created)
}

type CpmoveImportDBResponse struct {
	Imported bool   `json:"imported"`
	Error    string `json:"error,omitempty"`
}

// ImportCpmoveDatabase extracts one mysql/<name>.sql dump to a temp file and
// feeds it to the mysql client. The target database must already exist.
// ImportCpmoveDatabase, tek bir mysql/<ad>.sql dump'ını geçici dosyaya
// çıkarır ve mysql istemcisine verir. Hedef veritabanı önceden var olmalıdır.
func (a *Agent) ImportCpmoveDatabase(req *CpmoveImportDBRequest, resp *CpmoveImportDBResponse) error {
	if err := services.ValidateSQLIdentifier(req.TargetDB); err != nil {
		resp.Error = "invalid target database name"
		return nil
	}

	tr, done, err := openCpmove(req.Path)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	defer done()

	want := "mysql/" + req.DumpName + ".sql"
	tmp, err := os.CreateTemp("", "cpmove-sql-*")
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	found := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			resp.Error = fmt.Sprintf("archive read failed: %v", err)
			return nil
		}
		if cpmoveRel(hdr.Name) == want && hdr.Typeflag == tar.TypeReg {
			if _, err := io.Copy(tmp, tr); err != nil { //nosec G110 -- bounded by disk, admin-supplied archive
				resp.Error = err.Error()
				return nil
			}
			found = true
			break
		}
	}
	if !found {
		resp.Error = fmt.Sprintf("dump %s not found in archive", want)
		return nil
	}

	// Create the database (idempotent), then feed the dump. Database USERS
	// are deliberately not migrated in v1 — cPanel grants carry hashed
	// passwords tied to its mysql.sql; apps must be repointed manually.
	// Veritabanını oluştur (bağımsız), sonra dump'ı yükle. Veritabanı
	// KULLANICILARI v1'de bilerek taşınmaz — uygulama config'leri elle
	// yeni kimlik bilgilerine yönlendirilmelidir.
	dbIdent, err := services.QuoteMySQLIdentifier(req.TargetDB)
	if err != nil {
		resp.Error = fmt.Sprintf("invalid target database name: %v", err)
		return nil
	}
	if out, err := exec.Command("mysql", "-e",
		fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s;", dbIdent)).CombinedOutput(); err != nil {
		resp.Error = fmt.Sprintf("mysql create failed: %v: %s", err, strings.TrimSpace(string(out)))
		return nil
	}

	cmd := exec.Command("bash", "-c", fmt.Sprintf("mysql %s < %s", req.TargetDB, tmp.Name()))
	if out, err := cmd.CombinedOutput(); err != nil {
		resp.Error = fmt.Sprintf("mysql import failed: %v: %s", err, strings.TrimSpace(string(out)))
		return nil
	}
	resp.Imported = true
	return nil
}
