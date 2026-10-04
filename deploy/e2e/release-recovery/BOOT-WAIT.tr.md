# Gerçek sistemde açılış bekleme kabulü

*2026-09-21 · P0.2 / D-025 ilkeleri 2, 4 ve 5 · yalnız tek kullanımlık deney*

## Sonuç ve kapsam

Debian AK, gerçek `waiting_for=starting` ertelemesini ve mevcut yerel zamanlayıcının
aynı işleme bağlı geri almayı tamamlamasını geçti. Bu sonuç
[AJ'nin nihai okuyucu kabulünü](BOUND-WORKER.tr.md) genişletir; P0.2'yi kapatmaz.
[Makine tarafından okunabilir özet](BOOT-WAIT-AK.json), kaynak kimliklerini,
sınırlı sonuçları ve saklanan 15 kanıt dosyasının özetlerini içerir. Kimlik
bilgilerini, özel laboratuvar kimlik malzemesini veya mutlak kanıt yollarını içermez.

Deney, AJ'nin aynı yayımlanmamış Alpha81/B ve Alpha82/C arşivlerini ve ayrı
üretilmiş deney imza güvenini kullanır. Gerçek Agent isteği
`a9d913db174173a92a92b0ede7ee03eb`, gerçek Go güncelleyicisini başlatır.
Mevcut hata aracı; istek, çalışan dosya, tam yedek ve değişmez bağlantıyı doğrulayıp
SIGKILL uygular. Ardından kimliği denetlenen tek QMP yeniden başlatması, yerel
geri almayı `payload_restored` aşamasında keser.

Ayrı adlı ve süre sınırı olan deney oneshot birimi, yeniden başlatmadan önce
**çalıştırılmadan** etkinleştirilir. Sonraki açılışta `multi-user.target` işine
katılır ve gerçek bekleme kaydı okunurken gerçek systemd durumunu `starting`
olarak tutar. Doğrulamadan sonra veya 120 saniye sonunda çıkar; systemd ayrıca
150 saniyelik sınır uygular. `systemctl` yerine geçmez; ürün gözlemlerini yazmaz,
kurtarmayı başlatmaz, kilidi/işlemi veya kurtarma birimlerini değiştirmez.
Yalnız nonce/DMI/QEMU ile kayıtlı konuk ve tek mühürlü sonraki açılış niyeti kabul edilir.

## Gözlenen sıra

1. Doğrulanmış kurtarma kesintisinden sonra konuğun boot ID değeri değişir.
2. `18:54:21Z` anında yerel runner, tam istek için `recovering`,
   `terminal_proof=none` ve `waiting_for=starting` kaydeder. Gerçek
   `is-system-running`, `starting` ve çıkış 1 döndürür; o açılışın günlüğü
   ertelemeyi kaydeder.
3. Root kurtarma CLI'ı gerçek kaydı okur. Kurtarma hizmeti çıkış 0 ile durmuş,
   işlem kilidi serbesttir. Etkin tanımlayıcı, bağlantı ve yedek manifesti özetleri
   gözlenen ertelemenin öncesi ve sonrasında aynıdır. Gözlenen erteleme dalı
   kurtarma alt sürecini başlatmaz.
4. Deney açılış işi çıkar. Farklı bir yerel kurtarma çağrısı `18:55:09Z` anında
   geri almayı tamamlar. Elle kurtarma/başlatma veya ikinci güncelleme yapılmaz.
5. Gerçek CLI ile kimlik doğrulamalı HTTP, `recovered / rollback_verified`
   sonucunda birleşir; HTTP ayrıca belgelenmiş `panel_state=ready` alanını taşır.
   Anonim HTTP 401 döndürür. Eski `.wait` dosyası aynı kalır; nihai okuyucular
   artık `waiting_for` göstermez.
6. Kurulu ve çalışan Panel/Agent özetleri başlangıç B ile eşleşir. Etkin işlem
   tanımlayıcısı yoktur. Programlar Alpha81/B'ye dönerken monoton sürüm tabanı
   ve temel 82/C olarak kalır.

Bekleme sırasında kalıcı `previous_failure`, `none` değerindedir; CLI bu alanı
atlayarak döndürür. `update_failed` daha sonra kaydedilir ve nihai sonuçta korunur.
Bu nedenle deney, önceden kaydedilmiş bir hatanın gerçek bekleme boyunca
korunmasını **kanıtlamaz**; bunun bileşen testi ayrı kalır.

## Yeniden üretme ve doğrulama

Yeni kayıtlı laboratuvar ve [işleme bağlı worker yordamını](BOUND-WORKER.tr.md)
kullanın. Tam worker niyeti ve yardımcılar aktarıldıktan sonra mevcut korumalı
aktarımla `guest_boot_wait.py` dosyasını aktarın; kabul edilen güncellemeden önce
konuğun içinde `arm --operation-id <tam-kimlik>` çalıştırın. Birimi elle başlatmayın.
Normal worker sonlandırma ve korumalı kurtarma yeniden başlatması deneyi başlatır.
Root'a özel bekleme günlüğünü/makbuzunu, önceki/sonraki boot ID değerlerini,
gerçek başlatma/kesinti/reset kanıtlarını, yerel günlüğü ve nihai okuyucuları saklayın.

Çevrimdışı doğrulayıcı yalnız saklanan dosyaları okur; sunucuya bağlanmaz:

```sh
python3 deploy/e2e/release-recovery/verify_boot_wait.py \
  --evidence-dir /var/tmp/cp-release-drill-20260921-ak/evidence/debian13 \
  --operation-id a9d913db174173a92a92b0ede7ee03eb
```

15 yeni birim testi; eski/yanlış istek ipuçlarını, yanlış açılış kimliğini, tutulan
kilidi, çalışan runner'ı, farklı HTTP sonucunu, değişen kurtarma verisini, yanlış
çalışan programı ve aynı çağrıya/ters zaman sırasına dayalı tekrar iddialarını reddeder.
Bunlar destekleyici kontrollerdir; gerçek sistem kanıtının yerine geçmez.

AK'nin iki konuğu durdurulmuştur; özel diskleri ve kanıtları saklanır. Sunucu
sahibinin kurulu paneli, üretim sürümü, ürün şeması, kurtarma ABI'ı veya erişim
koruması değiştirilmemiştir.

## Açık kalan kabul

Gerçek `initializing` ve `stopping` ipuçları, önceden bilinen hatanın beklemede
korunması, bekleme sırasında HTTP/tarayıcı erişimi, diğer platformlar/kesinti
noktaları ve geniş P0 matrisi açıktır. Etkin işlem başlangıç korumaları, kurtarma
ertelemişken Panel/Agent'ı bilerek kapalı tutabilir. Bu deney bağımsız root CLI'ını
kanıtlar; kesintisiz HTTPS erişimini kanıtlamaz. Üretim imzası/dağıtımı, veritabanı
dönüşümü ve barındırılan iş yüklerinin kesintisizliği AK ile kanıtlanmaz.
