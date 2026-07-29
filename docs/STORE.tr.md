# Mağaza ve abonelik hakkı işletimi

Bu belge, veritabanı destekli Eklentiler Mağazası'nı, güven sınırını ve panel
yöneticisinin kullanabildiği işlemleri açıklar.

## Saklama modeli

Mağaza mevcut CelikPanel SQLite veritabanını kullanır. Ayrı bir katalog
veritabanı veya üçüncü bir SQLite dosyası oluşturmaz.

Mağaza veri modeli şunları kullanır:

- yeni `store_offerings`: tipli teklif kataloğu;
- yeni `store_offering_components`: teklif ile gerekli yönetilen bileşenler
  arasındaki tipli eşleme;
- mevcut `subscription_entitlements`: aboneliğin sahip olduğu haklar;
- mevcut `service_scan_cache`: en son gözlenen bileşen durumu.

İşletim kararları tipli sütunlarda ve incelenmiş Go kodunda kalır.
`metadata_json` yalnızca sunum içindir ve tam olarak şunları içerebilir:

- yerelleştirilmiş `name`;
- yerelleştirilmiş `description`;
- `icon`;
- `tags`.

Kabuk komutu, paket reçetesi, SQL, dosya yolu, fiyat, izin, yayın durumu veya
bileşen eşlemesi `metadata_json` içine konulmamalıdır.

## Teklif durumu

API birbirinden bağımsız dört durum boyutunu korur:

| Boyut | Örnekler | Anlam |
| --- | --- | --- |
| `release_state` | `available`, `coming_soon`, `retired` | Ürün yaşam döngüsü |
| `platform_state` | `supported`, `unsupported`, `blocked`, `unknown` | Gerekli bileşenlerin bu sunucuda kullanılabilirliği |
| `entitlement_state` | `included`, `owned`, `not_owned`, `suspended`, `expired` | Abonelik hakkı |
| `runtime_state` | `running`, `installed`, `not_installed`, `stopped`, `error`, `unknown`, `not_applicable` | Gözlenen bileşen durumu |

İstemciler bir boyutu diğerinden çıkarmamalıdır. Sonraki işlemi göstermek için
`primary_action.enabled` ve `blocker_reason` kullanılmalıdır.

`state`, `state_reason`, `action` ve `action_path` uyumluluk alanları bu ana
durumdan türetilir ve eski istemciler için bulunur.

## HTTP API

Katalog görünümü:

```text
GET /api/v1/store
GET /api/v1/store/{offering_id}
```

Desteklenen sorgu parametreleri:

- `subscription_id=<pozitif tamsayı>` abonelik görünürlüğünü denetledikten sonra
  hak durumunu ekler;
- `locale=en|tr` üst düzey yerelleştirilmiş ad ve açıklamayı seçer.

`subscription_id` verilmezse uç yalnız keşif içindir: hak verme modundaki
teklifler `subscription_required` bildirir, etkin işlem sunmaz ve edinilemez.
İzin verilen her sorgu parametresi en fazla bir kez bulunabilir.

Bilinmeyen yol, yöntem, sorgu parametresi, dil ve teklif kimliği reddedilir.
Eksiksiz iki dilli metadata `metadata` içinde bulunmaya devam eder.

Bileşen topolojisi yalnız yöneticiyedir. Yönetici olmayanlar için
`component_ids` ve `manage_path` boştur; yöneticiye ise yalnız güvenli ve yetkili
bir yönetim işlemiyle verilir. Yeni istemciler `primary_action` alanını ana
kaynak sayar ve uyumluluk alanlarından devre dışı bir işlemi yeniden canlandırmaz.

Uyumluluk kataloğu:

```text
GET /api/v1/products
```

Bu uç aynı Mağaza tablolarından üretilir. CelikPanel faturalandırma verisi
uydurmadığı için bilinçli olarak `monthly_price_cents: null` döndürür.

Abonelik hakkı işlemleri:

```text
GET    /api/v1/subscriptions/{subscription_id}/entitlements
POST   /api/v1/subscriptions/{subscription_id}/entitlements
DELETE /api/v1/subscriptions/{subscription_id}/entitlements/{offering_id}
```

Yönetici bir hakkı şöyle verir:

```json
{
  "product_id": "vpn",
  "expires_at": "2027-01-01T00:00:00Z"
}
```

`expires_at` isteğe bağlıdır. Verilirse gelecekteki bir RFC 3339 zaman damgası
olmalıdır. Bozuk veya süresi geçmiş kayıtlı zaman damgaları kapalı davranır.

GET normal abonelik görünürlük kurallarını izler. Tipli bir bayi hak havuzu
modeli gelene kadar POST ve DELETE yalnızca yöneticidir. Bayiler Mağaza
API'sinden gizli yönetici işlemi veya `/services` yönetim yolu alamaz.

Hak verme ve kaldırma, hak kaydını ve denetim kaydını aynı işlem içinde yazar.
Aynı etkin hakkın aynı süre sonuyla birebir tekrarı, ilk yanıt kaybolduktan sonra
teklif kullanımdan kaldırılmış veya tarama önbelleği bayatlamış olsa bile salt
okunur başarıdır. Kullanımdan kaldırılmış teklifte gerçek durum değişikliği yine
reddedilir. Koşullu upsert, eşzamanlı birebir isteklerin yinelenen denetim olayı
oluşturmasını önler. Bitmiş kaldırmayı tekrarlamak da idempotenttir. Yönetici,
yayın, platform, çalışma veya tarama önbelleği durumu engelli olsa bile mevcut
bir hakkı her zaman kaldırabilir.

Teklif satırları yaşam döngüsü kaydıdır ve fiziksel olarak silinmemelidir. Bir
teklifi `release_state = retired` ile kullanımdan kaldırın; böylece mevcut
hakları göstermek, tekrar denemek, denetlemek ve kaldırmak için gereken katalog
eşleşmesi korunur. Fiziksel silme, katalog yolu olmayan bir hak bırakabilir ve
desteklenmez.

## Kurulum sınırı

Bir teklifi edinmek yalnızca abonelik hakkını kaydeder.

Asla şunları yapmaz:

- paket kurmaz veya kaldırmaz;
- servis başlatmaz, durdurmaz veya yapılandırmaz;
- host agent'ı çağırmaz;
- bileşen taraması başlatmaz;
- DNS, TLS, posta veya güvenlik duvarı ayarını değiştirmez.

Bileşen kurulumu ve servis yönetimi, Bileşenler sayfasındaki açık yönetici
işlemleri olarak kalır. Bu ayrım, tüm sunucu değişikliklerini panelde görünür
kılar ve Mağaza'da gezinmeyi güvenli tutar.

## Tarama önbelleği ve kapalı davranış

Mağaza `service_scan_cache` okur; canlı tarama başlatmaz. Önbellek 15 dakika
kullanılabilir. Eksik, ileri tarihli, eski, bozuk, yinelenen veya eski biçimli
tarama verisi bilinmeyen ve işlem yapılamayan platform durumu üretir.

Mağaza yeniden tarama istediğinde:

1. **Bileşenler** sayfasını açın.
2. **Yeniden tara** işlemini seçin.
3. Taramanın bitmesini bekleyin.
4. **Eklentiler** sayfasına dönüp Mağaza'yı yenileyin.

Bileşen gerektiren hak ancak yeni önbellek gerekli tüm bileşenleri kullanılabilir
olarak doğrularsa verilir. Hak işleminin kendisi yine kurulum veya agent çağrısı
yapmaz.

## Güvenli yönlendirme

`manage_path` rastgele JSON değil, tipli bir dahili panel yoludur. Veritabanı
kısıtı ve backend doğrulaması; harici URL'leri, protokole göreli URL'leri, ters
eğik çizgileri, denetim karakterlerini ve `.`/`..` geçiş parçalarını reddeder.
Mağaza yolları kanonik, düz panel rotalarıdır; yüzde kodlu ve çift kodlu
çeşitler, belirsiz tekrarlı kod çözümüne güvenmek yerine reddedilir.

Web istemcisi her katalog yanıtını gerçekten yüklenen abonelik kimliğine bağlar.
Yükleme başlarken veya hata alınca önceki ürünler temizlenir, eski/iptal edilmiş
yanıtlar yok sayılır ve değişiklik işlemleri yeni seçilen değer yerine yüklenen
abonelik kimliğini kullanır. Katalog yalnız yüklenen kimlik güncel seçimle
eşleşirken gösterilir.

Web istemcisi yalnız etkin bir `primary_action` kullanır. Eski `action_path`
veya `manage_path` değerleri bu ana işlemi geçersiz kılamaz.

Geçerli örnekler:

```text
/domains
/services/wireguard
```

## Katalog bakımı

Geçiş yalnızca dürüst durumlar ekler:

- yayınlanan teklifler `available` olarak işaretlenir;
- tamamlanmamış ürünler `coming_soon` olarak işaretlenir;
- sahte fiyat saklanmaz;
- bileşen gereksinimleri açık tipli satırlardır.

Teklif eklemek veya değiştirmek için:

1. Yeni bir geçiş ekleyin; daha önce dağıtılmış geçişi düzenlemeyin.
2. Tipli yaşam döngüsü, hak modu, kategori, satıcı ve güvenli dahili yönetim
   yolunu belirleyin.
3. `metadata_json` verisini yalnızca sunum için kullanın; İngilizce ve Türkçe
   ad ile açıklamayı birlikte verin.
4. Gerekli her bileşeni `store_offering_components` tablosuna ekleyin.
5. Geçiş, API, yetkilendirme, önbellek ve yol doğrulama testlerini ekleyin.
6. Yayından önce `go test`, `go vet`, web derlemesi ve Mağaza arayüzünü
   doğrulayın.

SQLite veritabanını doğrudan düzenlemek acil durum yöntemidir; normal yönetici
arayüzü değildir. Yetkilendirme, doğrulama, idempotentlik ve denetim kaydı
korunsun diye Mağaza ve hak API'lerini kullanın.
