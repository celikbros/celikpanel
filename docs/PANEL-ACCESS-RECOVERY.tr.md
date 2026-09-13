# Panel bağlantısının kurtarılması

13 Eylül 2026 · [English](PANEL-ACCESS-RECOVERY.md)

Durum: Alpha76 sürüm kaynağı. Kurulu panelleri sahipleri günceller.

Erişim durumu isteği kesildiğinde arayüz bunu lisans reddine dönüştürüyor,
tarayıcıyı `/activate` adresine yönlendiriyor ve lisans anahtarı formunu açıyordu.
Tarayıcı eski aşamayı gösterirken işlem sunucuda devam edebiliyordu.

Erişim kapısı artık başarısız okumayı doğrulanmış retten ayırır. Önceden doğrulanan
karar yalnızca aynı kullanıcı adı için ve sunucunun belirlediği süre dolana kadar
korunur. Bilinmeyen veya süresi dolmuş erişim yönetimi engeller; mevcut URL korunur
ve bağlantı kurtarma seçenekleri gösterilir. Açık ret ve sunucunun
`license_required` yanıtı erişimi kilitlemeye devam eder. Odak ve zamanlayıcı
kontrolleri devam eden isteği paylaşır. İlk lisans durumu okunamazsa anahtar formu açılmaz.

Bağlantısı kesilen işlem ekranında sayfayı yenileme seçeneği bulunur. Yenileme aynı
kalıcı işlemi arar; değişiklik kilidini kaldırmaz ve yeni işlem başlatmaz. Ayrıca
arka plan isteğinin gösteremediği sertifika uyarısının tarayıcıda tam sayfa
gezinmesi sırasında gösterilebilmesini sağlar.

Otomatik yönetilen panel TLS sertifikalarında kullanılabilir ilk kendinden imzalı
sertifika, SNI göndermeyen istemcilerde (IP adresleri dahil) varsayılan kalır.
Alan adı SNI bilgisiyle gelen istemci yönetilen sertifikayı alır. Açık sertifika
ayarları ve özel TLS geri çağrıları korunur. Eksik, süresi geçmiş veya kullanılamaz
başlangıç sertifikası burada yeniden üretilmez; yönetilen sertifikayı engellemez.
IP ile girişte tarayıcı sertifika uyarısı gösterebilir; tarayıcı kontrolleri,
oturum ayrımı ve API yetkilendirmesi kapatılmaz.

Herkese açık GET `/api/v1/panel/access-address`, etkin yönetilen sertifikanın alan
adını veya boş değer döndürür. Giriş ve kurtarma ekranı otomatik yönlendirme yapmadan
bu adresi gösterebilir. Host başlığı, sorgu ve tarayıcı belleği hedef adres kaynağı
değildir. Diğer metotlar herkese açık değildir ve işleyici tarafından reddedilir.
Lisans veya agent isteği gerekmez.

## Doğrulama ve sınırlar

- Kayıtlı kararın süresi, açık ret, istek yarışları, güvenli adres bağlantıları ve
  işlem yenilemesi dahil 441 arayüz testi geçti.
- Arayüz derlemesi ve paket boyutu kontrolleri geçti.
- TLS, lisans, kimlik doğrulama ve başlangıç testleri Linux üzerinde Go 1.26.5 ile
  geçti. Genişletilmiş Windows çalıştırmasında mevcut dizin eşitleme sınırlaması
  nedeniyle `TestLicenseActivityAcrossAuthenticatedRoles` başarısız oldu; Linux çalıştırması geçti.
- Yerel Chrome ile EN/TR, 1440px ve 390px boyutlarında sekiz giriş ve bağlantı
  kurtarma senaryosu kontrol edildi. Tekrar deneme, yenileme, URL koruma ve değişiklik
  isteği gönderilmemesi doğrulandı. Bunlar canlı sunucu değil, örnek API yanıtları kullandı.

Frankfurt günlüğü, tarayıcı phpMyAdmin bağlantı kesintisini gösterdikten sonra
PostgreSQL, Redis ve posta hazırlığına ilerleme olduğunu gösteriyor. Kaybolan HTTP
yanıtının nedenini veya kurulumun tamamen bittiğini kanıtlamıyor. Bu değişiklik
kurtarma davranışını düzeltir; bütün bağlantı kesintilerinin nedeninin çözüldüğü
iddia edilmez. Kurulu panelleri yalnızca kullanıcı panelin güncelleme arayüzünden günceller.
