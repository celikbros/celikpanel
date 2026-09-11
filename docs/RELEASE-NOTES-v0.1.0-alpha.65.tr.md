# CelikPanel v0.1.0-alpha.65

*Yönlendirmeli sunucu kurulumu · [English](RELEASE-NOTES-v0.1.0-alpha.65.md)*

Yeni kurulumlarda yönetici, aktivasyondan sonra sunucunun amacına uygun bir planı
inceleyerek kuruluma ilerler. Mevcut kurulumlarda Dashboard erişimi korunur;
güncelleme kendiliğinden bir barındırma paketi kurmaz veya kurulumu tekrarlatmaz.

## Neler değişiyor

- **Dört kurulum amacı:** web barındırma, web ve e-posta, Node.js uygulamaları veya
  yetkili DNS. Her plan, yönetici açıkça başlatmadan önce bileşenleri, erişim
  kurallarını ve sunucu kimliğinde yapılacak değişiklikleri gösterir.
- **Üç DNS yönetim seçeneği:** yönetilen yerel DNS, başka bir CelikPanel DNS
  sunucusuna yetkili bağlantı veya harici sağlayıcıda tutulan kayıtlar. Uzak
  bağlantının yetkilendirilmesi, kayıt yayımlaması ve iptali bağlantıya ait alan
  adlarıyla sınırlıdır. Mevcut alan adlarının DNS sahipliği korunur.
- **Doğrulanmış tamamlanma:** panel HTTPS'i ve sertifika yenilemesi, güvenlik
  duvarı, seçilen servisler ve DNS hazırlığı gerçek kontrollerden geçer. E-posta
  için kimlik, TLS ve teslimat önkoşulları da doğrulanır. Yerel primary/secondary
  çiftinde aktarım hazırlığı, kayıt yayımlama yetkisinden ayrı doğrulanır.
- **Kaldığı yerden sürdürülebilen işlemler:** incelenen plan ve alt işlem
  kimlikleri sunucuda saklanır. Yeniden bağlantıda aynı işlemin sonucu doğrulanır.
  Sonucu belirsiz işlemler çakışan değişikliklere kapalı kalır; kesinleşmiş terminal
  hatalardan sonra plan yeniden incelenebilir.
- **Lisans ve çalışan servislerin ayrılması:** geçersiz lisans yönetimi ve yeni
  kurulum adımlarının başlatılmasını engeller. Mevcut servisler ve sertifika
  yenilemesi sürer; kabul edilmiş işlemler güvenle tamamlanır veya sonuçları doğrulanır.

Bu sürüm ayrıca BIND secondary katalog yapılandırmasını, yerel hosts kayıtlarından
etkilenebilen genel DNS kontrollerini, Certbot alt sürecinin sahipliğini ve kurulum
sırasında güvenlik duvarı kurallarının korunmasını düzeltir. Uzak DNS değişiklikleri
ilgisiz doğrulama kayıtlarını korur ve çakışan e-posta yönlendirmelerini reddeder.

## Mevcut kurulumlar ve veritabanı değişiklikleri

**039–042** geçişleri; kurulum durumunu, alan adı bazında DNS sahipliğini, kalıcı
planları/işlemleri ve yetkili uzak DNS yayın durumunu ekler. Önceden kurulmuş
sunucular mevcut kurulum olarak kalır. Alan adlarının yerel DNS sahipliği korunur;
yeni bir varsayılan seçmek mevcut alan adlarının DNS'ini taşımaz.

Dashboard üzerindeki **Sunucu kurulumunu incele** girişi mevcut sunucular için
isteğe bağlıdır. Bu sayfayı açmak yazılım kurmaz veya yapılandırmayı değiştirmez.
Yeni bir kurulum planı ayrıca incelenmeli ve açıkça başlatılmalıdır. Tamamlanmış
kurulum; giriş, aktivasyon veya panel güncellemesi nedeniyle sıfırlanmaz.

## Boston, Frankfurt veya başka bir kurulu paneli güncelleme

Sürüm kullanıma açıldığında yönetici aşağıdaki adımları her sunucunun kendi
panelinde ayrı ayrı uygular:

1. **Ayarlar → CelikPanel güncellemeleri** bölümünü açın.
2. **Güncellemeyi kontrol et** seçeneğine basın ve sunulan sürümün
   **v0.1.0-alpha.65** olduğunu doğrulayın.
3. Gösterilen hedefi inceleyin; panel işleme hazır olduğunda
   **v0.1.0-alpha.65 imzalı güncellemesini başlat** düğmesine basın.
4. İşlem ekranının tamamlanmasını ve yeniden yüklenmesini bekleyin.
   **Mevcut sürüm** alanında **v0.1.0-alpha.65** yazdığını doğrulayın.
   Bağlantı koparsa işlem ekranının mevcut güncellemeye yeniden bağlanmasını bekleyin.

Yönetim lisans nedeniyle kilitliyse yönetici olarak giriş yapıp aktivasyon
sayfasındaki **CelikPanel’i güncelle** bölümünü açın. Aynı kontrol ve başlatma
düğmeleri burada da bulunur. Güncelleme lisansı etkinleştirmez veya yenilemez.

Sürümün yayımlanması hiçbir sunucuya kurulum yapmaz. Kurulu panellerde
güncellemeyi yalnızca kullanıcı, CelikPanel'in kendi güncelleme arayüzünden başlatır.

## Doğrulama ve sınırlar

Kaynak testleri, seçili yarış denetimleri, tüm paketlerde vet, ön yüz üretim derlemesi
ve kontrollü tarayıcı senaryoları geçti. Kesin kaynak anlık görüntüleri ve sonraki
kontrollerin kapsamı [kaynak kabul kaydındadır](validation/server-setup-remote-dns-20260911/README.tr.md).
[Gerçek profil kaydı](validation/server-setup-profiles-20260911/README.tr.md);
geçici Debian 13 VM'lerinde tamamlanan dört amacı, barındırma sertifikalarının
gerçek zamanlayıcıyla yenilenmesini, yetkili DNS çiftinde aktarımı ve lisans kilitliyken
servisleri ile yenilemesi süren web sunucusunu belgeler. Güvenlik duvarı kalıcılığı da
[üç işletim sisteminde gerçek yeniden başlatma kontrollerinden](validation/server-setup-firewall-20260911/README.tr.md) geçti.

Bu ortamlar kontrollü DNS/SMTP ve yalıtılmış ACME güven kökleri kullanır. Genel bir
sertifika otoritesinden sertifika alınmasını, İnternet üzerinden e-posta teslimatını,
genel nameserver delegasyonunu veya üretimde uzak DNS eşleştirmesini kanıtlamaz.
Çift sunuculu ilk kurulum, eş nameserver'ın yayımlanmış IPv6 adresini henüz
doğrulayamıyor; bu kurulum yolu için yalnızca IPv4 nameserver yayını veya harici DNS
kullanın. Mevcut AAAA kayıtları kendiliğinden silinmez.
