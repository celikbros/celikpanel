# Gerçek çalışan ve yerel kurtarma kabul deneyi

*P0.2 / D-025 ilkeleri 2, 4 ve 5. Yalnız geçici test ortamı.*

Bu deney, gerçek Go güncelleme çalışanının gözlemini yerel snapshot bağlantısına
ve kimlik doğrulamalı kurtarma okuyucusuna bağlar. Önceki AB/AD deneyleri doğrudan
yerel bootstrap kullandığından bu bağlantıyı kanıtlamaz.

## Güven ve işlem sınırları

- `current_worker_baseline.py`, yayımlanmamış Alpha81 paketini değiştirilmemiş
  kurucusuyla temiz konuğa kurar. Yeni AJ başlangıç paketinin kimliği aşağıda
  sabitlenmiştir. Asıl kayıt yardımcısı test açık anahtarını ve sürüm tabanını
  kaydeder. Mevcut kurulum ve yinelenen başlangıç reddedilir.
- `worker_fixture_origin.py` ayrı geçici imza anahtarı ve CA üretir. Yalnız kayıtlı
  konuk bu CA'ya güvenir ve `celikpanel.net` adını yerel test kaynağına çözer.
  HTTPS sunucusu beş sabit güncelleme yolunu sunar: son sürüm, manifest, imza,
  arşiv ve arşiv sağlama toplamı. Anahtarları, lisansı veya gelişigüzel dosyaları
  sunmaz. İmza özel anahtarı test ana makinesinde kalır.
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
kanıtıdır; yerel kurtarma kabulü değildir.

AF daha sonra ürünün ortak dizin sözleşmesindeki çelişkiyi gösterdi: runtime
kaydı `/usr/libexec/celikpanel` dizinini `0700` oluştururken sonraki adım `0755`
bekliyordu. Durdurulmuş konuk ve günlükleri korundu. Temiz kurucu artık ortak
dizini kayıt öncesinde mevcut katı sözleşmeyle hazırlar. Önceden farklı metadata
taşıyan sahip dizinine dokunmaz; özel runtime dizinleri `0700` kalır.
Gerçek kilit/FD üzerinden Bash–Go regresyonu önceki hatayı üretip eksik dizin,
mevcut `0755` ve çakışan `0700` durumlarını doğrular. Şema ve runtime ABI değişmedi.

AG, gerçek kurucuyla temiz kurulumu ve gerçek DNS test hazırlığını tamamladı.
Test RPC sürücüsü daha sonra Alpha82 hedefini güncelleme RPC çağrısından önce
reddetti. Bu sonuç hazırlığı doğrular; gerçek güncelleme çalışanının veya
kurtarmanın kabulünü kanıtlamaz.

AH, `72772a3f867c35ed0118a58930626e3d` kimliğiyle tek bir gerçek güncelleme başlattı.
Çalışan, test kaynağında bulunmayan arşiv sağlama toplamı yolunu indirirken HTTP 404
ile başarısız oldu. Hedef kontrol noktasına ulaşmadı; yapay kill veya kurtarma
sırasında yeniden başlatma uygulanmadı. Gerçek root kurtarma CLI'ı ve kimlik
doğrulamalı HTTP okuyucusu tam bu işlem için `failed` / `update_failed` bildirdi.
Kimlik doğrulamalı okuma HTTP 200, anonim okuma 401 döndürdü. Bunlar bilinen hatanın
doğru izlenmesini kanıtlar; başarılı kurtarma kanıtı değildir. Test kaynağı artık
beşinci sağlama toplamı yolunu da içerir. Ana makinedeki yeniden başlatma yardımcısı,
hiç oluşmayabilecek alt kurtarma devrini beklemeden önce sonlanan update-kill
kanıtını kontrol eder.

AI, `b679a4a758619ab22c6339439643a6ef` işlemini başlattı. Arşiv ve programlar
Alpha81 ve Alpha82 olarak adlandırılmış olsa da iki paketin sürüm politikası hâlâ
80 diyordu. Kurulu temel aynı sırada farklı commit'e ait olduğundan gerçek
monotonluk ön kontrolü hedefi doğru biçimde reddetti. Yapay hata uygulanmadı.
Sorun, test sürümü bilgilerinin tutarsızlığıydı; başarılı güncelleme veya yerel
geri alma kanıtlanmadı.

AG, AH ve AI konukları durduruldu. Kayıtlı diskler ve özel kanıtlar
`/var/tmp/cp-release-drill-20260921-ag`,
`/var/tmp/cp-release-drill-20260921-ah` ve
`/var/tmp/cp-release-drill-20260921-ai` altında korunuyor. Yeni başlangıç için
bu ortamlar tekrar kullanılmaz.

## Yeni AJ hazırlığı: tutarlı, yalıtılmış sürüm kimlikleri

`/var/tmp/cp-release-drill-20260921-aj` altındaki AJ sonucu henüz bekleniyor.
İki gerçek Git test commit'i; program/arşiv sürümünü, paket sürüm politikasını ve
bootstrap sürüm/sıra değerlerini eşleştirir. Bunlar yalıtılmış test commit'leridir,
üretim sürümü etiketleri değildir. Üretim dalının sürüm politikası ve üretim imza
güveni değiştirilmedi.

| Test paketi | Sürüm / politika sırası | Gerçek kaynak commit'i | Arşiv SHA-256 |
| --- | --- | --- | --- |
| B, temiz başlangıç | `v0.1.0-alpha.81` / `81` | `45dfc265bfd7997e9a0e0d39b0ebf60a57f58c00` | `13e1463712915b92ed38a3905ab048bd3ad128f2a0c570b9bcc74161cf7645bf` |
| C, hedef | `v0.1.0-alpha.82` / `82` | `9bdb27473f0341fc1ab47cb72d1269839ca0ddee` | `7c3db8658033dcce0765db5703202157c285460d53d09cdb73ff2e132e78efea` |

B politikası önceki sıra olarak 80 / `v0.1.0-alpha.80` değerlerini ve etiketin
gösterdiği gerçek sürüm commit'i `bd14d97efc5cfd19acd70ddf0edb9c6343317e2b` değerini
kullanır. C politikası önceki sıra olarak 81 / `v0.1.0-alpha.81` ve B'nin tam
commit'ini kullanır. Arşivler bu kayıtlı kaynak ağaçlarından derlenir; paketlenmiş
sabit dosyalar gerçek Git blob'larıyla karşılaştırılır. Yerel kurucu temeli,
değiştirilmemiş kayıt yardımcısı test güveni tabanını yayımlar. Test araçları bu
kayıtları elle düzeltmez ve aynı sırada farklı commit reddini gevşetmez. Otomatik
geri alma, önceki programları geri yüklerken daha yeni monoton temeli koruyabilir.

Bu hazırlıklar AJ'nin yerel sonucunu kanıtlamaz. Gerçek çalışan bağlantısı, kontrol
noktası hatası, kesilen kurtarma, geri yüklenen son durum ve aynı işlemi gösteren
CLI/kimlik doğrulamalı HTTP için bu ortamda gözlenmiş sonuç hâlâ gereklidir.

Tam P0.1/P0.2/P0.3 matrisi, üretim sürüm imzası yolu, eski sürüm şema geçişleri
ve bağımsız iş yükü yaşam döngüsü kabul işleri açık kalır.
