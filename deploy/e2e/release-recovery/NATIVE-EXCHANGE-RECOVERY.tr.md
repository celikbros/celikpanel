# Veritabanı değişimindeki kesintiden sonra kurtarma sırasında yeniden başlatma

*15 Eylül 2026 · [English](NATIVE-EXCHANGE-RECOVERY.md) · D-025 / P0.3*

Bu kapalı deney, kayıtlı ve geçici bir sanal makinede aynı güncelleme işlemine iki
arıza uygular: gerçek veritabanı değişimi yayın makbuzundan önce kesilir; başlayan
otomatik geri alma da `payload_restored` noktasında bir QEMU yeniden başlatmasıyla
kesilir. Yeniden başlatma isteğinin kabul edilmesi kurtarmanın başarısı sayılmaz.

Etkilenen [dayanıklılık ilkeleri](../../../docs/RESILIENCE-CONTRACT.tr.md) 2–5'tir:
anlamlı kanıt, gerçek güvensiz sınırda durma, açık kurtarma sözleşmesi ve başarısız
adaydan bağımsız kurtarma. Değişiklik deney ve kabul kanıtı kapsamındadır. Ürün
kodu, şema38→42 geçişleri, `recovery-material/v3` ve veritabanı kabul, mühür,
yayın ve geri yükleme kayıtlarının şemaları değişmez.

## Sabit yetki ve ikinci kesintinin kabulü

`native_exchange_recovery_trial.py` yalnız
`database-exchange-recovery-reboot` profilini seçer. Niyet, kapı, izleyici, sonuç ve
kopya yolları ayrıdır; yüklenen 20 yardımcının özeti sabitlenir. Mevcut WAL ve
yalnız değişim profilleri bu profilin yeniden başlatma yetkisini kullanamaz.
Değişiklik yapan kipler `--execute` ve kayıtlı deney makinesinin tam kimliğini
ister. Serbest PID, komut, sunucu veya kontrol noktası parametresi yoktur.

İlk kesinti mevcut [gerçek değişim gözlemcisini](NATIVE-DATABASE-EXCHANGE.tr.md)
kullanır: seçili doğrulayıcının `renameat2(RENAME_EXCHANGE)` çağrısı sıfır döner;
güncelleyicinin kabul edilen süreçleri `published.json` oluşmadan duraklatılır.
Bağımsız kontrol; asıl dosyanın After, saklanan dosyanın Before olduğunu; snapshot,
kurtarma paketi, malzeme, kilitler, süreç kimlikleri ve makbuzları doğrular.
Sınırlı dosya tanıtıcılarıyla alınan kopyalar, asılları SQLite ile açmadan korunur.

Bu nokta tutulurken mevcut kurtarma gözlemcisi yalnız `payload_restored` için
`reboot` eylemine hazırlanır. Mühürlü niyet aynı işlem, snapshot, manifest,
transaction token ve seçili kurtarma çalışma ortamına bağlanır. Kontrol noktası
kabul sırasında ve sonrasında tekrar doğrulanır. Ancak ardından tam güncelleyici
birimine SIGKILL gönderilir. Başarısız kabul kesintiye izin vermez; sonucu bilinmeyen
kesintide başarı makbuzu yazılmaz. Yalnız kabul edilen yardımcı iptal edilir;
iptal sonucunun bilinmediği durum da başarı sayılmaz.

Yerel systemd OnFailure normal ürün kurtarmasını başlatır. Gözlemci gerçek ve
kalıcı `payload_restored` kaydını, yerel kurtarma sürecini doğrular ve o cgroup'u
dondurur. Bu nokta veritabanı geri yüklemesinden sonradır; ters değişim sistem
çağrısının içindeki bir kesinti değildir.

Ana makine, QMP bağlantısından önce ilk kesintinin tam olay zincirini ve ona bağlı
kurtarma niyetini ister. Kesinti kanıtını ve önceki açılış günlüğünü korur, güncel
dondurulmuş kurtarma kanıtını yeniden denetler, tek kullanımlık yeniden başlatma
niyetini kalıcılaştırır. Aynı bağlantıda QEMU PID/başlama zamanı, soket sahipliği
ve inode'u, eş kimliği ve VM UUID eşleşmelidir. Kanıt süresi içinde yalnız bir
`system_reset` gönderilir. Konuk gözlemci yeniden başlatma yapamaz; kaçırılan
kontrol noktası veya bilinmeyen sonuç aynı denemede tekrar uygulanmaz.

## Son durum kabulü

`exchange_recovery_evidence.py` korunmuş kanıtı çevrimdışı kontrol eder. Başarı;
farklı açılış ve kurtarma invocation kimliği, aynı snapshot/token ve paket,
yeniden başlatma öncesi yerel OnFailure/geri alma kayıtları, sonraki açılışta yerel
geri alma tamamlanması, son `schedulers_restored` kaydı ve temizlenmiş transaction
işaretlerini gerektirir. Eski bir başarı kaydı, yeniden başlatma yanıtı veya yalnız
çalışan bir süreç yeterli değildir.

Bağımsız son kontrol ayrıca ters değişim niyet/makbuz zincirini, asıl Before
inode'unun geri gelmesini, saklanan After baytlarını, değişmemiş mühürlü çalışma
verisini, eski çalışan/disk ikililerini ve 55 eski tablonun tüm tipli satırlarının
korunmasını doğrulamalıdır. Ana makinedeki SQL yalnız yeni özel kopyalarda çalışır.
DNS A/SOA UDP/TCP, sunulan/kurulu TLS parmak izi ve HTTPS durumu ayrı anlık
kontrollerdir. Son durumdan sonra yapılan ölçüm amaçlı dondur/kopyala/çöz adımı
kurtarmadan açıkça ayrılır.

## Sınırlar

QEMU yeniden başlatması fiziksel güç kaybı veya depolama önbelleği dayanıklılığı
kanıtı değildir. Dolu SQL örnekleri gerçek barındırılan iş yüklerini, kesintisiz
erişimi, bütün geçişleri, bütün kurtarma noktalarını veya imzalı güncelleme kabulünü
kanıtlamaz. Tam P0.3 matrisi açıktır. Kurulu kullanıcı panellerinde güncelleme,
canlı kurtarma, lisans politikası değişikliği veya yeni ürün sürümü yapılmaz.

## X: Debian gerçek sistem kabulü

Yeni geçici kök `/var/tmp/cp-release-drill-20260915-x`, gerçek Alpha64/schema38 ve
önceki değişim deneyiyle aynı yayımlanmamış adayı kullandı: kaynak
`cb3165456bb4ba4654dc19d51a5eafc13721a5fb`, arşiv SHA256
`52dd34b435ba6d1fd1429aaadf875bf0cacae9f0cbeeb249ea9795e74a08698d`, seçili kit
`ed7c3eebe46d7ee5a1ad4b566ef4794daf295cb695151be40a4577b8fdc5a770`.
DNS gözlem araçları ve kalıcı günlük depolaması başlangıç kurulumu öncesinde
hazırlandı. Yirmi yardımcı özeti incelenen kaynakla eşleşir.

`6c5e5a31ed6164cbc6888de7748ecbf0` işlemi şu snapshot'ı kullandı:
`20260915T202701Z-from-unknown-to-cb3165456bb4ba4654dc19d51a5eafc13721a5fb-c8105cd7d2840cc6589e11faa4199d7c`.
Gerçek başarılı değişim, yayın makbuzunun yokluğu ve tam güncelleyici birimine
SIGKILL doğrulandı. Yerel OnFailure geri almayı başlattı; UTC 20:31:47'de kalıcı
`payload_restored` noktası tutuldu ve kayıtlı QEMU'ya bir yeniden başlatma gönderildi.
Yeni açılış ve son kurtarma invocation kimlikleri farklı; snapshot, token ve kit
aynı kaldı.

İlk açılış kurtarma denemesi, systemd hâlâ `starting` olduğu için 20:32:00'da
başarısız oldu. İlk ana makine denetleyicisi bu hatada durdu ve hata kaydını korudu.
Yeniden başlatma, güncelleme veya elle kurtarma tekrarlanmadı. Mevcut yerel
zamanlayıcı (`OnUnitInactiveSec=30s`) 20:32:30'da tekrar çalıştı; aynı geri alma
20:32:46'da `schedulers_restored` noktasına ulaşıp tamamlandı. Salt-okur kontroller
bu sonraki son durumu doğruladıktan sonra ayrı denetleyici **ilk** son durum
ölçümünü yaptı. Başarısız deneme günlükte ve neden-sonuç kanıtında tutulur. Bu,
ilk açılış denemesinde başarı veya kesintisiz erişim değil, sonunda otomatik
kurtarma kanıtıdır.

Bağımsız ana makine analizi gerçek şema38→42 çiftini, 55 eski tablo ve 99 eski
satırı, dört migration ekini ve beklenen on yeni tablo/varsayılanı doğruladı.
Kurtarmadan sonra şema38 ve asıl Before inode'u geri geldi. Kesinti anındaki tüm
rowid ve tipli değerler değişmeden korundu; sonradan üç ölçüm satırı eklenmesiyle
son toplam 102 oldu, dolayısıyla küresel eşitlik **DIFFERENT**. Ham DB/WAL'dan
yeniden oluşturulan özel kopya kaydedilmiş yedekle eşleşti. Yayın niyeti, ters
değişim geri yükleme niyeti/makbuzu, saklanan After ve çalışma baytları ayrıca
kontrol edildi.

İki koordinatör eski başlangıç ikililerini çalıştırdı; transaction işaretleri
temizlendi. Önce/sonra yetkili A/SOA UDP/TCP yanıtları aynıydı; sunulan/kurulu TLS
parmak izleri eşleşti ve HTTPS 200 döndü. Son durumdan sonraki ayrı ölçüm amaçlı
dondur/kopyala/çöz adımı bir kez uygulandı ve iki koordinatörün süreç kimlikleri
korundu. Ardından kayıtlı iki X konuğu da durduruldu; diskler ve kanıt saklandı.
Kurulu kullanıcı panellerine erişilmedi.

| Korunan kanıt | SHA256 |
|---|---|
| Debian son durum sonucu | `72a28de7be253dc3958e0ee50deafda12d85f33ff80e575d7744d0769c563915` |
| Ana makine satır incelemesi, `analysis/debian13-exchange-rows-xqg53a0o/review.json` | `65518d099257b4dc536e3e46b5db8099ee51fd1801700d7bc05d55be44276d43` |
| Ana makine mührü, `analysis/host-seal-1789504648780978582.json` (92 girdi, 20 yardımcı özeti) | `554ea5d45b1837a253f154100300f513b3897df034e97d586f98ef8dc6b06774` |

Son yerel pakette 517 test çalıştı: 516 geçti; gerçek ikili girdisi bulunmayan bir
test açıkça atlandı. İki odaklı izleyici modülü root altında 49, `nobody` altında
49 test geçirdi; atlama yoktu ve gerçek alt süreç exchange çağrıları dahildi.
Bu yerel ve gerçek sistem sonuçları CI'dan ayrıdır. İki kesintili gerçek kabul
yalnız Debian içindir; Arch, diğer kontrol noktaları, gerçek iş yükleri ve kalan
P0.3 kabul işleri açıktır.
