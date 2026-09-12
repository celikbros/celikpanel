# CelikPanel v0.1.0-alpha.73

*Kurulum ve servis işlemlerinde uygulanabilir yönlendirme · [English](RELEASE-NOTES-v0.1.0-alpha.73.md)*

Kurulum artık mevcut gereksinimi ve sonraki eylemi adım listesinden önce gösterir. Birincil ve ikincil DNS yönlendirmesi, incelenen plandaki diğer sunucuyu belirtir ve onun ne zaman hazırlanacağını açıklar. Eş doğrulamasının beklenmesi, diğer sunucunun kapalı olduğu anlamına gelmez. Harici DNS, isteğe bağlı kayıt yönetimi yetkilendirmesi, sertifikalar, güvenlik duvarı kontrolleri, bileşen hazırlığı ve CelikPanel lisans beklemesi ayrı yönlendirmeler alır.

DNS hazırlık denetimi, eş sunucudan aktarım doğrulaması için sınırlı süre kullanmadan önce zorunlu yerel kontrolleri tamamlar. Ulaşılamayan eş artık yanıt süresini tüketip doğrulanmış yerel kurulum, çalışma ve sahiplik bilgilerini kaybettirmez. Eksik veya zaman aşımına uğramış eş kanıtı, birincilde kayıt yayımlama ya da ikincilde aktarım hazırlığı izni vermez.

İlerleme bilgisi, düzenlenebilir taslaktan değil kabul edilmiş plandan ve tam işlem kimliğinden gelir. İlerlemeyi okumak kayıt yazmaz, agent'a bağlanmaz veya ikinci işlem başlatmaz. İsteğe bağlı bağlam bilgisi olmadan eski yanıtlar kullanılabilir. CelikPanel sonuç durumunu denetlerken servis kurulumu hatası görünür kalır; bağlantı kaybı, kurulumun sürdüğünün kanıtı yerine sonucu bilinmeyen durum olarak gösterilir. Sayfayı yenilemek veya hata mesajını kapatmak kurulumu yeniden denemez.

Doğrulama: 421 arayüz testi, üretim derlemesi ve paket boyutu kontrolleri, odaklı panel/agent Go ve yarış durumu testleri, 28 Türkçe/İngilizce masaüstü/mobil tarayıcı senaryosu. Bunlar yerel regresyon ve kontrollü tarayıcı kontrolleridir; sürümün sunuculara kurulduğu veya bütün servis adaptörlerinin incelendiği iddiası değildir.

[İşlem yönlendirmesi gereksinimi](OPERATION-GUIDANCE.tr.md) ürünün tamamı için geçerlidir. Bu sürüm, üçüncü taraf ürünlerin tümü için lisans etkinleştirme veya doğrulama adaptörü sağlamaz. [Panelden bağımsız çalışma sınırları](OWNER-INDEPENDENCE.tr.md) değişmedi; yerel DNS aktarımı, isteğe bağlı panel üzerinden kayıt yönetiminden ayrı kalır.

Sürüm değişikliğinden sonra CelikPanel kayıtlı işlemleri yeniden denetler. Önceki derlemeye bağlı planda kalan adımlar için yeni bir inceleme gerekebilir; güncelleme, kayıtlı kurulumları kendiliğinden tekrarlamaz.

## Güncelleme

Her sunucuda **Ayarlar → CelikPanel güncellemeleri → Güncellemeleri kontrol et** yolundan **v0.1.0-alpha.73** sürümünü kendiniz yükleyin. Kayıtlı kurulum işlemini incelemek ve yönlendirmeyi izlemek için **Ayarlar → Sunucu kurulumu** bölümüne dönün. Sürüm yayımlama kurulu panelleri güncellemez veya sunucu kurulumunu başlatmaz. Sürüm paketi Linux amd64 içindir.
