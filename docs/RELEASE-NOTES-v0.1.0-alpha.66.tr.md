# CelikPanel v0.1.0-alpha.66

*Açık kurulum tercihi · [English](RELEASE-NOTES-v0.1.0-alpha.66.md)*

Alpha65 sonrasında kurulumu henüz değerlendirilmemiş sunucularda sihirbazın
girişi kolayca gözden kaçıyordu. Bu sürüm ilk karşılaşmada iki açık seçenek sunar:
**Sihirbazla kur** veya **Kendim yapılandıracağım**.

- Manuel tercih sunucuda saklanır; tekrar girişte veya başka tarayıcıda sihirbaz
  otomatik açılmaz ve Dashboard kurulum daveti gizlenir.
- **Ayarlar → Sunucu kurulumu** üzerinden sihirbaza her zaman dönülebilir.
- Yeni kurulumlar da bu seçimi sunar. Önceden başlatılmış işlemler kaldığı yerden
  devam eder; tamamlanmış kurulumlar sıfırlanmaz.
- Manuel tercih kurulumu tamamlanmış saymaz. Lisans, güvenlik, DNS ve servis
  gereksinimleri korunur. Çalışan veya bekleyen kurulum varken tercih değişmez.

Bu sürüm yeni veritabanı geçişi içermez. Tercih mevcut panel ayarlarında tutulur;
DNS kayıtlarını, çalışan servisleri veya sertifikaları değiştirmez.

357 arayüz testi, kurulum Go testleri ve yarış denetimi geçti. Türkçe/İngilizce,
masaüstü/mobil tarayıcı senaryolarında seçim, tercihin korunması ve sihirbaza dönüş
doğrulandı. [Uygulama ve kanıt](SERVER-SETUP-GUIDANCE.md).

## Panelden güncelleme

Önce Boston'da **Ayarlar → CelikPanel güncellemeleri → Güncellemeyi kontrol et**
yolunu izleyin. **v0.1.0-alpha.66 imzalı güncellemesini başlat** düğmesine basın.
İşlem tamamlanınca mevcut sürümün Alpha66 olduğunu doğrulayın; ardından Frankfurt'u
aynı şekilde güncelleyin. Kurulu panel güncellemelerini yalnızca kullanıcı başlatır.

Güncelleme sonrasında Dashboard'a geçtiğinizde, henüz tercih yapmadıysanız seçim
ekranı açılır. Sihirbazı seçmek servis kurulumunu başlatmaz; kurulum planını ayrıca
inceleyip başlatırsınız. Lisans kilidinde güncelleme, aktivasyon sayfasındaki
**CelikPanel’i güncelle** bölümünden yapılabilir.
