package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/alicelik/celikpanel/internal/hostcmd"
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
	if err := requireCronInstalled(); err != nil {
		return err
	}
	return listCronJobsFor(username, resp)
}

// listCronJobsFor reads the proven user's crontab into resp.
// listCronJobsFor, kanıtlanmış kullanıcının crontab'ını resp'e okur.
func listCronJobsFor(username string, resp *ListCronJobsResponse) error {
	// A crontab that cannot be read is an unknown state, not an empty list:
	// answering "no jobs" here is what let the next added job replace the
	// owner's whole crontab.
	// Okunamayan crontab boş liste değil bilinmeyen durumdur.
	content, err := readCrontab(username)
	if err != nil {
		return err
	}
	resp.Jobs = parseCrontab(content)
	if resp.Jobs == nil {
		resp.Jobs = []CronJob{}
	}
	resp.Version = cronVersion(content)
	return nil
}

// AddCronJob adds a new cron job
func (a *Agent) AddCronJob(req *AddCronJobRequest, resp *bool) error {
	username, err := cronTenantUser(req.CronTenant)
	if err != nil {
		return err
	}
	if err := requireCronInstalled(); err != nil {
		return err
	}
	if err := addCronJobFor(username, req); err != nil {
		return err
	}
	*resp = true
	return nil
}

// addCronJobFor appends one job to the proven user's crontab.
// addCronJobFor, kanıtlanmış kullanıcının crontab'ına bir görev ekler.
func addCronJobFor(username string, req *AddCronJobRequest) error {
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

	cronMu.Lock()
	defer cronMu.Unlock()

	// The pre-image: never append to a crontab that was not read.
	// Ön görüntü: okunmamış bir crontab'a asla ekleme yapılmaz.
	existing, err := readCrontabForChange(username, req.Version)
	if err != nil {
		return err
	}

	// The same schedule and command twice is refused: it runs the command
	// twice, and the two lines share one ID, so a later change or delete could
	// not tell them apart.
	// Aynı zamanlama ve komut ikinci kez eklenmez: komutu iki kez çalıştırır ve
	// iki satır tek kimliği paylaşır.
	wanted := strings.Join(strings.Fields(req.Schedule+" "+req.Command), " ")
	for _, job := range parseCrontab(existing) {
		if job.Schedule+" "+job.Command == wanted {
			return errors.New(transport.CronJobDuplicate)
		}
	}

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

	return setCrontab(username, newCrontab)
}

// UpdateCronJob updates an existing cron job
func (a *Agent) UpdateCronJob(req *UpdateCronJobRequest, resp *bool) error {
	username, err := cronTenantUser(req.CronTenant)
	if err != nil {
		return err
	}
	if err := requireCronInstalled(); err != nil {
		return err
	}
	if err := updateCronJobFor(username, req); err != nil {
		return err
	}
	*resp = true
	return nil
}

// updateCronJobFor rewrites one job in the proven user's crontab.
// updateCronJobFor, kanıtlanmış kullanıcının crontab'ında bir görevi yeniden yazar.
func updateCronJobFor(username string, req *UpdateCronJobRequest) error {
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

	cronMu.Lock()
	defer cronMu.Unlock()

	existing, err := readCrontabForChange(username, req.Version)
	if err != nil {
		return err
	}
	lines := strings.Split(existing, "\n")

	// The job is the one line whose own text is this ID, enabled or disabled.
	// Nothing else in the crontab is rewritten.
	// Görev, kendi metni bu kimlik olan tek satırdır; etkin ya da devre dışı.
	index, err := cronJobLine(lines, req.ID)
	if err != nil {
		return err
	}
	wanted := strings.Join(strings.Fields(req.Schedule+" "+req.Command), " ")
	for i, line := range lines {
		if text, _, ok := cronJobText(line); ok && i != index && strings.Join(strings.Fields(text), " ") == wanted {
			// Changing this job into a copy of another would leave two lines
			// nothing could tell apart afterwards.
			// Bu görevi bir başkasının kopyasına çevirmek, sonradan ayırt
			// edilemeyen iki satır bırakır.
			return errors.New(transport.CronJobDuplicate)
		}
	}
	newLine := req.Schedule + " " + req.Command
	if !req.Enabled {
		newLine = "# DISABLED: " + newLine
	}

	// A description is written above the job only where no comment stands
	// there already. A comment that is there may be the owner's and is left as
	// it is; a disabled job above is a job, not a comment.
	// Açıklama, görevin üstüne yalnız orada bir yorum yoksa yazılır.
	describe := req.Comment != "" && !cronPlainCommentAt(lines, index-1)
	if lines[index] == newLine && !describe {
		return nil
	}
	lines[index] = newLine
	if describe {
		lines = append(lines[:index], append([]string{"# " + req.Comment}, lines[index:]...)...)
	}

	return setCrontab(username, strings.Join(lines, "\n"))
}

// DeleteCronJob deletes a cron job
func (a *Agent) DeleteCronJob(req *DeleteCronJobRequest, resp *bool) error {
	username, err := cronTenantUser(req.CronTenant)
	if err != nil {
		return err
	}
	if err := requireCronInstalled(); err != nil {
		return err
	}
	if err := deleteCronJobFor(username, req); err != nil {
		return err
	}
	*resp = true
	return nil
}

// deleteCronJobFor removes one job from the proven user's crontab.
// deleteCronJobFor, kanıtlanmış kullanıcının crontab'ından bir görevi çıkarır.
func deleteCronJobFor(username string, req *DeleteCronJobRequest) error {
	cronMu.Lock()
	defer cronMu.Unlock()

	existing, err := readCrontabForChange(username, req.Version)
	if err != nil {
		return err
	}
	lines := strings.Split(existing, "\n")

	// Exactly the job's own line goes, enabled or disabled. The line above it
	// stays whatever it is: it may be the owner's comment or another, disabled
	// job, and the Panel cannot prove it wrote it. Before 9 Oct 2026 the line
	// above was removed whenever it began with `#`, and every blank line of
	// the crontab went with it.
	// Yalnız görevin kendi satırı gider. Üstündeki satır ne ise kalır: sahibin
	// yorumu ya da başka, devre dışı bir görev olabilir.
	index, err := cronJobLine(lines, req.ID)
	if err != nil {
		return err
	}
	lines = append(lines[:index], lines[index+1:]...)

	return setCrontab(username, strings.Join(lines, "\n"))
}

// cronJobText returns the job a crontab line holds: the line itself, or for a
// job the Panel disabled the text after `# DISABLED:`. A blank line, an
// environment assignment and an ordinary comment are not jobs.
// cronJobText, bir crontab satırının tuttuğu görevi döndürür: satırın kendisi
// ya da Panel'in devre dışı bıraktığı görevde `# DISABLED:` sonrasındaki metin.
func cronJobText(line string) (text string, disabled, ok bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "", false, false
	}
	if strings.HasPrefix(trimmed, "#") {
		rest, isDisabled := strings.CutPrefix(trimmed, "# DISABLED:")
		if !isDisabled {
			return "", false, false
		}
		trimmed, disabled = strings.TrimSpace(rest), true
	}
	if parseCronLine(trimmed) == nil {
		return "", false, false
	}
	return trimmed, disabled, true
}

// cronJobLine finds the one line that is the job with this ID. A job is
// identified by its whole text, never by where it stands. Two lines with the
// same text are the same job twice; which of them a request means cannot be
// known, so nothing is changed.
// cronJobLine, bu kimlikteki görev olan tek satırı bulur. Görev konumuyla
// değil bütün metniyle tanınır. Aynı metinli iki satır aynı görevin iki
// kopyasıdır; isteğin hangisini kastettiği bilinemez, hiçbir şey değiştirilmez.
func cronJobLine(lines []string, id string) (int, error) {
	found := -1
	for i, line := range lines {
		text, _, ok := cronJobText(line)
		if !ok || generateCronID(text) != id {
			continue
		}
		if found >= 0 {
			return -1, errors.New(transport.CronJobAmbiguous)
		}
		found = i
	}
	if found < 0 {
		return -1, fmt.Errorf("cron job not found: %s", id)
	}
	return found, nil
}

// cronPlainCommentAt reports whether the line is an ordinary comment: not a
// blank line, not a job and not a job the Panel disabled.
// cronPlainCommentAt, satırın sıradan bir yorum olup olmadığını bildirir.
func cronPlainCommentAt(lines []string, index int) bool {
	if index < 0 || index >= len(lines) {
		return false
	}
	trimmed := strings.TrimSpace(lines[index])
	if !strings.HasPrefix(trimmed, "#") {
		return false
	}
	_, _, job := cronJobText(lines[index])
	return !job
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
			// A job the Panel disabled is listed with the same ID it has when
			// enabled, so it can be enabled, changed and deleted again.
			// Panel'in devre dışı bıraktığı görev, etkinken taşıdığı kimlikle
			// listelenir.
			if text, _, ok := cronJobText(line); ok {
				parts := parseCronLine(text)
				jobs = append(jobs, CronJob{
					ID:       generateCronID(text),
					Schedule: parts[0],
					Command:  parts[1],
					Enabled:  false,
					Comment:  currentComment,
				})
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
	// The whole text of the job, hashed so that two different jobs cannot
	// share an ID. The earlier 32-bit sum (the Java string hash) gave the same
	// ID to commands as close as "…/Aa.sh" and "…/BB.sh", and a change or
	// delete then reached the first of them.
	// Görevin bütün metni; iki farklı görev aynı kimliği paylaşamasın diye
	// özetlenir. Önceki 32 bitlik toplam, "…/Aa.sh" ile "…/BB.sh" kadar yakın
	// komutlara aynı kimliği veriyordu.
	sum := sha256.Sum256([]byte(line))
	return hex.EncodeToString(sum[:8])
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

// cronMu makes read, compare and write one step for the Panel's own requests,
// so two changes built from the same list cannot both be written.
// cronMu, Panel'in kendi istekleri için oku-karşılaştır-yaz adımını tek adım
// yapar.
var cronMu sync.Mutex

var (
	errCronStateUnreadable = errors.New(transport.CronStateUnreadable)
	errCronVersionRequired = errors.New(transport.CronVersionRequired)
	errCronStateChanged    = errors.New(transport.CronStateChanged)
)

// cronListCrontab runs `crontab -u <user> -l` and returns what it printed, what
// it said on standard error and its exit status (-1 when it did not exit).
// Swapped by tests.
// cronListCrontab, `crontab -u <kullanıcı> -l` çalıştırır. Testlerde değiştirilir.
var cronListCrontab = func(username string) (output []byte, stderr string, exitCode int, err error) {
	cmd := exec.Command("crontab", "-u", username, "-l")
	// The one answer that is read as text below must not arrive translated.
	// Aşağıda metin olarak okunan tek yanıt çevrilmiş gelmemelidir.
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANGUAGE=C")
	output, err = cmd.Output()
	if err == nil {
		return output, "", 0, nil
	}
	exitCode = -1
	var exited *exec.ExitError
	if errors.As(err, &exited) {
		exitCode = exited.ExitCode()
	}
	return output, hostcmd.Stderr(err), exitCode, err
}

// readCrontab returns the user's crontab. "This user has no crontab" is the one
// legitimate empty answer, and it is recognised narrowly: exit status 1, no
// output, and exactly `no crontab for <user>` on standard error — the line
// Debian/Ubuntu cron and cronie both print. Every other failure (the spool
// cannot be opened, the user is refused, the command was killed, an
// implementation that words it differently) leaves the state unknown, and an
// unknown crontab is never listed as empty and never written over.
//
// readCrontab kullanıcının crontab'ını döndürür. "Bu kullanıcının crontab'ı
// yok" tek meşru boş yanıttır ve dar tanınır: çıkış durumu 1, çıktı yok ve
// standart hatada tam olarak `no crontab for <kullanıcı>`. Diğer her hata
// durumu bilinmez bırakır; bilinmeyen crontab boş diye listelenmez ve üstüne
// yazılmaz.
func readCrontab(username string) (string, error) {
	output, stderr, exitCode, err := cronListCrontab(username)
	if err == nil {
		return string(output), nil
	}
	said := strings.TrimSpace(stderr)
	if exitCode == 1 && strings.TrimSpace(string(output)) == "" && said == "no crontab for "+username {
		return "", nil
	}
	log.Printf("cron: the crontab of %s could not be read (exit status %d): %s",
		username, exitCode, hostcmd.Bounded(strings.Join(strings.Fields(said), " "), 300))
	return "", errCronStateUnreadable
}

// cronVersion identifies the exact crontab bytes a list was read from.
// cronVersion, bir listenin okunduğu crontab baytlarını tanımlar.
func cronVersion(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "ct1-" + hex.EncodeToString(sum[:])
}

// readCrontabForChange is the pre-image of every cron write. It refuses when
// the crontab cannot be read, when the request does not say which crontab it
// was built from, and when that is no longer the crontab on the server.
// readCrontabForChange her cron yazısının ön görüntüsüdür. Crontab okunamazsa,
// istek hangi crontab'dan kurulduğunu söylemezse ya da o artık sunucudaki
// crontab değilse reddeder.
func readCrontabForChange(username, version string) (string, error) {
	existing, err := readCrontab(username)
	if err != nil {
		return "", err
	}
	if version == "" {
		return "", errCronVersionRequired
	}
	if version != cronVersion(existing) {
		return "", errCronStateChanged
	}
	return existing, nil
}

// errCronNotInstalled carries the exact transport text so the Panel can
// classify it; see transport.CronNotInstalled.
// errCronNotInstalled, Panel'in sınıflandırabilmesi için tam taşıma metnini
// taşır; bkz. transport.CronNotInstalled.
var errCronNotInstalled = errors.New(transport.CronNotInstalled)

// cronLookPath is swapped by tests; production resolves `crontab` on PATH,
// which is the command every cron implementation (cron, cronie,
// systemd-cron, …) provides and the one these RPCs run.
// cronLookPath testlerde değiştirilir; üretimde `crontab` PATH'te çözülür.
var cronLookPath = exec.LookPath

// requireCronInstalled answers the one known host condition before any
// crontab is read or written. Without it a missing cron made ListCronJobs
// return an empty list (indistinguishable from "no jobs") and Update/Delete
// report "cron job not found" — each hiding the real reason.
// requireCronInstalled, herhangi bir crontab okunmadan ya da yazılmadan önce
// bilinen tek makine koşulunu yanıtlar. Onsuz eksik cron, ListCronJobs'u boş
// liste ("görev yok"tan ayırt edilemez) ve Update/Delete'i "cron job not
// found" döndürmeye itiyordu — her biri gerçek nedeni gizliyordu.
func requireCronInstalled() error {
	if _, err := cronLookPath("crontab"); err != nil {
		return errCronNotInstalled
	}
	return nil
}

func setCrontab(username, content string) error {
	// Report a missing cron package honestly instead of a bare "operation
	// failed" — scheduled tasks need the cron service installed first.
	// Eksik cron paketini "operation failed" yerine dürüstçe bildir —
	// zamanlanmış görevler önce cron servisinin kurulmasını ister.
	if err := requireCronInstalled(); err != nil {
		return err
	}
	// A crontab ends with a newline; Debian's crontab refuses a file that does
	// not. Delete joins the remaining lines without one.
	// Crontab satır sonuyla biter; Debian'ın crontab'ı bitmeyeni reddeder.
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return cronInstallCrontab(username, content)
}

// cronInstallCrontab replaces the user's crontab. Swapped by tests.
// cronInstallCrontab kullanıcının crontab'ını değiştirir. Testlerde değiştirilir.
var cronInstallCrontab = func(username, content string) error {
	cmd := exec.Command("crontab", "-u", username, "-")
	cmd.Stdin = strings.NewReader(content)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("crontab: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
