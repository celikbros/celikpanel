# Sihirbaz düzenleme durumunu geri yükleme — 12 Eylül 2026

[English](README.md)

Kaynak düzeltmesi yerelde doğrulandı; henüz yayımlanmadı. Kurulu paneller güncellenmedi veya yapılandırılmadı.

## Neden

Sihirbaz adımı, kaydedilmemiş alanlar, inceleme ve onay React durumundaydı.
Bileşen yeniden açıldığında özelleştirilmiş taslaklar, kullanıcı Erişim ve DNS
veya İnceleme adımına ulaşmış olsa bile Bileşenler adımından başlıyordu.

Lisans kontrolü her dakika ve sekmeye dönüşte çalışır. Başarılı zamanında
kontrol zaten sayfayı korur. Sürenin dolması, başarısız doğrulama veya açık lisans
reddi yönetim görünümünü kaldırır; erişim yeniden doğrulanınca sihirbaz yeniden
açılır. Tarayıcı yenilemesi de aynı durum kaybını ortaya çıkarır. Bu sınırlar
 yerelde yeniden üretildi; kullanıcının canlı ağ kayıtlarıyla kesin tetikleyici
saptanmış değildir.

## Değişiklik

Gizli bilgi içermeyen taslak ve adım; sunucu sürümü, kullanıcı, kaynak adresi ve
sekme kapsamında sessionStorage'da tutulur. Yalnızca düzenlenebilir ve aynı
sürümdeki taslak geri yüklenir. Başka oturumun yeni taslağı veya çalışan/tamamlanmış
bir işlem bu kayıtla değiştirilemez.

İnceleme, kabul edilmiş bir işlem bulunmadığı doğrulandıktan sonra sunucudan yeniden
alınır. Plan veya başlatma onayı önbelleğe alınmaz. Eski/başarısız inceleme, alanları
koruyarak Erişim adımına döner. Kurulum başlayınca, tamamlanınca veya manuel çıkışta
bu kayıt temizlenir. Kalıcı işlem takibi ve lisans denetimi yetkili kalır.

Tarayıcı depolaması kapalıysa normal kurulum çalışır; geri yükleme sunucuda kayıtlı
taslakla sınırlıdır. Sekmeyi kapatmak bu düzenleme önbelleğini sona erdirir.

## Doğrulama

İlk düzeltmede 61 odaklı test, 397 web testi ve üretim derlemesi geçti. Sonraki DNS
değişikliğiyle toplam 399 web testi geçti. Yerel üretim paketi, taklit API ve Chrome
ile Türkçe/İngilizce 1440px/390px test edildi: sekme geçişi, kaydedilmemiş Erişim
alanlarıyla yenileme, Erişim ve İnceleme sırasında lisans nedeniyle yeniden açılma,
İnceleme yenilemesi ve onayın sıfırlanması geçti. Canlı sunucu işlemi başlatılmadı.
Ayrıntılı makine çıktısı `browser-results.json` dosyasındadır.