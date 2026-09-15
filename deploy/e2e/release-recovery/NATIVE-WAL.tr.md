# Gerçek WAL kesintisi

*15 Eylül 2026 · [English](NATIVE-WAL.md) · D-025 / P0.3*

Bu test düzeneği, kayıtlı ve geçici bir Linux konuğunda değişmemiş ürün migrator'ını
çalıştırır. Başarılı bir gerçek WAL yazımını gözler, durdurulmuş durumu korur ve
yalnız bağımsız doğrulamadan sonra tek bir kesintiye izin verir. Aynı işlemin yerel
kurtarması ve son veri korunması ayrı kabul kontrolleridir. Debian S ve yeni Arch T
denemelerinde sınırlı gerçek geri alma ve kesinti anındaki satırların korunmasına
ilişkin bağımsız kontroller geçti. Daha geniş
P0.3 kabulü açık kalıyor.

Etkilenen [dayanıklılık sözleşmesi](../../../docs/RESILIENCE-CONTRACT.tr.md),
P0.3 ve 2–5 numaralı ilkelerdir: kalıcı işlem kimliği, gerçeğe uygun gözlemler,
korunan kanıttan kurtarma ve sunucu sahibinin durumunu koruma. Bu çalışma yalnız
test düzeneği kodu ekler. Ürün SQL'i, geçiş geçmişi, `recovery-material/v3`,
veritabanı kabul/yayımlama şemaları ve desteklenen 38→42 geçişi değişmez.
Kurulu müşteri panellerini güncelleme yetkisi vermez.

## Sabit ürün girdileri

Her deneme gerçek Alpha64 ikilileri ve doğrulanmış 38 geçiş kimliğiyle başlar.
Aday, son [yalıtılmış veritabanı deneyinde](ISOLATED-DATABASE.tr.md) kullanılan,
Go 1.26.5 ile derlenmiş aynı yayımlanmamış yerel pakettir; Alpha81 arşiv etiketi
yayımlanmış bir sürüm anlamına gelmez.

| Girdi | Kimlik |
|---|---|
| Alpha64 kaynak | `3ee8dac009c7e3db1d940f9b8be078e186693a80` |
| Alpha64 arşiv SHA256 | `c669e52a6686865e9fc0515e2839f4cfea7501730877e48654a35def63557979` |
| Alpha64 panel SHA256 | `c6e91e9e478d261c104397518fdae0145733832be7eeb3e53a123ef30c5d7460` |
| Aday kaynak | `cb3165456bb4ba4654dc19d51a5eafc13721a5fb` |
| Aday kaynak ağacı | `336778673eb5bdfb19515a626e2e7c4bf08e3b5b` |
| Aday arşiv SHA256 | `52dd34b435ba6d1fd1429aaadf875bf0cacae9f0cbeeb249ea9795e74a08698d` |
| Aday panel SHA256 | `74e5b00e674d720bc40caea0e2fe46c04d8d8a9d9d5085496983a7c26cc121ad` |
| Seçili kurtarma kiti manifest SHA256 | `ed7c3eebe46d7ee5a1ad4b566ef4794daf295cb695151be40a4577b8fdc5a770` |

Ana makine her denemenin tam arşivini, kaynak kökenini ve yüklenen yardımcıların
özetlerini kaydeder. Yardımcılar denemeler arasında farklı olabilir; sonraki bir
kaynak düzeltmesi önceki gözlemin kimliğini değiştirmez. Özel kayıt değerleri,
süreç kimlikleri, alınan veritabanları ve ham günlükler kanıt deposunda kalır.
Son kaynak ayrıca 17 yardımcı adından oluşan sabit envanteri, sınırlı ve symlink
izlemeyen kanıt okumalarını, açık ana makine değişiklik onayını ve olay zincirinin
tümüyle tutarlı ana makine sonucunu şart koşar. Bu son koruma düzeltmeleri kayıtlı
S/T kesintilerinden sonra yapılıp yerelde test edildi; konukların dondurulmuş
yardımcılarını veya deney kanıtlarını geriye dönük değiştirmedi.

## Gerçek test düzeneği API'si ve yetki

`native_wal_trial.py`, tek bir kayıtlı konuk işlemini hazırlar, kurar, başlatır ve
toplar. Hazırlama/kurma/başlatma değişiklikleri açık `--execute` gerektirir; toplama
salt okunurdur. Ayrı, değişmez deneme kayıtları, belirsiz aktarımın ikinci bir
güncelleme veya kesinti başlatmasını önler. Mevcut hazır paket düzeneği arşivi ve
gerçek başlangıç durumunu doğrular; imzalı herkese açık Agent/UI kabulü bu deneyin
kapsamı dışındadır.

`guest_native_wal_trial.py`, değişmemiş bootstrap güncelleyicisini çalıştırmadan
önce kısa ömürlü bir test kapısı kullanır. Kapının çalıştırılabilir dosyası,
PID/başlangıç zamanı, systemd çalıştırma kimliği, cgroup ve kayıtlı VM kimliği
sabitlenir. Süresi sınırlı özel dosya izni, izleyici tam ilk göreve bağlanıp onu
durdurduktan sonra kapıyı açar. Kapının bir son süresi vardır; güncelleyiciyi
çalıştırırken systemd kimliğini korur. Ürün koduna veya SQL'e işaret eklemez.

`waltrace/native_trace.py`,
`trace_native(registration, callbacks, timeout=...)` arayüzünü sunar. Kayıt tek
bir `celikpanel-self-update-<operation>.service` birimini, worker PID/başlangıç
zamanını, açılış kimliğini ve kapının çalıştırılabilir dosya kimliğini belirtir.
Rastgele PID alan bir komut satırı yoktur. Callback sınırı güncel yazıcı kabulünü,
kapının açılmasını, bağımsız kontrol noktası doğrulamasını ve ayrıca yetkilendirilmiş
tam birim kesintisini sağlar.

Linux amd64 izleyicisi, doğrulanmış nonleader exec dahil çekirdek fork/clone/exec
olaylarını izler. Yalnız kabul edilmiş aday `panel --migrate-only` süreç ailesinde
sistem çağrısı izlemeye geçer; diğer alt süreçlerin yaşam döngüsü izlenir.
Beklenmeyen görev kimlikleri, desteklenmeyen çekirdek durumları, eksik kabul,
değişen dosyalar, zaman aşımı ve sınır aşımı `inconclusive` üretir. Temizleme,
yalnız hâlâ bu izleyicinin tuttuğu ve kabul edilen kimliği değişmemiş görevlerden
ayrılır. EXITKILL özellikle yoktur: izleyicinin hatası, dışarıdan bağlandığı
güncelleyiciyi öldürme yetkisi değildir. Doğrulanamayan temizleme, başarılı ayrılma
olarak gösterilmez.

## Fiziksel WAL sınırı

Olumlu gözlem, kabul edilmiş çalışma veritabanının tam WAL descriptor ve inode'u
üzerinde eşleşen, başarılı bir `pwrite64` giriş/çıkışı gerektirir. Önceki başarılı
bir yazımdan gerçek ve eksiksiz bir başlık ya da commit ile biten ön görüntü zaten
gözlenmiş olmalıdır. Yeni baytlar bu görüntüyü korumalı ve yeni commit işareti
eklemeden checksum'u geçerli, eksiksiz bir noncommit frame eklemelidir; başarılı
yazım gözlenen ekleme sınırında biter. Ayrıştırıcı, sonraki WAL görüntüsünü keserek
önceden gözlenmemiş bir görüntü üretmez.

Kabul edilen bütün güncelleyici görevleri durdurulup tam cgroup envanteriyle
karşılaştırılırken yazıcı o çıkışta durmaya devam eder. Bağımsız
`guest_wal_checkpoint.inspect(...)` okuyucusu kayıtlı konuğu, worker'ı, yerel özel
kilidi, snapshot'ı, material'ı, veritabanı kabulünü, canonical Before'u, aday ikiliyi,
kimlik bilgilerini, ortamı, çalışma dosyalarını ve WAL baytlarını yeniden kontrol
eder. `celikpanel/lab-native-wal-checkpoint/v1` kanıtı döndürür; bağlanmaz ve
sinyal göndermez.

Aile durmaya devam ederken ham çalışma DB/WAL/SHM dosyaları ve önceki WAL görüntüsü
sınırlı descriptor okumalarıyla kopyalanır. Hiçbir özgün dosya SQLite üzerinden
açılmaz. Controller kopyalamadan sonra bütün kontrol noktasını yeniden doğrular;
izleyici de tam birime SIGKILL izni vermeden önce görev, kabul ve WAL kimliğini
yeniden kontrol eder. `cut-sent` yalnız kesintiyi kaydeder; kurtarmanın başarılı
olduğu anlamına gelmez.

Bu, **sistem çağrısı çıkışında tutulan fiziksel noncommit WAL yazımını** kanıtlar.
SQLite Commit girişinin durumu bilinmez: gerçek migrator'da test işlem işareti
yoktur. Hiçbir sistem çağrısı argümanı, dönüş değeri, süreç belleği, ürün SQL'i veya
geçiş değiştirilmez. [Ayrıştırıcı testlerinin](WAL-FIXTURE.tr.md) gösterdiği gibi,
checksum'u geçerli noncommit baytlar geri almadan sonra da kalabilir. Bu baytlar
veya başarılı yazım, fsync ya da elektrik kesintisinde kalıcılık kanıtı değildir.

## Dolu veri ve son doğrulama

SQL düzeneği `.example.test` altında ilişkili, oturum açmayan kullanıcı, abonelik,
alan adı, takma ad ve rezervasyon kayıtları içerir. Hazırlık gerçek schema38'i
doğrular, özel bir kopya oluşturur ve yalnız ayrıca kayıtlı geçici başlangıç
sistemine, koordinatörler durmuşken ve yerel sürüm kilidi altında kurar.
Dosya değiş tokuşu özgün inode ve baytları korur; son yol adı ve descriptor
kontrolleri, sahibin değişikliğini geri almak yerine reddeder. Bu işlem site,
posta kutusu veya dışarıya hizmet veren alan adı kurmaz.

Kesintiden sonra düzenek systemd'nin yerel OnFailure kurtarmasını gözler.
Kurtarma, geri alma veya başka bir güncellemeyi elle başlatmaz. Kabul, aynı işlem,
snapshot ve kurtarma çalıştırmasını son ikililere, temizlenmiş işaretlere, kalıcı
veritabanı kayıtlarına ve hizmet gözlemlerine bağlamalıdır.

Ayrı bir son durum ölçümü, kararlı ham DB/WAL/SHM kopyası almak için zaten aktif
olan tam koordinatörleri kısa süre dondurabilir. PID/başlangıç zamanı, çalıştırma
kimliği ve cgroup, freeze/thaw çevresinde kontrol edilir; temizleme her zaman
yetkili thaw'ı dener ve doğrulanamayan sonucu bildirir. Bu ölçüm yerel kurtarma
sonrasında yapılır ve otomatik kurtarmanın parçası sayılamaz. SQLite incelemesi
thaw sonrasında yalnız özel kopyalarda çalışır. Eski 55 tablonun tümü, eski
sütunlar, rowid'ler ve tür bilgili değerler tablo dışlamadan karşılaştırılmalıdır;
yeni metrics satırları, eksik veya değişmiş eski satırlardan ayrı bildirilir.

## Korunan denemeler ve sınırlı kabul

| Deneme | Gerçek kesinti sınırı | Kurtarma gözlemi | Son kabul |
|---|---|---|---|
| S / Arch | `inconclusive`: kilit cihaz kontrolü reddetti; SIGKILL yok | İzleyici ayrıldı; güncelleme normal tamamlandı | WAL kesintisi kabulü yok |
| S / Debian 13 | Bağımsız doğrulanmış fiziksel WAL yazımı; tam birime SIGKILL; temizleme doğrulandı | Aynı snapshot ile yerel OnFailure geri alması; schema38 geri geldi | Kayıtlı sınır içinde PASS; kesinti anındaki tüm satırlar korundu |
| T / yeni Arch | Düzeltilmiş gözlemci; bağımsız doğrulanmış fiziksel WAL yazımı; tam birime SIGKILL | Aynı snapshot ile yerel OnFailure geri alması; schema38 geri geldi | Kayıtlı sınır içinde PASS; kesinti anındaki tüm satırlar korundu |

İki başarılı kesintide de daha önce gözlenmiş 32 baytlık WAL başlığı 4152 bayta çıktı:
commit frame olmadan, 4096 baytlık sayfa içeren tek eksiksiz frame. Başarılı
`pwrite64`, 56 konumunda 4096 bayt döndürdü ve gözlenen EOF'ta tam olarak bitti.

Debian son durum toplayıcısı `scoped-pass` bildiriyor: schema38 ve eski 55 tablonun
tümü korunuyor; eksik veya değişmiş eski satır yok, dışlanan tablo yok. İlişkili
test kayıtları dahil başlangıçtaki 126 satırın tümü korunmuş. Metrics satırları
33'ten 80'e çıktı; bu 47 ek kayıt nedeniyle bütün veritabanının eşitliği iddia
edilmiyor. Kabul edilmiş çalışma DB/WAL/SHM kanıtı değişmemiş. Sonuç SHA256:
`82d3b2bc1d7041dce8785ebb872070c493767db867bfa5dfd9e46a43ec37adb6`.

Önceki WAL ve kesinti anındaki tam WAL ile oluşturulan özel kopyaların bağımsız
incelemesinde görünür içerik eşit bulundu: 55 tablo, 153 satır, 1–38 geçişleri ve
iki alan adı. Özgün alınmış dosyalar değişmedi. Bu görüntüler daha sonraki kesinti
anını gösterir; 126 satırlık korunma karşılaştırmasının kaynağı ise dolu başlangıç
veritabanıdır. Bağımsız inceleme raporu SHA256:
`b455fed20f82d775a3b1afdf66d848b425007858525e2e6da54728077abaa117`.

Yeni Arch son durum sonucu da schema38 ve eski 55 tablonun tümünü bildiriyor;
başlangıçtaki 97 satırın tümü korunmuş, eksik veya değişmiş eski satır yok.
Metrics satırları 4'ten 33'e çıkmış. Sonuç SHA256:
`4bd6d1b07315c8c648791cc167ca8a587382471c00bc62a6d0df5aaddde1117d`.
Bağımsız Arch önceki-WAL/tam-kesinti-WAL incelemesinde de görünür içerik eşit
bulundu: 55 tablo, 107 satır, 1–38 geçişleri ve iki alan adı. Rapor SHA256:
`004589c2c9d8ea0e101bed2c1570b19de17a93d700b7ec984621c2503334d70b`.

Başlangıç korunması, daha sonraki kesintiye kadar eklenen satırları tek başına
kanıtlayamaz. Bu nedenle ana makinedeki ayrı karşılaştırmalar, kesinti anındaki
her satırı son veritabanıyla, 55 tablonun tümünde rowid ve tür bilgili değerleri
dahil ederek eşleştirdi:

| Son durum/kesinti kanıtı | S / Debian 13 | T / Arch |
|---|---|---|
| Kesinti → son durum satırları | 153 → 173 | 107 → 126 |
| Eksik / değişmiş kesinti satırları | 0 / 0 | 0 / 0 |
| Kesinti → son durum metrics | 60 → 80 (+20) | 14 → 33 (+19) |
| Korunma raporu SHA256 | `2d65179c722bf7fda1fdbef3108b52255e77d06d8d08dcdc10edf93297d73a8e` | `a27ff5807ab3e465436c4356bdc2e0c6e9d499778aea6ad67a31f4161912c0c6` |

Eklenen her satır, kesinti anındaki son örnekten sonraki bir metrics örneğidir.
Genel semantik eşitlik **DIFFERENT**; eksik veya değişmiş eski satır ve dışlanan
tablo yoktur. Son ham DB/WAL'den bağımsız türetilen özel yedekler, doğrulanmış son
yedeklerle eşleşir. Korunan özgün girdiler değişmedi.

Bağımsız son denetimde her sistem için 32 başvurulan dosya özeti kontrolü ve
21 kanıtlar arası bağ kontrolü geçti. İki kurtarma da çalışan/diskteki Alpha64
ikili eşleşmesini, schema38'i, özgün canonical Before inode'unu ve çalışma
DB/WAL/SHM kanıtının tümünü korudu; işlem işaretleri temizlendi. Material'ın
16 veri dosyası, seçili kitin 12 payload dosyası ve manifest'i, korunan 303 aday
dosyası ve 94 panel varlığı doğrulandı. Kaydedilmiş sahip birim beforeimage'ları
geri geldi. Dört yetkili DNS kontrolü, korunan TLS ve HTTPS 200 anlık kontrollerdir;
kesintisiz erişimi kanıtlamaz. Ayrı ölçüm sonrasında iki koordinatörün thaw'ı da
başarıyla tamamlandı. Kayıtlı konuklar koruyucuları üzerinden durduruldu; diskler
ve kanıtlar korunuyor.

İlk Debian son durum toplaması freeze/capture adımından önce reddetti: iki kanıt
okuyucusu aynı zamanları farklı biçimlerde, `{Sec,Nsec}` ve tamsayı nanosaniye
olarak gösteriyordu. Katı ve kayıpsız bir gösterim dönüşümü, ikinci toplamanın
aynı alanları karşılaştırmasını sağladı. İlk ret ve iki toplama denemesi korunuyor.
Güncelleme, kesinti veya kurtarma tekrarlanmadı; sonraki geçici freeze yalnız
yukarıdaki son durum ölçümü içindi.

İlk Arch gözlemcisi, kilit kaydının cihazını hatalı olarak `fstat().st_dev` ile
eşitledi. Btrfs, getattr üzerinden altbirim cihazını; çekirdek kilit kaydı ise
superblock cihazını bildirir. Düzeltme, tam FD'nin `mnt_id` değerini o PID'nin
mountinfo kaydından çözer; ayrı canonical yol/FD cihaz ve inode kontrolünü korur.
Namespace, seçili mount satırı, fdinfo ve FD metadata'sı yeniden okunur. Tam FD
kanıtının yerine genel inode araması koymaz. Bu davranış çekirdeğin
[Btrfs getattr](https://raw.githubusercontent.com/torvalds/linux/v6.16/fs/btrfs/inode.c),
[kilit raporlama](https://raw.githubusercontent.com/torvalds/linux/v6.16/fs/locks.c),
[fdinfo](https://raw.githubusercontent.com/torvalds/linux/v6.16/fs/proc/fd.c) ve
[mountinfo](https://raw.githubusercontent.com/torvalds/linux/v6.16/fs/proc_namespace.c)
uygulamalarına dayanır. Reddedilen S denemesi korunur; T yeni başlangıç durumuyla
açılır.

Son üst düzey çevrimdışı root koşusu umask077 altında 444 test çalıştırdı:
443 geçti; gerekli girdileri verilmediği için bir gerçek ikili girdi testi açıkça
atlandı. İç dizindeki 27 izleyici testi UID65534 altında ayrıca geçti. Dar root
koşularında da 22 dolu başlangıç, 21 kontrol noktası ve beş sınırlı kanıt okuyucusu
testi geçti. İzleyici testlerinde çekirdek sınırı taklit
edilir; kontrol noktası testlerinde gerçek yerel flock ve farklı Btrfs cihaz
durumu bulunur; başlangıç testleri özel dosya sistemi değiş tokuşunu ve
ret/temizleme yollarını çalıştırır. Bu yerel sayılar uzak CI sonucu veya gerçek sistem
kabulü değildir. Önceki kontrollü alt süreç deneyi, bu değişmemiş ürün yolundan
ayrı kalır.
CI, `test_native_trace.py` dosyasını içteki `waltrace` dizininden açıkça çalıştırır;
üst düzey test keşfi, çekirdek sınırını taklit eden bu testlerin yerine geçmez.
Üç dar root çağrısı, dolu başlangıç, kontrol noktası dosya sistemi ve sınırlı
okuyucu testlerini geçici yerel dosyalarda çalıştırır; VM açmaz.

Bu kayıt daha geniş P0.3 hata matrisini, elektrik kesintisinden kurtarmayı,
gözlenmemiş dosya sistemi/mimari durumlarını, kesintisiz hizmet erişimini veya
dolu gerçek barındırma kabulünü kapatmaz. Tamamlanan kapsam, kayıtlı her sistemde
tek bir fiziksel WAL yazım kesintisi, otomatik geri alma ve kesinti anındaki SQL
satırlarının eksiksiz korunmasıdır. Olası bütün geçiş kesinti noktalarını kanıtlamaz.
