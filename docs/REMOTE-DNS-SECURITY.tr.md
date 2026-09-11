# Uzak DNS yayınlama uygulama kapsamı

*[English](REMOTE-DNS-SECURITY.md) · [Uygulama durumu](SERVER-SETUP-STATUS.tr.md)*

**Durum:** uygulama ve yerel testler 11 Eylül 2026'da açıkça yetkilendirildi.
Kullanıcı ayrı kapsamlı izin isteğini devam onayıyla yanıtladı;
[onay kaydına](REMOTE-DNS-AUTHORIZATION.md) bakın. Önceki otomatik inceleme retleri
geçmiş kayıtlardır; bekleyen izin isteği değildir. Uygulama aşağıdaki koşullara
tabidir; kullanılabilirlik uygulama durumu belgesinde kaydedilir.

Bu belge [SERVER-SETUP-PLAN.tr.md](SERVER-SETUP-PLAN.tr.md), bölüm 3, seçenek 2'de
onaylanan yönün kaynak kodu kapsamını kaydeder:

> **Mevcut CelikPanel DNS altyapısını kullan:** yetkilendirilmiş yayıncı/tüketici
> ilişkilerini kur ve uzak otoriteyi doğrula. Tek başına eş IP'si yönetim izni
> vermez. Uzak DNS kullanan web sunucusunun DNS secondary olması veya
> authoritative motor kurması gerekmez.

Kullanıcı planı “ok anlaştık” ile kabul edip ardından “İyi düşündüysen, eminsen
yapalım.” diyerek uygulama istedi. Bu kapsam o onaydan çıkarılmıştı. Yukarıda
kaydedilen sonraki açık yetkilendirme, aşağıdaki kimlik bilgisi taşıyan bağlantı
bileşenini de kapsar. Bu kapsam gerçek sunucuları eşleştirmeye, kimlik bilgilerini
bir sunucuya göndermeye, sürüm yayımlamaya veya kurulu panel güncellemeye izin
vermez. Kaynak kodu eklemek bu işlemleri gerçekleştirmez. AGENTS.md gereğince
kurulu panel güncellemelerini kullanıcı CelikPanel üzerinden başlatır.

## Açık yönetici eşleştirmesi

Hazır bir authoritative DNS sunucusundaki oturum açmış yönetici, açık bir işlemle
on dakika geçerli ve tek kullanımlık eşleştirme kodu oluşturur. Tüketici sunucunun
yöneticisi bu kodu ve hedef HTTPS panel adresini girip eşleştirmeyi açıkça başlatır.
Sayfa açmak, kurulum değerlendirmek veya taslak modu seçmek kimlik bilgisi
oluşturmaz ve ilişki kurmaz.

Kodu veya sınırlı kimlik bilgisini yalnız seçilen uç nokta alır. İstemci her
bağlantı girişimini yeniden çözümlenen genel IP adreslerine sabitler, hedefin
normal güvenilir TLS sertifikasını doğrular, yönlendirme ve proxy kullanımını
reddeder. Özel, loopback, link-local, ayrılmış ve doğrudan IP olarak yazılmış uç
noktalar reddedilir. Kodlar ve kimlik bilgileri URL'lerde, açık durum yanıtlarında,
denetim metninde veya günlüklerde bulunmaz. Bekleyen eşleştirmenin tam kimliği
kalıcı kaydedilir; kayıp yanıt ikinci bir yetkilendirme oluşturmadan uzlaştırılır.
İptal için alıcının tam işlem makbuzu gerekir; genel bir HTTP yetkilendirme
hatası, hiç yetki verilmediğini kanıtlamaz. Çözülememiş girişim kurtarma için
korunurken ayrıca onaylanmış yeni bir eşleştirme sürdürülebilir.

## Kimlik doğrulama ve sınırlı yetki

Yalnız üç tam makine yolu tanımlanır: eşleştirme kabulü, alıcı durumu ve alıcı
yayını. İstek makine çağrısı sayılmadan önce TLS ile doğrulanmış tek kullanımlık
kod veya sınırlı kimlik bilgisi kurulmuş olmalıdır. Tarayıcı çerezleri, Origin,
Referer ve fetch metadata başlıkları reddedilir. Yönetici eşleştirmesi, listeleme
ve iptal işlemleri normal yönetici oturumunu ve CSRF kontrollerini korur. Lisans
denetimi bağımsız kalır ve alıcıda da uygulanır.

Alıcı yeniden kullanılabilir istemci sırlarını değil kimlik bilgisi özetlerini
saklar. İstemci yalnız kendi istemci kimliğine kalıcı olarak ait bölgeleri
yayımlayabilir. Mevcut yerel bölgeyi benimseyemez, başka istemcinin bölgesini
sahiplenemez, topolojiyi değiştiremez, kabuk komutu çalıştıramaz veya alıcı paneli
yönetemez. Yetki iptali sonraki yayınları engeller; sunulan bölgeler ve mevcut
işler korunur.

## Kalıcı yayın ve kanıt

Gönderen, yayımdan önce kurallara uygun kayıt kümesini ve artan nesil numarasını
kalıcı kaydeder. Alıcı, CelikPanel'in mevcut yerel DNS yayın mekanizmasını
kullanmadan önce tam sahipliği ve nesli kaydeder. Aynı neslin yeniden gönderimi
yayını uzlaştırır; içeriğini değiştirmek, eski nesli tekrarlamak veya farklı
istemci kullanmak reddedilir. Kayıp HTTP yanıtı başarı değildir. Gönderen, alan
adı silindikten sonra da nesil geçmişini saklar. Aynı bağlantı üzerinden yeniden
oluşturulan bölge, uygulanmış silme makbuzundan sonraki nesille devam eder; eski
silme isteği sonraki nesli kaldıramaz. Sahiplik başka istemciye otomatik
aktarılmaz. Tam kimliği daha önce yetkilendirilmiş bekleyen yayın, eş geçici
olarak kullanılamazken uzlaştırılabilir; her yeni nesil yine güncel otorite kanıtı
gerektirir.

Hazırlık, yapılandırılmış eş IP'siyle değil authoritative motorun ve bağımsız
sunucudaki secondary'nin güncel, kimliği doğrulanmış kanıtıyla belirlenir.

## Doğrulama sınırı

Testler geçici veritabanları, yalıtılmış HTTP/RPC düzenekleri ve yalnız teste
özgü güven kökü ile adres yönlendirmesi kullanan gerçek yerel TLS dinleyicisi
kullanır. Üretim kimlik bilgileri veya canlı sunucu eşleştirmesi gerekmez. Kabul testleri; kimliksiz ve
istemciler arası ret, süre dolması/iptal, uç nokta kısıtlamaları, kayıp yanıt
kurtarması, mevcut bölgelerin korunması ve değişmez alan adı sahipliğini
kapsamalıdır. Gerçek sürüm yayımlama ve kurulu panel güncelleme ayrı işlemlerdir.
