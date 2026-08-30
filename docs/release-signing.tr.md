# İmzalı sürüm ve rollback-floor sözleşmesi

*[English](release-signing.md)*

Otomatik güncelleme güven kökü operatöre aittir. `celikpanel.net`, JSON dosyası,
`latest.txt`, HTTPS ve bitişik checksum dağıtım yardımcılarıdır; bunların hiçbiri
yetkili bir güncellemeyi seçemez veya yetkilendiremez.

## Canonical imzalı manifest v2

`release-manifest-v2.sig`, aşağıdaki sırayla tam on ASCII ve LF ile sonlandırılmış
satırın üzerindeki ham, ayrık Ed25519 imzasıdır:

    format=celikpanel-release-manifest-v2
    sequence=42
    version=v1.2.3-alpha.4
    commit=40-lowercase-hex-characters
    published_at=YYYY-MM-DDTHH:MM:SSZ
    os=linux
    arch=amd64
    archive=celikpanel-v1.2.3-alpha.4-linux-amd64.tar.gz
    archive_sha256=64-lowercase-hex-characters
    archive_size=positive-canonical-decimal

`sequence`, operatörün atadığı ve `1..9223372036854775807` aralığında monotonik
artan bir tamsayıdır; JavaScript sayısı olarak değil, her zaman metin olarak
taşınır. `published_at` yalnızca bilgi amaçlıdır ve hiçbir zaman sıralama
otoritesi değildir. Arşiv boyutu 2 GiB ile sınırlıdır. Sürüm, build metadata
içermeyen canonical SemVer'dir; sayısal prerelease tanımlayıcılarında baştaki
sıfırlara izin verilmez.

İmzalı nesneler değişmez, platforma göre ayrılmış yollar kullanır:

    /releases/VERSION/OS/ARCH/release-manifest-v2
    /releases/VERSION/OS/ARCH/release-manifest-v2.sig
    /releases/VERSION/OS/ARCH/celikpanel-VERSION-OS-ARCH.tar.gz
    /releases/VERSION/OS/ARCH/celikpanel-VERSION-OS-ARCH.tar.gz.sha256

İmzalayıcı, `bin/panel` ve `bin/agent` dosyalarının iddia edilen mimariye ait,
sınırlandırılmış regular ELF64 üyeleri olduğunu ve `go version -m` çıktısının
tam imzalı `GOOS`/`GOARCH` çiftini bildirdiğini kontrol eder. Bu nedenle izole
imzalama runner'ında build metadata okuyabilen bir Go aracı gerekir. Arşiv,
yayımlamadan hemen önce yeniden hash'lenir. Portal oluşturucu da kaynak/staged
byte eşitliğini kanıtlar ve staged kopyayı imzalamadan önce yeniden hash'ler.
Eski imzasız generic yollar ile altı argümanlı portal build'i değişmeden kalır.

## Operatör kapısı, CI ve portal assembly

Bu repoda production private key üretilmez veya saklanmaz. Canonical Ed25519
public key bilinçli olarak `deploy/release-signing-ed25519.pem` yolunda tracked
tutulur ve `download-portal/get.sh` tarafından sabitlenen public trust root'tur.
Her etiketli sürüm, `CELIKPANEL_RELEASE_SIGNING_ED25519_PEM` GitHub Actions
secret'ını ve monotonik `CELIKPANEL_RELEASE_SEQUENCE` repository variable'ını
gerektirir. İmzalayıcı private key'den public key'i türetir ve tracked PEM ile
tam byte eşitliği ister. Eksik key, sequence veya key uyuşmazlığı tag job'unu
fail-closed bitirir. Başarılı tag CI tam altı ürün yayımlar: generic
archive/checksum, linux/amd64 archive/checksum ve ayrık imzalı
manifest/signature.

Tercih edilen portal publisher private key almaz. Tamamlanmış tag job'undan altı
değişmez ürünü indirin; ek veya eksik dosya olmadığını, iki checksum dosyasını
ve generic ile platform arşivlerinin byte düzeyinde aynı olduğunu doğrulayın:

    (cd CI_ASSET_DIRECTORY && \
      sha256sum -c celikpanel-VERSION.tar.gz.sha256 && \
      sha256sum -c celikpanel-VERSION-linux-amd64.tar.gz.sha256 && \
      cmp -s celikpanel-VERSION.tar.gz \
        celikpanel-VERSION-linux-amd64.tar.gz)

Ardından dört yetkili platform ürününden yeni ve daha önce var olmayan bir
portal candidate oluşturun:

    CELIKPANEL_RELEASE_SIGNING=pre-signed \
    CELIKPANEL_RELEASE_SEQUENCE=42 \
    CELIKPANEL_RELEASE_TREE=EXACT_40_HEX_TAG_TREE \
    CELIKPANEL_RELEASE_OS=linux \
    CELIKPANEL_RELEASE_ARCH=amd64 \
    CELIKPANEL_RELEASE_SIGNED_MANIFEST_FILE=CI_ASSET_DIRECTORY/celikpanel-VERSION-linux-amd64.release-manifest-v2 \
    CELIKPANEL_RELEASE_SIGNED_SIGNATURE_FILE=CI_ASSET_DIRECTORY/celikpanel-VERSION-linux-amd64.release-manifest-v2.sig \
      deploy/build-download-portal.sh VERSION COMMIT PUBLISHED_AT \
        CI_ASSET_DIRECTORY/celikpanel-VERSION-linux-amd64.tar.gz \
        CI_ASSET_DIRECTORY/celikpanel-VERSION-linux-amd64.tar.gz.sha256 \
        NEW_PORTAL_CANDIDATE

`pre-signed` modu private-key environment variable'ını reddeder. Her girdiyi
açılmış bir descriptor'a sabitler; resmi dosya adlarını, tam canonical checksum
satırını, bütün release argümanlarıyla eşleşen tam on satırlı manifest'i ve
tracked PEM tarafından doğrulanan ham 64-byte imzayı ister. Ayrıca imzalı
arşivde LF ile sonlandırılmış tam `release.version`, `release.commit` ve
`release.tree` üyelerini gerektirir. Platform archive, checksum, manifest ve
signature byte düzeyinde kopyalanır ve staging sonrasında yeniden doğrulanır.
Generic compatibility archive bu tam platform archive'dan yeniden oluşturulur;
hiçbir zaman update otoritesi değildir.

Private-key `required` modu production portal host için değil, izole bir
imzalama runner'ı için kullanılabilir olmaya devam eder:

    CELIKPANEL_RELEASE_SIGNING=required \
    CELIKPANEL_RELEASE_SIGNING_KEY_FILE=/run/secrets/release-ed25519.pem \
    CELIKPANEL_RELEASE_SEQUENCE=42 \
    CELIKPANEL_RELEASE_OS=linux \
    CELIKPANEL_RELEASE_ARCH=amd64 \
      deploy/build-download-portal.sh VERSION COMMIT PUBLISHED_AT \
        PLATFORM_ARCHIVE PLATFORM_CHECKSUM OUTPUT_DIR

### Sürüm arşivi bootstrap doğrulaması

İmzalamadan veya yayımlamadan önce reproducible `make dist` çıktısının,
incelenmiş ilk kurulum kodunu ve trust root'u dönüşüm olmadan içerdiğini
doğrulayın:

    root=celikpanel-VERSION
    tar -xOzf PLATFORM_ARCHIVE "$root/libexec/get.sh" \
      | cmp -s - download-portal/get.sh
    tar -xOzf PLATFORM_ARCHIVE "$root/install.sh" \
      | cmp -s - install.sh
    tar -xOzf PLATFORM_ARCHIVE "$root/deploy/release-signing-ed25519.pem" \
      | cmp -s - deploy/release-signing-ed25519.pem

Ayrıca bu üç yol ile `release.version`, `release.commit` ve `release.tree`
için tam ve tek regular archive üyesi gerektirin. Arşiv, doğrulanmış imzalı
manifest'te digest ve boyutu bulunan tam arşiv olmalıdır. Portal publication,
aynı dosya sisteminde ayrı bir atomic exchange olmaya devam eder; candidate
canlı ağaca hiçbir zaman merge edilmez, bütün önceki sürümler ve rollback backup
korunur.

### Production portal publication

`deploy/publish-download-portal.ps1` desteklenen tek production portal giriş
noktasıdır. Sürüme özel promoter'lar ve `.tmp/` altında assembled scriptler
release mekanizması değildir. Tracked publisher tam generic promoter byte'larının
snapshot'ını alır, sınırlandırılmış public verifier ve candidate package'ı
upload edip sabitler, sonra atomic transaction için bu promoter'ı tam bir kez
stream eder.

Transaction, live exchange sonrasında bir public verification pass çalıştırır;
rollback backup yayımlandıktan sonra hiçbir pass çalıştırmaz. Bu pass eski bir
release altındaki yolu hiçbir zaman istemez: en fazla 15 GET isteği yapar, yeni
yetkili platform archive'ı tam bir kez getirir ve en fazla arşivin imzalı boyutu
artı 1 MiB response data'ya izin verir. Önceki release'ler ve rollback backup,
publication lock tutulurken pinned inode ve inventory üzerinden yerel olarak
kanıtlanır. Redirect, encoded response, eksik veya tam olmayan `Content-Length`,
değişmiş yerel byte'lar ile request veya byte budget aşımı transaction'ı
fail-closed bitirir.

## Kurulu trust material ve anti-rollback floor

Her release incelenmiş updater'ı `libexec/get.sh` olarak paketler; kurulum bu tam
byte'ları atomik olarak `/usr/libexec/celikpanel/get.sh` yolunda yayımlar.
Privileged worker portal'dan getirilen bir scripti değil, bu kurulu kopyayı
çalıştırmalıdır. Ed25519 public key yalnızca operatör kurulum sırasında
`CELIKPANEL_RELEASE_PUBLIC_KEY_FILE=/exact/path.pem` verdiğinde provision edilir.
Tam canonical byte'ları `/etc/celikpanel/release-signing-ed25519.pem` yoluna
atomik olarak kurulur; eksik input hiçbir zaman key oluşturmaz, değiştirmez veya
uydurmaz.

Güvenilir çağrı tamdır ve hiçbir zaman `latest` kullanmaz:

    /usr/libexec/celikpanel/get.sh --update --version VERSION \
      --require-signed-manifest --expected-sequence SEQUENCE \
      --minimum-sequence CURRENT_FLOOR \
      --expected-commit COMMIT \
      --expected-archive-sha256 ARCHIVE_SHA256 \
      --expected-archive-size ARCHIVE_SIZE

Panel içi worker ve update API hiçbir zaman ilk güveni seçmez. Bunun yerine
yeni kurulum, public ve release'e sabitlenmiş `download-portal/get.sh` üzerinden
girer: yalnızca embedded version ve sequence değerini kabul eder; portal-root
PEM'i indirir; embedded SHA-256 trust-anchor digest'ini, ayrık manifest'i,
archive digest/size değerlerini, internal checksum'ları ve release commit'ini
doğrular; ardından persistent update lock'u tutarken bu tam authenticated
identity'yi `install.sh` dosyasına geçirir. Installer kurulu panel/agent kimliği
kanıtlandıktan sonra aynı public key ve sequence floor'u preflight edip kaydeder.
`latest` bu akışı hiçbir zaman yetkilendirmez.

İlk kurulum trust enrollment sonrasında kesilirse yalnızca aynı release-pinned
bootstrap ve açık `--install` ile yeniden deneyin. Live portal sonraki bir
release'e ilerlediğinde, onun güncel `get.sh` dosyası eski floor'u kurtarmak için
kullanılmamalıdır. Varsa zaten kurulu `/usr/libexec/celikpanel/get.sh` dosyasını
kullanın veya tam eski değişmez release asset'ini doğruladıktan sonra bu
asset'ten `libexec/get.sh` çıkarın. Kurtarmayı zorlamak için `sequence.floor`
dosyasını hiçbir zaman düzenlemeyin veya düşürmeyin.

Enrollment sonrasında worker önceden var olan
`/var/lib/celikpanel-release-state/sequence.floor` dosyasını gerektirir ve
sequence değerini `--minimum-sequence` olarak geçirir. Normal upgrade'de bu
current floor, tam expected target sequence değerinden kesinlikle düşüktür; tam
same-sequence/same-version retry tek eşitlik durumudur.

### Mevcut sunucularda bir kerelik enrollment

Yeni Alpha41 ve sonrası kurulumlar yukarıdaki signed bootstrap akışını kullanır
ve ayrı bir manual enrollment gerektirmez. Mevcut bir kurulumda önce incelenmiş
panel/agent çiftini normal paired release prosedürüyle kurun veya yükseltin ve
iki binary'nin de amaçlanan build'i bildirdiğini doğrulayın. Salt okunur
`--inspect-build-identity` modunu sunmayan eski bir release enroll edilemez: önce
bu özelliği elle onaylanmış paired release olarak dağıtın, sonra kurulu release'i
enroll edin. İlk signed in-panel update kesinlikle daha büyük bir sequence
kullanmalıdır.

Helper'ı yalnızca kodu operatör tarafından incelenmiş authenticated checkout
veya verified release tree içinden çalıştırın. Portal'a hiçbir zaman bağlanmaz,
servis restart etmez ve `latest` değerini hiçbir zaman otorite kabul etmez:

    sudo bash deploy/enroll-signed-release-trust.sh \
      --sequence CURRENT_TRUSTED_SEQUENCE \
      --version CURRENT_INSTALLED_VERSION \
      --commit CURRENT_INSTALLED_40_HEX_COMMIT \
      --public-key-file /canonical/root-owned/release-ed25519-public.pem

Public-key kaynağı canonical, root:root, single-link olmalı ve group/other
writable olmamalıdır. Helper persistent update lock'u almadan önce ve aldıktan
sonra sabit kurulu `/opt/celikpanel/bin/panel` ve `/opt/celikpanel/bin/agent`
probe'larını çalıştırır. İki version/commit kimliği birbiriyle ve operatörün her
açık argümanıyla eşleşmelidir. Ancak bundan sonra tam key ve üç satırlı floor'u
atomik olarak yayımlar; dosyaları ve parent directory'leri fsync eder. Tam retry
idempotent'tır. Farklı key, daha düşük veya daha yüksek sequence, başka version
ile aynı sequence, uyuşmayan binary çifti, güvensiz metadata veya eşzamanlı
update, trust değiştirilmeden reddedilir.

Enrollment güncel release'i geriye dönük olarak signed yapmaz. Bir sonraki
signed release ilerletebilsin diye operatörün onayladığı current floor'u kaydeder.
Private Ed25519 key'i bütün sunucuların dışında tutun; yalnızca public key enroll
edilir. Aynı açık prosedürü, paired build identity doğrulandıktan sonra her
sunucuda ayrı ayrı çalıştırın.

Floor, root:root mode-0700 directory altında root:root mode-0600 regular
single-link dosyadır. Tam üç satırlı biçimi şöyledir:

    format=celikpanel-release-sequence-floor-v1
    sequence=42
    version=v1.2.3-alpha.4

Etkin minimum, worker tarafından sağlanan current floor ile bağımsız olarak
yeniden okunan durable floor'un maksimumudur. Daha düşük sequence reddedilir.
Aynı sequence yalnızca aynı version için kabul edilir ve güvenli retry sağlar;
başka version ile aynı sequence reddedilir. Root-owned nonblocking flock bütün
signed yolu serialize eder; böylece eşzamanlı trusted invocation'lar floor'ları
sıra dışı yayımlayamaz. Signature, platform, size, digest, internal checksum ve
`release.commit` kontrollerinin tümü geçtikten sonra—ancak herhangi bir host
mutation'dan önce—updater floor'u atomik olarak ilerletir ve fsync eder. Signed
download'lar redirect'i reddeder.

Installer persistent `/var/lib/celikpanel-release-state/update.lock` dosyasını,
tam root:root mode-0700 state directory altında root:root mode-0600, zero-byte,
single-link dosya olarak atomik biçimde önceden provision eder. Updater yalnızca
önceden var olan bu inode'u açar ve nonblocking-flock uygular; lock'u hiçbir
zaman oluşturmaz veya değiştirmez. Descriptor bootstrap ve apply-only kurulum
boyunca inherited kalır; installer yalnızca aynı yolu yeniden doğrular ve bu
nedenle recursive flock uygulayıp self-deadlock oluşturmaz.

Linux agent RPC ile admin-only panel API/UI bu sözleşmeyi kullanır. Check ve
start, pinned key veya canonical pre-existing floor eksik, güvensiz ya da
tutarsız olduğunda fail-closed davranır. UI hiçbir zaman URL, filesystem path,
command, sequence, commit, digest veya size sağlamaz; yalnızca trusted check
yolunun döndürdüğü tam signed identity'yi onaylayıp gönderebilir.

Snapshot schema v6 kurulu incelenmiş updater'ı ve present/absent durumunu içerir.
Apply-only installer tam trusted-release kopyasını yalnızca `update.sh` önceki
byte'ları durable olarak kaydettikten sonra yayımlar ve update completion kurulu
kopyayı kanıtlar. Rollback bunu verified snapshot'tan geri yükler veya kaldırır;
metadata ve byte'larını kanıtlayarak old agent/new updater mixed-version
durumunu önler.
