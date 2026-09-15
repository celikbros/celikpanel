# Ayrı kopyada veritabanı dönüşümünün gerçek sistem deneyi

*15 Eylül 2026 · [English](ISOLATED-DATABASE.md) · D-025 / P0.3*

Bu kayıt [kaynak sözleşmesini](../../../docs/RECOVERY-ISOLATED-DATABASE.tr.md)
tamamlar. İlk Q adayı ile sonraki üst dizin kabul düzeltmesini ayrı tutar;
iki kaynak için koşulsuz ortak gerçek sistem kabulü verilmez. Deneyler hiçbir
müşteri sunucusunu değiştirmedi.

## Gerçek eski durum ve ölçülen sınırlar

Her temiz Arch ve Debian 13 konuğu sabit, yayımlanmış Alpha64
`3ee8dac009c7e3db1d940f9b8be078e186693a80` kaynağını kullanır. Kabul öncesinde
kurulu ve çalışan Agent/Panel özetleri ile 38 migration kimliğinin tamamı
kontrol edilir. Gerçek Agent BIND kayıtlarını hazırlar; A/SOA UDP/TCP otoriter
yanıtları, başlangıç TLS'i, yerel HTTPS ve `sqlite_sequence` dahil 55 SQLite
tablosu kaydedilir. Koordinatör unit'lerinin önceki halleri deney sahibinin
kayıtlı düzenlemelerini içerir.

İmzasız yerel aday, kayıtlı geçici deney düzeneği üzerinden uygulanır; genel
imzalı Agent/UI kabulü sınanmaz. VM UUID/nonce/DMI, ana makine süreci, worker
invocation/PID/başlangıç/cgroup, çalıştırılabilir dosya ve yerel kilit kontrolleri
her konukta tek kabul edilmiş güncellemeyi bağlar. Deney kurtarmayı başlatmaz,
kullanıcı geri alma komutu çalıştırmaz. Yerel OnFailure kurtarması aynı işlemi
tamamlamalıdır.

Arch `candidate-installed` noktasını hedefler. Donmuş süreçte ek DB gözlemi
gerçek durumu sınıflandırır; başlangıç görüntüsünün aynı kalması SQLite'ın hiç
transaction başlatmadığını kanıtlamaz. Debian `completion-database-verified`
noktasını hedefler. Yalnız bağımsız veritabanı hazır kanıtından sonra saklanan
üç sabit dosya (`rollback.sh`, `bin/agent`, `web/dist/index.html`) karantinaya
alınır ve tam güncelleyici öldürülür. Asılları deney karantinasında korunur.
Geçici yerel port tutma bu gözlem aralığını uzatır; kurtarma kanıtı değildir.

## Q: ilk kaynak

Ürün kaynağı `0610b239a7bb976874a2c347099bf88e779237d1`, ağaç
`5732227c911fc3469f6e9ae0d8068f156a2a41c1`; deney kaynağı
`adf7a1216a33d5ad12dc7f800b95ee88cee67b1f`. Aday arşiv SHA-256:
`05342cf1274b09184b4bef6535fb91849fbfecb4bb1b8e2555ec0ed6d311e45f`.
Kurtarma kiti manifest SHA-256:
`2f8981fa0db5688811757f5363cb860e17317f5c7b95b7a5c8563a8ee17f7def`.

İki arıza zinciri hedeflenen sınırları gözledi. Arch'ta asıl Before ve çalışma
Initial görüntüleri değişmemişti: schema38 ve tüm 38 kimlik, yalnız
`admission.json`, sonraki kayıt/build/yan dosya yoktu. Sonuç açıkça
`PASS-state-only`; migration WAL'ı ortasında kesinti değildir. Debian,
material v3 ve seçili bağımsız tam şema/geçmiş doğrulayıcısının başarısından
sonra completion.pending noktasında üç dosyayı karantinaya alıp süreci öldürdü.

İlk son durum okumaları Arch'ta otomatik eski Alpha64 geri almasını, Debian'da
adayın ileri tamamlanmasını gösterdi. İkisinde çalışan/disk binary özetleri
eşleşti, hizmetler aktifti, işlem işaretçileri kalkmıştı ve HTTPS 200 verdi.
Arch schema38/55 tabloyu korudu; Debian schema42/65 tabloya ulaştı. Her tam
snapshot 124, material verisi 16 dosya içerir.

Son toplama ve bağımsız incelemede Arch 25, Debian 29 sınırlı kontrol geçti;
indeksli 87/70 dosyanın tamamı yeniden özetlendi. Ayrı veritabanı/material
incelemesinde 27/43 kontrol geçti. Arıza kayıtları donmuş ve öldürülmüş worker'ı
yerel OnFailure ve aynı son işlemle bağlar. Debian'ın ilk iki olaylık kaydı,
son sekiz olayın başlangıcıdır; ikinci bir arıza değildir.

| Q kanıtı | Arch | Debian 13 |
|---|---|---|
| İndeks SHA-256 | `ba146e922e374918aa909d6b0480b93c02c582532af871d7e5a36f65b5b7a6db` | `9f7cca4dd723f31140f9302bc3d811e3db6da8007144920cb43da76c98acbbdf` |
| Sonuç SHA-256 | `c6148c5d5059d7fb2d2d0abd0ad97810ceb047410580a2fe82a07d3ffb420ee0` | `f51fa4ab51c3efc6ad21e03644331bb50706e92d3550d01d86d98ca6558740b6` |

Arch özgün asıl inode'u, başlangıç çalışma görüntüsünü ve admission kaydını
korudu. Debian'ın ham admission → seal → publication → published özet zinciri,
saklanan özgün Before inode/özet/mtime ve asıl After inode'u doğrulandı. Sonuç
kesinleştikten sonra kilit veya işlem işaretçisi uydurarak yalnız etkin işlemde
kullanılabilen doğrulayıcı çağrılmadı. Gerçek sonlandırma öncesi kontroller, kalıcı
kayıtlar ve seçili salt-okur tam şema/geçmiş/kuyruk denetleyicisi ayrı kanıtlardır.

Eski 55 tablonun tamamı eski kolon, rowid ve türü korunmuş değer izdüşümleriyle,
`excluded_tables=[]` kullanılarak karşılaştırıldı: eski satır kaybı veya değişimi
yok. Genel veritabanı sonucu iki sistemde de **DIFFERENT**. Arch metrics 99'dan
118'e, Debian 101'den 120'ye çıktı; her sistemde eklenen 19 örnek snapshot'tan
sonra. Debian'da ayrıca beklenen on yeni tablo, iki yeni domain kolonu ve tam
39–42 migration kayıtları var; 1–38 kimlikleri ve zamanları aynı kaldı. Domain
kayıtları boştu; dolu domain dönüşümü kanıtlanmadı. Yerel toplayıcı sınırlı
özetler dışa aktarır; ana makinede tekrar okumak için ham kullanıcı satırı çıkarmaz.

Arch kaydedilmiş sahip unit önceki hallerini geri yükledi. Debian yeni aday
unit'lerini kullanır; düzenlenmiş eskileri snapshot'ta korunur, canlı unit
eşitliği iddia edilmez. Son durum DNS/TLS/HTTPS kontrolleri geçti; kesintisiz
hizmet erişimi ölçülmedi. Öldürme sonrası thaw reddi ve kullanılamayan bir durum
gözlemi, başarılı gözlem gibi yorumlanmadan korunur.

İlk ana makine zaman damgası çağrısı ve ilk mühür betiği gözlemlerini bitirmeden
hata verdi. Düzeltilen yalnız ana makine toplayıcıları bu hataları korur; ikinci
konuk güncellemesi, arızası veya kurtarması başlatmadı. Q konukları mühürden
sonra kayıtlı kontrollerle durduruldu; diskler ve özel Q kanıtları korunuyor.

## Üst dizin kabul düzeltmesi

Kaynak incelemesi, yeni normal ön kontrol ve Before yakalamasının root sahipli
veya 0700 dizini kabul ettiğini; kurucu ve hizmet yaşam döngüsünün ise
panel:panel0750 beklediğini gösterdi. Mevcut snapshot üreticisi de önceki
karantina düzenini normalleştirebilir. Snapshot sonrasındaki sahip değişikliğini
yeni Before olarak kabul etmek, normalleştirmeye ve ardından kurtarma kimlik
uyuşmazlığına yol açabilir.

`cb3165456bb4ba4654dc19d51a5eafc13721a5fb` yeni ön kontrol/yakalamada
desteklenmeyen dizin düzenini reddeder, metadata'yı korur ve türü belli kullanıcı
yönlendirmesi verir. Tarihsel material okuyucuları ve karantina kurtarması aynı
sözleşmeyi korur. Düzeltme öncesinde sekiz yeniden üretim testi başarısızdı.
Sonrasında normal ve race koşularının her birinde 398 test geçti; dosya sistemi
desteğine bağlı mevcut iki xattr testi her koşuda atlandı. Vet geçti. Tarihsel
material okumaları ve eşzamanlı sahip değişiklikleri de bu kapsamdadır.

Q'nun olağan panel:panel0750 sonucu yalnız kayıtlı kaynağını kanıtlar;
düzeltilmiş adayı kanıtlamaz. Aşağıdaki R kaydı düzeltilmiş kaynağı ayrı ele alır.

## R: temiz konuklarda son kaynak

Aday kaynağı `cb3165456bb4ba4654dc19d51a5eafc13721a5fb`, ağaç
`336778673eb5bdfb19515a626e2e7c4bf08e3b5b`; deney kaynağı
`1919140c4350085b3b81bd8023dfc0e034fa2ad5`. Sabit Go 1.26.5 derlemesindeki altı
binary ve gerçek arayüz derlemesi; 303 paket dosyası ve 192 sabit Git dosyasıyla
birlikte doğrulandı. Arşiv SHA-256:
`52dd34b435ba6d1fd1429aaadf875bf0cacae9f0cbeeb249ea9795e74a08698d`.
Seçili kurtarma kiti manifesti:
`ed7c3eebe46d7ee5a1ad4b566ef4794daf295cb695151be40a4577b8fdc5a770`.
Alpha81 arşiv etiketi yayımlanmamış yerel test ürünüdür; sürüm yayını değildir.

| R kimliği | Arch | Debian 13 |
|---|---|---|
| Mühürlü dosya | 62 | 63 |
| İndeks SHA-256 | `976ad91c5d96f6c6e0103fa3717056075bd45a94bebed9da44fcd7f80ba460c6` | `89283f97a8cd8883917e12d95e6b5d297c2a119c9ab6a02f10582fb2edbe13e0` |
| Sonuç SHA-256 | `c6a4bd65b9e833ee10ebbd54eeb1ea8c8fdbd3c27c71e2b0214edb0b9b4d31ba` | `be74c999f8ecac73a2254fc9d2f66151cbbb739ba5c7e07042cfd06ec29cd0e4` |

Yeni kayıtlı konukların her birinde gerçek Alpha64/schema38 başlangıcının 13
kontrolü geçti. Konuk başına bir güncelleme ve bir gerçek worker SIGKILL kabul
edildi. Arch'ın altı olaylık zinciri otomatik geri almaya; Debian'ın sekiz olaylık
zinciri tam snapshot'ın ileri tamamlanmasına ulaştı. Her son kurtarma çağrısının
kimliği mühürlü özel kanıtta bağlıdır.
Sonraki etkisiz zamanlayıcı çağrıları ayrıdır. Elle kurtarma veya ikinci güncelleme
başlatılmadı.

Arch 25, Debian 29 sınırlı sonuç kontrolünün tamamı geçti. Arch eski çalışan/disk
ikili dosyalarını, özgün asıl inode'u, ilk çalışma görüntüsünü ve schema38/55
tabloyu korudu; yalnız admission kanıtı vardı. Donmuş DB gözlemi yine yalnız ilk
durumu kanıtlar. Debian, saklanan üç sabit dosya ve güncelleyici kaybından önce
schema38→42 dönüşümünü bağımsız doğruladı. Tam ham yayın makbuzu zinciri, özgün
Before karşılığı ve yeni asıl inode; schema42/65 tablo ve 303 saklanan aday
dosyasından 300'ü korundu. Her tam snapshot 124, material dizini 16 dosya içerir.

Eski 55 tablo izdüşümünde bütün eski satırlar, değişiklik veya dışlama olmadan
korundu. Genel karşılaştırma **DIFFERENT**: Arch metrics 15→19, Debian 16→20;
dört ek örneğin tamamı snapshot'tan sonra. Debian'ın on yeni tablosu, iki domain
kolonu ve 39–42 migration kayıtları amaçlanan diğer farklardır. Boş domain ve
yalnız toplu veri kanıtı sınırları aynıdır.

Son DNS, sunulan TLS, HTTPS 200, seçili salt-okur denetleyici ve işlem işaretçisi
yokluğu kontrolleri geçti. Arch sahip unit önceki hallerini geri yükler; Debian
aday unit'lerini kullanıp özgün sahip dosyalarını snapshot'ta korur. Kesintisiz
erişim veya sahibin keyfi metadata düzenini koruma kanıtlanmaz. İki öldürülmüş
worker kaydı da `thaw_exit=1` tutar; kullanılamayan durum mesajı da korunur. R,
mühürlü sonuca ulaşmak için başarısız toplayıcı veya tekrarlı gözlem gerektirmedi.

İki bağımsız ana makine incelemesi, 62/63 mühürlü dosyanın her birini yeniden
özetleyip arıza/OnFailure zincirlerini doğruladı. Ayrı DB/material/makbuz
incelemesinde 28/44 ek kontrol geçti. Doğrulamadan sonra iki R konuğu kayıtlı
kontrollerle durduruldu; diskler ve özel R kanıtları korunur.
Kabul, desteklenen normal üst dizin düzeni ve bu iki arıza sınırı içindir;
P0.3'ün tamamlandığını göstermez.

## Sınırlar

Tüm tablo karşılaştırması `excluded_tables=[]` tutar ve her gerçek farkı bildirir.
Yeni şema nesneleri, domain kolonları, dört yeni migration kaydı ve canlı metrics
yazımları bütün veritabanı eşitliği olarak adlandırılmaz. Dolu domain dönüşümü,
gerçek kesilmiş migration WAL'ının korunması, atomik değişim/makbuz arası yerel
kesinti, aktif yenileme zamanlaması, genel TLS güveni, eş DNS aktarımı, barındırılan
web/posta/veritabanı işleri ve kesintisiz erişim ayrı gözlem gerektirir.

Tam kill/reboot matrisi, genel imzalı kabul, eksik snapshot yakalama, sahibin
metadata geçişleri, sonraki ilgisiz sahip/veri değişiklikleri, kanıt temizliği
ve eski schema17/pre-ledger geçişleri açıktır. P0.3 kısmi kalır.

Sonraki [gerçek WAL deneyleri](NATIVE-WAL.tr.md), dolu SQL verisiyle tek bir fiziksel, commit edilmemiş dönüşüm yazısını ve otomatik geri almayı ayrı olarak sınar. Yukarıdaki Q/R kanıtı kendi kontrol noktalarıyla sınırlıdır; dolu domain verisinin başarılı dönüşümü ve gerçek barındırma kabulü açık kalır.
