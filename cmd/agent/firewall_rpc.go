package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// The firewall — the third leg of attack-surface management. A default-deny
// inbound policy means a server exposes only the ports it actually needs: the
// panel opens a service's ports when it is installed and closes them when it
// is removed, so the open-port set always equals the running-service set.
//
// Safety first: SSH is detected from the live listeners and ALWAYS kept open,
// and loopback + established connections always pass. A misconfigured rule can
// therefore never lock the operator out of the box. Turning the firewall off
// deletes our table entirely, returning to the distro default (all open).
//
// Güvenlik duvarı — saldırı yüzeyi yönetiminin üçüncü ayağı. Varsayılan-reddet
// gelen politikası: sunucu yalnız gerçekten ihtiyaç duyduğu portları açar.
// Panel servis kurulunca portlarını açar, kaldırılınca kapatır; açık-port
// kümesi her zaman koşan-servis kümesine eşittir.
//
// Önce güvenlik: SSH canlı dinleyicilerden tespit edilir ve DAİMA açık tutulur;
// loopback + kurulu bağlantılar her zaman geçer. Yanlış bir kural operatörü
// asla kutudan kilitleyemez. Güvenlik duvarını kapatmak tablomuzu tümüyle
// siler (dağıtım varsayılanına, her şey açık, döner).

const (
	fwTable                 = "celikpanel_fw"
	firewallSnapshotPath    = "/etc/celikpanel/firewall.nft"
	maxFirewallSnapshotSize = 64 << 10
	firewallSnapshotVersion = 2
	firewallRestoreUnitName = "celikpanel-firewall-restore.service"

	firewallPersistenceDisabled   = "disabled"
	firewallPersistenceMissing    = "missing"
	firewallPersistenceReady      = "ready"
	firewallPersistenceStale      = "stale"
	firewallPersistenceInvalid    = "invalid"
	firewallPersistenceUnverified = "unverified"
)

// firewallMu serializes status, apply and boot restore. nft itself applies a
// batch atomically, but without this process lock a Status→Apply sequence could
// observe a table that another RPC removes a millisecond later.
//
// firewallMu; durum, uygulama ve açılış geri yüklemesini sıraya koyar. nft bir
// paketi atomik uygular, ancak bu süreç kilidi olmadan Durum→Uygula dizisi başka
// bir RPC'nin bir milisaniye sonra sildiği tabloyu görebilirdi.
var firewallMu sync.Mutex

// firewallLastRestoreError is kept until a successful explicit apply/disable.
// A failed boot restore must be visible in FirewallStatus instead of looking
// like an intentionally disabled firewall.
//
// firewallLastRestoreError başarılı bir açık uygulama/kapatmaya dek tutulur.
// Başarısız açılış geri yüklemesi, bilerek kapatılmış bir güvenlik duvarı gibi
// görünmek yerine FirewallStatus'ta görünmelidir.
var firewallLastRestoreError string

// A directory-sync failure happens after the namespace mutation has committed.
// Keep it visible instead of rolling live nft state back against a snapshot
// whose rename/remove has already happened.
// Dizin eşitleme hatası ad alanı değişikliği tamamlandıktan sonra oluşur. Rename
// veya remove gerçekleşmiş snapshot'a karşı canlı nft durumunu geri almak
// yerine hatayı görünür tut.
var firewallLastPersistenceError string

// firewallCommandRunner keeps command execution replaceable in tests. The
// production runner still invokes only the fixed nft/ss commands assembled in
// this file; callers can never supply a command or argument.
//
// firewallCommandRunner, komut çalıştırmayı testlerde değiştirilebilir tutar.
// Üretim çalıştırıcısı yine yalnız bu dosyada oluşturulan sabit nft/ss
// komutlarını çağırır; çağıran taraf komut veya argüman veremez.
type firewallCommandRunner interface {
	LookPath(string) (string, error)
	Output(string, ...string) ([]byte, error)
	CombinedOutput(string, []string, string) ([]byte, error)
}

type sshProcessVerifier interface {
	VerifySSHDProcess(int) error
}

type sshConfigurationReader interface {
	ConfiguredSSHPorts() ([]int, error)
}

type sshSocketConfigurationReader interface {
	ConfiguredSSHSocketPorts() ([]int, error)
}

type hostFirewallCommandRunner struct {
	ctx context.Context
}

func (r hostFirewallCommandRunner) commandContext() context.Context {
	if r.ctx == nil {
		return context.Background()
	}
	return r.ctx
}

func (hostFirewallCommandRunner) LookPath(file string) (string, error) {
	return trustedCommandExecutablePath(file)
}

func (r hostFirewallCommandRunner) Output(name string, args ...string) ([]byte, error) {
	path, err := trustedCommandExecutablePath(name)
	if err != nil {
		return nil, err
	}
	return serviceMutationCommand(r.commandContext(), path, args...).Output()
}

func (r hostFirewallCommandRunner) CombinedOutput(name string, args []string, stdin string) ([]byte, error) {
	path, err := trustedCommandExecutablePath(name)
	if err != nil {
		return nil, err
	}
	cmd := serviceMutationCommand(r.commandContext(), path, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	return cmd.CombinedOutput()
}

func (hostFirewallCommandRunner) VerifySSHDProcess(pid int) error {
	return verifyRootSSHDProcess(pid)
}

func (r hostFirewallCommandRunner) ConfiguredSSHPorts() ([]int, error) {
	sshdPath, err := trustedSSHDExecutablePath()
	if err != nil {
		return nil, err
	}
	out, err := serviceMutationCommand(r.commandContext(), sshdPath, "-T").CombinedOutput()
	if err != nil {
		return nil, errors.New(commandFailureDetail("sshd -T failed", out, err))
	}
	return parseSSHDConfigurationPorts(out)
}

func (r hostFirewallCommandRunner) ConfiguredSSHSocketPorts() ([]int, error) {
	systemctlPath, err := trustedCommandExecutablePath("systemctl")
	if err != nil {
		return nil, err
	}
	var ports []int
	for _, unit := range []string{"ssh.socket", "sshd.socket"} {
		stateOut, stateErr := serviceMutationCommand(r.commandContext(),
			systemctlPath, "show", "--no-pager", "--property=LoadState", "--value", unit,
		).CombinedOutput()
		state := strings.TrimSpace(string(stateOut))
		if state == "not-found" {
			continue
		}
		if stateErr != nil {
			return nil, errors.New(commandFailureDetail("systemctl "+unit+" LoadState failed", stateOut, stateErr))
		}
		if state != "loaded" {
			return nil, fmt.Errorf("systemctl %s returned unsupported LoadState %q", unit, state)
		}
		listenOut, listenErr := serviceMutationCommand(r.commandContext(),
			systemctlPath, "show", "--no-pager", "--property=Listen", "--value", unit,
		).CombinedOutput()
		if listenErr != nil {
			return nil, errors.New(commandFailureDetail("systemctl "+unit+" Listen failed", listenOut, listenErr))
		}
		unitPorts, err := parseSystemdSocketListenPorts(listenOut)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", unit, err)
		}
		if len(unitPorts) == 0 {
			return nil, fmt.Errorf("%s is loaded but exposes no Stream listener", unit)
		}
		ports = append(ports, unitPorts...)
	}
	return dedupeSorted(ports), nil
}

// firewallStateStore keeps persistence replaceable in tests. Production uses
// one fixed root-written path; neither an RPC caller nor panel input can choose
// a file.
//
// firewallStateStore kalıcılığı testlerde değiştirilebilir tutar. Üretim,
// root'un yazdığı tek sabit yolu kullanır; RPC çağıranı veya panel girdisi dosya
// seçemez.
type firewallStateStore interface {
	Load() ([]byte, bool, error)
	Save([]byte) error
	Remove() error
}

type fileFirewallStateStore struct {
	path          string
	ownerVerifier func(os.FileInfo, string) error
	syncDirectory func(string) error
}

type firewallStateCommittedError struct {
	operation string
	err       error
}

func (e *firewallStateCommittedError) Error() string {
	return fmt.Sprintf("%s committed but its directory could not be durably synced: %v", e.operation, e.err)
}

func (e *firewallStateCommittedError) Unwrap() error {
	return e.err
}

func syncFirewallStateDirectory(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		_ = d.Close()
		return err
	}
	return d.Close()
}

func (s fileFirewallStateStore) syncStateDirectory(dir string) error {
	if s.syncDirectory != nil {
		return s.syncDirectory(dir)
	}
	return syncFirewallStateDirectory(dir)
}

func (s fileFirewallStateStore) Load() ([]byte, bool, error) {
	info, err := os.Lstat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("inspect persistent firewall snapshot: %w", err)
	}
	if err := ensureFirewallStateDir(filepath.Dir(s.path), s.ownerVerifier); err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("persistent firewall snapshot is not a regular file")
	}
	if err := s.verifyOwner(info, "persistent firewall snapshot"); err != nil {
		return nil, false, err
	}
	if info.Mode().Perm() != 0o600 {
		return nil, false, fmt.Errorf("persistent firewall snapshot permissions are %04o, want 0600", info.Mode().Perm())
	}
	if info.Size() <= 0 || info.Size() > maxFirewallSnapshotSize {
		return nil, false, fmt.Errorf("persistent firewall snapshot has invalid size %d", info.Size())
	}
	f, err := os.Open(s.path)
	if err != nil {
		return nil, false, fmt.Errorf("open persistent firewall snapshot: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxFirewallSnapshotSize+1))
	if err != nil {
		return nil, false, fmt.Errorf("read persistent firewall snapshot: %w", err)
	}
	if len(data) == 0 || len(data) > maxFirewallSnapshotSize {
		return nil, false, fmt.Errorf("persistent firewall snapshot has invalid size %d", len(data))
	}
	return data, true, nil
}

func (s fileFirewallStateStore) Save(data []byte) error {
	if err := validateFirewallSnapshot(data); err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := ensureFirewallStateDir(dir, s.ownerVerifier); err != nil {
		return err
	}
	if info, err := os.Lstat(s.path); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("persistent firewall snapshot is not a regular file")
		}
		if err := s.verifyOwner(info, "persistent firewall snapshot"); err != nil {
			return err
		}
		if info.Mode().Perm() != 0o600 {
			return fmt.Errorf("persistent firewall snapshot permissions are %04o, want 0600", info.Mode().Perm())
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect persistent firewall snapshot: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".firewall-*.tmp")
	if err != nil {
		return fmt.Errorf("create persistent firewall snapshot: %w", err)
	}
	tmpPath := tmp.Name()
	keep := false
	defer func() {
		_ = tmp.Close()
		if !keep {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("secure persistent firewall snapshot: %w", err)
	}
	tmpInfo, err := tmp.Stat()
	if err != nil {
		return fmt.Errorf("inspect persistent firewall snapshot: %w", err)
	}
	if err := s.verifyOwner(tmpInfo, "persistent firewall snapshot"); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write persistent firewall snapshot: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync persistent firewall snapshot: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close persistent firewall snapshot: %w", err)
	}
	// Rename is the commit point: before it the old snapshot is untouched;
	// after it readers see the complete new file, never a partial policy.
	// Rename commit noktasıdır: öncesinde eski snapshot değişmez; sonrasında
	// okuyucular kısmi politika değil eksiksiz yeni dosyayı görür.
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("publish persistent firewall snapshot: %w", err)
	}
	keep = true
	// The rename is visible before directory fsync. Report that partial commit
	// explicitly; callers must not roll live policy back against the new file.
	// Rename, dizin fsync'inden önce görünür olur. Bu kısmi commit'i açıkça
	// bildir; çağıran yeni dosyaya karşı canlı politikayı geri almamalıdır.
	if err := s.syncStateDirectory(dir); err != nil {
		return &firewallStateCommittedError{operation: "firewall snapshot save", err: err}
	}
	return nil
}

func (s fileFirewallStateStore) Remove() error {
	info, err := os.Lstat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect persistent firewall snapshot: %w", err)
	}
	if err := ensureFirewallStateDir(filepath.Dir(s.path), s.ownerVerifier); err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("persistent firewall snapshot is not a regular file")
	}
	if err := s.verifyOwner(info, "persistent firewall snapshot"); err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := os.Remove(s.path); err != nil {
		return fmt.Errorf("remove persistent firewall snapshot: %w", err)
	}
	// Removal is already visible here. Surface a directory-sync failure without
	// resurrecting the old live policy against an already removed snapshot.
	// Silme burada görünür durumdadır. Dizin eşitleme hatasını, kaldırılmış
	// snapshot'a karşı eski canlı politikayı diriltmeden görünür kıl.
	if err := s.syncStateDirectory(dir); err != nil {
		return &firewallStateCommittedError{operation: "firewall snapshot removal", err: err}
	}
	return nil
}

func (s fileFirewallStateStore) verifyOwner(info os.FileInfo, label string) error {
	if s.ownerVerifier != nil {
		return s.ownerVerifier(info, label)
	}
	return requireRootOwner(info, label)
}

func ensureFirewallStateDir(dir string, ownerVerifier func(os.FileInfo, string) error) error {
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create firewall state directory: %w", err)
		}
		info, err = os.Lstat(dir)
	}
	if err != nil {
		return fmt.Errorf("inspect firewall state directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("firewall state directory is not a real directory")
	}
	if ownerVerifier == nil {
		ownerVerifier = requireRootOwner
	}
	if err := ownerVerifier(info, "firewall state directory"); err != nil {
		return err
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("firewall state directory is group/world writable")
	}
	return nil
}

func requireRootOwner(info os.FileInfo, label string) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("%s owner UID is unavailable", label)
	}
	if stat.Uid != 0 {
		return fmt.Errorf("%s owner UID is %d, want 0", label, stat.Uid)
	}
	return nil
}

type ApplyFirewallRequest struct {
	ServiceMutationBinding
	Enabled  bool  `json:"enabled"`
	TCPPorts []int `json:"tcp_ports"`
	UDPPorts []int `json:"udp_ports"`
	// Persist is set only by the panel's explicit on/off action. Automatic
	// service sync may update an existing snapshot but can never create one.
	// Persist yalnız paneldeki açık aç/kapat eyleminde gönderilir. Otomatik
	// servis eşitlemesi mevcut snapshot'ı güncelleyebilir ama yenisini oluşturamaz.
	Persist bool `json:"persist"`
}

type FirewallStatusResponse struct {
	Enabled bool `json:"enabled"`
	// EngineAvailable: whether nftables (the engine the panel drives) is
	// installed. When false the panel routes the operator to Services to
	// install it, instead of failing an opaque "Turn on".
	// EngineAvailable: panelin kullandığı motor nftables kurulu mu. False ise
	// panel operatörü, anlamsız bir "Turn on" hatası yerine motoru kurmak için
	// Servisler'e yönlendirir.
	EngineAvailable  bool   `json:"engine_available"`
	TCPPorts         []int  `json:"tcp_ports"`
	UDPPorts         []int  `json:"udp_ports"`
	SSHPorts         []int  `json:"ssh_ports"`
	PersistenceState string `json:"persistence_state"`
	PersistenceError string `json:"persistence_error,omitempty"`
	SnapshotVersion  int    `json:"snapshot_version,omitempty"`
	Error            string `json:"error,omitempty"`
}

// ApplyFirewall installs (or tears down) our nftables table. Enabled=false
// removes it. Enabled=true builds a default-drop input chain that always
// admits loopback, established/related, ICMP and SSH, plus the requested
// service ports.
// ApplyFirewall, nftables tablomuzu kurar (ya da kaldırır).
func (a *Agent) ApplyFirewall(req *ApplyFirewallRequest, resp *FirewallStatusResponse) error {
	binding := ServiceMutationBinding{}
	if req != nil {
		binding = req.ServiceMutationBinding
	}
	// Every firewall write must join the durable global service mutation lease.
	// This prevents UI actions and post-install synchronization from racing
	// release updates, rollbacks, or another privileged component operation.
	// Her güvenlik duvarı yazımı kalıcı küresel servis mutation lease'ine
	// katılmalıdır. Böylece UI işlemleri ve kurulum sonrası eşitleme; sürüm
	// güncellemesi, geri alma veya başka ayrıcalıklı bileşen işlemiyle yarışmaz.
	ctx, finishStep, err := a.requiredServiceMutationStep(binding)
	if err != nil {
		*resp = FirewallStatusResponse{
			PersistenceState: firewallPersistenceUnverified,
			PersistenceError: err.Error(),
			Error:            "firewall mutation is blocked: " + err.Error(),
		}
		return nil
	}
	defer finishStep()
	return applyFirewallWithRunner(hostFirewallCommandRunner{ctx: ctx}, req, resp)
}

// applyFirewallWithRunner contains the transaction boundary separately from
// net/rpc so failure behaviour can be proved without changing the host
// firewall in tests.
//
// applyFirewallWithRunner, işlem sınırını net/rpc'den ayırır; böylece hata
// davranışı testlerde makinenin güvenlik duvarını değiştirmeden kanıtlanır.
func applyFirewallWithRunner(runner firewallCommandRunner, req *ApplyFirewallRequest, resp *FirewallStatusResponse) error {
	return applyFirewallWithRunnerAndStore(runner, fileFirewallStateStore{path: firewallSnapshotPath}, req, resp)
}

func applyFirewallWithRunnerAndStore(runner firewallCommandRunner, store firewallStateStore, req *ApplyFirewallRequest, resp *FirewallStatusResponse) error {
	firewallMu.Lock()
	defer firewallMu.Unlock()
	return applyFirewallLocked(runner, store, req, resp)
}

func applyFirewallLocked(runner firewallCommandRunner, store firewallStateStore, req *ApplyFirewallRequest, resp *FirewallStatusResponse) error {
	*resp = FirewallStatusResponse{}
	if req == nil {
		resp.Error = "missing firewall request"
		return nil
	}
	if _, err := runner.LookPath("nft"); err != nil {
		// The panel gates "Turn on" on EngineAvailable, so this is a belt-and-
		// suspenders guard — it never auto-installs (D-008: install is the
		// operator's explicit act, done from Services).
		// Panel "Turn on"u EngineAvailable'a kapılar; bu ek bir emniyet — asla
		// oto-kurmaz (D-008: kurulum operatörün açık eylemi, Servisler'den).
		resp.Error = "firewall engine (nftables) is not installed — install it from Services first"
		return nil
	}
	resp.EngineAvailable = true
	existingSnapshot, snapshotExists, err := store.Load()
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	if snapshotExists && req.Enabled {
		if err := validateFirewallSnapshot(existingSnapshot); err != nil {
			resp.Error = err.Error()
			return nil
		}
	}

	// Discover table presence without treating a command/protocol failure as
	// "absent". A false absence would hide the real operational fault.
	//
	// Tablo varlığını komut/protokol hatasını "yok" saymadan öğren. Yanlış bir
	// yokluk sonucu gerçek işletim hatasını gizler.
	tables, err := runner.Output("nft", "list", "tables")
	if err != nil {
		resp.Error = fmt.Sprintf("nft table discovery failed: %v", err)
		return nil
	}
	tablePresent := firewallTablePresent(tables)
	setFirewallPersistenceStatus(resp, existingSnapshot, snapshotExists, nil, tablePresent)
	var oldRules []byte
	if tablePresent {
		oldRules, err = runner.Output("nft", "list", "table", "inet", fwTable)
		if err != nil {
			resp.Error = fmt.Sprintf("nft table read failed: %v", err)
			return nil
		}
	}

	// Disable is intentionally separate. It removes our table only after an
	// explicit operator request; a failed delete leaves the current policy.
	//
	// Kapatma bilinçli olarak ayrıdır. Tablo yalnız operatörün açık isteğiyle
	// silinir; başarısız silme mevcut politikayı yerinde bırakır.
	if !req.Enabled {
		if tablePresent {
			out, err := runner.CombinedOutput("nft", []string{"delete", "table", "inet", fwTable}, "")
			if err != nil {
				resp.Error = commandFailureDetail("nft disable failed", out, err)
				return nil
			}
		}
		// Only an explicit persistent disable removes the snapshot and boot
		// unit. A transient live disable leaves the saved reboot policy intact.
		// Yalnız açık kalıcı kapatma snapshot'ı ve açılış unitini kaldırır.
		// Geçici canlı kapatma, kaydedilmiş yeniden başlatma politikasını korur.
		if req.Persist {
			// The persistent policy is removed only after nft confirmed that our
			// table is gone. If removal fails, restore the old live table so an
			// error preserves both sides of the previous state.
			// Kalıcı politika yalnız nft tablomuzun gittiğini doğruladıktan sonra
			// kaldırılır. Kaldırma düşerse eski canlı tablo geri yüklenir; böylece
			// hata önceki durumun iki yanını da korur.
			if err := store.Remove(); err != nil {
				var committed *firewallStateCommittedError
				if errors.As(err, &committed) {
					firewallLastPersistenceError = err.Error()
					resp.Enabled = false
					resp.PersistenceState = firewallPersistenceUnverified
					resp.PersistenceError = err.Error()
					resp.Error = err.Error()
					return nil
				}
				if tablePresent {
					// The explicit delete already proved the live table is absent. The
					// rollback must add the old table directly; deleting it again makes
					// a real nft transaction fail before restoration can run.
					// Açık silme canlı tablonun artık bulunmadığını kanıtladı. Geri alma
					// eski tabloyu doğrudan eklemeli; tabloyu yeniden silmeye çalışmak
					// gerçek nft işlemini geri yüklemeden önce düşürür.
					if rollbackErr := rollbackFirewallPolicy(runner, false, true, oldRules); rollbackErr != nil {
						resp.Error = fmt.Sprintf("%v; live policy rollback failed: %v", err, rollbackErr)
						return nil
					}
				}
				resp.Enabled = tablePresent
				resp.Error = err.Error()
				return nil
			}
			if err := setFirewallRestoreUnitEnabled(runner, false); err != nil {
				snapshotRollbackErr := restoreFirewallSnapshotState(store, existingSnapshot, snapshotExists)
				var liveRollbackErr error
				if tablePresent {
					liveRollbackErr = rollbackFirewallPolicy(runner, false, true, oldRules)
				}
				detail := fmt.Sprintf("disable firewall restore unit failed: %v", err)
				if snapshotRollbackErr != nil {
					detail += fmt.Sprintf("; snapshot rollback failed: %v", snapshotRollbackErr)
				}
				if liveRollbackErr != nil {
					detail += fmt.Sprintf("; live policy rollback failed: %v", liveRollbackErr)
				}
				firewallLastPersistenceError = detail
				resp.Enabled = tablePresent && liveRollbackErr == nil
				resp.PersistenceState = firewallPersistenceUnverified
				resp.PersistenceError = detail
				resp.Error = detail
				return nil
			}
			firewallLastPersistenceError = ""
			resp.PersistenceState = firewallPersistenceDisabled
			resp.PersistenceError = ""
			resp.SnapshotVersion = 0
		} else {
			setFirewallPersistenceStatus(resp, existingSnapshot, snapshotExists, nil, false)
		}
		firewallLastRestoreError = ""
		resp.Enabled = false
		return nil
	}

	sshPorts, err := detectSSHPortsWithRunner(runner)
	if err != nil {
		resp.Error = fmt.Sprintf("SSH listener discovery failed; firewall was not changed: %v", err)
		return nil
	}
	requestedTCP := dedupeSorted(req.TCPPorts)
	tcp := dedupeSorted(append(append([]int{}, requestedTCP...), sshPorts...))
	udp := dedupeSorted(req.UDPPorts)

	// Replacement is one nft transaction: an existing table's delete and its
	// complete replacement share the SAME `nft -f -` batch. If the batch fails,
	// nft commits none of it and the old policy remains active.
	//
	// Değiştirme tek nft işlemidir: mevcut tablonun silinmesi ve eksiksiz yeni
	// tablo AYNI `nft -f -` paketindedir. Paket başarısızsa nft hiçbirini
	// kaydetmez ve eski politika etkin kalır.
	rules := buildFirewallRuleset(tablePresent, tcp, udp)

	if out, err := runner.CombinedOutput("nft", []string{"-f", "-"}, rules); err != nil {
		resp.Error = commandFailureDetail("nft apply failed", out, err)
		return nil
	}

	// Explicit Save for reboot creates persistence and enables its boot unit.
	// Automatic service sync may update an existing snapshot but never enables
	// the unit or creates the first snapshot.
	// Açık Save for reboot kalıcılık oluşturur ve açılış unitini etkinleştirir.
	// Otomatik servis eşitlemesi mevcut snapshot'ı güncelleyebilir ama uniti
	// etkinleştirmez ve ilk snapshot'ı oluşturmaz.
	if req.Persist || snapshotExists {
		snapshot := encodeFirewallSnapshot(requestedTCP, udp, sshPorts)
		if err := store.Save(snapshot); err != nil {
			var committed *firewallStateCommittedError
			if errors.As(err, &committed) {
				firewallLastPersistenceError = err.Error()
				resp.Enabled = true
				resp.TCPPorts = tcp
				resp.UDPPorts = udp
				resp.SSHPorts = sshPorts
				resp.PersistenceState = firewallPersistenceUnverified
				resp.PersistenceError = err.Error()
				resp.SnapshotVersion = firewallSnapshotVersion
				resp.Error = err.Error()
				return nil
			}
			// The newly applied table is present regardless of whether an older
			// table existed, so remove that current table before restoring the
			// previous state.
			// Daha önce tablo bulunsun ya da bulunmasın, yeni uygulanan tablo şu an
			// vardır; önce onu sil, ardından önceki durumu geri yükle.
			if rollbackErr := rollbackFirewallPolicy(runner, true, tablePresent, oldRules); rollbackErr != nil {
				resp.Error = fmt.Sprintf("persist firewall policy failed: %v; live policy rollback failed: %v", err, rollbackErr)
				return nil
			}
			resp.Enabled = tablePresent
			resp.Error = fmt.Sprintf("persist firewall policy failed: %v", err)
			return nil
		}
		if req.Persist {
			if err := setFirewallRestoreUnitEnabled(runner, true); err != nil {
				snapshotRollbackErr := restoreFirewallSnapshotState(store, existingSnapshot, snapshotExists)
				detail := fmt.Sprintf("enable firewall restore unit failed: %v", err)
				if snapshotRollbackErr != nil {
					detail += fmt.Sprintf("; snapshot rollback failed: %v", snapshotRollbackErr)
				}
				firewallLastPersistenceError = detail
				resp.Enabled = true
				resp.TCPPorts = tcp
				resp.UDPPorts = udp
				resp.SSHPorts = sshPorts
				resp.PersistenceState = firewallPersistenceUnverified
				resp.PersistenceError = detail
				resp.Error = detail
				return nil
			}
			firewallLastPersistenceError = ""
		}
		resp.PersistenceState = firewallPersistenceReady
		resp.SnapshotVersion = firewallSnapshotVersion
	} else {
		resp.PersistenceState = firewallPersistenceMissing
	}

	firewallLastRestoreError = ""
	resp.Enabled = true
	resp.TCPPorts = tcp
	resp.UDPPorts = udp
	resp.SSHPorts = sshPorts
	return nil
}

// setFirewallRestoreUnitEnabled changes only boot-time activation links; it
// never starts the restore unit against the live host.
// setFirewallRestoreUnitEnabled yalnız açılış etkinleştirme bağlantılarını
// değiştirir; restore unitini canlı host üzerinde asla başlatmaz.
func setFirewallRestoreUnitEnabled(runner firewallCommandRunner, enabled bool) error {
	action := "disable"
	if enabled {
		action = "enable"
	}
	out, err := runner.CombinedOutput("systemctl", []string{action, firewallRestoreUnitName}, "")
	if err != nil {
		return errors.New(commandFailureDetail("systemctl "+action+" "+firewallRestoreUnitName+" failed", out, err))
	}
	return nil
}

// restoreFirewallSnapshotState restores the exact pre-operation bytes after a
// boot-unit activation failure; absence is restored as absence.
// restoreFirewallSnapshotState, açılış uniti etkinleştirme hatasından sonra
// işlem öncesi baytları aynen geri yükler; yokluk yine yokluk olarak korunur.
func restoreFirewallSnapshotState(store firewallStateStore, previous []byte, existed bool) error {
	if existed {
		return store.Save(previous)
	}
	return store.Remove()
}

func buildFirewallRuleset(replace bool, tcp, udp []int) string {
	tcp = dedupeSorted(tcp)
	udp = dedupeSorted(udp)
	var b strings.Builder
	if replace {
		b.WriteString(fmt.Sprintf("delete table inet %s\n", fwTable))
	}
	b.WriteString(fmt.Sprintf("table inet %s {\n", fwTable))
	b.WriteString("  chain input {\n")
	b.WriteString("    type filter hook input priority 0; policy drop;\n")
	b.WriteString("    iif lo accept\n")
	b.WriteString("    ct state established,related accept\n")
	b.WriteString("    ct state invalid drop\n")
	b.WriteString("    meta l4proto icmp accept\n")
	b.WriteString("    meta l4proto ipv6-icmp accept\n")
	if len(tcp) > 0 {
		b.WriteString(fmt.Sprintf("    tcp dport { %s } accept\n", joinInts(tcp)))
	}
	if len(udp) > 0 {
		b.WriteString(fmt.Sprintf("    udp dport { %s } accept\n", joinInts(udp)))
	}
	b.WriteString("  }\n}\n")
	return b.String()
}

func rollbackFirewallPolicy(runner firewallCommandRunner, currentPresent, oldPresent bool, oldRules []byte) error {
	var b strings.Builder
	if currentPresent {
		b.WriteString(fmt.Sprintf("delete table inet %s\n", fwTable))
	}
	if oldPresent {
		if len(oldRules) == 0 {
			return fmt.Errorf("old nft policy is empty")
		}
		b.Write(oldRules)
		if oldRules[len(oldRules)-1] != '\n' {
			b.WriteByte('\n')
		}
	}
	if out, err := runner.CombinedOutput("nft", []string{"-f", "-"}, b.String()); err != nil {
		return errors.New(commandFailureDetail("nft rollback failed", out, err))
	}
	return nil
}

type firewallSnapshotPolicy struct {
	Version        int   `json:"version"`
	TCPPorts       []int `json:"tcp_ports"`
	UDPPorts       []int `json:"udp_ports"`
	SSHPortsAtSave []int `json:"ssh_ports_at_save"`
}

// Version 2 keeps operator/service ports separate from automatically protected
// SSH ports, so boot can add the current trusted sshd configuration safely.
// Sürüm 2, operatör/servis portlarını otomatik korunan SSH portlarından ayırır;
// böylece açılış güncel ve güvenilir sshd yapılandırmasını güvenle ekleyebilir.
func encodeFirewallSnapshot(tcp, udp, ssh []int) []byte {
	policy := firewallSnapshotPolicy{
		Version:        firewallSnapshotVersion,
		TCPPorts:       dedupeSorted(tcp),
		UDPPorts:       dedupeSorted(udp),
		SSHPortsAtSave: dedupeSorted(ssh),
	}
	data, _ := json.Marshal(policy)
	return append(data, '\n')
}

// decodeFirewallSnapshot accepts only canonical V2 JSON or the exact legacy
// ruleset emitted by older CelikPanel builds. Arbitrary nft text never reaches
// the privileged `nft -f` command.
// decodeFirewallSnapshot yalnız kanonik V2 JSON'u veya eski CelikPanel
// sürümlerinin ürettiği tam kural kümesini kabul eder. Keyfi nft metni
// ayrıcalıklı `nft -f` komutuna asla ulaşmaz.
func decodeFirewallSnapshot(data []byte) (firewallSnapshotPolicy, bool, error) {
	if len(data) == 0 || len(data) > maxFirewallSnapshotSize {
		return firewallSnapshotPolicy{}, false, fmt.Errorf("persistent firewall snapshot has invalid size %d", len(data))
	}
	if data[0] == '{' {
		var policy firewallSnapshotPolicy
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&policy); err != nil {
			return firewallSnapshotPolicy{}, false, fmt.Errorf("decode persistent firewall snapshot: %w", err)
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			return firewallSnapshotPolicy{}, false, fmt.Errorf("persistent firewall snapshot has trailing data")
		}
		if policy.Version != firewallSnapshotVersion {
			return firewallSnapshotPolicy{}, false, fmt.Errorf("unsupported persistent firewall snapshot version %d", policy.Version)
		}
		if !bytes.Equal(data, encodeFirewallSnapshot(policy.TCPPorts, policy.UDPPorts, policy.SSHPortsAtSave)) {
			return firewallSnapshotPolicy{}, false, fmt.Errorf("persistent firewall snapshot is not canonical")
		}
		return policy, false, nil
	}

	var tcp, udp []int
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if p, ok := parsePortLine(line, "tcp dport"); ok {
			tcp = p
		}
		if p, ok := parsePortLine(line, "udp dport"); ok {
			udp = p
		}
	}
	expected := buildFirewallRuleset(false, dedupeSorted(tcp), dedupeSorted(udp))
	if string(data) != expected {
		return firewallSnapshotPolicy{}, false, fmt.Errorf("persistent firewall snapshot does not match CelikPanel's exact legacy ruleset")
	}
	return firewallSnapshotPolicy{
		Version:  firewallSnapshotVersion,
		TCPPorts: dedupeSorted(tcp),
		UDPPorts: dedupeSorted(udp),
	}, true, nil
}

func validateFirewallSnapshot(data []byte) error {
	_, _, err := decodeFirewallSnapshot(data)
	return err
}

func commandFailureDetail(prefix string, out []byte, err error) string {
	detail := strings.TrimSpace(string(out))
	if detail == "" {
		detail = err.Error()
	}
	return fmt.Sprintf("%s: %s", prefix, detail)
}

func firewallTablePresent(out []byte) bool {
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "table" && fields[1] == "inet" && fields[2] == fwTable {
			return true
		}
	}
	return false
}

// FirewallStatus reports whether our table is present and what it admits.
// FirewallStatus, tablomuzun var olup olmadığını ve neyi kabul ettiğini bildirir.
func (a *Agent) FirewallStatus(_ *struct{}, resp *FirewallStatusResponse) error {
	return firewallStatusWithRunnerAndStore(hostFirewallCommandRunner{}, fileFirewallStateStore{path: firewallSnapshotPath}, resp)
}

func firewallStatusWithRunnerAndStore(runner firewallCommandRunner, store firewallStateStore, resp *FirewallStatusResponse) error {
	firewallMu.Lock()
	defer firewallMu.Unlock()
	return firewallStatusLocked(runner, store, resp)
}

func firewallStatusLocked(runner firewallCommandRunner, store firewallStateStore, resp *FirewallStatusResponse) error {
	*resp = FirewallStatusResponse{}
	snapshot, snapshotExists, snapshotLoadErr := store.Load()
	snapshotErr := snapshotLoadErr
	if snapshotLoadErr == nil && snapshotExists {
		snapshotErr = validateFirewallSnapshot(snapshot)
	}
	if _, err := runner.LookPath("nft"); err != nil {
		setFirewallPersistenceStatus(resp, snapshot, snapshotExists, snapshotLoadErr, false)
		if snapshotExists {
			resp.Error = "persistent firewall policy exists but nftables is unavailable"
			resp.PersistenceState = firewallPersistenceUnverified
			resp.PersistenceError = appendFirewallError(resp.PersistenceError, resp.Error)
		}
		if snapshotErr != nil {
			resp.Error = appendFirewallError(resp.Error, snapshotErr.Error())
		}
		return nil
	}
	resp.EngineAvailable = true

	// `nft list table` uses the same non-zero exit for a missing table and for
	// permission/protocol failures. List all tables first so only a proven
	// absence is reported as off; every command fault remains visible.
	// `nft list table`, eksik tabloyla yetki/protokol hatasında aynı sıfır-dışı
	// sonucu verir. Önce tüm tablolar listelenir; yalnız kanıtlı yokluk kapalı
	// görünür, her komut arızası görünür kalır.
	tables, err := runner.Output("nft", "list", "tables")
	if err != nil {
		resp.Error = fmt.Sprintf("nft table discovery failed: %v", err)
		resp.PersistenceState = firewallPersistenceUnverified
		resp.PersistenceError = resp.Error
		return nil
	}
	live := firewallTablePresent(tables)
	setFirewallPersistenceStatus(resp, snapshot, snapshotExists, snapshotLoadErr, live)
	if !live {
		if snapshotExists {
			resp.Error = "persistent firewall policy exists but its live nft table is absent"
		}
		if snapshotErr != nil {
			resp.Error = appendFirewallError(resp.Error, snapshotErr.Error())
		}
		if firewallLastRestoreError != "" {
			resp.Error = appendFirewallError(resp.Error, firewallLastRestoreError)
		}
		return nil
	}

	resp.Enabled = true
	out, err := runner.Output("nft", "list", "table", "inet", fwTable)
	if err != nil {
		resp.Error = fmt.Sprintf("nft table read failed: %v", err)
		return nil
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if p, ok := parsePortLine(line, "tcp dport"); ok {
			resp.TCPPorts = p
		}
		if p, ok := parsePortLine(line, "udp dport"); ok {
			resp.UDPPorts = p
		}
	}
	sshPorts, err := detectSSHPortsWithRunner(runner)
	if err != nil {
		resp.Error = appendFirewallError(resp.Error, fmt.Sprintf("SSH listener discovery failed: %v", err))
	} else {
		resp.SSHPorts = sshPorts
	}
	if snapshotErr != nil {
		resp.Error = appendFirewallError(resp.Error, snapshotErr.Error())
	}
	if firewallLastRestoreError != "" {
		resp.Error = appendFirewallError(resp.Error, firewallLastRestoreError)
	}
	return nil
}

func appendFirewallError(current, next string) string {
	if strings.TrimSpace(current) == "" {
		return next
	}
	return current + "; " + next
}

func setFirewallPersistenceStatus(
	resp *FirewallStatusResponse,
	snapshot []byte,
	exists bool,
	loadErr error,
	live bool,
) {
	resp.PersistenceError = ""
	resp.SnapshotVersion = 0
	if firewallLastPersistenceError != "" {
		resp.PersistenceState = firewallPersistenceUnverified
		resp.PersistenceError = firewallLastPersistenceError
		return
	}
	if loadErr != nil {
		resp.PersistenceState = firewallPersistenceUnverified
		resp.PersistenceError = loadErr.Error()
		return
	}
	if !exists {
		if live {
			resp.PersistenceState = firewallPersistenceMissing
		} else {
			resp.PersistenceState = firewallPersistenceDisabled
		}
		return
	}
	policy, legacy, err := decodeFirewallSnapshot(snapshot)
	if err != nil {
		resp.PersistenceState = firewallPersistenceInvalid
		resp.PersistenceError = err.Error()
		return
	}
	if legacy {
		resp.SnapshotVersion = 1
	} else {
		resp.SnapshotVersion = policy.Version
	}
	if live {
		resp.PersistenceState = firewallPersistenceReady
	} else {
		resp.PersistenceState = firewallPersistenceStale
	}
}

// detectSSHPorts finds every port owned by a verified root sshd process.
// detectSSHPorts, doğrulanmış root sshd sürecinin sahip olduğu her portu bulur.
// Failure or an empty result is an error: assuming 22 can lock out a host whose
// SSH daemon deliberately listens on a custom port.
// Hata veya boş sonuç da hatadır: 22 varsayımı, SSH'ı bilerek özel portta
// dinleyen makineyi dışarıda kilitleyebilir.
func detectSSHPortsWithRunner(runner firewallCommandRunner) ([]int, error) {
	verifier, ok := runner.(sshProcessVerifier)
	if !ok {
		return nil, fmt.Errorf("SSH process verifier is unavailable")
	}
	return detectSSHPortsWithVerifier(runner, verifier)
}

func detectSSHPortsWithVerifier(runner firewallCommandRunner, verifier sshProcessVerifier) ([]int, error) {
	// -p is required for PID data; a process name is never trusted.
	// -p, PID verisi için gereklidir; süreç adına hiçbir zaman güvenilmez.
	out, err := runner.Output("ss", "-ltnpH")
	if err != nil {
		return nil, err
	}
	seen := map[int]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		pids, ok := parseSSProcessPIDs(line)
		if !ok {
			continue
		}
		trusted := false
		for _, pid := range pids {
			if verifier.VerifySSHDProcess(pid) == nil {
				trusted = true
				break
			}
		}
		if !trusted {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		local := fields[3]
		if index := strings.LastIndexByte(local, ':'); index >= 0 {
			if port, err := strconv.Atoi(local[index+1:]); err == nil && port > 0 && port < 65536 {
				seen[port] = true
			}
		}
	}
	if len(seen) == 0 {
		return nil, fmt.Errorf("no verified listening sshd port was found")
	}
	return dedupeSorted(mapKeys(seen)), nil
}

func parseSSProcessPIDs(line string) ([]int, bool) {
	const marker = "pid="
	var pids []int
	seen := map[int]bool{}
	rest := line
	for {
		index := strings.Index(rest, marker)
		if index < 0 {
			break
		}
		if index > 0 {
			previous := rest[index-1]
			if previous != ',' && previous != '(' && previous != ' ' && previous != '\t' {
				return nil, false
			}
		}
		rest = rest[index+len(marker):]
		end := 0
		for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
			end++
		}
		if end == 0 {
			return nil, false
		}
		if end < len(rest) {
			next := rest[end]
			if next != ',' && next != ')' && next != ' ' && next != '\t' {
				return nil, false
			}
		}
		pid64, err := strconv.ParseInt(rest[:end], 10, 32)
		if err != nil || pid64 <= 0 {
			return nil, false
		}
		pid := int(pid64)
		if !seen[pid] {
			seen[pid] = true
			pids = append(pids, pid)
		}
		rest = rest[end:]
	}
	return pids, len(pids) > 0
}

var trustedSSHDExecutablePaths = []string{
	"/usr/sbin/sshd",
	"/usr/bin/sshd",
	"/usr/local/sbin/sshd",
}

var trustedFirewallCommandPaths = map[string][]string{
	"nft":       {"/usr/sbin/nft", "/usr/bin/nft"},
	"ss":        {"/usr/sbin/ss", "/usr/bin/ss"},
	"systemctl": {"/usr/bin/systemctl"},
}

func trustedCommandExecutablePath(name string) (string, error) {
	candidates, ok := trustedFirewallCommandPaths[name]
	if !ok {
		return "", fmt.Errorf("firewall command %q is not allowlisted", name)
	}
	return firstTrustedExecutable(candidates, name)
}

func firstTrustedExecutable(candidates []string, label string) (string, error) {
	var lastErr error
	for _, candidate := range candidates {
		if _, err := validateTrustedExecutablePath(candidate, label); err == nil {
			return candidate, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			lastErr = err
		}
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("no trusted %s executable was found", label)
}

func validateTrustedExecutablePath(path, label string) (os.FileInfo, error) {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) || clean != path {
		return nil, fmt.Errorf("trusted %s executable path %q is not canonical and absolute", label, path)
	}
	for dir := filepath.Dir(clean); ; dir = filepath.Dir(dir) {
		info, err := os.Lstat(dir)
		if err != nil {
			return nil, fmt.Errorf("inspect trusted %s parent %s: %w", label, dir, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("trusted %s parent %s is not a real directory", label, dir)
		}
		if err := requireRootOwner(info, "trusted "+label+" parent"); err != nil {
			return nil, err
		}
		if info.Mode().Perm()&0o022 != 0 {
			return nil, fmt.Errorf("trusted %s parent %s is group/world writable", label, dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	info, err := os.Lstat(clean)
	if err != nil {
		return nil, fmt.Errorf("inspect trusted %s executable: %w", label, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("trusted %s executable is not a regular file", label)
	}
	if err := requireRootOwner(info, "trusted "+label+" executable"); err != nil {
		return nil, err
	}
	if info.Mode().Perm()&0o022 != 0 {
		return nil, fmt.Errorf("trusted %s executable is group/world writable", label)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return nil, fmt.Errorf("trusted %s file is not executable", label)
	}
	return info, nil
}

func trustedSSHDExecutablePath() (string, error) {
	return firstTrustedExecutable(trustedSSHDExecutablePaths, "sshd")
}

func parseSSHDConfigurationPorts(out []byte) ([]int, error) {
	var ports []int
	var listenAddresses []string
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "port":
			if len(fields) != 2 {
				return nil, fmt.Errorf("sshd -T returned a malformed port directive")
			}
			port, err := parseFirewallPort(fields[1])
			if err != nil {
				return nil, fmt.Errorf("sshd -T returned an invalid port %q", fields[1])
			}
			ports = append(ports, port)
		case "listenaddress":
			if len(fields) != 2 {
				return nil, fmt.Errorf("sshd -T returned a malformed listenaddress directive")
			}
			listenAddresses = append(listenAddresses, fields[1])
		}
	}
	ports = dedupeSorted(ports)
	if len(ports) == 0 {
		return nil, fmt.Errorf("sshd -T returned no SSH port")
	}
	if len(listenAddresses) == 0 {
		return ports, nil
	}
	var listenerPorts []int
	for _, address := range listenAddresses {
		port, explicit, err := parseListenAddressPort(address)
		if err != nil {
			return nil, fmt.Errorf("sshd -T returned invalid listenaddress %q: %w", address, err)
		}
		if explicit {
			listenerPorts = append(listenerPorts, port)
		} else {
			listenerPorts = append(listenerPorts, ports...)
		}
	}
	listenerPorts = dedupeSorted(listenerPorts)
	if len(listenerPorts) == 0 {
		return nil, fmt.Errorf("sshd -T returned no usable SSH listener")
	}
	return listenerPorts, nil
}

func parseFirewallPort(value string) (int, error) {
	port, err := strconv.Atoi(value)
	if err != nil || port <= 0 || port >= 65536 {
		return 0, fmt.Errorf("invalid port")
	}
	return port, nil
}

func parseListenAddressPort(value string) (int, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false, fmt.Errorf("empty address")
	}
	if strings.HasPrefix(value, "[") {
		close := strings.IndexByte(value, ']')
		if close < 0 {
			return 0, false, fmt.Errorf("missing closing bracket")
		}
		rest := value[close+1:]
		if rest == "" {
			return 0, false, nil
		}
		if !strings.HasPrefix(rest, ":") {
			return 0, false, fmt.Errorf("unexpected bracket suffix")
		}
		_, portText, err := net.SplitHostPort(value)
		if err != nil {
			return 0, false, err
		}
		port, err := parseFirewallPort(portText)
		return port, err == nil, err
	}
	if strings.Contains(value, "]") {
		return 0, false, fmt.Errorf("unexpected closing bracket")
	}
	if strings.Count(value, ":") == 1 {
		_, portText, err := net.SplitHostPort(value)
		if err != nil {
			return 0, false, err
		}
		port, err := parseFirewallPort(portText)
		return port, err == nil, err
	}
	// An unbracketed IPv6 address carries no distinguishable port; use Port.
	// Köşesiz bir IPv6 adresinde ayırt edilebilir port yoktur; Port değerini kullan.
	return 0, false, nil
}

func parseSystemdSocketListenPorts(out []byte) ([]int, error) {
	fields := strings.Fields(string(out))
	var ports []int
	for i := 0; i < len(fields); {
		if i+1 >= len(fields) || !strings.HasPrefix(fields[i+1], "(") || !strings.HasSuffix(fields[i+1], ")") {
			return nil, fmt.Errorf("systemd returned a malformed Listen value")
		}
		address, kind := fields[i], strings.TrimSuffix(strings.TrimPrefix(fields[i+1], "("), ")")
		i += 2
		if kind != "Stream" {
			continue
		}
		if strings.HasPrefix(address, "/") {
			continue
		}
		if port, err := parseFirewallPort(address); err == nil {
			ports = append(ports, port)
			continue
		}
		port, explicit, err := parseListenAddressPort(address)
		if err != nil || !explicit {
			return nil, fmt.Errorf("systemd returned an invalid Stream listener %q", address)
		}
		ports = append(ports, port)
	}
	return dedupeSorted(ports), nil
}

// Boot-time discovery uses effective sshd configuration because listeners may
// not exist before networking starts.
// Açılış keşfi etkili sshd yapılandırmasını kullanır; ağ başlamadan önce
// dinleyiciler henüz bulunmayabilir.
func detectConfiguredSSHPortsWithRunner(runner firewallCommandRunner) ([]int, error) {
	reader, ok := runner.(sshConfigurationReader)
	if !ok {
		return nil, fmt.Errorf("SSH configuration reader is unavailable")
	}
	ports, err := reader.ConfiguredSSHPorts()
	if err != nil {
		return nil, err
	}
	socketReader, ok := runner.(sshSocketConfigurationReader)
	if !ok {
		return nil, fmt.Errorf("SSH socket configuration reader is unavailable")
	}
	socketPorts, err := socketReader.ConfiguredSSHSocketPorts()
	if err != nil {
		return nil, err
	}
	ports = append(ports, socketPorts...)
	ports = dedupeSorted(ports)
	if len(ports) == 0 {
		return nil, fmt.Errorf("no configured SSH port was found")
	}
	return ports, nil
}

func verifyRootSSHDProcess(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid SSH process PID %d", pid)
	}
	processPath := filepath.Join("/proc", strconv.Itoa(pid))
	processInfo, err := os.Stat(processPath)
	if err != nil {
		return fmt.Errorf("inspect SSH process %d: %w", pid, err)
	}
	if !processInfo.IsDir() {
		return fmt.Errorf("SSH process %d proc entry is not a directory", pid)
	}
	if err := requireRootOwner(processInfo, fmt.Sprintf("SSH process %d", pid)); err != nil {
		return err
	}

	executableLink := filepath.Join(processPath, "exe")
	executablePath, err := os.Readlink(executableLink)
	if err != nil {
		return fmt.Errorf("read SSH process %d executable: %w", pid, err)
	}
	if strings.HasSuffix(executablePath, " (deleted)") {
		return fmt.Errorf("SSH process %d executable has been deleted", pid)
	}
	executableInfo, err := os.Stat(executableLink)
	if err != nil {
		return fmt.Errorf("inspect SSH process %d executable: %w", pid, err)
	}
	if !executableInfo.Mode().IsRegular() {
		return fmt.Errorf("SSH process %d executable is not a regular file", pid)
	}
	if err := requireRootOwner(executableInfo, fmt.Sprintf("SSH process %d executable", pid)); err != nil {
		return err
	}
	if executableInfo.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("SSH process %d executable is group/world writable", pid)
	}

	for _, trustedPath := range trustedSSHDExecutablePaths {
		trustedInfo, err := validateTrustedExecutablePath(trustedPath, "sshd")
		if err != nil {
			continue
		}
		if os.SameFile(executableInfo, trustedInfo) {
			return nil
		}
	}
	return fmt.Errorf("SSH process %d executable %q is not a trusted sshd", pid, executablePath)
}

// restoreFirewallSnapshot is called once before the agent accepts RPCs. It
// restores only CelikPanel's exact table from the fixed, root-written snapshot;
// all other nftables tables remain untouched.
//
// restoreFirewallSnapshot, agent RPC kabul etmeden önce bir kez çağrılır. Sabit,
// root-yazımlı snapshot'tan yalnız CelikPanel'in tam tablosunu geri yükler;
// diğer nftables tablolarına dokunmaz.
func restoreFirewallSnapshot() error {
	firewallMu.Lock()
	defer firewallMu.Unlock()
	err := restoreFirewallSnapshotLocked(hostFirewallCommandRunner{}, fileFirewallStateStore{path: firewallSnapshotPath})
	if err != nil {
		firewallLastRestoreError = fmt.Sprintf("persistent firewall restore failed: %v", err)
		return err
	}
	firewallLastRestoreError = ""
	firewallLastPersistenceError = ""
	return nil
}

func checkFirewallRestore() error {
	firewallMu.Lock()
	defer firewallMu.Unlock()
	return checkFirewallRestoreLocked(hostFirewallCommandRunner{}, fileFirewallStateStore{path: firewallSnapshotPath})
}

func checkFirewallRestoreLocked(runner firewallCommandRunner, store firewallStateStore) error {
	batch, exists, err := prepareFirewallRestoreBatch(runner, store)
	if err != nil || !exists {
		return err
	}
	if out, err := runner.CombinedOutput("nft", []string{"--check", "-f", "-"}, batch); err != nil {
		return errors.New(commandFailureDetail("nft restore preflight failed", out, err))
	}
	return nil
}

func restoreFirewallSnapshotLocked(runner firewallCommandRunner, store firewallStateStore) error {
	batch, exists, err := prepareFirewallRestoreBatch(runner, store)
	if err != nil || !exists {
		return err
	}
	if out, err := runner.CombinedOutput("nft", []string{"-f", "-"}, batch); err != nil {
		return errors.New(commandFailureDetail("nft restore failed", out, err))
	}
	return nil
}

func prepareFirewallRestoreBatch(runner firewallCommandRunner, store firewallStateStore) (string, bool, error) {
	snapshot, exists, err := store.Load()
	if err != nil || !exists {
		return "", exists, err
	}
	policy, legacy, err := decodeFirewallSnapshot(snapshot)
	if err != nil {
		return "", true, err
	}
	if _, err := runner.LookPath("nft"); err != nil {
		return "", true, fmt.Errorf("nftables is unavailable: %w", err)
	}
	configuredSSHPorts, err := detectConfiguredSSHPortsWithRunner(runner)
	if err != nil {
		return "", true, fmt.Errorf("configured SSH port discovery failed; firewall was not restored: %w", err)
	}
	tcp := append(append([]int{}, policy.TCPPorts...), configuredSSHPorts...)
	if !legacy {
		// Keep the last verified listener as a transition guard while also opening
		// the current effective configuration. The next normal panel sync drops a
		// stale listener port from the persisted V2 policy.
		// Güncel etkili yapılandırmayı açarken son doğrulanmış dinleyiciyi geçiş
		// koruması olarak tut. Sonraki olağan panel eşitlemesi eski dinleyici
		// portunu kalıcı V2 politikasından düşürür.
		tcp = append(tcp, policy.SSHPortsAtSave...)
	}
	rules := buildFirewallRuleset(false, tcp, policy.UDPPorts)
	tables, err := runner.Output("nft", "list", "tables")
	if err != nil {
		return "", true, fmt.Errorf("nft table discovery failed: %w", err)
	}
	var batch strings.Builder
	if firewallTablePresent(tables) {
		batch.WriteString(fmt.Sprintf("delete table inet %s\n", fwTable))
	}
	batch.WriteString(rules)
	return batch.String(), true, nil
}

func parsePortLine(line, prefix string) ([]int, bool) {
	if !strings.HasPrefix(line, prefix) {
		return nil, false
	}
	open, close := strings.IndexByte(line, '{'), strings.IndexByte(line, '}')
	if open < 0 || close < 0 || close < open {
		return nil, false
	}
	var ports []int
	for _, tok := range strings.Split(line[open+1:close], ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(tok)); err == nil {
			ports = append(ports, n)
		}
	}
	return ports, true
}

func joinInts(ns []int) string {
	parts := make([]string, len(ns))
	for i, n := range ns {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ", ")
}

func dedupeSorted(ns []int) []int {
	seen := map[int]bool{}
	var out []int
	for _, n := range ns {
		if n > 0 && n < 65536 && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Ints(out)
	return out
}

func mapKeys(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
