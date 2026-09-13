# CelikPanel v0.1.0-alpha.76

*Panel erişimi kurtarma ve doğru kurulum doğrulaması · [English](RELEASE-NOTES-v0.1.0-alpha.76.md)*

Yönetilen panel HTTPS, alan adı kullanılmayan bağlantılarda kullanılabilir ilk IP
sertifikasını korur; yapılandırılmış alan adı yönetilen sertifikayı kullanır.
Giriş ve kurtarma ekranlarında doğrulanmış panel adresine bağlantı gösterilir.
IP erişiminde tarayıcının sertifika uyarısını onaylamak gerekebilir. Sunucu sahibinin
açık TLS ayarları korunur; eksik başlangıç sertifikası yeniden oluşturulmaz.

Geçici erişim kontrolü hatası artık lisans reddi gibi gösterilmez ve doğrulanamayan
oturum aktivasyona gönderilmez. Önceden doğrulanmış erişim yalnızca ilk verilen
süreye kadar korunur. Diğer durumlarda tekrar kontrol ve sayfa yenileme seçenekleri
olan bağlantı kurtarma ekranı gösterilir. Doğrulanmış lisans kısıtlamaları ve sunucu
yetkilendirmesi geçerlidir. Kesilen işlemin kimliği sayfa yenilendiğinde korunur.

Kurulumun son doğrulaması, hazırlık kontrolleri ve korumalı tamamlama başarılı
olana kadar bekler. Eski erken başarı kayıtları tamamlanan servisler yeniden
kurulmadan düzeltilir. Gereksinimleri tekrar kontrol et düğmesi değişmeyen
gereksinimleri, bilinmeyen sonuçları, hataları veya başarılı kontrolleri yanında
açıklar. Kurulum adımının başarısı sunucunun hazır olmasıyla karıştırılmaz.

## Güncelleme ve devam

Bu sürümü **Ayarlar → CelikPanel güncellemeleri** bölümünden kendiniz kurun.
Sürümün yayımlanması kurulu sunucuları güncellemez. Güncelleme başarısız kurulumu
tekrar başlatmaz veya incelenmiş planını değiştirmez. Tamamlanan adımlar korunur;
çözülmemiş DNS, PTR, paket yöneticisi ve diğer gereksinimler ayrıca giderilmelidir.
Posta kimliği için seçilen posta adı, genel ileri ve ters DNS kayıtlarıyla eşleşmelidir.

## Doğrulama

Yerel arayüz testleri (442), üretim derlemesi ve paket boyutu kontrolleri geçti.
Linux Go testleri TLS seçimini, erişim sınırlarını, kurulum doğrulamasını ve plan
düzenleme korumalarını kapsar. Yerel Chrome senaryoları TR/EN masaüstü/mobil
bağlantı kurtarmayı ve dört doğrulama sonucunu kapsar. Etiket CI süreci yayımdan
önce tam sürüm commit'ini ve tekrarlanabilir imzalı arşivleri doğrular.

Sınırlar ve kanıtlar için [panel kurtarma](PANEL-ACCESS-RECOVERY.tr.md) ve
[doğrulama geri bildirimi](SETUP-VERIFICATION-FEEDBACK.tr.md) belgelerine bakın.
