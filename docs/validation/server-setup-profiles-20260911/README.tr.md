# Gerçek sunucuda kurulum profili doğrulaması

*11 Eylül 2026 · [English](README.md)*

Dört amaç da geçici Debian 13 sanal makinelerinde tamamlandı: web, uygulama,
web+posta ve bağımsız primary/secondary çiftiyle DNS. Üç barındırma profilinde
gerçek Certbot zamanlayıcısıyla sertifika yenilemesi geçti. DNS çiftinde gerçek
alan adı aktarımı ve eşleşen yetkili SOA/A yanıtları doğrulandı.
Bunlar geliştirme sonuçlarıdır; sürüm yayını veya kurulu panel güncellemesi değildir.

## Doğrulanan davranış

| Profil | Kurulum ve hazır olma | Yenileme |
|---|---|---|
| Web | Nginx, PHP ve MariaDB; güvenilir panel HTTPS; harici DNS; güvenlik duvarı; güncel son kontroller. Posta servisleri kurulmadı. | Kurulu Certbot zamanlayıcısı ve dağıtım kancası çalıştıktan sonra yeni güvenilir panel sertifikası sunuldu. |
| Uygulama | Node.js ve Nginx; güvenilir panel HTTPS; harici DNS; güvenlik duvarı; güncel son kontroller. Seçilmeyen posta, PHP veya MariaDB kurulmadı. | Zamanlayıcı ve kancadan sonra yeni güvenilir panel sertifikası sunuldu. |
| Web ve posta | Web servisleri, Postfix, Dovecot, Roundcube ve Rspamd; panel ve posta TLS; DNS kimliği ve teslimat kontrolleri; güvenlik duvarı; sekiz güncel kontrol hazır. | İki sertifika da yenilendi. SMTP gönderim 587 ve IMAPS 993 aynı yeni posta sertifikasını, panel 2083 yeni panel sertifikasını sundu. |

DNS primary ve secondary, beş güncel kontrolün tamamı hazır olarak kalıcı
işlemlerini tamamladı: HTTPS, yenileme, DNS, güvenlik duvarı ve servisler.
İkisinde de güvenilir HTTPS ve yenileme hazırlığı doğrulandı; gerçek zamanlayıcı
yenilemesi yukarıdaki üç barındırma profilinde ayrıca çalıştırıldı.

Yinelenen açık başlatma isteği aynı kalıcı işlemi döndürdü. Yenileme, kurulum
revizyonunu, tamamlanma zamanını ve alt işlem satırlarını değiştirmedi. Web
makinesinde imzalı süresi dolmuş lisans yanıtıyla yönetim API'si, yenilemeden önce
ve sonra `403 license_required` döndürdü; Nginx, PHP ve MariaDB çalışmaya devam
etti. Lisans kilidi sertifika yenilemesini engellemedi.

[Makine tarafından okunabilen sonuçlar](results.json) ve [hosting](hosting/)
altındaki `execution-evidence.json`, `renewal-before.json`, `renewal-after.json`,
`final-profile-proof.json` dosyaları kanıtları içerir. Toplanan dosyalar
[artifact özetleriyle](artifact-sha256.json) tanımlanır.

DNS çiftinin sonucu [final-native-results.json](dns/final-native-results.json)
içindedir. Primary'nin [son kanıtı](dns/dnsprimary/final-execution-evidence.json)
alan adı yanıtlarını ve salt-okur aktarım gözlemlerini;
secondary'nin [kanıtı](dns/dnssecondary/execution-evidence.json) tamamlanan
işlemini içerir. Secondary başlatılmadan önce primary DNS adımı tamamlandı.

## Ortam ve kapsam

HTTP süreci, yetkisiz panel hesabıyla çalışan ve açıkça etkinleştirilen Go test
sunucusuydu. Gerçek HTTPS, oturum/CSRF kontrolleri, üretim kurulum işleyicileri,
SQLite ve yeniden başlatma kurtarması kullanıldı. Sunucu değişikliklerini gerçek
yetkili agent ve mevcut işlem protokolü yürüttü. Kurulu panel ikilisi yoktu;
sunucu adı, test işareti ve QEMU kimliği yerleştirme öncesinde doğrulandı.
Boston, Frankfurt veya başka bir kullanıcı paneli güncellenmedi.

Her makinenin ayrı, izole ACME hizmeti ve güven kökü vardı. Certbot gerçek HTTP-01
doğrulaması ve sertifika üretimi yaptı. Yalnız testteki zamanlayıcı ayarları
yenilemeyi hemen çalıştırdı; kayıtlı ACME adresi, doğrulama eklentileri ve dağıtım
kancaları kullanıldı. Sonrasında asıl zamanlayıcı ayarları geri getirildi. DNS,
ters DNS ve SMTP bağlantı hedefi geçici makinelerde denetlendi. Posta makinesi,
genel ağ kimliğini modellemek için TEST-NET adresi ve yerel çözümleyici kullandı.
Bu sonuç genel sertifika otoritesinden üretimi, internet posta teslimatını veya
genel ad sunucusu delegasyonunu kanıtlamaz.

## Korunan başarısız denemeler ve düzeltmeler

Önceki denemeler `attempt-1` ve `attempt-2` altında saklanır; başarılı koşu olarak
yeniden adlandırılmadılar. Test başlangıç kimliği, çözümleyici ve paket
yapılandırması sorunları üçüncü temiz makine grubundan önce düzeltildi.

Gerçek sertifika üretimi bir üretim hatasını gösterdi: Certbot agent'ın ortak
grubunu devralıyordu. Artık gerçek alt süreç UID/GID 0 ve ek grup olmadan çalışır.
Açık yeniden üretim yalnız doğrulanmış, sabit yönetilen dizinlerin eski grup
bilgisini düzeltebilir; eski sertifika/anahtar dosyaları ve diğer sertifika
dizinleri benimsenmez veya değiştirilmez. Katı sertifika kaynak kontrolü korunur.

Posta doğrulamasında sunucu adı işleminin `/etc/hosts` kaydının doğru harici DNS
sonucunu gizlediği görüldü. Olumsuz sonuç
`hosting/webmail/readiness-before-mail-dns-fix.json` içinde korunur. Sınırlı
doğrudan DNS sorguları artık PTR ve ileri yön kayıtlarını, sınıfsız ters DNS CNAME
delegasyonu dahil doğrular. Yalnız geçici makinenin agent'ı değiştikten sonra aynı
bekleyen işlem hazır oldu; kurulum veya sertifika üretimi tekrarlanmadı. Önceki
ve sonraki alt işlem kimlikleri ve aday derleme bilgileri posta kanıtıyla saklanır.

İlk DNS çifti denemesinde BIND secondary katalog söz dizimi ve belirsiz kalan
başarısız alt işlemin kurtarılmasıyla ilgili hatalar bulundu. İlk durumlar,
günlükler, kapatma kaydı ve gerçek BIND yapılandırma denetimi
`dns-attempt-1/` altında korunur. Katalog alanı bildirimi artık alan yapılandırmasında,
abonelik ise yönetilen genel options bloğundadır. Yeni makbuzlar ayrı yapılandırma
sürümüne sahiptir; eski nesiller güncel sayılmadan tam kimlikleriyle kurtarılabilir.
Yönetilmeyen yapılandırma sessizce değiştirilmez. Genel ad sunucusu kontrolleri
yerel hosts kayıtlarından bağımsız, sınırlı doğrudan A ve AAAA sorguları kullanır.

Son temiz DNS çifti düzeltilmiş kaynakla tamamlandı. Alan adı oluşturulduktan
sonra test istemcisi, mevcut API'nin `DomainID` yanıtı yerine `domain_id` beklediği
için durdu. Bu denetimin günlüğü ve sürücüsü korundu. Salt-okur devam testi daha
önce oluşturulmuş alan adını doğruladı; alan adı oluşturma, servis kurulumu veya
ilk kurulum tekrarlanmadı. [Devam kaydı](dns/driver-continuation-record.json) ayrıntıları içerir.

## Kaynak kökeni

İlk tamamlanan web/uygulama koşuları `81727ee4…` kaynak anlık görüntüsünü ve
`db67d54c…` agent'ını kullanır; tam manifestleri `hosting/` altındadır. Posta
kurtarması `31b1185c…` kaynak anlık görüntüsü ve `64ad8615…` agent'ıyla yapılmıştır;
ayrı kayıtları `candidate-recovery/` altındadır. Önceki kayıtların üzerine
yazılmadı. Ortak `64a000…` derleme damgası test kimliğidir; Git revizyonu veya sürüm değildir.

Son DNS çifti `a08aa344…` kaynak görüntüsünü, `3a924da5…` agent'ını ve
`98550cbe…` test sunucusunu kullanır. Tam manifestler ve ikili dosya özetleri
`candidate-bind/` altındadır. Bu görüntüde panel, agent, BIND ve ortak DNS testleri
ile tüm paketlerin vet denetimi geçti. Ortak DNS kodu önceki posta davranışını ve
odaklı race testlerini korur. Önceki profil koşularının kaynak kapsamı ayrıdır.
Son beş sanal makine, kanıtları ve diskleri korunarak kapatıldı.

[Kaynak ve arayüz kontrolleri](../server-setup-remote-dns-20260911/README.tr.md),
[üç işletim sisteminde güvenlik duvarı/yeniden başlatma kanıtı](../server-setup-firewall-20260911/README.md)
ve [önceki bağımsız posta TLS kanıtı](../server-setup-mail-20260911/README.md)
ayrı kapsamlara ve günlüklere sahiptir. `fixtures/` altındaki sürücüler korumalı,
geçici sanal makine ortamını gerektirir.
