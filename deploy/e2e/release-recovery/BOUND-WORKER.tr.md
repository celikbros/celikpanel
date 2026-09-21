# Gerçek çalışan ve yerel kurtarma kabul deneyi

*P0.2 / D-025 ilkeleri 2, 4 ve 5. Yalnız geçici test ortamı.*

Bu deney, gerçek Go güncelleme çalışanının gözlemini yerel snapshot bağlantısına
ve kimlik doğrulamalı kurtarma okuyucusuna bağlar. Önceki AB/AD deneyleri doğrudan
yerel bootstrap kullandığından bu bağlantıyı kanıtlamaz.

## Güven ve işlem sınırları

- `current_worker_baseline.py`, `8a0c94035912a2a1cbddc47a44133088343aeeda`
  commit'inden yayımlanmamış Alpha81 paketini, değiştirilmemiş kurucusuyla temiz
  konuğa kurar. Asıl kayıt yardımcısı test açık anahtarını ve sürüm tabanını
  kaydeder. Mevcut kurulum ve yinelenen başlangıç reddedilir.
- `worker_fixture_origin.py` ayrı geçici imza anahtarı ve CA üretir. Yalnız kayıtlı
  konuk bu CA'ya güvenir ve `celikpanel.net` adını yerel test kaynağına çözer.
  HTTPS sunucusu yalnız dört sabit güncelleme yolunu sunar; anahtarları, lisansı
  veya gelişigüzel dosyaları sunmaz. İmza özel anahtarı test ana makinesinde kalır.
  Bu, üretim imza/dağıtım altyapısının doğrulaması değildir.
- Farklı gerçek commit'ten Alpha82 paketi; mevcut Agent RPC sürücüsü, gerçek
  `StartSystemUpdate`, Go çalışanı ve değiştirilmemiş imzalı güncelleyiciden geçer.
  Hata yardımcısı kabul, gözlem, bağlantı, sürüm tabanı veya başarı kaydı üretmez.
- `guest_bound_worker.py`, güncelleme birimini durdurmadan önce çalışan komutunu,
  cgroup'u, dosya özetini, tam snapshot'ı, önceki/hedef dosyaları ve tam
  işlem/hedef/token/snapshot bağlantısını doğrular. Normal Panel/Agent servislerine
  sinyal göndermez ve iş yükü verilerini değiştirmez.
- İsteğe bağlı kurtarma yeniden başlatması, gerçek başlangıç makbuzuna bağlı ayrı
  ana makine kabulü kullanır. Kontrol noktası kaçarsa sonuç belirsiz kalır;
  doğrudan bootstrap başlangıç kaydı uydurulmaz.
- Salt-okur root CLI ve kimlik doğrulamalı HTTP/tarayıcı aynı işlemi izler.
  Tarayıcıdaki işlem ipucu değişiklik yetkisi vermez. Başlatan arayüz denenmediyse
  uçtan uca tarayıcı güncelleme kabulü kanıtlanmış sayılmaz.

Yalnız yeni nonce/DMI/QEMU kayıtlı test ortamları kabul edilir. Kurulu kullanıcı
panelleri ve üretim kimlik/imza bilgileri kullanılmaz. Kurulu panellerde güncellemeyi
kullanıcı CelikPanel arayüzünden başlatır.

## Hazırlık kanıtı

2026-09-21 AE deneyi, güncelleme kabulünden önce durdu: ilk test arşiv açıcı,
özel `0077` umask nedeniyle paket dizinlerini `0700` oluşturdu. Gerçek kurucu
`runtime_unsafe_metadata` ile doğru biçimde reddetti. İki konuk durduruldu;
disk ve günlükler `/var/tmp/cp-release-drill-20260921-ae` altında korundu.

Arşiv açıcı artık paketin izinlerini aynen korur. Gerçek Alpha81 arşivinde
377 girdi izni ve 353 dosya özeti doğrulandı. Bu, test yardımcısı düzeltmesinin
kanıtıdır; yerel kurtarma kabulü değildir. Yeni AF deneyi sonuçlanmadan başarı
iddiasında bulunulamaz.

Tam P0.1/P0.2/P0.3 matrisi, üretim sürüm imzası yolu, eski sürüm şema geçişleri
ve bağımsız iş yükü yaşam döngüsü kabul işleri açık kalır.