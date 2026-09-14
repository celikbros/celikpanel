# Kurtarma erişimi ve doğru durum gözlemi

*[English](RECOVERY-ACCESS.md) · P0.2 kısmi uygulama · 2026-09-14*

Agent’ın kullanılamaması artık panelin HTTPS kurtarma görünümünü açmasını engellemez.
Oturum veya lisans bilgisi okunamadığında kullanıcı çıkış yapmış ya da lisansı eksik
sayılmaz. Bu, D-025 kapsamındaki sınırlı bir temel değişikliktir;
[dayanıklılık sözleşmesinin](RESILIENCE-CONTRACT.tr.md) tamamlandığı anlamına gelmez.

## Erişim sınırları

Panel önce mevcut veritabanını, oturum deposunu, gizli bilgi anahtarını ve TLS
malzemesini açar. Agent’a bağlanmadan ve olağan uzlaştırma/yönetim işlerini başlatmadan
önce ayrı, sabit bir başlangıç yönlendiricisiyle tek HTTPS dinleyicisini açar.
Bu yol ilk arayüzü, mevcut giriş/TOTP/çıkış ve kullanıcı sorgularını, hazır olma,
lisans erişimi ve panel adresi bilgisini sunar. Tam işlem gözlemini yalnız yönetici
okuyabilir. Sunucu yönetimi, webmail/veritabanı vekilleri, güncelleme başlatma,
uzak makine kimlikleri ve lisans aktivasyonu yazımları açılmaz. Normal yönlendirici,
başlangıç açıkça izin verene kadar erişilemez.

Agent bağlantısı deneme başına süre sınırı ve üst sınırı olan beklemeyle yeniden
denenir. Agent yok diye panel genel bağlantı süresi dolunca kapanmaz. Dinleyici
hatası veya sahibin durdurması bekleyen bağlantıyı kapatır. Token okuyucusu normal
dosya olmayan girdileri ve fazla büyük dosyaları reddeder; FIFO üzerinde takılmaz.
Normal dosyaya geçerli bağlantılarla uyumluluk korunur. Okunamayan mevcut token yeniden üretilmez. Token biçimi ve hizmet
ayarları değişmez; ilk token’ın atomik oluşturulması ayrı açık iştir.

Kesin olarak olmayan/süresi dolan oturum ve geçersiz kullanıcı yine reddedilir.
Depo okuma hataları `503 AUTH_STATUS_UNAVAILABLE` döndürür. Ek kullanıcının üst
hesabı, ikinci kullanıcı sorgusu ve giriş/TOTP kimlik çözümü de aynı ayrımı yapar.
Tüketilmiş TOTP girişimi geri açılmaz.

`GET /api/v1/license/access`, mevcut `can_use_panel` ve `valid_until` alanlarına
`state` ve `observation` ekler. Yalnız bilinen eksik, süresi dolmuş ve geçersiz lisans
aktivasyon gerektirebilir; yerel okuma veya uzak doğrulama hatası bunu kanıtlamaz.
Mevcut bir dakikalık doğrulama politikası korunur. Tarayıcı olumlu kararı yalnız tam
son tarihine kadar tutabilir; sunucu her yönetim isteğinde yetkiyi yine doğrular.

## Ortak gözlem sözleşmesi

| Yüzey | Sözleşme |
| --- | --- |
| Hazır olma | Kimlik doğrulamalı `GET /api/v1/panel/availability`; `celikpanel-panel-availability/v1`, `starting` veya `ready`; işlem ayrıntısı yok. |
| Kurtarma | Yalnız yönetici: `GET /api/v1/recovery/status?request_id=<32 küçük onaltılık karakter>`; `celikpanel-recovery-status/v1`; Agent RPC veya sürüm pazarlığı yok. |
| Üretici kaydı | `/var/lib/celikpanel-recovery-observations/<id>.status`; sabit alanlı `celikpanel-recovery-observation/v1`, en fazla 2 KiB; 0750 dizinde root:celikpanel 0640. |
| Yerel bağ | Tam istek, hedef commit, güncelleme tokenı ve snapshot’ın değişmez, yalnız-root bağı. Tarayıcıya dönmez; değişiklik izni değildir. |

Worker kabul/çalışma ve kanıtlanmış sonucunu kaydeder. Güncelleyici, durdurma
öncesinde worker’ın çekirdekteki cgroup kimliğini bağlar. Kurtarma yürütücüsü yalnız
bu tam bağı kullanır. `update_verified` ve `rollback_verified` sonuçları mevcut son
kanıtlardan sonra yayımlanır. Geç worker hatası tamamlanma kanıtını silemez.
Go ve shell, Agent’ın ana grubu root olmasa da root:root 0600 ortak yayım kilidini
kullanır. Yanlış sahiplik, başka istek veya desteklenmeyen düzen sessizce onarılmaz.
Gözlem hatası gerçek güncelleme/geri alma sonucunu değiştirmez ve yeni işlem açmaz.

HTTP okuyucu yol, sahiplik, izin, normal dosya türü, bağlantı sayısı, boyut, şema ve
okuma kararlılığını denetler. Yalnız sınırlı neden kodu ve zaman bilgisi döner;
yol, parola, token veya ham hata dönmez. Eksik/güvensiz kayıt `unavailable` olur;
aşama ve tamamlanma kanıtı üretilmez. Bilinen kayıt son üretici gözlemidir; sürecin
şu anda çalıştığının kanıtı değildir.

İlk arayüz paketindeki kurtarma görünümü, yenileme ve geç yüklenen güncelleme modülü
hatasında aynı kayıtlı işlem kimliğini korur. Doğrulanmamış oturum yönetici gözlemini
göremez; yalnız kesin 401 giriş ekranına döndürür. Başarısız veya sırası bozuk sorgu
son doğrulanmış sonucu, önceki hatayı ve zamanını korur. Sorgular yalnız GET’tir.

## Kanıt ve açık kalanlar

- Son Go 1.26.5 paket koşusunda panel 2530, auth 27, licensing 19, repositories
  25 ve transport 57 test geçti. Dört isteğe bağlı panel testi ve ortama bağlı iki
  lisans testi atlandı; izin reddi ayrıca `nobody` ile sınandı. Atlananlar yerel
  kabul kanıtı sayılmaz.
- Go 1.26.5 ile ayrı gerçek panel `main()` süreci, geçici SQLite, HTTPS ve gerçek
  yönetici parolası/oturumuyla Agent olmadan çalıştırıldı. Giriş ve aynı oturumla
  sorgular geçti; yönetim 503 kaldı; tek süreç SIGTERM ile düzgün kapandı.
- HTTP testleri anonim/kiracı/ek kullanıcı reddini, tam kimlik ve metot sınırlarını,
  başlangıç yalıtımını ve açık yönetim iznini sınar. Bağlantı testleri süre sınırı,
  iptal, dinleyici hatası ve geç gelen bağlantının kapatılmasını sınar.
- Yerel gözlem testleri gerçek Go/shell yayımını, devralınmış kilidi ve farklı süreç
  gruplarını kapsar. Worker testi kanıtsız başarı üretmez; gözlem yazımı reddedilse
  de gerçek worker sonucu korunur.
- Gerçek kurtarma runner’ının sözleşme testi, gözlem helper’ını doğrulanmış saklanan
  sürüme alır. Başarısız alt süreç `recovery_required`, telafi ve son kanıtlar
  `recovered` üretir; önceki hata ve değişmez bağ korunur. Alt süreç ve systemd
  fixture’dır; gerçek yerel geri yükleme gövdesi kanıtı değildir.
- Odaklı başlangıç/erişim race testi geçti. Son telefon yenileme kontrolünde 503
  sürerken tam işlem kimliği korundu; aktivasyona atma ve mutasyon isteği olmadı.
- 458 arayüz testi ve üretim derlemesi geçti. İlk paket mevcut bütçede kaldı:
  269,54 KiB ham / 84,68 KiB gzip. Yerel Chrome’da EN/TR, 1440/390 piksel ve dört
  hata durumu olmak üzere 16 birleşim sınandı. Aktivasyona atma, değişiklik isteği
  veya yatay taşma görülmedi. Sonraki 503 önceki doğrulanmış geri almayı korudu.
  Masaüstü/telefon görüntüleri incelendi. Bu tarayıcı testindeki API yanıtları
  fixture’dır; canlı sunucu sonucu değildir.

Bu testler tam yerel güncelleme/otomatik geri alma yaşam döngüsünü kanıtlamaz.
Bu çalışmada kurulu panel güncellenmedi. Panel dosyası, migration/oturum veritabanı,
gizli bilgi anahtarı, TLS ve ilk JS paketi hâlâ açılabilmelidir. Ayrı sürümlü
kurtarma yürütücüsü/erişimi, kesilen kurtarma ve yeniden başlatma, tüm hizmet
kontrolleri ve P0.2/P0.3 yerel hata matrisi açıktır.

Eski worker başlangıç gözlemi üretmediğinden ilk sürüm geçişine geriye dönük tam
istek bağı eklenmez; sonuç kullanılamaz kalır. En son snapshot tahmin edilmez.
İmzalı güncelleme kabulü, geri alma manifestleri, sahibin yetkisi ve kurulu paneli
yalnız kullanıcının güncellemesi sözleşmeleri korunur.
