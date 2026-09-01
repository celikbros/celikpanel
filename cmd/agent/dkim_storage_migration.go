package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Why the DKIM store lives outside /etc/celikpanel.
//
// OpenDKIM runs as its own unprivileged user and must read the private signing
// keys. The keys used to sit at /etc/celikpanel/dkim, and /etc/celikpanel is
// root:celikpanel 0750 — a mode the security audit pins exactly — so the only
// way to let opendkim reach them was to add opendkim to the celikpanel group.
//
// That group is the wrong tool. /etc/celikpanel also holds agent.token, the
// single credential that authorises every root RPC the agent exposes. Group
// membership is not per-file: it hands opendkim read access to the whole
// directory. OpenDKIM parses mail arriving from the internet; a flaw in it would
// have turned into the agent token, and the agent token is root. One DKIM key is
// a small loss, the agent token is the whole machine, and the old arrangement
// traded the second away to buy the first.
//
// The store therefore moves to /var/lib/celikpanel-dkim, owned root:opendkim.
// The chain above it (/var, /var/lib) is world-traversable, so opendkim reaches
// its keys through its OWN group and needs membership nowhere else. Nothing but
// DKIM material lives under that root, so the blast radius of the daemon being
// compromised is the DKIM keys — which is the correct blast radius.
//
// DKIM deposu neden /etc/celikpanel dışında yaşıyor.
//
// OpenDKIM kendi yetkisiz kullanıcısı olarak çalışır ve özel imzalama
// anahtarlarını okumak zorundadır. Anahtarlar eskiden /etc/celikpanel/dkim
// altındaydı ve /etc/celikpanel root:celikpanel 0750'dir — güvenlik denetiminin
// tam olarak sabitlediği bir kip — dolayısıyla opendkim'in onlara ulaşmasının
// tek yolu opendkim'i celikpanel grubuna eklemekti.
//
// Bu grup yanlış araçtır. /etc/celikpanel aynı zamanda agent.token'ı barındırır;
// agent'ın sunduğu her root RPC'sini yetkilendiren tek kimlik bilgisi odur. Grup
// üyeliği dosya başına değildir: opendkim'e bütün dizinin okuma iznini verir.
// OpenDKIM internetten gelen postayı ayrıştırır; ondaki bir açık agent
// token'ına, agent token'ı da root'a dönüşürdü. Bir DKIM anahtarı küçük bir
// kayıptır, agent token'ı ise bütün makinedir; eski düzen birincisini almak için
// ikincisini takas ediyordu.
//
// Bu yüzden depo /var/lib/celikpanel-dkim'e taşınır, sahibi root:opendkim.
// Üstündeki zincir (/var, /var/lib) herkesçe geçilebilir olduğundan opendkim
// anahtarlarına KENDİ grubu üzerinden ulaşır ve başka hiçbir yerde üyeliğe
// ihtiyaç duymaz. O kökün altında DKIM malzemesinden başka bir şey yaşamaz;
// dolayısıyla daemon ele geçirilirse patlama yarıçapı DKIM anahtarlarıdır — ki
// doğru patlama yarıçapı budur.
const (
	dkimStoreRoot = "/var/lib/celikpanel-dkim"

	// productionDKIMKeyDir is the one destination a real server ever uses. The
	// migration below deletes directories under /etc, so it must be certain it
	// is running on a real server and not inside a test or a developer's
	// redirected store — and the only honest way to be certain is to compare
	// the live store path against this.
	// productionDKIMKeyDir, gerçek bir sunucunun kullandığı tek hedeftir.
	// Aşağıdaki taşıma /etc altındaki dizinleri siler; bu yüzden gerçek bir
	// sunucuda çalıştığından emin olmalıdır, bir testin ya da bir geliştiricinin
	// yönlendirilmiş deposunun içinde değil — ve emin olmanın tek dürüst yolu,
	// canlı depo yolunu bununla karşılaştırmaktır.
	productionDKIMKeyDir = dkimStoreRoot + "/keys"
)

// legacyDKIMKeyDir and legacyDKIMTablesDir are where the store used to be. They
// are only ever read, and only in order to empty them. They are variables
// rather than constants for one reason: a test must be able to stand up a fake
// legacy store, because a migration nobody can exercise is a migration nobody
// knows works — and this one runs exactly once per upgraded server, on
// production keys, with no second chance.
// legacyDKIMKeyDir ve legacyDKIMTablesDir deponun eskiden bulunduğu yerdir.
// Yalnızca okunurlar, o da yalnızca boşaltmak için. Sabit değil değişken
// olmalarının tek sebebi şudur: bir test sahte bir eski depo kurabilmelidir,
// çünkü kimsenin sınayamadığı bir taşıma, kimsenin çalıştığını bilmediği bir
// taşımadır — ve bu, yükseltilen her sunucuda tam bir kez, üretim anahtarları
// üzerinde, ikinci şans olmadan çalışır.
var (
	legacyDKIMKeyDir    = "/etc/celikpanel/dkim"
	legacyDKIMTablesDir = "/etc/celikpanel/dkim-tables"
)

var (
	dkimMigrationMu   sync.Mutex
	dkimMigrationDone bool
)

// ensureDKIMStorageMigrated moves an existing store to its new home before any
// DKIM operation reads or writes one.
//
// Every DKIM entry point must call it, and its failure must stop that entry
// point. Continuing past a half-finished move would look like a server with no
// DKIM keys: the tables would be regenerated empty and OpenDKIM would stop
// signing for every domain, silently, with the keys still on disk one directory
// away. Refusing is visible; signing nothing is not.
//
// ensureDKIMStorageMigrated, herhangi bir DKIM işlemi bir depoyu okumadan ya da
// yazmadan önce mevcut depoyu yeni evine taşır.
//
// Her DKIM giriş noktası bunu çağırmalı ve başarısızlığı o giriş noktasını
// durdurmalıdır. Yarım kalmış bir taşımanın ötesine geçmek, hiç DKIM anahtarı
// olmayan bir sunucu gibi görünürdü: tablolar boş olarak yeniden üretilir ve
// OpenDKIM her alan adı için imzalamayı sessizce bırakırdı; oysa anahtarlar bir
// dizin ötede diskte durmaktadır. Reddetmek görünürdür, hiçbir şey imzalamamak
// değildir.
func ensureDKIMStorageMigrated() error {
	dkimMigrationMu.Lock()
	defer dkimMigrationMu.Unlock()
	if dkimMigrationDone {
		return nil
	}
	if dkimBaseDir != productionDKIMKeyDir {
		// Not a real server: CELIKPANEL_DKIM_DIR redirected the store, or a
		// test assigned dkimBaseDir directly. Either way the legacy paths
		// below belong to somebody else's machine, and removing them would be
		// a test destroying production state.
		// Gerçek bir sunucu değil: CELIKPANEL_DKIM_DIR depoyu yönlendirmiş ya
		// da bir test dkimBaseDir'i doğrudan atamış. Her iki durumda da
		// aşağıdaki eski yollar başkasının makinesine aittir ve onları
		// kaldırmak, bir testin üretim durumunu yok etmesi olurdu.
		dkimMigrationDone = true
		return nil
	}
	if err := migrateLegacyDKIMStore(dkimBaseDir); err != nil {
		// Deliberately not cached: a migration blocked by a transient fault
		// must be retried on the next call, not turned into a permanent
		// refusal for the life of the process.
		// Bilerek önbelleğe alınmaz: geçici bir arızayla engellenen taşıma bir
		// sonraki çağrıda yeniden denenmelidir, sürecin ömrü boyunca kalıcı bir
		// redde dönüşmemelidir.
		return err
	}
	dkimMigrationDone = true
	return nil
}

// migrateLegacyDKIMStore is the move itself, with the destination injected so a
// test can exercise it without being root.
// migrateLegacyDKIMStore taşımanın kendisidir; hedef enjekte edilir, böylece bir
// test onu root olmadan sınayabilir.
func migrateLegacyDKIMStore(destination string) error {
	if destination == "" || destination == legacyDKIMKeyDir {
		return errors.New("DKIM store migration: destination is still the legacy directory")
	}
	legacy, err := os.Stat(legacyDKIMKeyDir)
	if err != nil {
		if os.IsNotExist(err) {
			// Nothing to move. Every fresh install lands here.
			// Taşınacak bir şey yok. Her yeni kurulum buraya iner.
			return removeLegacyDKIMTables()
		}
		return fmt.Errorf("inspect legacy DKIM key directory: %w", err)
	}
	if !legacy.IsDir() {
		return fmt.Errorf("legacy DKIM key path %q is not a directory", legacyDKIMKeyDir)
	}

	if err := secureMkdirAll(destination, 0o750); err != nil {
		return fmt.Errorf("create DKIM key directory: %w", err)
	}
	entries, err := os.ReadDir(legacyDKIMKeyDir)
	if err != nil {
		return fmt.Errorf("read legacy DKIM key directory: %w", err)
	}
	moved := 0
	for _, entry := range entries {
		from := filepath.Join(legacyDKIMKeyDir, entry.Name())
		to := filepath.Join(destination, entry.Name())
		if _, err := os.Lstat(to); err == nil {
			// Already carried over by an earlier attempt. Two directories for
			// one domain is ambiguous, and guessing which key is current is
			// exactly the kind of guess that breaks mail signing — so the copy
			// already at the destination wins and the legacy one is left in
			// place for an operator to look at.
			// Daha önceki bir denemede taşınmış. Tek bir alan adı için iki dizin
			// belirsizdir ve hangi anahtarın güncel olduğunu tahmin etmek, posta
			// imzalamayı bozan türden bir tahmindir — bu yüzden hedefte zaten
			// duran kopya kazanır, eski olan bir operatörün bakabilmesi için
			// yerinde bırakılır.
			continue
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect DKIM destination %q: %w", to, err)
		}
		if err := os.Rename(from, to); err != nil {
			return fmt.Errorf("move DKIM material for %q: %w", entry.Name(), err)
		}
		moved++
	}
	// Only remove the legacy directory when it is genuinely empty. A leftover
	// entry means something above declined to move, and removing the directory
	// then would destroy a signing key — so a non-empty directory is left
	// standing rather than forced.
	// Eski dizin yalnızca gerçekten boşsa kaldırılır. Kalan bir girdi, yukarıda
	// bir şeyin taşınmayı reddettiği anlamına gelir; dizini o hâldeyken silmek
	// bir imzalama anahtarını yok ederdi — bu yüzden boş olmayan bir dizin
	// zorlanmaz, ayakta bırakılır.
	if err := os.Remove(legacyDKIMKeyDir); err != nil && !os.IsNotExist(err) && !isDirectoryNotEmptyError(err) {
		return fmt.Errorf("remove legacy DKIM key directory: %w", err)
	}
	if moved > 0 {
		log.Printf(
			"DKIM: moved signing material for %d domain(s) out of %s into %s; "+
				"opendkim no longer needs read access to the agent token directory",
			moved, legacyDKIMKeyDir, destination,
		)
	}
	return removeLegacyDKIMTables()
}

// isDirectoryNotEmptyError recognises the one os.Remove failure that is not a
// fault here. It is matched on text because the errno differs by platform
// (ENOTEMPTY on Linux, EEXIST on some others) and this is a diagnosis, not a
// control-flow decision that anything unsafe depends on.
// isDirectoryNotEmptyError, burada arıza sayılmayan tek os.Remove
// başarısızlığını tanır. Metne bakarak eşleştirilir çünkü errno platforma göre
// değişir (Linux'ta ENOTEMPTY, bazılarında EEXIST) ve bu bir teşhistir, güvenlik
// açısından bir şeyin dayandığı akış kararı değil.
func isDirectoryNotEmptyError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "not empty")
}

// removeLegacyDKIMTables deletes the old table directory. Tables are derived
// state — regenerated from the key directory on every configure — so removing
// them loses nothing, while leaving them under /etc/celikpanel would keep a
// reason for opendkim to want that group.
// removeLegacyDKIMTables eski tablo dizinini siler. Tablolar türetilmiş
// durumdur — her yapılandırmada anahtar dizininden yeniden üretilir — bu yüzden
// silinmeleri hiçbir şey kaybettirmez; /etc/celikpanel altında bırakılmaları ise
// opendkim'in o grubu istemesi için bir sebep olarak kalırdı.
func removeLegacyDKIMTables() error {
	if err := os.RemoveAll(legacyDKIMTablesDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove legacy DKIM table directory: %w", err)
	}
	return nil
}

// dropOpenDKIMFromPanelGroup undoes the membership the old arrangement created.
//
// It runs on every signing configure rather than once, because the membership
// can come back: reinstalling opendkim, restoring /etc/group from a backup, or
// running an older agent binary once would each re-add it. A privilege this
// system removed must stay removed without an operator remembering to check.
//
// Not being a member is the goal, so gpasswd failing because there is nothing to
// remove is success. A real failure is reported as a warning rather than a
// refusal: DKIM signing works without the membership, which is the entire point,
// and refusing here would break mail to punish a condition that predates us.
//
// dropOpenDKIMFromPanelGroup, eski düzenin oluşturduğu üyeliği geri alır.
//
// Bir kez değil her imzalama yapılandırmasında çalışır; çünkü üyelik geri
// gelebilir: opendkim'i yeniden kurmak, /etc/group'u yedekten geri yüklemek ya
// da eski bir agent ikilisini bir kez çalıştırmak onu yeniden eklerdi. Bu
// sistemin kaldırdığı bir ayrıcalık, bir operatörün kontrol etmeyi
// hatırlamasına gerek kalmadan kaldırılmış kalmalıdır.
//
// Üye OLMAMAK hedeftir; dolayısıyla gpasswd'nin kaldıracak bir şey olmadığı için
// başarısız olması başarıdır. Gerçek bir arıza, ret olarak değil uyarı olarak
// bildirilir: DKIM imzalama üyelik olmadan çalışır, bütün mesele budur; burada
// reddetmek, bizden önce gelen bir durumu cezalandırmak için postayı bozardı.
// openDKIMPanelGroupIsRedundant answers the one question that decides whether
// the celikpanel group may be taken away from opendkim: are the keys somewhere
// opendkim can reach without it? Only the production store is.
// openDKIMPanelGroupIsRedundant, celikpanel grubunun opendkim'den alınıp
// alınamayacağına karar veren tek soruyu yanıtlar: anahtarlar, opendkim'in o
// grup olmadan ulaşabileceği bir yerde mi? Yalnız üretim deposu öyledir.
func openDKIMPanelGroupIsRedundant() bool {
	return dkimBaseDir == productionDKIMKeyDir
}

func dropOpenDKIMFromPanelGroup(ctx context.Context) {
	// Never drop the membership while the keys still need it.
	//
	// The group is only redundant once the store actually lives at
	// productionDKIMKeyDir. If the store is still under /etc/celikpanel — an
	// operator override, or an agent whose unit file has not been replaced yet
	// — then removing opendkim from the celikpanel group takes away the only
	// way it can traverse /etc/celikpanel (root:celikpanel 0750) and reach its
	// own keys. Signing would stop for every domain while the DNS records keep
	// advertising the public halves, which is the loudest possible way to
	// break mail with a change meant to harden it.
	//
	// This is not hypothetical: the shipped agent unit pinned
	// CELIKPANEL_DKIM_DIR to the legacy path, so the migration stood down while
	// this ran unconditionally. The unit no longer pins it, and this guard
	// makes the two impossible to separate again.
	//
	// Anahtarlar hâlâ ona muhtaçken üyeliği asla düşürme.
	//
	// Grup, ancak depo gerçekten productionDKIMKeyDir altında yaşadığında
	// gereksizdir. Depo hâlâ /etc/celikpanel altındaysa — bir operatör
	// geçersiz kılması ya da unit dosyası henüz değiştirilmemiş bir agent —
	// opendkim'i celikpanel grubundan çıkarmak, onun /etc/celikpanel'i
	// (root:celikpanel 0750) geçip kendi anahtarlarına ulaşabildiği tek yolu
	// elinden alır. DNS kayıtları genel yarıları duyurmaya devam ederken her
	// alan adı için imzalama dururdu; postayı sertleştirmesi gereken bir
	// değişiklikle bozmanın en gürültülü yolu budur.
	//
	// Bu varsayımsal değildir: sevk edilen agent unit'i CELIKPANEL_DKIM_DIR'i
	// eski yola sabitliyordu, dolayısıyla taşıma devre dışı kalırken bu kod
	// koşulsuz çalışıyordu. Unit artık onu sabitlemiyor ve bu nöbet ikisinin
	// yeniden ayrılmasını imkânsız kılıyor.
	if !openDKIMPanelGroupIsRedundant() {
		return
	}
	out, err := serviceMutationCommand(ctx, "gpasswd", "-d", "opendkim", "celikpanel").CombinedOutput()
	if err == nil {
		log.Printf("DKIM: removed opendkim from the celikpanel group; it no longer reads the agent token")
		return
	}
	detail := firstLine(string(out))
	if strings.Contains(strings.ToLower(detail), "not a member") {
		return
	}
	log.Printf(
		"DKIM: opendkim may still be a member of the celikpanel group and could not be "+
			"removed; while it is, a compromise of opendkim reaches the agent token. "+
			"Remove it by hand with: gpasswd -d opendkim celikpanel (%v: %s)",
		err, detail,
	)
}
