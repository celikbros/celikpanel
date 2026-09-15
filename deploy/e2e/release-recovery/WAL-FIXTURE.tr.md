# WAL kesintisi önkoşulları ve dolu SQL örneği

Bu değişiklik yalnız deney araçlarını kapsar; D-025 / P0.3 ve 2–5 numaralı
ilkelere hizmet eder. Ürün migration'ı, yayın protokolü, kurtarma verisi, kurulu
sunucu yapılandırması veya lisans politikası değişmez. `recovery-material/v3`
ve desteklenen 38→42 geçişi aynıdır. Gerçek migration WAL kesintisi ve dolu
barındırma kabulü **açıktır**.

## Kanıtın sınırı

Checksum'u geçerli bir WAL kuyruğu SQLite geri aldıktan sonra da kalabilir.
Tek başına açık işlem kanıtı değildir. `wal_frames.py`; sınırlı header/frame
baytlarını, iki checksum byte sırasını, salt'ları, önceki tam prefix'i ve dosya
kimliğini kontrol eder. Asıl dosyaları SQLite ile açmaz. Kesilmiş, yeniden
yazılmış, sıfırlanmış veya eski kuyruk `unknown` kalır; sayfa içeriği sonuçta
çözümlenmez, hash alınır. Deney şeması
`celikpanel/lab-sqlite-wal-evidence/v1`'dir.

`wal_migration_identity.py`, önceden doğrulanmış kabul ile iki verilen gözlemi
karşılaştırır: işlemin cgroup'u, executable, komut, daraltılmış ortam, root
olmayan kimlikler, çalışma dizini ve WAL inode'u. Şema
`celikpanel/lab-migration-writer-match/v1`'dir. Kabulü doğrulamaz, süreç okumaz,
sürece bağlanmaz ve sinyal yetkisi vermez. Gerçek denetleyici kernel gözlemini,
duran görevlerin tam listesini ve yazının aynı işlemden geldiğini ayrıca kanıtlar.

## Kontrollü yazıcı deneyi

[`waltrace/`](waltrace/), depodaki değişmemiş modernc SQLite bağımlılığıyla geçici
bir Go çocuk süreci derler. Küçük cache ve büyük açık işlem bu yapay örnekte
WAL'a yazmayı zorlar; ürün SQL'i değişmez. İzleyici yalnız yeni oluşturduğu
bekleyen çocuğa ve kernel'in bildirdiği alt süreçlerine bağlanabilir. İstenen
herhangi bir PID'ye bağlanma modu yoktur. Başarılı `pwrite64` çıkışını gözler,
tamamlanmış prefix'i korur, kabul edilmiş bütün görevleri durdurur ve aynı
inode'a eklenmiş, checksum'u doğru tam noncommit frame gerektirir. Çocuk
işaretleri bu örnekte transaction'ın başladığını ve Commit'e girilmediğini
kanıtlar; gerçek migrator'da bu işaretler yoktur ve gerçek kabul yerine geçmez.

Kontrollü süreç ailesi sonlandırılır ve çıkışları toplanır; izleyici ölürse
EXITKILL devrededir. Ayrı SQLite kopyası yalnız tamamlanmış kaydı görmelidir.
Asıl DB/WAL/SHM baytları ve metadata korunur. Tamamlanan küçük işlem, zaman aşımı,
desteklenmeyen kernel durumu ve eksik kanıt `inconclusive` kalır; başarılı
kesinti sayılmaz. Şema `celikpanel/lab-waltrace-feasibility/v1`'dir.

Yerel deneyde 12416 offset'inde 4096 baytlık başarılı yazı gözlendi:
12392 bayt / 3 frame tamamlanmış prefix, 16512 bayt / 4 frame oldu; bir noncommit frame
eklendi, commit işareti eklenmedi. Bu kontrollü iş yükü sonucudur; gerçek
güncelleme/otomatik geri alma kabulü değildir. CI bunu iletişimi taklit eden
testlerden ayrı çalıştırır; VM başlatmaz.

## Dolu özel veritabanı

`populated_database.py`, yalnız standalone özel kaynağı sınırlı descriptor ile
okur. Sahibe özel yeni dizin ve kopya üretir; mevcut hedefe veya ürün yoluna
örnek veri yazmaz. Yeni kopyayı değiştirmeden önce kaynak SHA'sı, gerçek Alpha64
commit'i, 38 migration kimliğinin tamamı ve tam şema doğrulanır. Oturum açamayan
bir kullanıcı, abonelik, ilişkili iki domain, alias ve asıl trigger'ların
oluşturduğu altı hostname reservation eklenir. Adlar `.example.test` altındadır.
DNS, site, posta kutusu, hizmet, engine kaydı veya lisans hazırlanmaz.

Örnek eklenirken dört AUTOINCREMENT sayacının beklenen ilerlemesi ayrıca yazılır.
Sonrasında `sqlite_sequence` dahil **55 eski tablonun tamamı**, eski sütunlar,
rowid ve tipli değerler karşılaştırılır; dışlanan tablo yoktur. Eski satırın
kaybı veya değişmesi reddedilir. Yeni satırlar fark olarak raporlanır; bütün DB
eşit denmez. 42 kontrolü ayrıca 65 tabloyu, 42 migration kimliğini ve dolu
domain'lerde `local` / boş bağlantı varsayılanlarını gerektirir. Deney şemaları
`celikpanel/lab-populated-database-admission/v1` ve
`celikpanel/lab-populated-database-manifest/v1`'dir.

Ayrı yerel deney, yayımlanmış gerçek Alpha64 paneliyle yeni özel schema38 DB
oluşturdu; aday panel dolu kopyayı 42'ye dönüştürdü. İşlem root olmayan hesapla
çalıştı. 55 tablo / 91 eski satır ve örnek ilişkileri korundu; kaynak baytları
değişmedi. Bu, seçili ikililerin özel kopyada SQL dönüşümünü kanıtlar; kurulu
güncelleyicinin kabulünü, snapshot yayınını veya gerçek iş yükünü kanıtlamaz.

| Sabitlenen girdi | Kimlik |
|---|---|
| Eski kaynak | `3ee8dac009c7e3db1d940f9b8be078e186693a80` |
| Eski arşiv SHA256 | `c669e52a6686865e9fc0515e2839f4cfea7501730877e48654a35def63557979` |
| Eski panel SHA256 | `c6e91e9e478d261c104397518fdae0145733832be7eeb3e53a123ef30c5d7460` |
| Aday kaynak | `cb3165456bb4ba4654dc19d51a5eafc13721a5fb` |
| Aday panel SHA256 | `74e5b00e674d720bc40caea0e2fe46c04d8d8a9d9d5085496983a7c26cc121ad` |

## Sınırlı kontrolleri tekrarlama

Normal çevrimdışı takım parser, kimlik eşleştirici ve özel kopya testlerini bulur.
Gerçek ikili testi, iki açık girdi yoksa bunu belirterek atlar:

```bash
python3 -m unittest discover -s deploy/e2e/release-recovery -p 'test_*.py' -v
bash deploy/e2e/release-recovery/waltrace/run-local.sh
```

İzleyici çalıştırıcısı Go 1.26.5 linux/amd64 ve önceden mevcut modüller gerektirir;
araç veya paket kurmaz. `CELIKPANEL_WALTRACE_GO` tam yerel aracı seçebilir.
Mevcut Git kaynağını arşivler, çalışma kopyasından aldığı her deney dosyasını
kaydeder ve çalıştırma sonrası hashleri tekrar kontrol eder. Parent/child ptrace
izin verilen Linux'ta normal kullanıcıyla çalıştırılır.

Ayrı gerçek ikili testinde root olmayan kullanıcı,
`CELIKPANEL_POPULATED_OLD_PANEL` ve `CELIKPANEL_POPULATED_CANDIDATE_PANEL`
değişkenleriyle yukarıdaki iki sabit panel hashine uyan yerel dosyaları gösterir.
Test kurulu ürün yollarını reddeder, doğrulanmış ikilileri yeni özel dizine
kopyalar ve yalnız özel veri yoluyla `--migrate-only` çalıştırır. Komut:
`python3 -m unittest discover -s deploy/e2e/release-recovery -p 'test_populated_database_binaries.py' -v`.
Başarılı ve başarısız bütün denemeler korunur. Bu komutlar panel kurmaz veya
güncellemez.

## Doğrulama kaydı

Son çevrimdışı takımda 389 testin 388'i geçti; açık ikili dosya girdileri olmayan
bir gerçek ikili testi belirtilerek atlandı. Kontrollü yazıcıda root ve UID65534
ile ayrı ayrı 15 test geçti. Go 1.26.5 derleme/vet ve komut envanteri kontrolü geçti.
Sabit gerçek ikililerle son UID65534 koşusunda iki test geçti; bunlar FIFO yarış
reddi ve dolu özel kopyanın 38→42 dönüşümüdür. Asıl kaynağın ayrı before/after
kayıtlarının SHA256'sı aynıydı:
`817d3e5279d80e1db33a08320f8ffd0c3dd4bf51741ac91909d52577bd0b8cb0`.
Son gerçek ikili sonuç SHA256:
`08b49883a45b0c65c0cb66393c281a533d89d114f9718092be59304409b46c69`.
Özel kayıtların yolları ve örnek verileri yayımlanmaz.

## Kalan kabul

Gerçek denetleyici, kayıtlı geçici VM'yi ve tam güncelleme işlemini, kabulü ve
WAL yazısını ürün SQL'ini veya syscall sonucunu değiştirmeden bağlamalıdır.
Gerçek sudo/nonleader-exec yaşam döngüsünü desteklemeli ya da reddetmeli;
duran süreç listesini, başarısız DB/WAL/SHM'nin korunmasını ve aynı işlemin
otomatik kurtarılmasını kanıtlamalıdır. DDL uygun yazı gözlenmeden bitebilir;
bu sonuç belirsiz kalır. Dolu SQL satırları gerçek barındırma kanıtı değildir.
Bu değişiklik ve önceki Q/R ilk kopya kontrolü, bu kabul maddelerini veya geniş
P0.3 arıza matrisini kapatmaz.

Sonraki [gerçek WAL deneyi](NATIVE-WAL.tr.md), bu ayrı adaptörü ve sınırlı fiziksel yazma/otomatik geri alma gözlemlerini kaydeder. Kontrollü yazıcının Begin/Commit işaretlerini kullanmaz; önceki önkoşul sonucunun kapsamını genişletmez.
