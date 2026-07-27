package services

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"math/big"
	"path/filepath"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/repositories"
	"github.com/alicelik/celikpanel/internal/transport"
)

type SiteOrchestrator struct {
	db                  *sql.DB
	domainRepo          repositories.DomainRepository
	agentClient         *transport.ReconnectingClient
	basePath            string
	expectedBuildCommit string
}

func NewSiteOrchestrator(
	db *sql.DB,
	agentClient *transport.ReconnectingClient,
	expectedBuildCommit ...string,
) *SiteOrchestrator {
	commit := ""
	if len(expectedBuildCommit) > 0 {
		commit = strings.TrimSpace(expectedBuildCommit[0])
	}
	return &SiteOrchestrator{
		db:                  db,
		domainRepo:          repositories.NewPostgresDomainRepository(db),
		agentClient:         agentClient,
		basePath:            "/var/www/celikpanel",
		expectedBuildCommit: commit,
	}
}

type CreateSiteRequest struct {
	SubscriptionID int    `json:"subscription_id"`
	Domain         string `json:"domain"`
	ParentDomainID *int   `json:"-"`
	UseTemporary   bool   `json:"use_temporary"`
	// ProjectType selects what this domain does on this server: "php" or
	// "static" (a website — needs a web server) or "dnsonly" (no web hosting
	// at all: just the domain and its DNS zone — Plesk's "no web hosting").
	// The requirement follows the ROLE: a domain does not inherently need
	// PHP, nor even a web server. Empty means "php" (backward compatible).
	// ProjectType, bu domain'in bu sunucuda ne yapacağını seçer: "php" ya da
	// "static" (bir web sitesi — web sunucusu ister) veya "dnsonly" (hiç web
	// barındırma yok: yalnız domain + DNS zone'u — Plesk'in "no web
	// hosting"i). Gereksinim ROLÜ izler: bir domain doğası gereği PHP'ye,
	// hatta web sunucusuna bile muhtaç değildir. Boş, "php" demektir.
	ProjectType  string `json:"project_type"`
	PHPVersion   string `json:"php_version"`
	SSLType      string `json:"ssl_type"`      // letsencrypt, custom, none
	AccessMethod string `json:"access_method"` // ftp, sftp, both
}

type CreateSiteResponse struct {
	DomainID     int
	SiteID       int
	Domain       string
	TempDomain   string
	DocumentRoot string
	ProjectType  string
	FTPUser      string
	FTPPassword  string
	PHPVersion   string
}

// CreationProjectTypes are the types selectable at domain creation. node,
// proxy and forwarding exist as types but are configured on the domain's
// hosting page after creation (they need extra inputs: port, target URL).
// CreationProjectTypes, domain oluşturmada seçilebilen tiplerdir. node, proxy
// ve forwarding tip olarak vardır ama ek girdi istedikleri (port, hedef URL)
// için oluşturma sonrası domain'in barındırma sayfasından yapılandırılır.
var CreationProjectTypes = map[string]bool{"php": true, "static": true, "dnsonly": true}

// CreateSite orchestrates site creation via Agent RPC
func (so *SiteOrchestrator) CreateSite(ctx context.Context, req *CreateSiteRequest) (*CreateSiteResponse, error) {
	// Normalise the project type once; everything below branches on it.
	// Proje tipini bir kez normalize et; aşağıdaki her şey ona göre dallanır.
	if req.ProjectType == "" {
		req.ProjectType = "php"
	}
	if !CreationProjectTypes[req.ProjectType] {
		return nil, fmt.Errorf("unsupported project type %q (php, static, dnsonly)", req.ProjectType)
	}

	// 1. Create domain record
	domain := &core.Domain{
		SubscriptionID: req.SubscriptionID,
		Name:           req.Domain,
		ParentDomainID: req.ParentDomainID,
		Status:         "active",
	}
	err := so.domainRepo.Create(ctx, domain)
	if err != nil {
		return nil, fmt.Errorf("failed to create domain: %w", err)
	}
	req.Domain = domain.Name

	// DNS-only: the domain exists to be served by DNS (and later mail), not
	// hosted as a website. No system user, no docroot, no vhost, no FTP —
	// nothing to build on the OS, so the agent is never called. The DNS zone
	// itself is created by the caller (handleCreateDomain), same as web types.
	// Yalnız-DNS: domain, web sitesi olarak barındırılmak için değil, DNS'te
	// (ve ileride postada) sunulmak için var. Sistem kullanıcısı, docroot,
	// vhost, FTP yok — OS'ta kurulacak bir şey yok; agent hiç çağrılmaz. DNS
	// zone'unu, web tiplerinde olduğu gibi çağıran (handleCreateDomain) kurar.
	if req.ProjectType == "dnsonly" {
		site := &core.Site{
			DomainID:    domain.ID,
			ProjectType: "dnsonly",
			Status:      "active",
		}
		siteID, err := so.createSiteRecord(ctx, site)
		if err != nil {
			so.domainRepo.Delete(ctx, domain.ID)
			return nil, fmt.Errorf("failed to create site record: %v", err)
		}
		return &CreateSiteResponse{
			DomainID:    domain.ID,
			SiteID:      siteID,
			Domain:      req.Domain,
			ProjectType: "dnsonly",
		}, nil
	}

	// 2. Generate necessary data
	username := so.generateUsername(req.Domain)
	password := so.generatePassword()

	siteDir := filepath.Join(so.basePath, "subscriptions", fmt.Sprintf("%d", req.SubscriptionID), "sites", fmt.Sprintf("%d", domain.ID))
	documentRoot := filepath.Join(siteDir, "public_html")

	tempDomain := ""
	if req.UseTemporary {
		tempDomain = fmt.Sprintf("%s.celik.panel", strings.ReplaceAll(req.Domain, ".", "-"))
	}

	// 3. Create site record first (to get site ID)
	site := &core.Site{
		DomainID:     domain.ID,
		DocumentRoot: documentRoot,
		ProjectType:  req.ProjectType,
		PHPVersion:   req.PHPVersion,
		SSLEnabled:   req.SSLType != "none",
		Status:       "active",
	}

	siteID, err := so.createSiteRecord(ctx, site)
	if err != nil {
		so.domainRepo.Delete(ctx, domain.ID)
		return nil, fmt.Errorf("failed to create site record: %v", err)
	}
	site.ID = siteID

	// 4. Call Agent RPC to create site with sudo privileges
	agentReq := transport.CreateSiteRequest{
		ExpectedBuildCommit: so.expectedBuildCommit,
		SiteID:              siteID,
		SubscriptionID:      req.SubscriptionID,
		DomainID:            domain.ID,
		Domain:              req.Domain,
		TempDomain:          tempDomain,
		DocumentRoot:        documentRoot,
		ProjectType:         req.ProjectType,
		PHPVersion:          req.PHPVersion,
		SSLType:             req.SSLType,
		Username:            username,
		Password:            password,
	}

	var agentReply transport.CreateSiteResponse
	err = so.agentClient.Call("Agent.CreateSite", agentReq, &agentReply)
	if err != nil {
		// Rollback database records
		so.db.ExecContext(ctx, "DELETE FROM sites WHERE id = ?", siteID)
		so.domainRepo.Delete(ctx, domain.ID)
		return nil, fmt.Errorf("agent RPC failed: %v", err)
	}

	if !agentReply.Success {
		// Rollback database records
		so.db.ExecContext(ctx, "DELETE FROM sites WHERE id = ?", siteID)
		so.domainRepo.Delete(ctx, domain.ID)
		return nil, fmt.Errorf("site creation failed: %s", agentReply.ErrorMessage)
	}

	// 5. Update site record with PHP socket
	so.db.ExecContext(ctx, "UPDATE sites SET php_fpm_socket = ? WHERE id = ?", agentReply.PHPSocket, siteID)

	return &CreateSiteResponse{
		DomainID:     domain.ID,
		SiteID:       siteID,
		Domain:       req.Domain,
		TempDomain:   tempDomain,
		DocumentRoot: documentRoot,
		ProjectType:  req.ProjectType,
		FTPUser:      username,
		FTPPassword:  password,
		PHPVersion:   req.PHPVersion,
	}, nil
}

func (so *SiteOrchestrator) createSiteRecord(ctx context.Context, site *core.Site) (int, error) {
	if site.ProjectType == "" {
		site.ProjectType = "php"
	}
	query := `
		INSERT INTO sites (domain_id, document_root, project_type, php_version, php_fpm_socket, ssl_enabled, status)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`
	result, err := so.db.ExecContext(ctx, query,
		site.DomainID, site.DocumentRoot, site.ProjectType, site.PHPVersion, site.PHPFPMSocket, site.SSLEnabled, site.Status)
	if err != nil {
		return 0, err
	}

	id, err := result.LastInsertId()
	return int(id), err
}

func (so *SiteOrchestrator) generateUsername(domain string) string {
	return SiteUsername(domain)
}

// SiteUsername derives the deterministic system username for a domain
// (example.com -> example_com). Exported because deletion recomputes it —
// the sites table stores no username column.
// SiteUsername, bir domain'in belirlenimci sistem kullanıcı adını türetir
// (example.com -> example_com). Dışa açık; çünkü silme yeniden hesaplar —
// sites tablosunda kullanıcı adı sütunu yoktur.
func SiteUsername(domain string) string {
	username := strings.ReplaceAll(domain, ".", "_")
	username = strings.ReplaceAll(username, "-", "_")
	if len(username) > 32 {
		username = username[:32]
	}
	return username
}

func (so *SiteOrchestrator) generatePassword() string {
	// Credentials must come from a CSPRNG; math/rand seeded with the clock
	// is predictable. rand.Int rejection-samples, so no charset bias.
	// Kimlik bilgileri CSPRNG'den gelmelidir; saatle tohumlanan math/rand
	// tahmin edilebilirdir. rand.Int reddederek örnekler, bu yüzden
	// karakter seti önyargısı oluşmaz.
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%"
	password := make([]byte, 20)
	max := big.NewInt(int64(len(charset)))
	for i := range password {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			// crypto/rand failing means the OS entropy source is broken;
			// weak credentials are worse than stopping.
			// crypto/rand'ın çalışmaması, işletim sistemi entropi
			// kaynağının bozulması demektir; zayıf kimlik bilgisi
			// üretmek durmaktan daha kötüdür.
			panic(fmt.Sprintf("crypto/rand unavailable: %v", err))
		}
		password[i] = charset[n.Int64()]
	}
	return string(password)
}
