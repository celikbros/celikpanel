# CelikPanel v0.1.0-alpha.74

*Kurulum sırasında sunucu kimliği görünür kalır · [English](RELEASE-NOTES-v0.1.0-alpha.74.md)*

Sunucu kurulum sihirbazı artık CelikPanel'in ortak sayfa düzenini kullanır.
Lacivert yan alanda yönetim menülerinin yerini kurulum adımları alır.
Sunucu adı, IPv4 adresi ve sunucunun bildirdiği sürüm görünür kalır.
Dar ekranlarda sunucu kimliği üst alanda, sürüm bilgisi kalıcı alt alanda
bulunur. İlk tercih, yüklenme, değerlendirme hatası ve tamamlanma ekranları da
aynı düzeni kullanır.

DNS, panel HTTPS, güncelleme ve parola kurtarma erişimleri korunur. Sihirbazdan
Bileşenler sayfasına genel geçiş kaldırılmıştır. Kurulum düzeni menü sayaçları
için servis taraması istemez. Kurulum planı, onay, kayıtlı taslak, lisans ve
çalışan işlemlerin devam etme kuralları değişmez.

Doğrulama: 424 arayüz testi, üretim derlemesi ve paket boyutu kontrolleri geçti.
Masaüstü, mobil ve ara genişliklerde Türkçe ve İngilizceyi kapsayan 12 yerel
tarayıcı görünümü incelendi. Bu kontroller canlı sunucudaki kurulumun veya DNS
kurtarmanın tamamlandığını göstermez.

## Güncelleme

**v0.1.0-alpha.74** sürümünü **Ayarlar → CelikPanel güncellemeleri →
Güncellemeleri kontrol et** üzerinden kendiniz yükleyin. İmzalı paket Linux
amd64 içindir. Sürümün yayımlanması kurulu panelleri güncellemez veya kurulumu
başlatmaz. Etkin işlem nedeniyle güncelleme engelleniyorsa desteklenen kurtarma
yolu kullanılmalıdır; bu sürüm engeli atlamaz. Kayıtlı kurulum işlemleri
korunur; derleme değiştiğinde kalan adımların yeniden incelenmesi gerekebilir.
