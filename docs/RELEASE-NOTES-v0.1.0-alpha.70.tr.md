# CelikPanel v0.1.0-alpha.70

*Ayrı posta kimliği ve daha anlaşılır kurulum seçenekleri · [English](RELEASE-NOTES-v0.1.0-alpha.70.md)*

Posta kurulumu işletim sisteminin hostname’ini korur. Kayıtlı posta adresi, panel adresinden bağımsız olarak Postfix ve yenileme dahil posta TLS eşitlemesinde kullanılır. Agent artık posta profiline sistem adını değiştirme yetkisi vermez. Kurulum incelemesinde bu ad değişikliği gösterilmez. Önceden adı değiştirilmiş sunucular otomatik olarak eski adına döndürülmez.

Bağımsız posta kurulumunda sistemin tam nitelikli hostname’i olsa da ayrı bir posta adresi girilebilir. Kayıtlı posta kimliği olmayan mevcut kurulumlar önceki sistem adına dayalı kimliği korur; geçersiz kayıtlı kimlikte başka bir ad sessizce seçilmez, eşitleme durur.

Sihirbaz, barındırma profilinde yerel İkincil DNS seçeneğinin neden kapalı olduğunu ve yalnızca DNS kurulumu ya da başka bir DNS yönetim yönteminin nasıl seçileceğini açıklar. Web barındırma posta eklemez. Yalnızca DNS kurulumu, sunulan iki motor ve iki rolü destekler; web, posta veya veritabanı eklemez. Uygulama profilinde “veritabanı yok” seçimi artık son hizmet kontrolünde hata oluşturmaz.

Doğrulama: panel ve agent test paketleri, 388 arayüz testi, üretim derlemesi ve boyut kontrolü, 24 profil/DNS planı senaryosu, altı yerel Türkçe/İngilizce masaüstü/mobil tarayıcı senaryosu. Yerel testler genel DNS delegasyonunu, farklı motorlar arası zone aktarımını, genel sertifika otoritesinden sertifika alımını veya canlı posta teslimini kanıtlamaz.

## Güncelleme

**Ayarlar → CelikPanel güncellemeleri → Güncellemeleri kontrol et** yolundan **v0.1.0-alpha.70** sürümünü kendiniz yükleyin. **Ayarlar → Sunucu kurulumu** bölümüne dönüp kurulumu başlatmadan önce planı tekrar inceleyin. Geliştirme ve yayın sırasında kurulu paneller güncellenmedi.
