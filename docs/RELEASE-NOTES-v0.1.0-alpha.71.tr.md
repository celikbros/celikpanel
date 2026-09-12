# CelikPanel v0.1.0-alpha.71

*İkincil DNS sunucusunda barındırma · [English](RELEASE-NOTES-v0.1.0-alpha.71.md)*

Bir sunucu artık ikincil DNS hizmeti verirken web siteleri, uygulamalar ve e-posta da barındırabilir. Frankfurt/Boston çiftinde Frankfurt yayın birincili olarak kalır, Boston aktarılmış ikincil kopyayı sunar. Boston'un barındırma kayıtları yetkili Frankfurt bağlantısı üzerinden yayımlanır; siteleri ve e-postası Boston'da kalır. Bu yapıda BIND ve PowerDNS birlikte kullanılabilir.

Sihirbaz artık barındırma bileşenleriyle birlikte İkincil seçeneğini sunar ve birincilin HTTPS panel adresini incelemeye dahil eder. Yerel DNS ve güvenli panel erişimini hazırladıktan sonra açık bir DNS yayın bağlantısı için bekler. Birincilin kayıt kodunu aynı sihirbazda girip doğrulanan bağlantıyı onaylayarak kayıtlı kurulumu sürdürün. Mevcut alan adlarının kayıtlı DNS sahipliği korunur. Bu değişiklik bölgeye göre karşılıklı birincil roller eklemez; site dosyalarını, veritabanlarını veya posta kutularını çoğaltmaz.

Bağlantı, incelenen plana ve sunucu çiftine bağlanır. Tekrarlanan onaylar ve kaybolan yanıtlar aynı işlem üzerinden uzlaştırılır. Bekleyen plan düzenlendiğinde eski çalıştırmanın geç yanıtı DNS varsayılanlarını değiştiremez. Yeniden başlayan kurulum, tamamlanmış yerel DNS adımını korur; bitmeden önce hem yerel ikincil DNS'i hem uzaktan yayını doğrular.

Nginx seçildiyse panel sertifikasından önce hazırlanır; sonradan kurulup bağımsız sertifika yenilemesini bozmaz. DNS bekleme adımlarında kurulum kurtarma ve ayarlar erişilebilir kalır. E-posta kimliği, işletim sisteminin hostname'inden ayrı tutulmaya devam eder.

Doğrulama; otomatik panel ve arayüz kontrollerini, ayrıca yerel iki sanal makinede BIND birincil/PowerDNS ikincil çalıştırmasını içerir. Kontrollü DNS/ACME ortamı kurulum ve DNS aktarım yolunu doğrular; genel DNS delegasyonunu, genel sertifika otoritesinden sertifika alınmasını veya İnternet üzerinden e-posta teslimini kanıtlamaz. Postaya özgü yönlendirme ve hazırlık kontrolleri otomatik testlerle kapsanır. Sonuçlar ve sınırlar [ikincil DNS ile barındırma kabul kaydındadır](validation/secondary-hosting-20260912/README.md).

## Güncelleme

İki sunucuda da **Ayarlar → CelikPanel güncellemeleri → Güncellemeleri kontrol et** yolundan **v0.1.0-alpha.71** sürümünü kendiniz yükleyin. **Ayarlar → Sunucu kurulumu** bölümüne dönüp kurulumu başlatmadan önce sunucu çiftini ve planı inceleyin. Önce birincili başlatın, ardından sihirbazın yönlendirmesiyle ikincilde devam edin. Sürüm yayımlama sırasında kurulu paneller güncellenmez.
