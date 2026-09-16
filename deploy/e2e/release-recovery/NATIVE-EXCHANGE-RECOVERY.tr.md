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
hazırlandı. Yirmi yardımcı özeti incelenen çalışma dosyalarına bağlıdır. Ayrı kaynak karşılaştırması tüm yüklenen yardımcıları `a422ec739ce64e38602ca7b506762cfa01f0b529` commit ile doğrular: beşinde yalnız Git CRLF→LF satır sonu normalleştirmesi vardır; diğer 15 bayt düzeyinde aynıdır. Ham yüklenen özetler değiştirilmez. Kayıt: `analysis/helper-source-binding-1789504804577210406.json`, SHA256 `5265b66db7f53ca94f5d1df92d9ecef6f77024c7710f99ee77fc60bcb570cd57`.

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
Bu yerel ve gerçek sistem sonuçları CI'dan ayrıdır. X aşamasındaki iki kesintili
kabul yalnız Debian içindi; aşağıdaki Z, Arch sonucunu ekler. Diğer kontrol
noktaları, gerçek iş yükleri ve kalan P0.3 kabul işleri açıktır.

## Z: Arch gerçek sistem kabulü

16 Eylül yerel tarihli deney, yeni geçici kök
`/var/tmp/cp-release-drill-20260916-z` üzerinde yapıldı; aşağıdaki günlük saatleri
15 Eylül UTC'dir. X ile aynı gerçek Alpha64/schema38 başlangıcı, değiştirilmemiş
aday arşivi ve seçili kurtarma kiti kullanıldı. Yüklenen 20 yardımcı özeti bağımsız
olarak `f63229497da0eff906c91a0a70f870e964415ae7` commit'ine bağlandı: 15 dosya
bayt düzeyinde aynı, beş dosyada yalnız Git CRLF→LF normalleştirmesi var.
Ham yükleme özetleri korundu.

Önceki Y hazırlık denemesi, ek DNS gözlem aracı paket komutu sıfır dışında sonuç
verince başlangıç kurulumu veya gerçek kesinti kabulü öncesinde durdu. İlk
denetleyici o komutun çıktısını saklamadığı için paket hatasının kesin nedeni
belirsizdir. Y kanıtları korunarak durduruldu; yeniden kullanılmadı ve kurtarma
testi sayılmadı. Z, kalıcı günlüğü başlangıç kurulumu öncesinde hazırladı; BIND ve
gözlem aracını mevcut DNS örneği kurma işlemi sağladı. Sonraki komut hatalarında
stdout/stderr korunur. Y değerlendirmesinin özeti aşağıdadır.

`54ac53a2ba3b5f5c84076d2f064f392f` işlemi şu snapshot'ı kullandı:
`20260915T212447Z-from-unknown-to-cb3165456bb4ba4654dc19d51a5eafc13721a5fb-ebba15fe7f3f45aa50b11acb9a048eeb`.
Başarılı gerçek değişim, tutulan asıl After/saklanan Before çifti, yayın makbuzunun
yokluğu ve tam güncelleyiciye SIGKILL doğrulandı. Yerel OnFailure geri almayı
başlattı; kalıcı `payload_restored` noktası UTC 21:30:42'de tutuldu ve QEMU'ya bir
yeniden başlatma gönderildi. Yeni açılış ve son kurtarma invocation kimlikleri
farklı; işlem, snapshot, transaction token ve seçili kit aynı kaldı.

İlk iki açılış kurtarma denemesi, systemd hâlâ `starting` olduğu için 21:30:55 ve
21:31:26'da başarısız oldu. İki hata da günlükte ve neden-sonuç kanıtında korunur.
Ana makine yalnız mevcut yerel zamanlayıcıyı gözledi; ikinci güncelleme başlatmadı,
kurtarmayı elle çağırmadı. Zamanlayıcı üçüncü açılış denemesini 21:31:57'de
başlattı; aynı geri alma 21:32:14'te `schedulers_restored` noktası ve temizlenmiş
transaction işaretleriyle tamamlandı. Bu, sonunda otomatik kurtarmayı kanıtlar;
ilk denemede başarı veya kesintisiz erişim iddiası değildir.

Değiştirilen çift, 55 eski tablonun tamamında dolu şema38→42 anlam kontrollerini
geçti. Son şema38, asıl Before inode'unu ve kesinti anındaki 100 satırın tüm rowid
ve tipli değerlerini değiştirmeden geri getirdi. Sonraki bir ölçüm satırıyla son
toplam 101 oldu: küresel eşitlik **DIFFERENT**, eksilen veya değişen eski satır
sayısı sıfır. Ters değişim niyeti/makbuzu, değişmeyen kabul/mühür/yayın kayıtları,
saklanan yeni After baytları ve mühürlü çalışma dosyaları bağımsız denetlendi.
Ham DB/WAL'dan yeniden oluşturulan kopya kaydedilmiş yedekle eşleşti. İki çalışan/
diskteki başlangıç ikilisi, yetkili A/SOA UDP/TCP, sunulan/kurulu TLS parmak izleri
ve HTTPS 200 kontrolleri geçti. İlk ve tek son durum ölçümünde dondur/kopyala/çöz
adımı iki koordinatörün süreç kimliklerini korudu. Ardından kayıtlı iki Z konuğu
durduruldu; diskler ve özel kanıtlar saklandı.

Başlangıç kurucusu aday deneyi öncesinde çekirdek paketini yükseltmişti: çalışan
çekirdek `7.1.8-arch1-3`, kurulu modüller `7.2.6-arch2-1` içindi. Testteki yeniden
başlatma `7.2.6-arch2-1` çekirdeğini açtı; kurulu Linux/systemd paket sürümleri
bu yeniden başlatma boyunca değişmedi. Kurucunun yeniden başlatma uyarısı ve iki
gözlem korundu. Bu sonuç güvenlik duvarı/VPN hazır oluşunu veya iki açılışta aynı
çalışan çekirdekle kurtarmayı kanıtlamaz.

| Korunan kanıt | SHA256 |
|---|---|
| Arch son durum sonucu | `40af76b80cf2ecfc0a5246df748c2696dc2e4211a6ce6f749d8cea7f5d22e0dc` |
| Ana makine satır incelemesi, `analysis/arch-exchange-rows-bqne81lj/review.json` | `5bf0d718b5e0fc3f339ff83a6af20024756e087d83735d19be8aba872d7c50d4` |
| Ana makine mührü, `analysis/host-seal-1789507983843020713.json` (96 girdi, 20 yardımcı özeti) | `58583442a8d8563f0bb1a91f5046fd85835707083b408a81c418e7c110884f7b` |
| Yardımcı kaynak bağı, `analysis/helper-source-binding-1789507986010838196.json` | `a430639bad29f33d33bcd16ddcf1a6f35f98c758c100f85719eaa2f16130fc8b` |
| Y kökündeki hazırlık değerlendirmesi, `evidence/arch/y-preparation-assessment.json` | `99b160905ce9ee0471c0209199c3e529f695a48793f5a5c8442a4955e54fdfc2` |

Değişmeyen yerel pakette yine 517 test çalıştı: 516 geçti, gerçek ikili girdisi
olmayan biri açıkça atlandı. X ve Z, bu iki kesinti sınırını artık Debian ve Arch
üzerinde kapsıyor. Ürün kodu ve kalıcı şemalar değişmedi. Diğer kontrol noktaları,
gerçek iş yükü/yenileme bağımsızlığı, imzalı kabul, fiziksel güç kaybı dayanıklılığı
ve kalan P0 kabul işleri açıktır.

## AA: değişen runner deneyi hedef kesintilerden önce sonuçsuz kaldı

16 Eylül tarihli yeni Arch denemesi, runner düzeltmesini içeren
`8d62896f0fb5be329090923ad074115941c0328c` commit'ini (ağaç:
`fb57ef517982dec3b5d08673efcc658a23ace21b`) kullandı. Yayımlanmamış Alpha81 deney
arşivinin özeti `1c49df9ea65ec5758470b4ee347ed51cd2d00b7cf3f3f34253f58543d4a6cba8`.
Gerçek Alpha64/şema38 kurulumu, dolu SQL fixture'ı ve yetkili DNS ön kontrolü
`/var/tmp/cp-release-drill-20260916-aa` altında tamamlandı.
`d8b241a2e3665e04cc81f261628cb926` işlemi,
`release-recovery__90f1cb34a68e7020` hücresine ve
`a0eaacb0-3f8f-521f-8620-390d6c62649e` Arch UUID'sine bağlıydı.

Deneme hedeflenen iki hata sınırını da **doğrulayamadı**. Adayın
`recovery verify-compatibility --mode --normal` çağrısı beklerken salt-okur süreç
kaydı, panel-checker lideri 14295'i zombie, thread 14299'u ise izleyici 10882'ye
bağlı ptrace beklemesinde gösterdi. Bu thread saklanan `task-admitted` kayıtlarında
yoktu. Farkın ardındaki kesin çekirdek olay sırası henüz belirlenmedi. İzleyici
süre sınırına ulaşıp `trace-detach-incomplete-controller-watchdog-required` bildirdi;
`controller_cut_called=false`, `cleanup.complete=false` idi ve lider 14295 temizlik
sonucunda hâlâ listeleniyordu. Host yeniden başlatma denetleyicisi reset göndermeden
zaman aşımına uğradı. Kaydedilen denetleyici olayları `armed`, `gate_released`,
`trace_finished` oldu; exchange kesintisi veya kurtarma yeniden başlatma kanıtı oluşmadı.

Bu, **sonuçsuz ölçümdür**; değişen açılış hazırlığı yolunun geçtiği veya başarısız
olduğu kanıtı değildir. Değişiklik işlemi tekrarlanmadı. Mevcut durum, günlük,
ham iz ve süreç tanısı korundu; kayıtlı Arch ve Debian QEMU misafirleri diskleri
saklanarak durduruldu. Değişen runner için gerçek sistem kabulünden önce izleyicinin
düzeltilip doğrulanması ve yeni kayıtlı deneme gerekir. X/Z yalnız eski kaynaklarının
kanıtı olarak kalır.

Yerel aday Go 1.26.5 ve Node 26.8.1 ile oluşturuldu. Önceki iki derleme hazırlığı
hatası korundu: yanlış varsayılan Go sürümü ve Windows arşivinin CRLF dönüşümü.
Başarılı arşiv Git'in LF dışa aktarımını kullandı; runner ve checker kaynak listesi
baytları tam Git blob'larıyla karşılaştırıldı. Eski sürüm kurucusu yine çekirdek/modül
uyuşmazlığı bildirdi; yeni çekirdeği gözlemleyecek deney yeniden başlatması gerçekleşmedi.
Güvenlik duvarı/VPN veya hizmet bağımsızlığı iddiası yoktur.

| Korunan kanıt | SHA256 |
|---|---|
| AA sonuçsuzluk değerlendirmesi, `evidence/arch/aa-inconclusive-assessment.json` | `f923722f2c51c2a524e18e3e03c92c673435029cf4098a85c38d1332ac767c7a` |
| Son ham iz, `evidence/arch/aa-final-trace.jsonl` | `4e2018a59e2bd734c0a34542cb87329c8ca6acae0d07138ccf0cf96ac1971765` |
| Aday derleme kanıtı (iki başarısız hazırlık dahil) | `54c62e5c7a2637935efa509b5a9794deb962f32fc54af8c918765342bcf6e1aa` |
| Helper kaynak bağı, `analysis/helper-source-binding-1789531049641104441.json` (20 helper; beş fark yalnız CRLF) | `284e54c521105e7627bed733a4f6c49de9cc9afa78299b1a57a2e46616a17a63` |
