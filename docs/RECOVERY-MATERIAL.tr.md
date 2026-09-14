# Bağımsız kurtarma verisi

*14 Eylül 2026 · [English](RECOVERY-MATERIAL.md) · D-025 / P0.3*

Bu kaynak değişikliği, [bağımsız kurtarma çalışma ortamına](RECOVERY-RUNTIME.tr.md)
tamamlanmış v6 yedeğinden geri alma için ayrı bir veri sözleşmesi ekler. Bütün
dayanıklılık kabul matrisini tamamlamaz; kurulu sunucularda güncelleme başlatmaz.

## Sınır ve biçimler

Önceki geri alma bağımsız kod çalıştırsa da başarısız aday sürümün saklanan dosya
ağacının tamamına ihtiyaç duyuyordu. Aday dosyalarının kaybolması, sağlam eski
yedekten kurtarmayı da engelleyebiliyordu.

İlk şema veya ürün değişikliğinden önce seçili kurtarma programı, sabit özel dizin
altına `celikpanel/recovery-material/v1` kaydını mühürler. Dizin anahtarı doğrudan
kurallı yedek adının SHA-256 özetidir; ilgisiz eski kayıtlar taranmaz. Kayıt tam işlem token
özetini, v6 yedeğin adını ve manifest özetini, adayın kökenini, eski/yeni ürün
ağaçlarının tanımlarını ve sabit kurtarma verisi envanterini birbirine bağlar.
Yedek biçimi 6 değişmez.

Veride sürüm kökeni, kurtarma protokolü ve sıra politikası, kurtarma altyapısının
karşılaştırma dosyaları ve üç ürün servisinin unit dosyaları bulunur. Aday
Agent/Panel/web içeriği ve aday install/rollback girişleri kopyalanmaz. Saklanan
script baytları karşılaştırma verisidir; kod ayrı seçili kurtarma ortamından
çalışır. Bin/web yayınlama kaydı v2, kurtarma verisinin özetine bağlanır. Verisi
olmayan tam v1 işlem eski okuyucuyu kullanmaya devam eder.

Hazırlık; native root, FD 9 üzerinde tutulan işlem kilidi, tam aktif güncelleme,
durmuş koordinatörler, doğrulanmış kaynak/yedek ağaçları, değişmemiş eski kurulu
kaynaklar ve önceki yayınlama niyetinin yokluğunu gerektirir. Özel dosyalar ve
dizin kayıtları diske aktarılır; atomik yayın mevcut kaydı değiştiremez. Tekrar
deneme, önceden değiştirilmiş kaynaklara sonradan yetki üretemez. Kesintide kalan
özel hazırlık dizinleri korunur ve tamamlanmış kayıt olarak kabul edilmez.

## Kurtarma davranışı

Aktif geri alma ve geri almanın tamamlanma/zamanlayıcı aşamalarında yürütücü önce
mevcut veriyi doğrular; seçili ortama yalnız veri dizinini verir. Bin/web geri
yükleyici saklanan adayı açmadan eski yedeği ve kayıtlı hedef kanıtını kullanır.
Değişmiş kurulu kaynak için tam yayınlama günlüğü önceki/sonraki durumu
kanıtlamalıdır. Sahibin değişiklikleri tahmin edilen hedefle karşılaştırılıp ezilmez.

Yalnız doğrulanmış yokluk eski saklanan-aday yoluna izin verir. Bozuk, yabancı,
güvensiz kayıt veya v2 yayınla ilişkili eksik veri eski sözleşmeye indirgenemez.
Mevcut tam yedek veritabanı/TLS/unit ve çalışan süreç doğrulamaları zorunlu kalır.
Dosyaların geri gelmesi tek başına kurtarmanın tamamlandığı anlamına gelmez.

Seçili program, koordinatörler durmadan önce
`verify-material-support --layout snapshot-name-sha256-v1` ile tam veri düzenini
desteklediğini bildirmelidir. Eski seçili ortam korunur; güncelleme bu kesintiden önce reddedilir.
Bu dilim yeni bir kurtarma ortamını otomatik olarak seçili hâle getirmez.

Kapalı CLI komutları dahili root girişleridir: `verify-material-support`,
`prepare-recovery-material`, `material-root`. Yeni güncelleme başlatamaz, çağıranın
verdiği token veya çıktı dizinini kabul edemez, sahip doğrulamasını atlayamaz.
Sahibin kurtarma yolu `sudo /usr/libexec/celikpanel/recovery recover` olarak kalır.

## Kanıt ve kalan kabul işleri

Linux root testleri gerçek yayın sonrasında aday ağacının kaybolmasını, tekrar
denemeyi, veri yayınlama çevresindeki gerçek SIGKILL kesintisini, bozulma ve sahip
değişikliği reddini, geri alma tamamlanma işaretlerini sınar. Shell/CLI testleri
tutulan FD'yi, yokluk/hata ayrımını, kod/veri ayrımını ve değişiklik öncesi sırayı
sınar. Bunlar tek başına gerçek sistem kabulü değildir.

Geçici VM düzeneği, kurulu aday kontrol noktasından sonra saklanan adayın tam üç
dosyasını (`rollback.sh`, `bin/agent`, `web/dist/index.html`) karantinaya alıp tam
güncelleyici sürecini öldürebilir. Asılları korur; hata enjeksiyonunun kurulu
ürünleri, yedeği ve kurtarma ortamını değiştirmediğini doğrular. Özel deney kayıtları
ürün kurtarma yetkisi değildir. Yeni hata için gerçek sistem sonucu henüz bekliyor;
ayrı bir kabul kaydında bildirilecektir.

Tamamlanmamış yedek alma ve güncellemeyi ileri yönde tamamlama hâlâ saklanan aday
verisini gerektirir. Bütün kontrol noktaları, imzalı Agent kabulü, seçili kurtarma
ortamının yükseltilmesi, metadata geçişleri ve kanıt temizliği açıktır. P0.3 kısmidir.
