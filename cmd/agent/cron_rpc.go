package main

import (
	"errors"
	"fmt"
	"os/exec"
	"os/user"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

// CronJob represents a cron job entry
type CronJob = transport.CronJob

// ListCronJobsRequest for listing cron jobs
type ListCronJobsRequest = transport.ListCronJobsRequest

// ListCronJobsResponse contains cron job list
type ListCronJobsResponse = transport.ListCronJobsResponse

// AddCronJobRequest for adding a new cron job
type AddCronJobRequest = transport.AddCronJobRequest

// UpdateCronJobRequest for updating an existing cron job
type UpdateCronJobRequest = transport.UpdateCronJobRequest

// DeleteCronJobRequest for deleting a cron job
type DeleteCronJobRequest = transport.DeleteCronJobRequest

// ListCronJobs lists all cron jobs for a user
func (a *Agent) ListCronJobs(req *ListCronJobsRequest, resp *ListCronJobsResponse) error {
	// Reads are proved too: `crontab -u root -l` is a disclosure, not a
	// mutation, and an unproved name here would leak any account's schedule.
	// Okumalar da kanıtlanır: `crontab -u root -l` bir değişiklik değil bir
	// ifşadır ve burada kanıtlanmamış bir ad, herhangi bir hesabın zamanlamasını
	// sızdırır.
	username, err := cronTenantUser(req.CronTenant)
	if err != nil {
		return err
	}

	// Read crontab for user
	cmd := exec.Command("crontab", "-u", username, "-l")
	output, err := cmd.Output()
	if err != nil {
		// No crontab for user is not an error
		if strings.Contains(err.Error(), "no crontab") {
			resp.Jobs = []CronJob{}
			return nil
		}
		resp.Jobs = []CronJob{}
		return nil
	}

	resp.Jobs = parseCrontab(string(output))
	return nil
}

// AddCronJob adds a new cron job
func (a *Agent) AddCronJob(req *AddCronJobRequest, resp *bool) error {
	username, err := cronTenantUser(req.CronTenant)
	if err != nil {
		return err
	}
	if err := rejectCrontabInjection(map[string]string{
		"schedule": req.Schedule,
		"command":  req.Command,
		"comment":  req.Comment,
	}); err != nil {
		return err
	}

	// Validate schedule
	if !isValidCronSchedule(req.Schedule) {
		return fmt.Errorf("invalid cron schedule: %s", req.Schedule)
	}

	// Get existing crontab
	existing := getCrontab(username)

	// Build new entry
	var newEntry string
	if req.Comment != "" {
		newEntry = fmt.Sprintf("# %s\n%s %s", req.Comment, req.Schedule, req.Command)
	} else {
		newEntry = fmt.Sprintf("%s %s", req.Schedule, req.Command)
	}

	// Append new entry
	newCrontab := existing
	if newCrontab != "" && !strings.HasSuffix(newCrontab, "\n") {
		newCrontab += "\n"
	}
	newCrontab += newEntry + "\n"

	// Write new crontab
	if err := setCrontab(username, newCrontab); err != nil {
		return err
	}

	*resp = true
	return nil
}

// UpdateCronJob updates an existing cron job
func (a *Agent) UpdateCronJob(req *UpdateCronJobRequest, resp *bool) error {
	username, err := cronTenantUser(req.CronTenant)
	if err != nil {
		return err
	}
	if err := rejectCrontabInjection(map[string]string{
		"schedule": req.Schedule,
		"command":  req.Command,
		"comment":  req.Comment,
	}); err != nil {
		return err
	}

	// Validate schedule
	if !isValidCronSchedule(req.Schedule) {
		return fmt.Errorf("invalid cron schedule: %s", req.Schedule)
	}

	// Get existing crontab
	existing := getCrontab(username)
	lines := strings.Split(existing, "\n")

	// Find and update the job
	var newLines []string
	found := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			newLines = append(newLines, line)
			continue
		}

		// Generate ID for this line
		lineID := generateCronID(trimmed)
		if lineID == req.ID {
			found = true
			// Replace with updated job
			var newLine string
			if !req.Enabled {
				newLine = "# DISABLED: " + req.Schedule + " " + req.Command
			} else {
				newLine = req.Schedule + " " + req.Command
			}

			// Add comment if provided
			if req.Comment != "" && (i == 0 || !strings.HasPrefix(strings.TrimSpace(lines[i-1]), "#")) {
				newLines = append(newLines, "# "+req.Comment)
			}
			newLines = append(newLines, newLine)
		} else {
			newLines = append(newLines, line)
		}
	}

	if !found {
		return fmt.Errorf("cron job not found: %s", req.ID)
	}

	// Write new crontab
	if err := setCrontab(username, strings.Join(newLines, "\n")); err != nil {
		return err
	}

	*resp = true
	return nil
}

// DeleteCronJob deletes a cron job
func (a *Agent) DeleteCronJob(req *DeleteCronJobRequest, resp *bool) error {
	username, err := cronTenantUser(req.CronTenant)
	if err != nil {
		return err
	}

	// Get existing crontab
	existing := getCrontab(username)
	lines := strings.Split(existing, "\n")

	// Filter out the job to delete
	var newLines []string
	found := false
	skipNextComment := false

	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			continue
		}

		// Check if this is the job to delete
		if !strings.HasPrefix(trimmed, "#") {
			lineID := generateCronID(trimmed)
			if lineID == req.ID {
				found = true
				skipNextComment = true
				continue
			}
		}

		// Skip comment line before deleted job
		if skipNextComment && strings.HasPrefix(trimmed, "#") {
			skipNextComment = false
			continue
		}

		skipNextComment = false
		newLines = append([]string{line}, newLines...)
	}

	if !found {
		return fmt.Errorf("cron job not found: %s", req.ID)
	}

	// Write new crontab
	if err := setCrontab(username, strings.Join(newLines, "\n")); err != nil {
		return err
	}

	*resp = true
	return nil
}

// Helper functions

func parseCrontab(content string) []CronJob {
	var jobs []CronJob
	lines := strings.Split(content, "\n")
	var currentComment string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Check for comment
		if strings.HasPrefix(trimmed, "#") {
			// Check if it's a disabled job
			if strings.HasPrefix(trimmed, "# DISABLED:") {
				// Parse disabled job
				disabled := strings.TrimPrefix(trimmed, "# DISABLED:")
				parts := parseCronLine(strings.TrimSpace(disabled))
				if parts != nil {
					jobs = append(jobs, CronJob{
						ID:       generateCronID(strings.TrimSpace(disabled)),
						Schedule: parts[0],
						Command:  parts[1],
						Enabled:  false,
						Comment:  currentComment,
					})
				}
				currentComment = ""
			} else {
				currentComment = strings.TrimPrefix(trimmed, "# ")
			}
			continue
		}

		// Parse active job
		parts := parseCronLine(trimmed)
		if parts != nil {
			jobs = append(jobs, CronJob{
				ID:       generateCronID(trimmed),
				Schedule: parts[0],
				Command:  parts[1],
				Enabled:  true,
				Comment:  currentComment,
			})
		}
		currentComment = ""
	}

	return jobs
}

func parseCronLine(line string) []string {
	// Cron format: min hour dom month dow command
	// Need to extract first 5 fields and the rest is command
	fields := strings.Fields(line)
	if len(fields) < 6 {
		return nil
	}

	schedule := strings.Join(fields[:5], " ")
	command := strings.Join(fields[5:], " ")

	return []string{schedule, command}
}

func generateCronID(line string) string {
	// Simple content hash over bytes; iterating bytes keeps the uint32
	// conversion in range (a rune could exceed it in theory).
	// Baytlar üzerinden basit içerik özeti; baytları dolaşmak uint32
	// dönüşümünü aralıkta tutar (bir rune teoride bunu aşabilir).
	var hash uint32
	for _, c := range []byte(line) {
		hash = hash*31 + uint32(c)
	}
	return fmt.Sprintf("%08x", hash)
}

var cronUsernameRe = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// cronTenantUser proves WHOSE crontab the agent is about to open before it runs
// `crontab -u <name>` as root, and returns the only name it will accept.
//
// Two proofs, because one is not enough:
//
//  1. The username is re-derived here from the domain rather than taken from the
//     request. The panel is a courier; the agent re-derives every fact it acts
//     on. This is the same rule site creation already applies at
//     site_rpc.go:198.
//
//  2. The account's home directory must equal the home derived from the
//     subscription and domain identities. This is the proof that actually binds
//     the operation to a tenant, because SiteUsername is NOT injective: it maps
//     "." and "-" to "_" and truncates at 32, so "my-shop.com" and "my.shop.com"
//     collapse to one account name. Without the home check, owning either domain
//     would reach the other's jobs — the panel's ownership guard would pass, and
//     the crontab opened would belong to someone else. Homes come from integer
//     identities and cannot collide.
//
// The account must also be a real tenant account: a uid below 1000 is a system
// account, and root is the one this whole check exists to keep out.
//
// cronTenantUser, agent `crontab -u <ad>` komutunu root olarak çalıştırmadan
// önce KİMİN crontab'ını açacağını kanıtlar ve kabul edeceği tek adı döndürür.
//
// İki kanıt, çünkü biri yetmez:
//
//  1. Kullanıcı adı istekten alınmaz, burada alan adından yeniden türetilir.
//     Panel bir kuryedir; agent eylediği her olguyu yeniden türetir. Site
//     oluşturmanın site_rpc.go:198'de zaten uyguladığı kuralın aynısı.
//
//  2. Hesabın ev dizini, abonelik ve alan adı kimliklerinden türetilen ev
//     dizinine eşit olmalıdır. İşlemi bir kiracıya gerçekten bağlayan kanıt
//     budur, çünkü SiteUsername TEK YÖNLÜ DEĞİLDİR: "." ve "-" karakterlerini
//     "_" yapar ve 32'de keser; "my-shop.com" ile "my.shop.com" tek bir hesap
//     adına iner. Ev dizini denetimi olmadan, ikisinden birine sahip olmak
//     diğerinin görevlerine ulaşırdı — panelin sahiplik koruması geçerdi ve
//     açılan crontab başkasına ait olurdu. Ev dizinleri tam sayı kimliklerden
//     gelir ve çakışamaz.
func cronTenantUser(tenant transport.CronTenant) (string, error) {
	expectedHome, err := hostingpath.SiteHome(tenant.SubscriptionID, tenant.DomainID)
	if err != nil {
		return "", fmt.Errorf("refusing cron request without a tenant identity: %w", err)
	}
	domain := strings.TrimSpace(tenant.Domain)
	if domain == "" {
		return "", errors.New("refusing cron request without a domain")
	}
	username := services.SiteUsername(domain)
	if !cronUsernameRe.MatchString(username) {
		return "", fmt.Errorf("refusing cron user %q: not a site account name", username)
	}
	account, err := user.Lookup(username)
	if err != nil {
		return "", fmt.Errorf("look up cron user %q: %w", username, err)
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil || uid < 1000 {
		return "", fmt.Errorf("refusing cron user %q: not a tenant uid", username)
	}
	if path.Clean(account.HomeDir) != expectedHome {
		// The account exists and is a tenant account — but not THIS tenant's.
		// This is the collision case, and it is a cross-tenant boundary, so the
		// refusal names no other tenant's identity.
		// Hesap var ve bir kiracı hesabı — ama BU kiracının değil. Çakışma
		// durumu budur ve bir kiracılar arası sınırdır; bu yüzden ret, başka bir
		// kiracının kimliğini adlandırmaz.
		return "", fmt.Errorf(
			"refusing cron user %q: it does not belong to this domain", username,
		)
	}
	return username, nil
}

// rejectCrontabInjection refuses text that would not stay on the line it is
// written to. Schedule, command and comment are formatted into a crontab with
// Sprintf, so a newline in any of them appends attacker-chosen crontab lines —
// a second schedule, running as that user, that nothing in the panel displays.
// A carriage return and a NUL are refused for the same reason.
//
// rejectCrontabInjection, yazıldığı satırda kalmayacak metni reddeder.
// Zamanlama, komut ve yorum crontab'a Sprintf ile yazıldığı için herhangi
// birindeki satır sonu, saldırganın seçtiği crontab satırlarını ekler — o
// kullanıcı olarak çalışan ve panelin hiçbir yerde göstermediği ikinci bir
// zamanlama. Satır başı ve NUL aynı sebeple reddedilir.
func rejectCrontabInjection(fields map[string]string) error {
	for name, value := range fields {
		if strings.ContainsAny(value, "\n\r\x00") {
			return fmt.Errorf("cron %s must not contain line breaks", name)
		}
	}
	return nil
}

func isValidCronSchedule(schedule string) bool {
	fields := strings.Fields(schedule)
	if len(fields) != 5 {
		return false
	}

	// Basic validation - each field should be valid cron expression
	cronFieldPattern := regexp.MustCompile(`^(\*|[0-9]+(-[0-9]+)?(,[0-9]+(-[0-9]+)?)*|(\*/[0-9]+))$`)
	for _, field := range fields {
		if !cronFieldPattern.MatchString(field) {
			return false
		}
	}

	return true
}

func getCrontab(username string) string {
	cmd := exec.Command("crontab", "-u", username, "-l")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(output)
}

func setCrontab(username, content string) error {
	// Report a missing cron package honestly instead of a bare "operation
	// failed" — scheduled tasks need the cron service installed first.
	// Eksik cron paketini "operation failed" yerine dürüstçe bildir —
	// zamanlanmış görevler önce cron servisinin kurulmasını ister.
	if _, err := exec.LookPath("crontab"); err != nil {
		return fmt.Errorf("cron is not installed on this server")
	}
	cmd := exec.Command("crontab", "-u", username, "-")
	cmd.Stdin = strings.NewReader(content)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("crontab: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
