# Gerçek sistemde kurtarma deneme sınırı kabulü

*22 Eylül 2026 · P0.2/P0.3 · D-025 ilkeleri 2–5*

Debian AL deneyinde gerçek güncelleme işçisi kesildi, ilk kurtarma sırasında VM
bir kez yeniden başlatıldı, sonraki iki kurtarma girişimi de SIGKILL ile kesildi.
Üç otomatik hak tükenince zamanlayıcı yeni kurtarma başlatmadı. Aynı snapshot için
belgelenmiş kullanıcı komutu bir kez çalıştırıldı ve doğrulanmış geri alma tamamlandı.
[Makinece doğrulanan kayıt](DISPATCH-BUDGET-AL.json), dosya kimliklerini ve kanıt
hash'lerini içerir. Bu sınırlı kabul, P0.2/P0.3'ün tamamlandığı anlamına gelmez.

Önceki sürüm, yayımlanmamış Alpha81 test paketi
`45dfc265bfd7997e9a0e0d39b0ebf60a57f58c00`; aday ise Alpha82 test paketi
`20c72254de4211a0c56fa7e16b9aec6b0e68b888` idi. Adayın
`4bc2e18d1446fd9e48ae6918d8fa7cc41625c451` uygulamasından tek farkı test sürüm
sıralama politikasıdır. Yeni seçili kitin yürütücüsü, CLI'si ve gözlem yardımcısı
aday arşivle karşılaştırıldı; çalışma ortamının tüm manifesti doğrulandı.
Üretim sürümü yayımlanmadı, kurulu kullanıcı panelleri değiştirilmedi.

## Gözlenen sonuç

- Gerçek Agent isteği: `5c03d502560c27e1a5357b47c3fe5c1f`.
- İlk girişimin kalıcı kaydından sonra `payload_restored` noktasında kayıtlı QEMU
  yeniden başlatıldı. Açılıştaki gerçek `starting` beklemesi yeni hak tüketmedi.
- Zamanlayıcı 2. ve 3. hakları ayırdı. Her iki kurtarma da doğrulanmış aynı noktada
  kesildi. Süreç, cgroup, snapshot ve işlem kanıtı olmadan sinyal gönderilmedi.
- Sonraki iki yerel çağrı otomatik kurtarmayı durdurdu. İşlem ve üç kayıt korundu,
  kilit serbestti. CLI, `recovery_required / recovery_incomplete` gösterdi.
- Kullanıcının açık devam komutu tek bir `owner.*` kaydı oluşturdu. Üç otomatik
  kaydın hash'i değişmedi. Aynı işlem 21:05:01 UTC'de geri almayla tamamlandı.
- Diskteki ve çalışan Panel/Agent dosyaları önceki sürümle eşleşti. İşlem belirteci
  temizlendi; kurtarma kiti, sürüm eşiği ve foundation 82'de kaldı.
- CLI ve kimlik doğrulamalı HTTP aynı `rollback_verified` sonucunu verdi.
  Anonim sorgu 401 döndürdü; gerçek güncelleme hatası nihai kayıtta korundu.

Üç kesinti, üç kesin ve kalıcı alt işlem hatası değildir. Açılış beklemesinde
önceden kaydedilmiş hata yoktu; sınırda `recovery_incomplete`, nihai uzlaştırmada
`update_failed` kaydedildi. Bu deney önceden bilinen hatanın gerçek deneme sınırı
boyunca korunmasını veya sınırdayken tarayıcı erişimini kanıtlamaz.

## Yeniden üretim ve doğrulama

[İngilizce deney kaydı](DISPATCH-BUDGET.md), tam hazırlık ve toplama adımlarını
belirtir. Anahtarlar üretildikten sonra test açık anahtarı ilk kurulumdan **önce**
VM'ye aktarılmalıdır. AL'nin ilk hazırlığında bu aktarım eksikti; kurulum başlamadan
reddedildi. Bu hata saklandı; temiz durum doğrulanıp aynı girdilerle devam edildi.

`guest_dispatch_budget.py`, yalnız nonce/DMI ile doğrulanan test VM'sinde, önceden
mühürlenmiş isteğin 2. ve 3. girişimlerini keser. Ürün kayıtlarını değiştirmez,
kurtarma başlatmaz. Ayrı test servisi 650 saniyeyle sınırlıdır.
`guest_dispatch_budget_result.py`, sonucu toplar ve yalnız açık `owner-retry`
komutunda aynı snapshot için tek seferlik desteklenen root CLI çağrısını yapar.
Bu araçlar kurulu müşteri sunucuları için değildir.

Çevrimdışı doğrulama sunucuya bağlanmaz:

```sh
python3 deploy/e2e/release-recovery/verify_dispatch_budget.py \
  --evidence-dir /var/tmp/cp-release-drill-20260921-al/evidence/debian13 \
  --operation-id 5c03d502560c27e1a5357b47c3fe5c1f
```

Yanlış işlem/açılış kimliği, sıfırlanmış kayıtlar, dördüncü otomatik hak, ikinci
sahip çağrısı, kalan işlem belirteci, dolu kilit ve belirsiz sonuç reddedilir.
İki AL VM'si kapatıldı; diskler ve özel kanıtlar saklandı.

## Açık kalanlar

Kaydın yayımlanma anındaki güç kaybı, tekrarlanan kesin alt işlem hataları,
kullanıcı tekrarının kesilmesi/başarısızlığı,
sınırdayken HTTP/tarayıcı erişimi ve özel yönlendirme, üretim imzası, hizmet
sürekliliği ve bütün kontrol noktaları matrisi açıktır. Bu deney kalıcı kayıt
**yayımlandıktan sonraki** kesintiyi doğrular. Ürün şeması veya kurtarma protokolü
bu kabul çalışmasında değiştirilmedi.

## Arch AN kabulü

[Arch AN kanıtı](DISPATCH-BUDGET-AN.json), AL ile aynı temel ve aday arşivleriyle
aynı sınırlı politikayı doğrular. Gerçek Agent'ın kabul ettiği
`4c0ec761ef57aa38aea5b2b7f83964fd` işleminde ilk yerel geri alma `payload_restored`
noktasında tek QMP yeniden başlatmasıyla kesildi. Sonraki açılışta 2. ve 3.
denemeler aynı noktada tam süreç, cgroup ve snapshot kanıtıyla kesildi. Ardından
üç zamanlayıcı çalışması yeni kurtarma başlatmadan sınırın dolduğunu bildirdi.
Kilit serbestti; aynı geri alma bekliyordu. Tek açık kullanıcı komutu
`2026-09-21T21:50:01Z` anında geri almayı tamamladı. Üç otomatik kayıt özeti
korundu ve yalnız bir kullanıcı deneme kaydı eklendi. Çalışan ve kurulu dosyalar
eski sürümle; CLI ve yetkili HTTP sonucu birbiriyle eşleşti. Anonim HTTP reddedildi.

Yeniden açılan sistem systemd 261 (261.3-1-arch), çekirdek 7.2.6-arch2-1 kullanıyor.
Temel kurulum çekirdek paketlerini yükseltti; güvenlik duvarı veya VPN hazırlığı
bu deneyin kanıtı değildir. İkinci kabulden önce iki gerçek açılış beklemesi oldu.
Bu nedenle saklanan bekleme kaydı ilk gözlenen yayınla aynı değildir. Ayrı,
metadata denetimli okuma; son kaydın tam baytlarını, aynı isteğe ait olduğunu ve
sonuç kaydına göre eskidiğini doğrular. Doğrulayıcı farklı yerel bekleme
çalışmalarını şart koşar; son CLI/HTTP sonucunda eski bekleme gösterilmez.
İlk bekleme kaydının değişmeden korunduğu iddia edilmez. AL kanıtı da geçmeye devam eder.

Önceki AM denemesi **deneme sınırı için sonuçsuzdur**. Laboratuvar gözlemcisi kısa
açılış beklemesinde süreç bilgisini okuyamayınca durdu; planlanan ikinci/üçüncü
kesintileri uygulamadı. Yerel zamanlayıcı sonrasında geri almayı tamamladı.
Hata günlüğü, kesinti kaydı ve son durum
`/var/tmp/cp-release-drill-20260922-am/evidence/arch` altında korunur. AN temiz VM
kullanır. Düzeltilen sınırlı gözlemci okunamayan süreç bilgisini bilinmeyen sayıp
yeniden gözler; bu bilgi kesinti izni vermez. Tam doğrulama şartları korunur.
AM kayıtları yeniden yazılmadı.

Deney indirme kaynağı artık doğrulanmış platformun yerel CA deposunu kullanır.
Arch'ın root sahipliğindeki 0750 `/root` dizini kabul edilir; deney dosyalarının
dizini root-only 0700 kalır. Bunlar yalnız laboratuvar değişiklikleridir. AM'nin
ilk hazırlık retleri güven hazırlığı ve güncelleme kabulünden önce gerçekleşti.
AM ve AN'nin iki VM'si de durduruldu; özel kanıtlar saklanıyor.

```sh
python3 deploy/e2e/release-recovery/verify_dispatch_budget.py \
  --evidence-dir /var/tmp/cp-release-drill-20260922-an/evidence/arch \
  --operation-id 4c0ec761ef57aa38aea5b2b7f83964fd
```

Bu sonuç Arch deneme sınırı vakasını kapatır.
Yayın anında güç kaybı, tarayıcı yönlendirmesi, belirli çocuk işlem hataları,
kullanıcı yeniden denemesinin kesilmesi ve bütün P0.2/P0.3 kapsamı açık kalır.

## Debian AO yerel durma yönlendirmesi

[AO makinece okunabilir sonucu](BUDGET-GUIDANCE-AO.json), yeni durma ek kaydını
gerçek yerel yürütücüden seçili kurtarma CLI'ına kadar Türkçe ve İngilizce doğrular.
Yayımlanmamış test adayı `3218e50bc26cbe1b1544e312e4636b8c794d7809`, PR177'nin
`b2d739b2ecf6d31f9ee9f84282487941b5303a80` commit'inden yalnız test sürüm sırası
politikasıyla ayrılır. Arşiv SHA256 değeri
`3c048febf58a940328d6573d93179e6682157103ff4d176368ca105cb153b170`.
Başlangıç AL/AN ile aynı Alpha81 arşividir. İmza güveni izole test güvenidir;
üretim imzası veya normal arayüzden güncelleme kabulü kanıtlanmaz.

`af0de8c801a3d13c780e9b4089d60c98` işlemi gerçek Agent kabulünü, worker kesilmesini,
ilk kurtarmada QMP yeniden başlatmayı ve `payload_restored` noktasındaki iki ek
yerel kurtarma kesintisini geçti. Sonraki üç zamanlayıcı çağrısı sınırın dolduğunu
bildirdi. Salt-okur kayıt; root/panel dosya izinlerini, tam durum baytlarını ve
nanosaniyeli dosya kimliğini, bağlı durma bilgisini, gerçek JSON ve TR/EN kullanıcı
yönergelerini doğrular. Kilit serbestti; üç otomatik deneme kaydı ve bilinen
kurtarma hatası korunmuştu.

Desteklenen tek seferlik kullanıcı devamı `2026-09-21T22:24:36Z` anında geri almayı
tamamladı. Otomatik kayıtların hash'leri değişmedi; yalnız bir kullanıcı denemesi
eklendi. Kurulu ve çalışan Panel/Agent başlangıç sürümüyle eşleşti. Saklanan eski
durma kaydı, nihai doğrulanmış geri alma sonucunun önüne geçmedi. Geri yükleme
**sonrasında** yetkili HTTP aynı sonucu, anonim HTTP 401 verdi. Panel durmuşken
HTTP veya tarayıcı erişimi kanıtlandığı iddia edilmez.

`guest_budget_guidance.py` kapalı laboratuvar kimliğini ve önceki iki gerçek
kesintiyi doğruladıktan sonra yalnız ürün durumunu okur ve özel test kanıtı yazar.
Kullanıcı devamından önce ve sonra birer kez çalıştırılır. Çevrimdışı doğrulayıcı
önce tam deneme sınırı kanıtını, ardından yeni kaydın bağını, CLI metnini ve nihai
sonucun üstünlüğünü denetler. Olumsuz testler; eski kimlik, başka işlem, değişmiş
baytlar, bilinmeyen CLI, kaybolan hata, eksik yönerge ve hâlâ durma gösteren nihai
sonucu reddeder.

```sh
python3 deploy/e2e/release-recovery/verify_budget_guidance.py \
  --evidence-dir /var/tmp/cp-release-drill-20260922-ao/evidence/debian13 \
  --operation-id af0de8c801a3d13c780e9b4089d60c98
```

AO'nun iki VM'i durduruldu; disk ve özel kanıtlar saklandı. Yalnız yeni kaydın
Debian yerel CLI kabulü kapanır. Arch'ta yeni kayıt, durma sırasında gerçek tarayıcı
erişimi, kullanıcı devamının kesilmesi, yayın sınırındaki güç kaybı, üretim güveni
ve iş yükü bağımsızlığı açıktır. AL/AN kanıtı değişmedi. D-025 ilkeleri 2, 4, 5 /
P0.2–P0.3 kısmi kalır. Bu kanıt değişikliği ürün şeması, geçiş, kurulu panel
güncellemesi veya üretim sürümü içermez.
