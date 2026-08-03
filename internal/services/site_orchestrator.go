package services

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"strings"
	"time"

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

const (
	siteAgentCompensationTimeout = 2 * time.Minute
	siteDBCompensationTimeout    = 30 * time.Second
)

type deleteCreatedSiteRequest struct {
	ExpectedBuildCommit string
	SiteID              int
	SubscriptionID      int
	DomainID            int
	Domain              string
	Username            string
	PHPVersion          string
	SiteHome            string
}

type deleteCreatedSiteResponse struct {
	Success bool
	Error   string
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
	// InitialStatus is an internal fail-closed creation control. It is not
	// accepted from the public HTTP request; an empty value preserves the
	// normal active-site behaviour.
	InitialStatus string `json:"-"`
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
	initialStatus := strings.TrimSpace(req.InitialStatus)
	if initialStatus == "" {
		initialStatus = "active"
	}
	if initialStatus != "active" && initialStatus != "pending" {
		return nil, fmt.Errorf("unsupported initial site status %q", initialStatus)
	}

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
		Status:         initialStatus,
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
			Status:      initialStatus,
		}
		siteID, err := so.createSiteRecord(ctx, site)
		if err != nil {
			cause := fmt.Errorf("failed to create site record: %w", err)
			return nil, errors.Join(cause, so.rollbackCreatedSiteMetadata(domain.ID, 0))
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
		Status:       initialStatus,
	}

	siteID, err := so.createSiteRecord(ctx, site)
	if err != nil {
		cause := fmt.Errorf("failed to create site record: %w", err)
		return nil, errors.Join(cause, so.rollbackCreatedSiteMetadata(domain.ID, 0))
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
	err = so.agentClient.CallContext(ctx, "Agent.CreateSite", agentReq, &agentReply)
	if err != nil {
		cause := fmt.Errorf("agent RPC failed: %w", err)
		return nil, errors.Join(cause, so.rollbackCreatedSite(agentReq, domain.ID, siteID))
	}

	if !agentReply.Success {
		cause := fmt.Errorf("site creation failed: %s", agentReply.ErrorMessage)
		return nil, errors.Join(cause, so.rollbackCreatedSite(agentReq, domain.ID, siteID))
	}

	// 5. Update site record with PHP socket
	result, err := so.db.ExecContext(ctx, "UPDATE sites SET php_fpm_socket = ? WHERE id = ?", agentReply.PHPSocket, siteID)
	if err == nil {
		err = requireSingleSiteMutation(result, "record PHP-FPM socket")
	}
	if err != nil {
		cause := fmt.Errorf("failed to record PHP-FPM socket: %w", err)
		return nil, errors.Join(cause, so.rollbackCreatedSite(agentReq, domain.ID, siteID))
	}

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

// rollbackCreatedSite tears down privileged state before removing its durable
// metadata. If the agent cannot confirm physical cleanup, the records stay in
// SQLite so an operator can see and retry the incomplete site instead of
// losing the only recovery identity for orphaned files, users and vhosts.
func (so *SiteOrchestrator) rollbackCreatedSite(
	created transport.CreateSiteRequest,
	domainID, siteID int,
) error {
	if err := so.rollbackCreatedAgentSite(created); err != nil {
		return errors.Join(
			err,
			fmt.Errorf(
				"rollback site metadata: retained domain %d and site %d because agent cleanup was not confirmed",
				domainID,
				siteID,
			),
		)
	}
	return so.rollbackCreatedSiteMetadata(domainID, siteID)
}

func (so *SiteOrchestrator) rollbackCreatedAgentSite(created transport.CreateSiteRequest) error {
	ctx, cancel := context.WithTimeout(context.Background(), siteAgentCompensationTimeout)
	defer cancel()

	req := deleteCreatedSiteRequest{
		ExpectedBuildCommit: created.ExpectedBuildCommit,
		SiteID:              created.SiteID,
		SubscriptionID:      created.SubscriptionID,
		DomainID:            created.DomainID,
		Domain:              created.Domain,
		Username:            created.Username,
		PHPVersion:          created.PHPVersion,
		SiteHome:            filepath.Dir(created.DocumentRoot),
	}
	var reply deleteCreatedSiteResponse
	if err := so.agentClient.CallContext(ctx, "Agent.DeleteSite", &req, &reply); err != nil {
		return fmt.Errorf("rollback agent site: %w", err)
	}
	if !reply.Success {
		message := strings.TrimSpace(reply.Error)
		if message == "" {
			message = "agent did not confirm site deletion"
		}
		return fmt.Errorf("rollback agent site: %s", message)
	}
	return nil
}

func (so *SiteOrchestrator) rollbackCreatedSiteMetadata(domainID, siteID int) error {
	ctx, cancel := context.WithTimeout(context.Background(), siteDBCompensationTimeout)
	defer cancel()

	tx, err := so.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("rollback site metadata: begin transaction: %w", err)
	}
	fail := func(cause error) error {
		rollbackErr := tx.Rollback()
		if errors.Is(rollbackErr, sql.ErrTxDone) {
			rollbackErr = nil
		}
		if rollbackErr != nil {
			rollbackErr = fmt.Errorf("rollback metadata transaction: %w", rollbackErr)
		}
		return errors.Join(cause, rollbackErr)
	}

	if siteID > 0 {
		result, err := tx.ExecContext(ctx, "DELETE FROM sites WHERE id = ?", siteID)
		if err != nil {
			return fail(fmt.Errorf("rollback site metadata: delete site %d: %w", siteID, err))
		}
		if err := requireSingleSiteMutation(result, "rollback site metadata"); err != nil {
			return fail(err)
		}
	}

	result, err := tx.ExecContext(ctx, "DELETE FROM domains WHERE id = ?", domainID)
	if err != nil {
		return fail(fmt.Errorf("rollback site metadata: delete domain %d: %w", domainID, err))
	}
	if err := requireSingleSiteMutation(result, "rollback domain metadata"); err != nil {
		return fail(err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("rollback site metadata: commit: %w", err)
	}
	return nil
}

func requireSingleSiteMutation(result sql.Result, operation string) error {
	if result == nil {
		return fmt.Errorf("%s: database returned no result", operation)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: read affected rows: %w", operation, err)
	}
	if rows != 1 {
		return fmt.Errorf("%s: affected %d rows, want exactly 1", operation, rows)
	}
	return nil
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
