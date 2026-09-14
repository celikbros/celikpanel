# Bağımsız kurtarmanın gerçek sistem kabulü

*14 Eylül 2026 · [English](INDEPENDENT-RUNTIME.md) · D-025 / P0.2–P0.3*

[Çalışma sözleşmesi](../../../docs/RECOVERY-RUNTIME.tr.md), kayıtlı kurtarma kodunu
Panel/Agent başlangıcından ve aday yaşam döngüsü kodundan ayırır. Aşağıdaki sonuçlar
kayıtlı, geçici QEMU sunucularından gelir. Ayrıntılı VM kimlikleri, yerel yollar,
işlem kimlikleri ve ham günlükler yerel kanıtta korunur; burada yayımlanmaz.
Bu deneylerde kurulu müşteri paneli güncellenmedi.

## Başlangıç ve aday

Her temiz sunucu gerçek imzalı Alpha75 ile, gerçek Agent BIND kurulumu ve eklenip
düzenlenmiş DNS bölgesiyle başladı. Kurulu ve çalışan başlangıç SHA-256 değerleri:

- Agent: `e8e69c520cf4112a5c60380378b8347021aaa035cdaacd820c9927436beff078`
- Panel: `568a7b15f20e5666fced0dcec77f52e83e596efd6e0969d3bf05508faa485d05`

Ölçülen snapshot'tan önce koordinatör unitlerine zararsız sahip yorumları eklendi;
yerel servis yöneticisi yeniden yüklendi. Yerel aday temiz
`4820c4a9172180131148554c98c4a02df3d5d040` commit'inden derlendi; arşiv SHA-256
`f5493fbd8e8e594ea23f850541c2a68be05be4c524ab3ad5b7c888de24e54b49`.
Bu, **imzasız yerel aday kabulüdür**; imzalı Agent kabul yolunu sınamaz.
Denetleyici yalnız kayıtlı geçici sunucularını ve tek başlatmayı kabul eder.

## Korunan deney sonuçları

| Deney | Gözlenen davranış | Kabul |
|---|---|---|
| E — ilk kaynak `982a9d0` | Kayıt dinamik shell FD parametresini servisler durmadan reddetti; eski servisler çalıştı. Miras kilidin sabit FD 9'a eşlenmesiyle düzeltildi. | Kurtarma sınanmadı. |
| F — aday `4820c4a` | İki gerçek güncelleme tamamlandı, tam aday dosyalarıyla HTTPS sunuldu. Düzeneğin kimlik kontrolleri hata uygulamasını engelledi. | Normal güncelleme geçti; kesinti kabulü belirsiz. |
| G — denetleyici `ca8343c` | Gerçek güncelleyici SIGKILL, Arch ve Debian 13'te bağımsız otomatik geri almayı tetikledi. Eski çalışan programlar, sahip unitleri, DNS/TLS/web ve işlemin tamamlanması doğrulandı. | Güncelleyici kesintisi geçti. İkinci kurtarma hatası uygulanamadı: systemd bekleyen işi olan oneshot servisini dondurmayı reddetti. |
| H — denetleyici `b4d9742` | Tam çekirdek cgroup dondurma ve aşama doğrulaması, Arch'ta gerçek kurtarma SIGKILL ve Debian 13'te tek VM reset uyguladı. Elle geri yükleme veya ikinci güncelleme olmadan otomatik kurtarma tamamlandı. | Belirtilen iki kurtarma kesintisi geçti. |

F'deki PATH'e bağlı yorumlayıcı ve kilit gözlem sorunları düzenekte düzeltildi.
G'deki dondurma kısıtı ayrı geçici oneshot servislerle yeniden gösterildi. H,
açık tutulan cgroup inode kimliği ve çekirdeğin frozen kanıtını kullanır.
Başarılı düzenek testleri, gerçek hata kanıtı yerine geçmez. Önceki başarısız veya
belirsiz deneyler ve G'nin düzeltilmiş türetilmiş özeti saklandı; ham gözlemler
tekrar yazılmadı.

## Geri yükleme sırasında kurtarmanın kesilmesi

| Sunucu | Gerçek ikinci kesinti | Yerel devam | Sonuç |
|---|---|---|---|
| Arch | `payload_restored` aşamasında kurtarmaya SIGKILL | Aynı boot'ta yeni servis invocation'ı; aşama sıra numarası `2 → 7` | Tam eski çalışan sürüm otomatik geri geldi. |
| Debian 13 | `runtime_verified` aşamasında tek VM reset | Yeni boot ve servis invocation'ı; aşama sıra numarası `4 → 9` | Tam eski çalışan sürüm otomatik geri geldi. |

Yerel kanıt, kesilen ve tamamlayan çağrıları aynı snapshot/token/runtime kimliğine
bağlar. Son aşama kaydı gerçek geri alma son kaydıyla eşleşir. Debian'ın ilk
açılış kurtarması systemd `starting` durumundayken reddedildi; yerel zamanlayıcı
sonraki denemede aynı işlemi tamamladı. Bu ara hata korunur; kesintisiz başarı
olarak sunulmaz.

İki sunucuda da eski **kurulu ve çalışan** program hash'leri, sahibin unit dosyaları
ve yerel servis yöneticisi ayarı (`NeedDaemonReload=no`) geri geldi. Panel ve Agent
aktifti; HTTPS 200 döndü; dört A/SOA TCP/UDP sorgusu, kurulu/sunulan TLS ve web ağacı
korundu. Etkin işlem kalmadı. Her deneyde snapshot'ın 129, korunan adayın 279 ve
kayıtlı kurtarma paketinin 12 dosyası doğrulandı. **Belirtilen iki kesinti sınırı
için sonuç başarılıdır.**

Tam veritabanı karşılaştırması yalnız `metrics_samples` nedeniyle **DIFFERENT**
kalır. Tablo/satır dışlanmadı. Ek salt-okur karşılaştırma rowid ve bütün alanları
kapsar: Arch'taki beş ve Debian'daki altı yedek ölçüm satırı değişmeden kaldı;
eksik/değişmiş satır sıfırdır. Sırasıyla 17/18 ek satırın hepsi snapshot sonrasında
tarihlidir. Diğer tablo ve şema özetleri eşleşti. G de iki sunucudaki altı yedek
ölçüm satırını korudu; 24 sonraki satır eklendi. Bu kayıt koruma kanıtıdır; bütün
veritabanlarının aynı olduğu iddiası değildir.

## Korunan kanıt özetleri

Bu özetler yerel test raporlarını tanımlar; kimlik bilgisi veya kurtarma token'ı
değildir. Mühürlü dizin Arch'ın 15, Debian'ın 19 dosyasını kapsar; her özet bağımsız
olarak tekrar doğrulandı. İki VM kayıtlı korumaları üzerinden kapatıldı; diskler
ve kanıtlar saklandı.

| H raporu | Arch SHA-256 | Debian SHA-256 |
|---|---|---|
| Mühürlü kanıt dizini | `dee8f97cede5cbabde9fb5a39b2cc9dbf8b7afc7ea7d486b249d946880458ae4` | `9d1a124a83b61545b8d09fa09aac45eb8587390baf88744e40c98b3badaf0b0d` |
| Yerel sonuç | `02955633ccfae3ebcb7fc948651ddac7203f8777a4cfc85e2bc5dd94b98855ba` | `4aff9fc9b38df08556c0c5097f04cec4529cc78f4fad32ff18f29488319fa60c` |
| Aşamadan devam | `07a493002bcd9d46b0d4eb149569d16d17a7a38d01a1002690e833cca22dfd91` | `b7fce9c2bae0f902e63ca2d297a088ee803a9518140fd26acbd635ae24fed375` |
| Ölçüm satırlarının korunması | `8f19ee7d1022d89e22bddbcd8d50d0321ffdec3c675c45c3f348e3a2e75ea1c2` | `8ed38895c1ce46ad9d438dac4317fba25aeefcf7e5471d4c365177b0ae02c632` |

## Kaynak kontrolleri ve açık kabul

[CI 34868947743](https://github.com/celikbros/celikpanel/actions/runs/34868947743),
`b4d9742b8807878dbb643be397903fb8e0d16d04` için başarılı: 21 iş geçti; etiket sürümü
yayımlanmadı. Derleme, vet, Go/race, web, shell/kurtarma sözleşmeleri ve yeniden
üretilebilir arşiv kontrolü geçti. Python düzeneği **191/191** testi geçti.
Sonraki denetleyici commit'leri yerel adayın üretim davranışını değiştirmedi.

Tam aşama matrisi, imzalı Agent aday kabulü, korunan aday verisinden bağımsızlık,
desteklenen metadata geçişleri, kanıt temizliği, dış sertifika yenilemesi, mail
ve yerel ikincil DNS hizmet matrisi açıktır. Bu sonuçlar P0.1–P0.5 maddelerini
kapatmaz; yayımlanmış üretim sürümü değildir.
