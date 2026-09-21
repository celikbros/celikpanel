# Kurtarmada sınırlı deneme

*21 Eylül 2026 · D-025 ilkeleri 2–5 · P0.2/P0.3 kısmi uygulama*

## Sorun ve kapsam

Yerel kurtarma zamanlayıcısı, önceki çağrı sonlandıktan 30 saniye sonra çalışır.
Kalıcı deneme sınırı bulunmadığından aynı kesin hata, değişiklik yapan kurtarma
gövdesini sınırsız yeniden çalıştırabiliyordu. Tek başına zaman aşımı veya systemd
başlatma sınırı, işleme bağlı ve yeniden başlatmada korunan bir sınır sağlamaz.

Yürütücü artık **aynı snapshot için en fazla üç otomatik alt işlem** başlatır.
Bu, bütün hataları kalıcı sayan bir sınıflandırma değil, ihtiyatlı toplam deneme
sınırıdır. Hak, alt işlem başlamadan önce harcanır; kesilmiş veya sonucu belirsiz
deneme harcanmış kalır. İşletim sistemi geçişini bekleme, kilit çekişmesi, bekleyen
işlem bulunmaması ve salt-okur son doğrulama hak tüketmez. Zamanlayıcı açık kalır;
sınır dolunca aynı işlemi gözlemleyebilir fakat yeni kurtarma alt işlemi başlatamaz.

## Kalıcı sözleşme

`/var/lib/celikpanel-release-state/recovery-dispatch/v1/<snapshot>/`, işlem
belirteçlerinden, uygulama verilerinden ve yetki vermeyen gözlemlerden ayrıdır.
Root'a özel 0700 dizinlerinde `1`, `2`, `3` adlı, üzerine yazılmayan root:root 0600
kayıtları tutulur. Şema `celikpanel-recovery-dispatch/v1`; alanlar sırasıyla
`schema`, `snapshot`, `attempt`, `token_sha256`, `operation`, `phase` olur.
Her satır ve son satır yeni satır karakteriyle biter.

Snapshot kimliği güncellemeden geri almaya geçiş boyunca sabittir. Token özeti,
o denemenin kaynağını kaydeder; dışarıdan verilen bir değişiklik yetkisi değildir.
Otomatik kayıtlar kesintisiz sıra oluşturmalıdır. Yanlış izin, bağlantı, FIFO,
yanlış snapshot/şema, kesilmiş içerik veya ortada eksik kayıt işlemi durdurur.
Sahibin tekrar komutu bu denetimleri atlayamaz ve izinleri değiştiremez.
Mevcut yerel kilit; inceleme, hak ayırma ve alt işlem başlatmayı birlikte korur.
Geçici kayıt kalıcı diske yazılır, mevcut hedefin üzerine yazmadan yayımlanır;
alt işlemden önce dizin de kalıcılaştırılır. Yayımlanmamış `.pending.*` dosyaları
kanıt olarak kalır, alt işlem yetkisi vermez. Yayımlanmış kayıt, alt işlemden önce
kesinti olmuş olsa bile harcanmış sayılır. Tamamlanınca kayıtlar silinmez; bu
çalışma kanıt temizleme mekanizması eklemez.

İşlem, snapshot, hizmet ve yetki denetimleri korunur. Kaydı olmayan eski işlem
üç hakla başlar; geçmiş denemeler sonradan uydurulmaz. Eski seçili kitler bu
politikayı uygulamaz. Yeni kaynak, mevcut doğrulanan kit geçişiyle etkinleşir;
veritabanı, gözlem v1, snapshot biçimi ve kit protokolü değişmez. Eski CLI, yeni
tekrar argümanlarını sessizce yetki saymak yerine reddeder.

## Sahibin eylemi ve gözlemler

Sınır dolunca aynı işlem `recovery_required`, `terminal_proof=none`,
`reason=recovery_incomplete` olarak bildirilir. Önceden doğrulanmış hata hem Go
hem shell üreticisinde korunur. Doğrulanmış nihai sonuç önceliğini korur.
Servis günlüğü nedeni ve tam işlem için şu komutu gösterir:

```
sudo /usr/libexec/celikpanel/recovery recover --retry --snapshot <bekleyen tam snapshot>
```

Sunucu sahibi önce `journalctl -u celikpanel-release-recovery.service` çıktısını
inceleyip bildirilen nedeni giderir. Komut yalnız o snapshot için **tek** alt
işleme izin verir. Yeni güncelleme başlatamaz, başka işlemi seçemez, hazır olma,
kilit veya kanıt denetimlerini atlayamaz; otomatik hakları yenilemez. Özel
`owner.<rastgele>` kaydı bu açık denemeyi saklar. Kilit doluysa, işlem yoksa veya
snapshot farklıysa sonraya yetki kaydetmeden reddedilir. İşletim sistemi hâlâ
geçişteyse yalnız ertelenir; açık izin sonraki çağrıya taşınmaz.

HTTP/CLI v1 mevcut kurtarma-gerekiyor durumunu korur. Yerel yürütücü ayrıca
aynı durum kaydına bağlı otomatik deneme sınırı bilgisini yayımlar; sözleşme
aşağıdadır. Eski okuyucular genel yönlendirmeyi korur. Durum sorgusu tekrar
yetkisi vermez.

## Kanıt ve açık kabul

Gerçek shell yürütücüsünün sözleşme testi, izole dosya sisteminde modellenmiş
systemd hazır oluşu ve test alt işlemiyle geçti. Üç hak ayırma, yürütücünün SIGKILL
ile kesilmesi, yeni süreçte sınırın korunması, kilidin bırakılması, değişmeyen
belirteçler, sahip tekrarı, değişmeyen otomatik kayıtlar, güvensiz/kesik/eksik
kayıtlar, yabancı snapshot reddi ve nihai doğrulama kapsandı. Gerçek geri alma giriş
noktasının kilit devri ve bağımsız kit shell sözleşmeleri de geçti. Shell/Go hata
korunumu ile yarış denetimli CLI/gözlem testleri geçti.

[Debian AL gerçek sistem kabulü](../deploy/e2e/release-recovery/DISPATCH-BUDGET.tr.md),
yeni kit seçimini, ilk kayıttan sonra yeniden başlatmayı, iki ek gerçek kesintiyi,
otomatik hakların tükenmesini ve değişmeyen otomatik kayıtlarla tek sahip tekrarını
kanıtladı. CLI ve kimlik doğrulamalı HTTP nihai geri alma sonucunda eşleşti.
Önceki AJ/AK sonuçları yeni politikanın kanıtı sayılmaz. Kayıt yayın sınırındaki
güç kaybı, gerçek tekrarlanan kesin hatalar, tarayıcıdaki özel
yönlendirme ve geniş kesinti/hizmet matrisi açıktır. Üretim sürümü veya kurulu
kullanıcı paneli değiştirilmedi. P0.2/P0.3 açık kalır.

[Arch AN kabulü](../deploy/e2e/release-recovery/DISPATCH-BUDGET.tr.md#arch-an-kabulü),
AL ile aynı adayda üç otomatik deneme sınırı ve açık kullanıcı komutuyla devam
sınırını doğrular. Önceki AM gözlemci hatası sonuçsuz olarak korunur. Birden çok
açılış bekleme yayını, tek kaydın değişmeden korunmasından ayrılır. Yalnız bu
Arch vakası kapanır; yayın anındaki güç kaybı, tarayıcı yönlendirmesi ve bütün
P0.2/P0.3 kabul kapsamı açık kalır.

## Uyumlu durma yönlendirmesi (2026-09-22)

D-025 ilkeleri 2, 4, 5 / P0.2. Yeni bilgi yalnız yerel yürütücünün üç otomatik
kaydı doğrulayıp yeni deneme başlatmadığı dalda yayımlanır. `<istek>.automatic`
dosyası `celikpanel-recovery-automatic/v1` şemasını ve beş kanonik, satır sonlu
alanı kullanır: `schema`, `request_id`, `observation_identity`,
`observation_sha256`, `automatic_recovery=paused_retry_limit`. Mevcut sekiz alanlı
gözlem v1 kaydı değişmez. Veritabanı veya kurtarma kiti protokolü göçü yoktur.

İsteğe bağlı dosya bekleme sözleşmesini izler: en fazla 2 KiB, tek bağlantılı
normal dosya, root sahipliği ve panel grubuyla 0640; doğrulanmış 0750 gözlem
kökünün altında. Mevcut yayın kilidi altında, durum dosyası yenilendikten sonra
o dosyanın tam baytlarına ve nanosaniyeli GNU-stat kimliğine bağlanır. Aynı
baytları yazan eski üretici bile yeni dosya kimliğiyle önceki ipucunu geçersiz
kılar. Bozuk, güvensiz, eskimiş veya desteklenmeyen ek bilgi yok sayılır; geçerli
v1 sonucu okunmaya devam eder. Nihai kanıt üstün gelir. Ek bilginin yayımlanamaması
kilidi tutamaz veya yeni kurtarma başlatamaz. Eski okuyucu/üretici bunu bilmek
veya silmek zorunda değildir. Değişiklik yetkisi özel deneme kayıtlarındadır;
bu halka açık gözlem yetki vermez.

Yönetici ekranı ve kullanıcı CLI'si kaydedilen üç deneme sınırını açıklar,
sorumlunun sunucu sahibi olduğunu söyler, kurtarma günlüğü komutunu gösterir.
Kullanıcı bildirilen nedeni giderir ve günlükte aynı işlem için gösterilen tek
seferlik devam komutunu kullanır. Genel hatadan sınırın dolduğu çıkarılmaz;
yeni değişiklik API'si eklenmez. İşlem kimliği, gözlem zamanı ve önceki hata
korunur. Sonraki okuma başarısızsa son doğrulanmış bilgi, güncel sonucun bilinmediği
açıklamasıyla görünür kalır. Kontrol ve sayfa yenileme salt okumadır.

Doğrulama: gerçek shell üreticisi ve Go okuyucusu arasında uyumluluk; eskime,
aynı baytlarla tekrar yayın, yanlış istek/özet/kimlik/şema, güvensiz izin,
bağlantı ve FIFO; nihai kanıt üstünlüğü ve önceki hata; CLI ve yalnız yöneticiye
açık HTTP; modellenmiş systemd ve sınırlı alt işlemle tam yürütücü sözleşmesi;
462 web testi ve üretim derlemesi. Yerel Chrome'da EN/TR, 1440/390 px, yeniden
yükleme, sonraki okuma hatası, yalnız GET, taşma ve sayfa hatası kontrol edildi.
Bu tarayıcı yanıtları ve shell işletim sistemi durumu test verisidir. AL/AN,
önceki yerel deneme sınırını kanıtlar; **yeni ek bilgiyi kanıtlamaz**. Yeni bilginin
yerel yürütücüden tarayıcıya kabulü ve Panel durmuşken erişim açıktır. Üretim
sürümü veya kurulu kullanıcı paneli değiştirilmedi.

[Debian AO yerel kabulü](../deploy/e2e/release-recovery/DISPATCH-BUDGET.tr.md#debian-ao-yerel-durma-yönlendirmesi)
yeni durma kaydının üç gerçek kesintiden sonra seçili CLI'a TR/EN ulaştığını ve
tek kullanıcı devamıyla doğrulanan geri almanın eski durma kaydından üstün olduğunu
kanıtlar. Eski uygulamalar geri yüklenir; sonrasında yetkili HTTP aynı sonucu verir.
Debian'da yeni kayıttan CLI'a kabul kapanır; Panel durmuşken tarayıcı erişimi ve
tüm P0.2/P0.3 matrisi açık kalır.
