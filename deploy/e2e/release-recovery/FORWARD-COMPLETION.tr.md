# Aday verisi eksikken gerçek güncelleme tamamlama

*15 Eylül 2026 · [English](FORWARD-COMPLETION.md) · D-025 / P0.3*

Bu kayıt [kaynak sözleşmesini](../../../docs/RECOVERY-FORWARD-COMPLETION.tr.md)
tamamlar. İlk aday ile son başarı kanıtı düzeltmesini ayrı tutar. İki deneme de
P0.3'ü veya bütün gerçek iş yükü/hata matrisini kapatmaz.

## Başlangıç durumu ve kesinti sınırı

Her temiz Arch ve Debian 13 QEMU konuğuna gerçek imzalı Alpha75
`5aa03fd5b6775b21834ff7b1ce0695d92f50ae93` kurulur. Kabulden önce eski kurulu ve
çalışan programlar doğrulanır. Kurulu Agent yerel BIND kayıtlarını hazırlar;
UDP/TCP A ve SOA yanıtları, başlangıç TLS'i, WAL-aware gözlemle 65 SQLite tablosu
ve sahibin değiştirdiği koordinatör servis dosyalarının önceki halleri kaydedilir.
Veritabanında migration 1–42 zaten bulunur. Hazır olma kontrolü kanıtlanır; yeni
veritabanı sürümü dönüşümü kanıtlanmaz. Müşteri sunucusu hedef değildir.

Yerel aday kayıtlı kaynaktan Go 1.26.5 ile derlenir. İmzasız yerel arşivdir;
imzalı Agent/panel arayüzü kabul yolu değildir. Kayıtlı denetleyici tam VM
UUID/nonce/DMI, ana makinedeki QEMU süreci, işlem, işçi invocation/PID/başlangıç zamanı/cgroup,
program ve yerel flock kimliklerini doğrular. Her konukta tek güncelleme kabul
edilir. İşçi ürünün OnFailure kurtarma ilişkisini kullanır; test kurtarmayı
başlatmaz, sahibin geri alma komutunu çalıştırmaz.

`completion.pending` aşamasındaki günlük satırı yalnız gözlem aralığını buldurur.
Tam işçi dondurulmuşken eksiksiz yedek, seçili kurtarma ortamı, material v2 ve
bağımsız katı veritabanı kontrolü doğrulanır. Ardından saklanan adaydan tam üç
dosya karantinaya alınır: `rollback.sh`, `bin/agent`, `web/dist/index.html`.
Asılları ve özetleri test karantinasında kalır; kurulu dosyalar, yedek ve kurtarma
kiti değiştirilmez. Tam güncelleyici cgroup'una bir kez SIGKILL gönderilir. Geçici
loopback 2083 dinleyicisi yalnız kontrol noktası aralığını genişletir, sonra
kapatılır; kendisi kurtarma kanıtı değildir.

## O denemesi: son kanıt düzeltmesinden önceki kaynak

Aday `c500908739a00540fcc4a3da64a1264b8dcdc27c`, ağaç
`a7a2af338a850ff5e31e0d3b6af258bce5aa1fa7`, arşiv SHA-256
`f7f2230029dad527bde299830a0aeba1967da51ec6d6922f38bd6970c2d90187`.
Denetleyici `c58773d59d0f4ae0d67a6f80d4274068b82f258d`; sonraki değişikliği yalnız
port bağlandıktan sonraki gözlem hata verirse test dinleyicisini kapatır.
Özel kanıt kökü: `/var/tmp/cp-release-drill-20260915-o/evidence/<node>`.

| Kimlik | Arch | Debian 13 |
|---|---|---|
| İşlem | `131bcd11ab62d0c27ba42c708533c0e8` | `73b223fdad076ff01e6f8d99fe610552` |
| Mühürlü indeks SHA-256 | `38140a5ce5c868d31b5dc6e5b53bcc33a5935c4b4f0ac2212255dfa27f3316ff` | `d6d14217ee0be520a58edca358b72c3c05d9a8fc9de8dc170d5960b15b299947` |
| Sonuç SHA-256 | `4a9c6a7f6cc16b274a57eab1182c0d358f8febd77fcc972cfcf42d77364d6492` | `da3ab3bf5bd938ab01749bc92c492491e9928b6e90926d3e76a57a491cca6d85` |

İki sistemde de veritabanının hazır olduğu tamamlama noktası kanıtlandı, seçilen
üç aday dosyası kaldırıldı ve tam güncelleyici öldürüldü. Yerel günlükler işçinin
OnFailure olayını aynı kurtarma invocation'ına ve tam yedek için `Previous
pending update finalized` satırına bağlıyor. Önceki zamanlayıcı/kilit dolu
gözlemleri ayrı kaydediliyor; tamamlanma sayılmıyor. Son bağımsız gözlem hedefin
kurulu/çalışan özetlerini, etkin Panel/Agent'ı, işlem işaretlerinin yokluğunu,
129 doğrulanmış yedek dosyasını, değişmeyen 16 dosyalık kurtarma verisini ve seçili
ortamı doğruladı. İlk 296 aday dosyasının 293'ü kaldı; üç dosya son gözlemde de yoktu.

Yetkili DNS yanıtları ve sunulan başlangıç TLS'i eşleşti; loopback HTTPS 200 döndü.
Kurulu panel web dosyaları aday envanteriyle eşleşti. Canlı koordinatör servis
dosyaları yetkili adayın baytlarına geçti, servis yöneticisinin reload durumu
temizdi. Sahibin önceki servis dosyaları doğrulanmış yedekte korundu; canlı
servis dosyalarında **değişmeden korunmadı**.

65 veritabanı tablosunun tamamı karşılaştırıldı; dışlama yok. Genel sonuç yalnız
`metrics_samples` nedeniyle **DIFFERENT** kalır. Konukta ek salt-okur satır
karşılaştırması, Arch'ta 18 ve Debian'da 19 eski satırın aynı kaldığını, ikisine de
sonradan 15 satır eklendiğini buldu. Bağımsız inceleme programı ve mühürlü özeti
doğruladı; özel ham veritabanı satırları laboratuvar ana makinesinde yeniden
karşılaştırılmak üzere dışa aktarılmadı.

Arch'ın ilk birleşik son gözlem komutu yedek/veritabanı gözlemlerini kaydettikten
sonra 1 ile çıktı. Sarmalayıcı alt sürecin stderr'ını dışa aktarmadığından neden
bilinmiyor; ilk kanıt korunuyor. Gözlemci salt-okur olarak yeniden çalıştırılınca
kontrol geçti; iki okuma arasında onarım veya ek güncelleme/kurtarma komutu
çalıştırılmadı. Bu sonraki anlık gözlemdir; kesintisiz erişim
kanıtı değildir. İki izleyici de SIGKILL sonrasında `thaw_exit=1` kaydetti;
dondurmanın kaldırılmasının doğrulandığı söylenmiyor.

Bağımsız inceleme yalnız laboratuvar ana makinesindeki 36 Arch ve 37 Debian mühürlü dosyayı yeniden
özetledi; sekiz olaylık işlem zincirini ve 26 sonuç kontrolünü doğruladı. Bu,
**c500908 için sınırlı PASS** sonucudur. Konuklar kayıtlı kimlik denetimleriyle
durduruldu; diskler ve özel kanıtlar korunuyor.

## Son başarı kanıtı düzeltmesi ve ayrı kabul

Kaynak incelemesi son tam dosya kanıtının panel başlangıcından önce kaldığını
buldu. Gerçek tamamlama akışından çıkarılan test, geç sahip web değişikliğine
rağmen 0 çıkışı ve tamamlanma mesajı üretti. Bu başarısız kanıt
`/var/tmp/cp-terminal-proof-red-xgtd7gr4/runtime-shell-contract.log` içinde korunur.

`0c338822faf7ad3ca450bb4534aecdeccb3f26fd`, kontrollü başlangıçlardan sonra ve son
kalıcı işaret silinmeden önce material-v2 kanıtını yeniler. Sekiz gerçek shell
akışı geç ürün/çalışma/etkinlik değişikliklerinin reddini ve kanıtın korunmasını,
ayrıca temiz tamamlama ve yalnız zamanlayıcı yollarını doğrular. Bunlar gerçek
konukta sahip değişikliği kabulü değildir. Son shell kanıtları
`/var/tmp/cp-terminal-proof-final-4syjdexe/` ve
`/var/tmp/cp-forward-final-shell.5gUb8y78/` içindedir.

O denemesi değişen kaynağı doğrulamaz. Aşağıdaki P denemesi temiz konuklar kullanır.

## P denemesi: düzeltilen kaynağın yerel hata kurtarmasıyla tamamlanması

Aday ve denetleyici `0c338822faf7ad3ca450bb4534aecdeccb3f26fd`, ağaç
`a95db6fbe0a670f3d6732d54163a51eceea5e5bb`. Arşiv SHA-256:
`cc9c9ac5a5172243ede22d2f4c7f3be8ec92e3e08f12255aee36ebea1573afbc`.
Özel kanıt kökü: `/var/tmp/cp-release-drill-20260915-p/evidence/<node>`.

| Kimlik | Arch | Debian 13 |
|---|---|---|
| İşlem | `ef61e2b627c1937204818678e5b72e2a` | `1f8eaf6e94a503e1384fbee8eab0c261` |
| Mühürlü indeks SHA-256 | `0919fd639cb51c30559fb5f439b0cd7cde9d84bd78a5519cd45259d244df12a1` | `5e5f7fe931701b21d80efd3c07e9d680819d34b8bcb97ebeb4583e40b510b5bb` |
| Sonuç SHA-256 | `48134596e1940b117ff3d101a8dc761595cc12a9f0680b4e05f690074f67ac89` | `09b1060a8c8ef83b4904f097c553663618a2bdf72f410ad8bd336eb147e74388` |

İki tam sekiz olaylık hata zinciri de veritabanı hazır tamamlama noktasına ulaştı,
aynı üç saklanan dosyayı karantinaya aldı ve yalnız güncelleyiciyi öldürdü. Yerel
OnFailure kurtarması aynı yedekleri 14 Eylül UTC'de (İstanbul'da 15 Eylül)
Arch'ta `23:36:38.846819Z`, Debian'da `23:36:48.795966Z` anında tamamladı.
Son kurtarma invocation'ları sırasıyla `a67a647d1e02442fa0e681a1f24b9212` ve
`1586fad75a24498bb407d37a5b2078df`. Sahip kurtarması veya ikinci güncelleme yapılmadı.

Son kanıt etkin hedefin kurulu/çalışan programlarını, tam aday web dosyalarını,
129 yedek dosyasını, değişmeyen 16 dosyalık kurtarma verisini ve aynı seçili ortamı
`b9cf85f8d6a7d60459af7bd8cef8fe252905a3db8b8bb62b96b9178afdbdc2e6` doğruladı.
Saklanan aday ağacında 293 dosya kaldı; karantinaya alınan üç dosya bu ağaçta yoktu. Dört işlem işareti de
yoktu. Anlık DNS/TLS gözlemleri eşleşti, loopback HTTPS 200 döndü. Servis dosyasının
önceki hali/canlı aday ayrımı ve yok/etkin olmayan Certbot zamanlayıcısı sınırları
O ile aynıdır.

Bütün tabloların karşılaştırması yine yalnız metrics nedeniyle **DIFFERENT**
kalır. Ek yerel karşılaştırma Arch'ta 11, Debian'da 12 eski satırın eksilmeden veya
değişmeden kaldığını ve her konuğa sonradan altı satır eklendiğini doğruladı. Bu
aynı özet kanıt sınırıdır; ana makinede ham satırların yeniden karşılaştırılması değildir.

Geçici ana makine gözlemcisi ilk olarak etiket kontrolünde `AttributeError:
p_collect_local has no attribute re` ile durdu; henüz konuk komutu göndermemişti.
İz ve geçmiş kaydı korundu. `re` doğrudan içe alınarak yalnız bu ana makine
programı düzeltildi; ürün, test düzeneği ve işlem değişmedi. Gerçek konuk yedek ve
son kanıt okumaları 0 döndü; stdout/stderr ayrı kaydedildi. Öldürülen işçi için
thaw çıkışı 1 olarak korunuyor; doğrulanmış thaw denmiyor.

Mühürlü kayıt Arch'ta 47, Debian'da 48 dosya ve sistem başına 28 kapsam kontrolü
içeriyor. Bu, yukarıdaki hata altında **0c33882 için sınırlı ileri tamamlama PASS**
sonucudur. Son işaret kontrolleri düzeltilmiş kaynakla çalıştı; kasıtlı geç sahip
değişiklikleri gerçek deneyin değil shell regresyonlarının kapsamındadır.
Bağımsız inceleme yalnız ana makinede bütün mühürlü dosyaları ve işlem zincirini
doğruladı. İki konuk da kayıtlı kimlik denetimleriyle durduruldu; diskler ve özel
kanıtlar korunuyor.

## Kalan sınırlar

Certbot zamanlayıcıları yok/etkin değildi; çalışan yenileme düzeni test edilmedi.
TLS kanıtı başlangıçtaki kendinden imzalı sertifika ve anlık HTTPS gözlemleriyle
sınırlıdır; genel güven, edinim veya yenileme değildir. Gerçek DNS eş aktarımı,
barındırılan sitenin HTTP yanıtı, posta, veritabanı iş yükleri, tarayıcı kurtarma
arayüzü ve kesintisiz erişim burada ölçülmedi. Her aday kopyası değil üç saklanan
dosya kaldırıldı. Gerçek değişmeyen ürün/no-op veya geç sahip değişikliği deneyi
iddia edilmiyor.

Dönüşüm öncesi tamamlama penceresinde desteklenen bağımsız devam/telafi hâlâ yok;
kaynak sözleşmesindeki açık eksik geçerlidir. Eksik yedek alma, yeni işlemle
tarihsel geri alma, kurtarma kesintisi/reboot birleşimleri, metadata geçişleri,
imzalı kabul ve kanıt temizliği açıktır. Deneyler kurulu kullanıcı panelinde
güncelleme veya sürüm yayını yapmadı.
