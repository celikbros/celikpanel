# Gerçek veritabanı exchange kesintisi

*15 Eylül 2026 · [English](NATIVE-DATABASE-EXCHANGE.md) · D-025 / P0.3*

Bu düzenek, kayıtlı ve atılabilir bir Linux konuğunda değişmemiş ürünün taşınmış
veritabanını yayımlamasını gözler. Gerçek atomik exchange çağrısını syscall
çıkışında, yayımlama makbuzundan önce tutar; bağımsız doğrulamadan sonra tek bir
kesintiye izin verir. **Kaydedilen kapsamda Arch U ve Debian W kabulü geçti.** Kesinti, kurtarma sonucu değildir.

Etkilenen [dayanıklılık ilkeleri](../../../docs/RESILIENCE-CONTRACT.tr.md) 2–5'tir:
anlamı belirli kanıt, tehlikeli eylemin gerçek sınırında durması, her değişikliğin
kurtarma sözleşmesi ve aday bozulduğunda bağımsız kurtarma. Yalnız test düzeneği
kodu eklenir; ürün SQL'i, migration kimlikleri, `recovery-material/v3` ve veritabanı
admission/seal/publication/restoration şemaları değişmez.

## Sabit girdiler ve yetki

Başlangıç, gerçek Alpha64 ikililerini ve doğrulanmış schema38'i; doldurulmuş özel
SQL test verileriyle kullanır. Aday, [ayrılmış veritabanı taşıması](ISOLATED-DATABASE.tr.md)
ve [gerçek WAL kesintisindeki](NATIVE-WAL.tr.md) aynı yayımlanmamış Go 1.26.5 yerel
ürünüdür; arşiv etiketi bir sürüm yayını anlamına gelmez.

| Girdi | SHA256 veya kaynak kimliği |
|---|---|
| Alpha64 kaynağı | `3ee8dac009c7e3db1d940f9b8be078e186693a80` |
| Aday kaynağı | `cb3165456bb4ba4654dc19d51a5eafc13721a5fb` |
| Aday arşiv SHA256 | `52dd34b435ba6d1fd1429aaadf875bf0cacae9f0cbeeb249ea9795e74a08698d` |
| Seçilen kurtarma kiti manifest SHA256 | `ed7c3eebe46d7ee5a1ad4b566ef4794daf295cb695151be40a4577b8fdc5a770` |

Sabit exchange profili, yüklenen 19 yardımcının tamamını sabitler ve kendi değişmez
deneme kayıtlarını kullanır. İzleme öncesinde VM kaydı, arşiv kimliği, boot, worker
PID/başlama zamanı, systemd invocation, cgroup ve başlangıç kapısı doğrulanır.
Yardımcılar her denemede ayrı kaydedilir; sonraki değişiklik önceki kanıtı yeniden
etiketleyemez. Özel kayıt değerleri, süreç kimlikleri, veritabanları ve ham günlükler gizli tutulur.

## Gerçek yayımlama sınırı

`waltrace/exchange_trace.py`,
`trace_database_publication(registration, callbacks, timeout=900)` arayüzünü sunar;
WAL izleyicisinin süreç yaşam döngüsünü paylaşır. Kernel fork/clone/exec geçişleri
izlenir; yalnız seçilen root `bin/panel-checker --publish-update-database=<snapshot>`
ailesi syscall izlemeye geçer. Değişmez beklenti; işlemi, token'ı, worker başlangıcını,
snapshot'ı, tam checker yolunu ve özetini bağlar. Yerel kilit FD9 aynı işlemin
bu yayımlayıcısına ait olmalıdır; adayın migration çalışması ayrı bir adımdır.

Linux amd64 üzerinde yalnız syscall316 `renameat2` ve tam flags2
(`RENAME_EXCHANGE`) eşleşir. Girişte kabul edilmiş cgroup'un tamamı durdurulur;
süreç belleğinden yalnız sınırlandırılmış iki `celikpanel.db\0` adı okunur. Belleğe,
register'lara, argümanlara, sonuçlara, ürün SQL'ine veya işaretçilerine yazılmaz.
Bağımsız `guest_exchange_checkpoint.inspect_entry(...)`; seçilen kiti, kilidi,
snapshot'ı, materyali, admission/seal/publication kayıtlarını, dizin FD'lerini ve
tam canonical Before/staged After çiftini doğrular; bağlı makbuzlar bulunmamalıdır.

Yalnız yayımlayan TID ilerletilir. Eşleşen başarılı çıkış tam sayı sıfır döndürmeli,
diğer bütün görevler durmuş kalmalıdır. Bağımsız `inspect(...)`; aynı yetkiyi ve
görev envanterini, canonical After'ı, aynı build dizininde korunan Before'u,
`published.json` ve restoration makbuzlarının yokluğunu tekrar kanıtlar.
Çift karşılaştırmasında yalnız rename kaynaklı dosya ctime'ı dışarıda bırakılır;
baytlar, inode, sahiplik, izinler ve kaydedilen diğer metadata bağlı kalır.
Bu nokta dizin fsync'lerinden ve makbuzdan öncedir; atomik görünürlük güç kaybı dayanıklılığı değildir.

Denetleyici tam iki dosyayı sınırlandırılmış descriptor'larla kopyalar; çifti
tekrar denetler ve tam unit'e SIGKILL öncesinde bütün kontrol noktasını bağımsız
olarak yeniden doğrular. Yakalanan asıl dosya SQLite ile açılmaz. İzleyicinin
rastgele PID arayüzü veya EXITKILL seçeneği yoktur. Desteklenmeyen durumlar, değişen
kimlikler, bu sınırı bölen sinyaller, başarısız exchange ve tamamlanmayan ayrılma `inconclusive`
kalır; yalnız kendisinin izlediği, kimliği hâlâ eşleşen görevlerden ayrılır.
`cut-sent` kesintiyi kaydeder; son başarıyı değil.

## Bağımsız veri ve kurtarma kanıtı

`database_exchange_rows.verify_pair(...)`, dondurulmuş Before/After dosyalarının
yeni özel kopyalarını inceler. Schema38→42'yi sabit migration kimlikleriyle;
eski 55 tablonun tamamındaki bütün rowid'leri ve türü korunmuş sütunları, hiçbir
tabloyu dışlamadan doğrular. Bu projeksiyonlarda yalnız ledger ekleri39–42 kabul
edilir. Yeni 10 tablo da denetlenir: tam legacy setup tekil kaydı, geçerli dinamik
zamanı, diğer yeni tabloların boşluğu ve eski her domainde `local`/boş uzak-ID varsayılanları.
9 SQL regresyonu; seed sonrası satırları, rowid değişimini, BLOB/text karışmasını,
beklenmeyen ekleri, değişen varsayılanları ve uygunsuz girdileri kapsar. Bunlar bileşen testidir.

Gerçek sistem kabulü ayrıca aynı işlem/snapshot'ın, elle kurtarma veya ikinci
güncelleme olmadan systemd OnFailure üzerinden desteklenen son duruma ulaşmasını
gerektirir. Son ürün dosyaları, işaretçiler, veritabanı makbuzları, satırlar ve
servis gözlemleri uyuşmalıdır. Sabit kanıt için sonradan ayrı coordinator
dondurma/kopyalama/çözme kullanılırsa bu açıkça kaydedilir; kurtarma sonrası ölçümdür,
otomatik kurtarmanın parçası veya kesintisiz hizmet kanıtı sayılmaz.

## Kayıtlı düzeneği çalıştırma

Yalnız doldurulmuş seed'i ve arşivi doğrulanmış, önceden kayıtlı atılabilir başlangıç
kullanılır. Bunlar host komutlarıdır; kurulu panel kurtarma talimatı değildir:

```bash
python3 deploy/e2e/release-recovery/native_database_exchange_trial.py --work-root "$work_root" --node "$node" --mode prepare --archive "$archive" --archive-sha256 "$archive_sha256" --execute
python3 deploy/e2e/release-recovery/native_database_exchange_trial.py --work-root "$work_root" --node "$node" --mode arm --execute
python3 deploy/e2e/release-recovery/native_database_exchange_trial.py --work-root "$work_root" --node "$node" --mode start --execute
python3 deploy/e2e/release-recovery/native_database_exchange_trial.py --work-root "$work_root" --node "$node" --mode collect
```

`node`, `arch` veya `debian13` olur; diğer değişkenler mevcut kayıtlı çalışma kökünü
ve doğrulanmış ürünü belirtir. Prepare/arm/start açık `--execute` ister;
collect salt-okurdur. Mevcut deneme kayıtları sessiz yeniden başlatmayı engeller.

## Kanıt ve kabul

| Kanıt | Mevcut sonuç |
|---|---|
| Root çevrimdışı test grubu | 495 test: 494 PASS; gerçek ikili girdi eksikliğinden 1 açık atlama |
| Alt dizin izleyici testleri, root ve yetkisiz `nobody` | İkisinde de 49/49 PASS, atlama yok; mevcut WAL regresyonları ve dört adil bekleme testi dahil |
| Gerçek, yeni fork edilmiş çocuk süreç syscall testleri | Başarılı exchange çocuk dönmeden inode'ları değiştirir; gerçek ENOENT pozitif kanıtı reddeder |
| U / Debian 13 | Giriş/çıkış, tam unit kesintisi ve yerel kurtarma gözlendi; DNS gözlemcisi, ardından kayıtlı VM kullanılamadığı için son kabul belirsiz |
| U / Arch | Tam exchange kesintisi, yerel OnFailure geri alma, schema38 dönüşü; 55 tablodaki 132 kesinti anı satırı korundu, dört sonraki ölçüm satırı eklendi |
| V / Debian 13 | Exchange öncesi yayımlayıcı süre aşımı; kontrolcü kesintisi yok; otomatik geri alma ve noktasal sağlık gözlendi; exchange kabulü belirsiz |
| W / Debian 13 | Tam exchange kesintisi, yerel OnFailure geri alması, schema38; 55 tablodaki 98 satır korundu, sonraki tek metrics örneği eklendi |

Bütün başarısız veya belirsiz denemeler kayıtta korunmalıdır. Yerel testler ve
önceki WAL/ayrılmış-DB sonuçları bu yeni gerçek sistem sınırını kanıtlamaz.
Geniş P0.3 açık kalır: diğer kontrol noktası/kurtarma hataları, reboot veya güç
kaybı, imzalı Agent/UI kabulü ve gerçek barındırma/yenileme davranışları bu kabulün
dışındadır. SQL test verileri gerçek barındırılan domain hazırlamaz. Kurulu
panellerde güncellemeyi yine sunucu sahibi panel arayüzünden başlatır.


### Korunan U kanıtı (15 Eylül)

Çalışma kökü `/var/tmp/cp-release-drill-20260915-u`. Arch işlemi
`19152b3b45032a69c9b02320896b6236`, snapshot
`20260915T135605Z-from-unknown-to-cb3165456bb4ba4654dc19d51a5eafc13721a5fb-4903c3e01a78b58dd2393e1f1ea037bd`.
Değişmez niyet kaydındaki 19 yardımcı dosya özeti incelenen kaynakla eşleşiyor.

Çıkış kanıtı gerçek başarılı exchange'i, giriş kanıtını ve tam SIGKILL kaydını
bağlıyor. Aynı boot/işlem günlüğü ve yerel son kontrol noktası, systemd OnFailure
ile otomatik geri alma arasındaki ilişkiyi doğruluyor. İlk admission, seal ve
publication kayıtları değişmemiş; `published.json` yok. Restoration niyeti ters
exchange istiyor; sonuç makbuzu tam bu niyetin özetine bağlı. İlk Before inode'u
yeniden canonical; taşınmış After dosyasının baytları ve rename ctime dışındaki
metaverisi korunuyor. Seçilen kurtarma kiti, çalışan/diskteki Alpha64 ikilileri
uyuşuyor; işlem işaretçileri temizlenmiş ve work dosyaları korunmuş.

Host analizi SQLite ile yalnız yeni özel kopyaları açıyor. Değiştirilmiş çiftte
schema38→42, 132 eski satırın tamamı, 55 eski tablo, dört migration kaydı ve beklenen
on yeni tablo/varsayılan doğrulandı. Son veritabanı schema38; kesinti anındaki 132
satırın rowid ve türü korunmuş değerlerinde kayıp/değişiklik yok. Dört sonraki ölçüm
satırı toplamı 136'ya çıkarıyor; genel eşitlik **DIFFERENT**. Son ham DB/WAL'den
ayrı kopyada türetilen yedek, saklanan doğrulanmış yedekle uyuşuyor. Orijinal kanıt
SQLite ile açılmıyor.

UDP/TCP üzerinden dört yetkili A/SOA sorgusu beklenen test değerlerini döndürüyor;
sunulan TLS ile diskteki sertifikanın parmak izi aynı, HTTPS 200. Ayrı son ölçümde
yalnız iki panel coordinator'ı dondurulup kopyalandı ve çözüldü; iki çözme de aynı
süreç kimlikleriyle doğrulandı. Bu anlık kontroller ve ölçüm arası, kesintisiz hizmet
veya gerçek barındırma kanıtı değildir.

| Korunan kanıt | SHA256 |
|---|---|
| Arch son işlem sonucu | `6cfb4d26684507e3a2173242bda0ebae17bf57e06e769941c2ac11d7ca09ab23` |
| Yeni host satır incelemesi, `analysis/arch-exchange-rows-lmv6za_6/review.json` | `5264c9b3156bf01770bdad44105c6b98fbbfa9d9499a37df5d68e114024ac73a` |
| Host doğrulaması, `analysis/host-seal-1789495982920555018.json` (52 girdi bağı) | `4d658dff9afe5ede2a4fc450a789fea058b7424e36a0af2f350414a9dffdb66c` |

Debian işlemi `553062756ff312b11fdceb7c5f5df099` için yerel kurtarma gözlemleri
var; ancak ilk son-kontrol toplayıcısı ölçüm dondurmasından önce durdu:
`/usr/bin/dig` yoktu. DNS sonucu **bilinmiyor**; DNS arızasının kanıtı değil.
Sonraki salt-okur tanı named'in aktif olduğunu ve sorgu aracının eksikliğini gördü.
Ayrı toplama denemesi, VM artık çalışmadığı için kayıtlı-QEMU kontrolünde, konuk
sisteme erişmeden reddedildi. İkinci güncelleme, kurtarma veya kopyalama başlatılmadı.
Eski gözlemler, diskler ve kanıt korunuyor; kontrollü kapatma veya Debian kabulünün
tamamlandığı iddia edilmiyor. Yeni kimlikle kaydedilmiş Debian denemesi kesintiden
önce gözlemci gereksinimlerini doğrulamalı ve son veri/hizmet kanıtını tamamlamalı.
Son host doğrulamasında iki kayıtlı U QEMU PID'i de yoktu; bu kontrollü yeniden
başlatma veya güç kesintisi deneyi sayılmaz.

Son yerel kontrolde üst dizindeki 495 testin 494'ü geçti; gerçek ikili girdi
eksikliğinden bir test açıkça atlandı. İlgili iki alt dizin izleyici modülü root ve
`nobody` altında ayrı ayrı 45/45 geçti, atlama yok. Daha geniş alt dizin keşfi ayrıca
60 test çalıştırdı: 56 PASS, mevcut kontrollü çocuk ikili girdileri için dört atlama.
Bu geniş sayım hedefli gerçek exchange-çocuk testlerinin yerine geçmez. Bunlar yerel
sonuçlardır; CI sonucu değildir.

### V: korunan belirsiz gözlem ve izleyicide adil bekleme

`/var/tmp/cp-release-drill-20260915-v` altında yeni, yalnız Debian denemesinde
`bind9-dnsutils` başlangıçtan ve kesintiden önce kuruldu; UDP/TCP üzerinden yetkili
A/SOA yanıtları ve iki koordinatör doğrulandı.
`1907a660bbe2291e2190d31e67cb4c76` işlemi exchange sınırına ulaşmadı. Seçilen
yayımlayıcı, ürünün değişmeyen üç dakikalık komut süresi dolunca sonlandırıldı;
gözlemci, kontrolcü kesintisi yapmadan `no-exact-native-database-exchange-boundary`
bildirdi. Yerel OnFailure geri alması tamamlandı. Sonraki salt-okur kontrollerde
aynı DNS test yanıtları ve başlangıç ikililerini çalıştıran aktif koordinatörler
bulundu. Kayıtlı konuklar disk ve kanıtları korunarak durduruldu. Bu sonuç exchange
kabulü veya son veritabanının bütün satırlarının karşılaştırılması değildir.

Eski izleyici daima en küçük kayıtlı TID'yi önce sorguluyordu. Bir regresyon,
bu görev sürekli hazırken diğerlerinin süresiz bekleyebildiğini gösterdi.
Düzenek artık her alınan olaydan sonra sırayı döndürüyor; yalnız o an kayıtlı
TID'leri sorguluyor ve aynı süre sınırını koruyor. Dört regresyon sürekli hazır
olmayı, değişen görev kümesini, hazır olmayan/çıkmış görevleri ve süre sınırında
reddi kapsıyor. Aday, ürün süreleri ve bütün kontrol noktası/kimlik denetimleri
değişmedi. V günlüğü süre aşımını; birim testi zamanlama kusurunu kanıtlar.
Bu kusurun gerçek süre aşımının tek nedeni olduğu ileri sürülmez.

V'nin dokuz girdiyi bağlayan kanıt mührü:
`analysis/v-inconclusive-seal-1789499043977694487.json`, SHA256
`d65f485ed829d6d1a87fc557b754573647932f494842f3821fd907a112dbf4a8`.
Değişiklik sonrası yerel testlerde 495 test (494 geçti, gerçek ikili girdisi eksik
bir açık atlama), ayrıca root ve `nobody` altında 49'ar hedefli izleyici testi
geçti; bu grupta atlama yok ve iki gerçek çocuk exchange çağrısı bulunuyor.

### W: Debian gerçek exchange kabulü

Yeni `/var/tmp/cp-release-drill-20260915-w` dizini; aynı sabit başlangıcı, adayı
ve kurtarma paketini, adil bekleme düzeneğiyle kullandı. DNS gözlem aracı
başlangıçtan önce hazırlandı. `885d5b78165aaa2bd3c247628970120d` işlemi,
`20260915T190442Z-from-unknown-to-cb3165456bb4ba4654dc19d51a5eafc13721a5fb-1ce830889e8d3010c8d1527409e28512`
snapshot'ını kullandı. Değişmez deneme kaydındaki 19 yardımcı özeti incelenen
kaynakla aynı; `native_trace.py` SHA256 değeri
`1bd8b693cfa83ec3d18f74d827608f9ef18ca34f2ad5ec65508de19782abb8a8`.

Gerçek giriş/çıkış kanıtları ve tam birim kesinti makbuzu, başarılı atomik değişimi
yayın makbuzundan önceki kontrol noktasına bağlıyor. Yerel systemd OnFailure,
elle kurtarma veya ikinci güncelleme olmadan ters exchange ile otomatik geri aldı.
Aynı işlemin günlüğü, son kontrol noktası, korunan Before/After dosyaları, geri alma
niyeti/makbuzu, seçilen kurtarma paketi ve çalışan/diskteki Alpha64 ikilileri uyumlu.
Sürüm işlem işaretçileri temizlendi; `published.json` oluşmadı.

Host üzerinde yeni özel SQLite kopyaları, exchange anındaki schema38→42 çiftini,
55 tablodaki 98 eski satırın tamamını, dört migration ekini ve on yeni tablo/varsayılanı
bağımsız doğruluyor. Son schema38 veritabanı, kesinti anındaki bütün satır kimlikleri
ve tür bilgili değerleri koruyor; eski satır kaybı/değişimi yok. Sonraki tek metrics
örneğiyle toplam 99 satır var; genel eşitlik **DIFFERENT**. Saklanan ham DB/WAL'dan
özel kopyada yeniden kurulan veritabanı doğrulanmış yedekle aynı. Özgün kanıtlar
SQLite ile açılmıyor.

Dört yetkili UDP/TCP A/SOA gözlemi, kesinti öncesi ve kurtarma sonrası aynı.
Sunulan/kurulu TLS parmak izleri eşleşiyor; loopback HTTPS 200 dönüyor. Ayrı son
ölçüm, iki koordinatörü bir kez dondurup/kopyalayıp çözdü; ikisinin de çözülmesi
aynı süreç kimlikleriyle doğrulandı. Kayıtlı iki W konuğu, disk ve kanıtları
korunarak durduruldu. Bunlar noktasal kontroller ve süreç ölümü sonrası kurtarma
kanıtıdır; kesintisiz hizmet veya güç kaybı dayanıklılığı değildir.

| Korunan W kanıtı | SHA256 |
|---|---|
| Debian son sonuç | `98b0459ac4c384e17c9aa4bdf8e94d6dbdd134b6e705d9ec825a51aa7ec26295` |
| Yeni host satır incelemesi, `analysis/debian13-exchange-rows-gfvx5uxx/review.json` | `d99cdb578712c9ca119bebeae266e1e8ed44349c58f8ddddc19e3155e2746713` |
| Son host mührü, `analysis/host-seal-1789499468326072969.json` (76 girdi bağı) | `cc7c76b062e727f4ac034c31d9efa20e2fb95eb2ba2defad2daeb2beb8b76080` |

Mühür, kopyalanmış açıklama etiketlerinin düzeltmesini de koruyor: ilk host raporu
laboratuvar V/toplayıcı denemesi2 diyor, son sonuçta ise eski U seed kapsam etiketi
kalıyordu. Bütün korumalı kimlikler ve yakalanan girdiler W'ye bağlı. Son host raporu
aynı satırları W/deneme1 etiketiyle yeniden hesaplıyor; özgün raporlar değişmedi.
Bunun için konuğa erişim, ikinci yakalama veya kurtarma yapılmadı. 76 girdi bağı,
19 yardımcı özetini ve gerçek olay zincirini de yeniden denetliyor. Eski U/V Debian
sınırlamaları kayıtta kalıyor; W yalnız bu gerçek exchange kabul işini kapatıyor.
Tam P0.3, gerçek barındırılan hizmetler, yeniden başlatma/güç kaybı ve güncellemeyi
yalnız sahibin başlatması gereksinimleri yukarıdaki gibi devam ediyor.

Sonraki [Debian X kurtarma sırasında yeniden başlatma deneyi](NATIVE-EXCHANGE-RECOVERY.tr.md), bu kesintiyi yerel geri alma sırasında yeniden başlatmayla birleştirir. Ayrı kanıt ilk açılış hatasını korur ve mevcut zamanlayıcının sonunda bütün kesinti anı satırlarını koruyarak kurtardığını doğrular; geniş P0.3 matrisi açıktır.
