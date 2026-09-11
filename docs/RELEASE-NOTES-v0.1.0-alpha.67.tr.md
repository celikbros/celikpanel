# CelikPanel v0.1.0-alpha.67

*Özel kurulum ve anlaşılır güncelleme hataları · [English](RELEASE-NOTES-v0.1.0-alpha.67.md)*

Yöneticiler artık bileşenleri kurulum sihirbazının içinde seçebilir. **Özel kurulum**
seçeneğini kullanın veya hazır bir profil seçip **Seçimi özelleştir** düğmesine
basın. Yeni Bileşenler adımı gerekli bağımlılıkları ekler, çakışmaları açıklar ve
kurulu yazılımları gösterir. Bir seçimi kaldırmak kurulu yazılımı veya veriyi silmez.

Kurulum planı, güvenlik duvarı portları, Node sürümü, posta profilleri ve tamamlanma
kontrolleri seçilen bileşenlere göre hazırlanır. DNS, güvenilir ve yenilenebilir
panel HTTPS sertifikası ve güvenli erişim şartları korunur. Mevcut Alpha66 kurulum
planlarının kayıt kimliği değişmez. Bileşenler sayfası sonraki servis yönetimi için
kullanılmaya devam eder; toplu kaldırma veya bileşen güncelleme akışı eklenmez.

Güncelleme reddedildiğinde asıl neden artık temizlik mesajları arasında kaybolmaz.
Paket yöneticisi çakışması açıkça doğrulanmış ve kurulu dosyalar değişmemişse,
kullanıcıya anlaşılır bir yeniden deneme açıklaması gösterilir. Diğer hatalarda
koşulsuz olarak güvenli geri dönüş yapıldığı söylenmez. Paket kilitleri ve işlem
kontrolleri korunur; işletim sisteminin paket işlemleri durdurulmaz ve güncelleme
kendiliğinden başlatılmaz.

381 arayüz testi, backend kurulum ve eşzamanlılık testleri, güncelleme hatası/geri
dönüş kontrolleri ve Türkçe/İngilizce masaüstü/mobilde sekiz özel kurulum tarayıcı
senaryosu geçti. Yeni seçimler mevcut desteklenen işlemleri kullanır; her yeni
kombinasyon için gerçek sunucu kurulum testi yapıldığı iddia edilmez.

## Alpha66’dan güncelleme

Önce Boston’da **Ayarlar → CelikPanel güncellemeleri → Güncellemeleri kontrol et**
yolunu açın ve imzalı **v0.1.0-alpha.67** güncellemesini başlatın. Tamamlanmasını
bekleyip mevcut sürümü doğrulayın; ardından Frankfurt’ta aynı işlemi yapın.
Kurulu panel güncellemesini yalnızca kullanıcı başlatır.

Sonrasında **Ayarlar → Sunucu kurulumu → Kurulum sihirbazını aç** yolunu kullanın.
Hazır bir profili özelleştirin veya Özel kurulum seçin. Sunucuyu hazırlayacak planı
ayrıca inceleyip başlatın. Panel güncellemesi hosting bileşenlerini kurmaz ve
kaydedilmiş manuel/sihirbaz tercihini sıfırlamaz.
