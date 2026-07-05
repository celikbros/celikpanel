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
	db          *sql.DB
	domainRepo  repositories.DomainRepository
	agentClient *transport.ReconnectingClient
	basePath    string
}

func NewSiteOrchestrator(db *sql.DB, agentClient *transport.ReconnectingClient) *SiteOrchestrator {
	return &SiteOrchestrator{
		db:          db,
		domainRepo:  repositories.NewPostgresDomainRepository(db),
		agentClient: agentClient,
		basePath:    "/var/www/celikpanel",
	}
}

type CreateSiteRequest struct {
	SubscriptionID  int
	Domain          string
	UseTemporary    bool
	PHPVersion      string
	SSLType         string // letsencrypt, custom, none
	AccessMethod    string // ftp, sftp, both
}

type CreateSiteResponse struct {
	DomainID       int
	SiteID         int
	Domain         string
	TempDomain     string
	DocumentRoot   string
	FTPUser        string
	FTPPassword    string
	PHPVersion     string
}

// CreateSite orchestrates site creation via Agent RPC
func (so *SiteOrchestrator) CreateSite(ctx context.Context, req *CreateSiteRequest) (*CreateSiteResponse, error) {
	// 1. Create domain record
	domain := &core.Domain{
		SubscriptionID: req.SubscriptionID,
		Name:           req.Domain,
		Status:         "active",
	}
	err := so.domainRepo.Create(ctx, domain)
	if err != nil {
		return nil, fmt.Errorf("failed to create domain: %v", err)
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
		SiteID:         siteID,
		SubscriptionID: req.SubscriptionID,
		Domain:         req.Domain,
		TempDomain:     tempDomain,
		DocumentRoot:   documentRoot,
		PHPVersion:     req.PHPVersion,
		SSLType:        req.SSLType,
		Username:       username,
		Password:       password,
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
		FTPUser:      username,
		FTPPassword:  password,
		PHPVersion:   req.PHPVersion,
	}, nil
}

func (so *SiteOrchestrator) createSiteRecord(ctx context.Context, site *core.Site) (int, error) {
	query := `
		INSERT INTO sites (domain_id, document_root, php_version, php_fpm_socket, ssl_enabled, status)
		VALUES (?, ?, ?, ?, ?, ?)
	`
	result, err := so.db.ExecContext(ctx, query,
		site.DomainID, site.DocumentRoot, site.PHPVersion, site.PHPFPMSocket, site.SSLEnabled, site.Status)
	if err != nil {
		return 0, err
	}
	
	id, err := result.LastInsertId()
	return int(id), err
}

func (so *SiteOrchestrator) generateUsername(domain string) string {
	// Convert domain to valid username: example.com -> example_com
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
