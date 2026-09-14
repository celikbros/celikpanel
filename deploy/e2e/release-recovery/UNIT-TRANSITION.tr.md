# Birim dosyası yayımı yarıda kesildiğinde kurtarma

*[English](UNIT-TRANSITION.md) · P0.1 / P0.3 sınırlı uygulama · 2026-09-14*

**Sınırlı gerçek geri alma senaryosu Arch ve Debian 13'te geçti.** Yayımlanmamış
aday, birim dosyaları yayımlandıktan ve reload yapılmadan önce kesildi; iki konuk
da eski çalışan sürümü otomatik geri getirdi. Bu çalışma üretim paneline kurulmadı
ve sürüm olarak yayımlanmadı. Önceki [gerçek hatalar](RESULTS.tr.md) kayıtlıdır.
P0.1 ve P0.3, [dayanıklılık sözleşmesi](../../../docs/RESILIENCE-CONTRACT.tr.md)
kapsamında açık kalmaktadır.

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
- Diğer aday kontrol noktaları, geri yükleme sırasında kesinti ve kurtarma sürecinin
  kendisi dahil desteklenen kontrol noktalarında yeniden başlatma.
- Tam eski/hedef kaynak kanıtı kurulamayan, kayıp veya kısmen üzerine yazılmış
  tarihsel koordinatör birimlerinin yeniden oluşturulması.
- Desteklenen matris boyunca veritabanı, TLS yenileme, DNS eşi, posta ve diğer
  bağımsız iş yüklerinin tam kabulü. Önceki kanıt sınırları geçerlidir.

Bu çalışmanın geçmesi, her durumda kendi kendini onarma veya P0.1/P0.3’ün
kapanması olarak sunulmamalıdır. Gerçek gözlemler ve sınırları ayrıca kaydedilir.

## 14 Eylül gerçek ortam kabulü

**Bu kontrol noktası iki sistemde de GEÇTİ.**
`/var/tmp/cp-release-drill-20260914-d` altında korunan
`release-recovery__9512ecbd3287c9f0` hücresinde iki temiz konuk ve gerçek imzalı
Alpha75 başlangıcı kullanıldı. Gerçek Agent BIND'ı devraldı, bir bölge oluşturdu
ve düzenledi. Ölçüm öncesinde iki koordinatör birimine zararsız bir sahip yorumu
eklendi ve systemd yeniden yüklendi; ikisi de `NeedDaemonReload=no` bildirdi.
Böylece aday yayımı, snapshot'ta kayıtlı gerçek eski revizyonu değiştirdi;
bekleyen reload durumu yapay olarak oluşturulmadı. Debian DNS gözlem bağımlılığı
ölçülen başlangıçtan önce kuruldu.

Tam kaynak commit'i temiz bir dışa aktarımdan Go1.26.5 ve normal `make dist`
yoluyla derlendi (yerel arayüz derlemesi Node26.8.1/npm12.0.2). Aday **imzasız ve
yayımlanmamıştı**; mevcut yerel hazır paket başlatıcısından geçti. Bu, imzalı Agent
güncelleme kabulünü kanıtlamaz. Her işlemde bir kalıcı başlatma kaydı, gerçek
systemd işçisi ve gerçek `OnFailure` kurtarma servisi vardı.

Tam işçi `active` aşamasında dondurulduğunda aday programları, kurulu birim ve
yardımcı özetleri, saklanan sürümdeki 249 dosya ve snapshot'taki 129 dosyanın tümü
doğrulandı. İki koordinatör de `NeedDaemonReload=yes` bildirdi. Ardından yalnız
kimliği doğrulanan işçinin cgroup'u sonlandırıldı. Elle geri alma veya ikinci
güncelleme başlatma yapılmadı.

| Sistem | İşlem | Sonlandırma UTC | Geri alma tamamlandı UTC | Saniye |
| --- | --- | --- | --- | --- |
| Arch | `ee810aa215a03c4ecdfdf4d59301a42c` | `11:16:00.079092` | `11:16:08.122731` | 8.043639 |
| Debian 13 | `0b1350e697c913a02325ff2a03a7d30c` | `11:18:21.623794` | `11:18:31.279986` | 9.656192 |

Her günlükte tam bir otomatik kurtarma çağrısı, gerçek geri yükleme gövdesine giriş,
standart veritabanı geri yüklemesi ve tam geri-alma-bitti mesajı kaydedildi.
Bağımsız gözlemler şunları doğruladı:

- Orijinal Alpha75 Agent ve panel özetleri hem diskte hem **çalışan süreçlerde**
  eşleşti; iki servis aktifti ve eski web dosyaları aynıydı.
- Başlangıçtaki sahip birim dosyaları geri geldi; yüklenen korumalar
  `NeedDaemonReload=no`, yerel HTTPS ise HTTP200 bildirdi.
- UDP ve TCP üzerinden yetkili A/SOA yanıtları ile kurulu/sunulan başlangıç
  sertifikası parmak izleri aynı kaldı.
- Aktif işlem kalmadı ve kurtarma servisi başarıyla çıktı.

Ölçülen 8–10 saniye bu iki denemenin sonucudur; genel kurtarma süresi taahhüdü
değildir. Gözlemciler dolu WAL gördüğünden canlı veritabanı içeriği karşılaştırması
**bilinmiyor** kaldı; günlükteki geri yükleme mesajı bunun yerini tutmaz. Başlangıç
TLS'i sertifika düzenleme veya yenileme kapsamı değildir. Arch çalışan çekirdek
modüllerini bulamadığını da bildirdi; güvenlik duvarı/VPN çalışması doğrulanmadı.
İkincil DNS aktarımı, posta, uygulama veritabanı trafiği, gerçek hata sırasında
sahip düzenlemesi, kurtarma sürecinin kesilmesi ve reboot bu denemede sınanmadı.
Geniş matris **SONUÇSUZ** durumdadır; iki bağımsız denetimde de `p0_complete=false`.

Özel kayıtlarda tam günlükler, önce/sonra gözlemleri, hata kanıtları ve başlangıç
düzenlemeleri saklanır. Sınırlı denetim özetlerinin kimlikleri aşağıdadır;
kimlik bilgileri ve özel anahtar içerikleri yayımlanmaz.

```text
candidate commit: bd77acd0cd55408718feebb6a1e605ccadfd97be
candidate tree: e7b73136a9b8af2cbe455fcfadab985bf7cefbc3
archive SHA256: 5ef7140eea054c5461b65f12f7adb8a2820bfde9ba07607d25e3a44291c7d473
Arch snapshot: 20260914T111552Z-from-unknown-to-bd77acd0cd55408718feebb6a1e605ccadfd97be-48485908c8f32206ff52be642245cae7
Debian snapshot: 20260914T111811Z-from-unknown-to-bd77acd0cd55408718feebb6a1e605ccadfd97be-c413728cb4adc05de792ac97519fe7fb
Arch native-outcome-manager-verified.json SHA256: 52731c76eeed9fd142cc4c142899ca17de375d279b364b12e7e8a44c86adde3b
Debian native-outcome-manager-verified.json SHA256: c57541474b36f6fa573d2f13f7739302f0a57d249fab80136d2b64927ee687bf
```
