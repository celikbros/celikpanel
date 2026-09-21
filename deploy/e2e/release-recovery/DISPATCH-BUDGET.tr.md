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
kullanıcı tekrarının kesilmesi/başarısızlığı, Arch üzerinde aynı kabul,
sınırdayken HTTP/tarayıcı erişimi ve özel yönlendirme, üretim imzası, hizmet
sürekliliği ve bütün kontrol noktaları matrisi açıktır. Bu deney kalıcı kayıt
**yayımlandıktan sonraki** kesintiyi doğrular. Ürün şeması veya kurtarma protokolü
bu kabul çalışmasında değiştirilmedi.
