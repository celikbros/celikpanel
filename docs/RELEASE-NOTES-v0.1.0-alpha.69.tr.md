# CelikPanel v0.1.0-alpha.69

*Daha sade erişim ve DNS kurulumu · [English](RELEASE-NOTES-v0.1.0-alpha.69.md)*

Kurulum sihirbazında yerel DNS ayarları **Bu sunucu** ve **Diğer DNS sunucusu** olarak gruplandı. Her grupta rol, nameserver ve IP bilgileri birlikte gösterilir; karşı sunucunun rolü otomatik belirlenir. Rol değiştirildiğinde ad ve IP aynı fiziksel sunucuda kalır. Karşı sunucunun adı tekrar yazılmak yerine kendi grubundan türetilir. Kayıtlı eşleştirmede çelişki varsa görünür kalır ve incelemeye geçmeden düzeltilmesi gerekir.

DNS yöntemi seçenekleri ve HTTPS açıklaması kısaltıldı. Yalnızca seçilen yöntemle ilgili ayarlar gösterilir; ayrıntılı eşleştirme yönergesi isteğe bağlı açılır. Sunucunun bildirdiği kullanılabilir genel IPv4 adresi boş alana doldurulur; kayıtlı adresler ve kullanıcının değişiklikleri korunur. Masaüstünde yan yana duran gruplar mobilde alt alta gelir.

DNS, HTTPS yenileme, güvenlik duvarı, lisans, uzak bağlantı doğrulaması ve incelenmiş kurulum planı kontrolleri korunur. Sihirbazı açmak servis kurulumunu başlatmaz.

Doğrulama: 386 arayüz testi, üretim derlemesi ve paket boyutu kontrolü, yerel örnek API ile altı Türkçe/İngilizce masaüstü/mobil tarayıcı senaryosu. Doğrulama sırasında kurulu paneller güncellenmedi.

## Güncelleme

**Ayarlar → CelikPanel güncellemeleri → Güncellemeleri kontrol et** yolundan **v0.1.0-alpha.69** sürümünü kendiniz yükleyin. Yenilenen Erişim ve DNS adımını görmek için **Ayarlar → Sunucu kurulumu** bölümünden sihirbazı açın.
