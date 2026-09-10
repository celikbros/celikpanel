# Lisans denetimi incelemesi — 10 Eylül 2026

[English](LICENSE-REVIEW-2026-09-10.md)

## Bulgular ve canlı kanıt

Bildirilen davranış Alpha63'te sunucu tarafındaki bir yetkilendirme hatasıdır;
yalnızca tarayıcıda eski durumun gösterilmesi değildir. Eski lisans yöneticisi,
imzalı belgeyi `offline_until` tarihine kadar izin sayıyor. Merkezin açık ret
yanıtı hata döndürüyor fakat belgeyi geçersizleştirmiyor. İmza, eski belgeyi kimin
verdiğini kanıtlar; aktivasyonun hâlâ geçerli olduğunu kanıtlamaz.

Yaklaşık 18:53 UTC'de salt okunur SSH incelemesiyle doğrulanan durum:

| Hedef | Gözlenen durum |
|---|---|
| Boston, `2.25.80.4` | Alpha63; kurulu `license.json` yok; panel ve agent çalışıyor |
| Frankfurt, `72.62.38.15` | Alpha63; eski lisans belgesi duruyor; panel ve agent çalışıyor |
| `celikpanel.net` | Yayımlanan son sürüm Alpha63; Frankfurt'un merkezdeki lisansı şu anda bir sunucuya bağlı değil |

İki sunucudaki panel ikilisinin SHA-256 değeri aynı:
`dc38563e34dd49c114f7a941e9c9d351fb606f762c4c04c14b53a4c275b9a482`.
Frankfurt'un belgesi `1789055155` zamanında verilmiş;
`refresh_after=1789141555`, `offline_until=1789659955`. Bunlar 24 saatlik
yenileme aralığı ve yedi günlük çevrimdışı kullanım izni demek. Yıllık lisans
bitişi bu geçici yetkilendirme penceresinden ayrı.
İnceleme sırasında canlı lisans değiştirilmedi, etkinleştirilmedi, silinmedi veya
elle yeniden yazılmadı. Canlı servis yeniden başlatılmadı.

## Önceki oturumdan bulunan düzeltme

Önceki oturum, Alpha64'ün temel düzeltmesini `fix/license-revocation-64` dalında
`0d64e7c8b4039e662adfb9141547a7f432705106` commit'iyle zaten hazırlamış.
Bu sürüm iki sunucuda kurulu değil ve portalda yayımlanmamış.

- Her kimliği doğrulanmış yönetim isteği ortak lisans durumunu denetler.
  Yönetici, bayi, müşteri ve ek kullanıcı rollerinin tamamı kapsanır.
- İmzalı belgenin 45. saniyesinden sonra yenileme denenir. Merkez doğrulaması
  başarılı olmazsa erişim 60. saniyede biter. Eski yedi günlük belgeler de yerelde
  aynı 60 saniyelik sınıra çekilir.
- Merkezin açık reddi, bunu saptayan isteği hemen engeller ve kalıcı ret kaydı
  oluşturur; paneli yeniden başlatmak erişimi geri getirmez.
- Ağ kesintisi yeni bir kullanım süresi vermez. Tekrar doğrulama için gerekli
  bilgiler korunur ve yeniden denemeler ortak bekleme süresine tabidir.
  Kullanılmayan sunucu lisans merkezine periyodik sorgu göndermez.
- Tarayıcı yönetim sayfalarını kaldırıp aktivasyon ekranını gösterir. Doğrudan
  sayfa adresi hem arayüzde hem sunucuda uygulanan engeli aşamaz.
- Root agent ve mevcut servis/zamanlayıcı yolları bu denetime bağlı değildir.
  Siteler, veritabanları, posta ve önceden zamanlanmış işler çalışmaya devam eder.
- Aktivasyon, kişinin kendi hesabını kurtarması ve yöneticinin sınırlı imzalı
  güncelleme akışı açık kalır. Bu istisnalar hosting yönetimi yetkisi vermez ve
  güncelleme imzası denetimlerini gevşetmez.

Bu politika anlık bildirimle iptal değil, sınırlı bir tespit süresi sağlar:
başarılı doğrulamadan hemen sonra değiştirilen anahtar bir sonraki kontrole
kadar kullanılabilir; mevcut yetki belgenin verilişinden en geç 60 saniye sonra
sona erer. Yıllık lisans süresi bu pencereyi uzatmaz.

## Eklenen regresyon kapsamı

`cmd/panel/license_activity_test.go`, daha önce kimlik sorgusunun ardından
doğrudan `Refresh` çağırıyordu. Bu nedenle yönetim katmanı doğrulama yapmasa bile
test geçebilirdi. Artık gerçek yönetim istekleri üzerinden otomatik yenileme,
merkezden ret, tüm rollerde ortak durum ve lisans yöneticisi yeniden
oluşturulduktan sonra erişimin kapalı kalması sınanıyor.

`portal-membership/tests/service.php` artık anahtar değişiminin aynı sunucu ve
IP'deki eski aktivasyon belirtecini iptal ettiğini ayrıca doğruluyor. Bu kontrol
sunucu bağlantısının kaldırılmasına veya makine kimliğinin değişmesine dayanmıyor.

## Doğrulama ve yayın sınırı

Hedefli Go lisans/panel testleri, üyelik entegrasyon testleri, 321 arayüz testi
ve arayüzün üretim derlemesi inceleme sırasında geçti. Yerel Chrome'da yedi
rol/dil/ekran senaryosu sınandı: 1440 ve 390 pikselde Türkçe/İngilizce görünümler,
ret sonrası kilit, başarılı doğrulamada formun korunması ve doğrudan adresle
erişimin engellenmesi. Tarayıcı senaryoları canlı lisanslar yerine test API
yanıtları kullanır.

Son doğrulamalar geçti: temiz kaynak kopyasında `make test vet`, lisans/panel
yarış denetimi, 66 üyelik servis kontrolü, 31 üyelik HTTP kontrolü, beş yalıtılmış
SMTP testi ve sürüm sırası, indirme portalı, imzalı manifest sözleşmeleri.
Kaynak kopyası son iki test değişikliğini de içeriyordu. İlk genel çalışma,
`artifacts/` altındaki Git tarafından izlenmeyen eski Go deney parçalarını da
derlemeye aldı; bu yerel deney dosyalarına dokunulmadı.

Canlı ortam hâlâ Alpha63. Bu inceleme, hatanın canlı sunucularda giderildiği
anlamına gelmez. Kalan operasyonel adım Alpha64'ün incelenerek yayımlanması,
portal yayını ve panelin imzalı güncelleme akışıyla önce Boston'a, doğrulamadan
sonra Frankfurt'a kurulmasıdır. Yayın; üyelik verilerini, imzalama sırlarını ve
mevcut lisans anahtarlarını korumalıdır.
