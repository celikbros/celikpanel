# Yerel DNS bağımsızlığı doğrulaması

*12 Eylül 2026 · [English](README.md)*

Bu kayıt kaynak doğrulamasıdır; kurulu panel güncellemesi veya sürüm yayımı değildir.

- Web: 399 testin tamamı, üretim derlemesi ve paket boyutu kontrolleri geçti.
- Sunucu: Go 1.26.5 ile `cmd/panel` ve `internal/binddns` testlerinin tamamı geçti.
  Kurulum/DNS odaklı testler ve yarış denetimleri de geçti. Manuel ikincil testleri
  iki motoru; web, web/posta, uygulama ve özel kurulum amaçlarını kapsar.
- Tarayıcı: yalnızca localhost üzerinde üretim paketi ve taklit API kullanıldı.
  Türkçe/İngilizce, 1440px/390px doğrulandı. Manuel ikincil adres istemiyor;
  otomatik seçim adresi gösteriyor; manuele dönüşte kullanılmayan geçersiz adres
  kayıt sırasında temizleniyor. Sekme geçişi, yenileme ve lisans nedeniyle yeniden
  açılma düzenleyici durumunu koruyor. İnceleme yeniden alınıyor ve başlatma onayı
  temizleniyor. Sunucu kurulumu veya uzaktan yetkilendirme başlatılmadı.
  Kanıt: `browser-results.json`.
- Yerel DNS: önceki Alpha71 test ortamından iki geçici Debian 13 QEMU kopyası.
  BIND birincil `192.0.2.10`, PowerDNS ikincil `192.0.2.20`. Panel ve ajan hizmetleri
  durduruldu; standart yönetim dosyaları devreden çıkarıldı. Birincilde normal
  `named.conf.local`, bölge ve katalog dosyaları kullanıldı. Bölge ekleme, A kaydı
  değiştirme, DNS hizmetlerini yeniden başlatma ve katalog üyesini kaldırma,
  yönetim hizmetleri olmadan ikincile yansıdı. Kanıt: `native-proof.json`.

`native-probe.py` bu test ortamına özel yoklamadır. Yalnızca belirtilen yerel QEMU
kökünü, localhost SSH yönlendirmelerini ve önceden doğrulanmış sunucu anahtarlarını
kullanır; üretim yapılandırma betiği değildir. Kaydedilen kopyaların ve yönetim
hizmetlerini durdurma hazırlığının mevcut olmasını bekler. Testten sonra iki makine
QMP ile kapatıldı; temel diskler değiştirilmedi. Kanıt: `shutdown-native.json`.

Sınır: test DNS'i kapsar; bütün hosting hizmetlerinin kaldırılmasını veya makinenin
yeniden başlatılmasını kapsamaz. Güvenlik duvarı açılışı ve posta sertifikası
dağıtımında kalan ajan bağımlılıkları [incelemede](../../OWNER-INDEPENDENCE.tr.md)
kaydedilmiştir.