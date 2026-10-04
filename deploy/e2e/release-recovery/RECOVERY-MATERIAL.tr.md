# Kurtarma verisinin gerçek sistem kabulü

*14 Eylül 2026 · [English](RECOVERY-MATERIAL.md) · D-025 / P0.3*

[Veri sözleşmesi](../../../docs/RECOVERY-MATERIAL.tr.md), geri alma verisini
başarısız aday ağacından ayırır. Deneyler yalnız kayıtlı, geçici QEMU sunucularında
çalıştı. Ayrıntılı sunucu yolları, VM kimlikleri, işlem kimlikleri ve ham günlükler
yerel kanıtta saklandı. Müşteri paneli güncellenmedi.

## Kaynak ve başlangıç

Her temiz Arch ve Debian 13 sunucusu gerçek imzalı Alpha75, Agent üzerinden
edinilmiş yerel BIND ve eklenip düzenlenmiş DNS bölgesiyle başladı. Koordinatör
unitlerindeki sahip yorumları snapshot öncesinde vardı; yerel servis yöneticisi
yeniden yüklenmişti. Başlangıçtaki kurulu ve çalışan programların özetleri:

- Agent: `e8e69c520cf4112a5c60380378b8347021aaa035cdaacd820c9927436beff078`
- Panel: `568a7b15f20e5666fced0dcec77f52e83e596efd6e0969d3bf05508faa485d05`

Son aday temiz `bc0ebc051de0fc542c5f90e7c0bbb519a5c0f111` commit'inden,
`76492d58a7b9c1b86e2282f0e6d090127b71bd5a` ağacından derlendi. Arşiv SHA-256:
`4d5bde282e7d2342a4b42930f7749104c658b933cf8908da71db3279cf7542c1`.
Bu **imzasız yerel aday kabulüdür**; imzalı Agent güncelleme yolu değildir.
Sonraki `fbb9b62` commit'i yalnız CI test süresi sınırını değiştirir.

Ön deney I, `bed635d` kaynağıyla iki sistemde de üç aday dosyası yokken
güncelleyici kesintisini geçti. Kurtarmayı ikinci kez kesmedi. Aşağıdaki son J
deneyi ayrıca yedek adıyla doğrudan veri seçimini ve seçili çalışma ortamıyla açık
veri düzeni uzlaşmasını kullanır; I bu sonraki değişikliklerin kanıtı değildir.
Önceki kanıtlar saklanır.

## Birleşik kesintiler ve otomatik kurtarma

Kurulu aday kontrol noktasında düzenek saklanan adayın tam şu dosyalarını
karantinaya aldı: `rollback.sh`, `bin/agent`, `web/dist/index.html`. Ardından tam
güncelleyici sürecini öldürdü. Geri yüklemeyi ürünün yerel kurtarma servisi ve
zamanlayıcısı yürüttü; elle geri yükleme veya ikinci güncelleme başlatılmadı.

| Sunucu | Otomatik kurtarma sırasındaki ikinci kesinti | Gözlenen devam | Sonuç |
|---|---|---|---|
| Arch | Aktif geri almada `payload_restored` aşamasında SIGKILL | Aynı boot'ta yeni kurtarma çağrısı | Tam eski çalışan sürüm otomatik geri geldi. |
| Debian 13 | Tamamlanma beklerken `runtime_verified` aşamasında korumalı tek VM reset | Yeni boot sonrasında yeni kurtarma çağrısı | Tam eski çalışan sürüm otomatik geri geldi. |

İkinci kesinti gerçek aşamaya, açık tutulan cgroup/PID kimliğine, işleme, yedeğe ve
seçili kurtarma ortamına bağlandı. Üç kurallı aday yolu eksik kalırken iki sistem de
aynı kurtarma işlemini tamamladı. Aşama sırası Arch'ta `2 → 7`, Debian'da `4 → 9`
ilerledi. Debian'ın ilk açılış denemesi systemd hâlâ `starting` olduğu için
reddedildi (status 1); yerel zamanlayıcı ardından kurtarmayı tamamladı. Bu ara hata
kanıtta korunur.

Üç asıl dosya özel karantinada aynı içerik, inode ve metadata ile kaldı; yeniden
adlandırmanın ctime farkları kaydedildi. Saklanan aday manifesti değişmedi; kalan
280 kayıt envanterle eşleşti. Düzenek enjeksiyon çevresinde sabit kurulu
program/web hedeflerini ve altyapı dosyalarını da kontrol etti. Özel hata kayıtları
deney kanıtıdır; ürünün geri yükleme yetkisi değildir.

İki sunucuda da 129 snapshot, 12 seçili kurtarma ortamı ve 15 kurtarma veri dosyası
doğrulandı. Veri kaydının kurallı özeti, yedek adı dizin anahtarı, işlem bağı, veri
manifesti ve on iki kaynak dosya eşlemesi kontrol edildi. İkinci kesintiden önce
ve devamdan sonraki gözlemler aynı yedeği ve kurtarma ortamını doğruladı. Ayrı bir
hata öncesi kurtarma verisi envanteri toplanmadı; rapor böyle bir önce/sonra gözlemi
iddia etmez.

## Korunan durum

İki sistem de başlangıçtaki tam özetlere **diskte ve çalışan süreçlerde** döndü.
Panel ve Agent aktifti; HTTPS 200 yanıtladı. Dört yetkili A/SOA TCP/UDP DNS sorgusu,
kurulu ve sunulan TLS, web ağacı ve sahip unitleri eşleşti. Yerel servis yöneticisi
`NeedDaemonReload=no` bildirdi; işlem temizlendi, kurtarma zamanlayıcısı beklemeye
döndü.

Tam veritabanı sonucu yalnız `metrics_samples` nedeniyle **DIFFERENT** kaldı;
65 tablo veya satırlarından hiçbiri dışlanmadı. Bütünlük ile diğer tablo/şema
karşılaştırmaları geçti. Ek yerel salt-okur toplayıcı; Arch'ın dokuz, Debian'ın on
yedek ölçüm satırını aynı rowid ve türlenmiş değerlerle koruduğunu, eksik/değişmiş
satırın sıfır olduğunu ve iki sunucuda da altı sonraki örnek eklendiğini bildirdi.
Bağımsız inceleme toplayıcıyı ve mühürlü toplu sonucunu kontrol etti; dışarı alınmış
ham veritabanı satırlarından korumayı yeniden hesaplamadı. Bu, kapsamı belli veri
koruma kanıtıdır; bütün veritabanlarının eşitliği değildir.

Bunlar hata öncesi/sonrası hizmet sorgularıdır. Kesinti boyunca sürekli erişimi,
mail teslimini, dış sertifika yenilemesini veya yerel ikincil DNS aktarımını ölçmez.

## Kanıt ve kabul sınırı

Bağımsız inceleme Arch'ın 33, Debian'ın 38 mühürlü rapor dosyasını yeniden
doğruladı. İki sunucu kayıtlı korumaları üzerinden kapatıldı; diskler ve kanıtlar
saklanır. Bu özetler yerel raporları tanımlar; kurtarma token'ı değildir.

| J raporu | Arch SHA-256 | Debian 13 SHA-256 |
|---|---|---|
| Mühürlü kanıt dizini | `39ce09565852f876959fc7f8e14a3da1e1a63fb3d2b44d93f8a9f7c8301db3e4` | `e9f905d3860b76a720bc7ba98fb2da034889a778cfb0d0b331a588547e14a92e` |
| Yerel sonuç | `7e17893c9a07e91aeddad6c2d31a518cfac866899b5efe9e37e38db6e576e542` | `00db6d02e728023a537c02fd5268d538fdf5a496147ffe1ff230abb14d016851` |

[CI 34878728810](https://github.com/celikbros/celikpanel/actions/runs/34878728810),
`fbb9b62fb911ace820c5987c609e8eb54e63c3a7` için geçti: Go derleme/vet/tam testler,
race grupları, shell/kurtarma sözleşmeleri, web ve yeniden üretilebilir arşiv
kontrolleri başarılı. Yerel Python düzeneği 214/214 testi geçti. Önceki koşu Go'nun
toplam varsayılan paket süresine takıldı; açık 20 dakika sınırı bütün testleri ve
gerçek parola hash işlemlerini korur. Kriptografik parametre azaltılmadı.

Sonraki kaynak incelemesinde, eksik kurtarma verisiyle birlikte niyeti kaybolmuş
`published` kaydının geç reddedilerek eski veri yoluna girebildiği bulundu. Dar
kapsamlı düzeltme bu bozuk durumu, geri alma koordinatörleri durdurmadan önce,
kurtarma verisi sorgusunda reddeder. Olumsuz durum testleri ayrı kanıttır; bu
gerçek sistem deneyleri söz konusu bozulmayı uygulamadı. İkinci uyumluluk kontrolü,
kurtarma verili yedeğe yeni tokenla tarihsel geri almayı aktif işaretçi oluşturmadan
veya servisleri durdurmadan reddeder. Desteklenmeyen bu yeni işlem önceki
güncellemenin yetkisini kullanamaz; burada sınanan aynı tokenlı otomatik
kurtarmadan ayrıdır.

**İki birleşik kesinti sınırı geçti.** Aday ağacının tamamının kaybolması Linux
root testlerinde sınanır; bu gerçek sistem deneyleri yalnız belirtilen üç aday
dosyasını kaldırdı. Tamamlanmamış yedek alma ve güncellemeyi ileri yönde tamamlama
hâlâ saklanan aday verisini gerektirir. Seçili kurtarma ortamının yükseltilmesi,
diğer bütün aşama birleşimleri, imzalı Agent kabulü, metadata geçişleri, temizleme
ve tam yerel hizmet matrisi açıktır. P0.3 kısmidir; bu yayımlanmış bir sürüm değildir.
