# Birim dosyası yayımı yarıda kesildiğinde kurtarma

*[English](UNIT-TRANSITION.md) · P0.1 / P0.3 sınırlı uygulama · 2026-09-14*

**Adayın gerçek VM kabulü bekliyor.** Bu çalışma kapsamında hiçbir üretim paneli
güncellenmedi ve halka açık sürüm yayımlanmadı. Önceki
[gerçek deneme sonuçları](RESULTS.tr.md) hatayı kanıtlar; bu değişikliğin kabul
kanıtı değildir. P0.1 ve P0.3,
[dayanıklılık sözleşmesi](../../../docs/RESILIENCE-CONTRACT.tr.md) kapsamında açık
kalmaktadır.

## Sorun ve sınırlı değişiklik

Güncelleyici, değişmeyen servis birimi baytlarını yeniden yayımlayıp
`daemon-reload` öncesinde durabiliyordu. Kurtarma giriş noktası ise geri yükleme
gövdesine ulaşmadan `NeedDaemonReload=no` koşulunu arıyordu. Geçerli bir yarım
yayım böylece kurtarmayı engelliyordu.

[Koruma kütüphanesi](../../release-transaction-guard.sh) üç kanıtı ayırır:

| Kanıt | İzin verdiği işlem |
| --- | --- |
| Koruma dosyaları | Yardımcının tam içeriğini, dosya metadata’sını ve drop-in dizinlerinin doğrudan içeriğini salt-okur doğrular. Yalnız koruma dosyası ile Agent’ın isteğe bağlı, tanımlı runtime-koruma drop-in’i kabul edilir. systemd’yi sorgulamaz veya değiştirmez. |
| Geri yüklemeye kabul | Aynı dosya kanıtına ek olarak yüklenmiş drop-in yolları, birim dosyası yolu ve başlangıç koşulu tam eşleşmelidir. Bekleyen reload yalnız **iki** koordinatör de inactive veya failed durumunda, `MainPID=0` ve `ControlPID=0` iken kabul edilir. Bilinmeyen gözlemler kabul edilmez. Bu kontrol servis başlatma veya otomatik reload izni vermez. |
| Katı yüklenmiş koruma | Yöneticinin dosya değişikliklerini tamamen okumuş olması koşulu korunur. Rollback, mevcut reload aşamasından sonra ve servisleri etkinleştirmeden veya başlatmadan önce katı koruma ile kurtarma temelini yeniden doğrular. |

Kurtarma temelinin onaylanmamış `.intent` kaydı hâlâ rollback kabulünü engeller;
bu değişiklik eksik temelin güvenli olduğunu varsaymaz.

## Yetki ve dosyaların yayımlanması

[Rollback](../../../rollback.sh), birim geçişini kabul etmeden önce eksiksiz v6
snapshot manifestini, gerekli içerikleri, hedef commit/tree bilgisini ve ilgili
işlem kimliğini doğrular. Apply-only
[kurucu](../../../install.sh), eksiksiz snapshot kapsamını ve tam etkin güncelleme
bağını bağımsız olarak yeniden denetler; veritabanı ve TLS içeriklerinin anlamsal
kabulü güncelleyici/rollback denetimlerinin parçası olmaya devam eder.

[Birim geçişi kütüphanesi](../../release-unit-transition.sh) yalnız sabit Agent,
panel ve firewall-restore birim dosyalarını işler. Kurulu her dosya doğrulanmış
snapshot veya hedef sürüm ile eşleşmelidir. Bu iki durumun dosya bazında karışımı,
tanınan bir yarım yayımdır. Bilinmeyen baytlar, güvensiz metadata veya beklenmeyen
koordinatör dosyası yokluğu reddedilir; sunucu sahibinin değişiklikleri korunur.

Değişen dosyalar hedeflerinin yanında hazırlanır, denetlenir, diske yazımı
doğrulanır ve atomik yeniden adlandırma ile yayımlanır. Hazırlama sonrasında,
dosya değiştirilmeden önce kabul denetimi tekrarlanır. Zaten eşleşen dosyalara
dokunulmaz; inode ve zaman damgaları korunur. Geri yükleme aynı kuralları kullanır;
firewall biriminin yokluğu yalnız snapshot bunu kaydetmişse geri yüklenir.
Sunucu sahibinin ilgisiz birim dosyaları değiştirilmez. Kütüphane systemd’yi
yeniden yüklemez ve servislerin etkinleştirilme durumunu değiştirmez.

## Kanıt ve kalan işler

[Koruma testi](../../test-release-transaction-guard.sh) salt-okur davranışı,
bekleyen reload ayrımını, sunucu sahibinin drop-in’lerini ve geçersiz yüklenmiş
tanımları sınar. [Birim testi](../../test-release-unit-transition.sh) gerçek dosya
sistemi yayımını, kısmi hatayı, yeniden denemeyi ve araya giren sahip
değişikliklerini çalıştırır. CI ikisini de root metadata’sıyla yürütür. Bu
kontroller tam gerçek rollback veya iş yükü kurtarmasını kanıtlamaz.

Şunlar bu çalışmanın kapsamı dışında kalır:

- P0.3’ün istediği ayrı sürümlenen asgari kurtarma yürütücüsü ve eksiksiz kalıcı
  kontrol noktası sözleşmesi; kurtarma mevcut saklanan-sürüm yolunu kullanır.
- Adayın gerçek VM kabulü, geri yükleme sırasında kesinti ve kurtarma sürecinin
  kendisi dahil desteklenen kontrol noktalarında yeniden başlatma.
- Tam eski/hedef kaynak kanıtı kurulamayan, kayıp veya kısmen üzerine yazılmış
  tarihsel koordinatör birimlerinin yeniden oluşturulması.
- Desteklenen matris boyunca veritabanı, TLS yenileme, DNS eşi, posta ve diğer
  bağımsız iş yüklerinin tam kabulü. Önceki kanıt sınırları geçerlidir.

Bu çalışmanın geçmesi, her durumda kendi kendini onarma veya P0.1/P0.3’ün
kapanması olarak sunulmamalıdır. Gerçek gözlemler ve sınırları ayrıca kaydedilir.
