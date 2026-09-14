# Kurtarma ortamı geçişinin gerçek sistem kabulü

*14–15 Eylül 2026 · [English](RUNTIME-PROMOTION.md) · D-025 / P0.3*

[Geçiş sözleşmesi](../../../docs/RECOVERY-RUNTIME-PROMOTION.tr.md), seçili kurtarma
kitinin değiştirilmesini ele alır. Deneyler yalnız kayıtlı tek kullanımlık QEMU
konuklarında yapılır. Ana makine yolları, konuk/işlem kimlikleri ve ham günlükler
özel kanıtlarda kalır. Düzenek hiçbir müşteri panelini güncellemez.

## Kaynak ve önceki kit

Uygulama tabanı gerçek imzalı Alpha75'tir. Düzenek, önceki kurtarma kitini
kaydetmeden önce kurulu Agent üzerinden yerel BIND hizmetini ve DNS verisini
hazırlar. Yerel sürüm kilidi altında eski kitin gerçek `enroll-runtime` komutunu
çağırır; seçici veya geçiş kaydı uydurmaz. Bu, eski uygulama üzerinde kit kaydıdır;
**önceki uygulamanın baştan sona başarılı güncellemesi değildir**.

Önceki kaynak: `bc0ebc051de0fc542c5f90e7c0bbb519a5c0f111`, ağaç
`76492d58a7b9c1b86e2282f0e6d090127b71bd5a`; arşiv SHA-256
`4d5bde282e7d2342a4b42930f7749104c658b933cf8908da71db3279cf7542c1`.

İlk geçiş adayı (K/L): `b34ab808f99d1d611d90d5665c10e88c724fa494`, ağaç
`f4a993ae42ff13f6a1b7bec79e7e930acb5fd56b`; arşiv SHA-256
`7863d227739d3849fca7b85dbb9a71e2b2764031501d26f4c485fd42f6e1a49d`.
İlk geçiş düzeneği kaynağı `58c5cda278ac7d2b143650d96c8a06343e982afe`.
Yeni M/N kabulü için düzeltilmiş aday
`ad782d803b1ba9531b1a3e92627ebdd748fe7c89`, ağaç
`2314b935e2f498184bb9a19e3d7c023d72934770`; arşiv SHA-256
`bf7da9a4ca9a779b3e79a9f569bf085cff5b37d7a050dd6465ee2be84005489f`.
Bu arşivler **imzasız yerel derlemedir**; imzalı Agent kabul yolu değildir.

## Ayrı kabul sınırları

1. Yeni başlatıcı/eski seçici aralığında gerçek yerel süreç, özel kilit, boş
   işlem işaretçileri, tam eski/yeni kitler ve imzalı uygulama tabanı kanıtlanır;
   sonra yalnız tam ilgili süreç öldürülür. Kaçırılmış aralık sonuçsuzdur; düzenek
   kayıt uyduramaz veya aynı kabulü yeniden başlatamaz.
2. Gerçek sabit girişten salt-okur veri yeteneği sorgulanır. Eski seçili program
   kendi çalıştırılabilir dosya kanıtını geçmeli, seçim değişmemelidir. Bu,
   programın gerçekten yürütüldüğünü kanıtlar; yedek geri yükleme değildir.
3. Aynı korunan geçişi bitirmek için açık sahip `recover` komutu çalıştırılır ve
   eski uygulamanın değişmediği doğrulanır. Bu ilk kayıt düzeneği eski uygulamanın
   altyapısıyla otomatik tamamlanmayı kanıtlamaz.
4. Ayrı yeni konuk ve kabulde kit geçişi tamamlanır; ardından uygulama güncellemesi
   kesilir ve yerel hizmetlerle gerçek otomatik yedek geri yükleme doğrulanır.
   Önceki yalnız ön kontrol kesintisi bu deneyin yerine geçmez.

## Sonuç kaydı

K deneyi, Arch ve Debian 13 üzerinde yeni başlatıcı/eski seçici aralığını gördü.
Gözlemci tam ilgili güncelleyiciyi dondurduktan sonra eski staging yolunu okumaya
çalıştı. Gerçek prebuilt bootstrap bu dizini çoktan kalıcı sürüm yoluna taşımıştı.
Düzenek `FileNotFoundError` kaydetti, **SIGKILL göndermedi** ve aynı süreci serbest
bıraktı. Toplama sırasında gözlem/özet dosya adı çakışması da bulundu; ham gözlem
ve günlük kanıtları korundu.

İki güncelleyici daha sonra ileri güncellemeyi normal tamamladı. Diskteki ve
çalışan süreçlerdeki aday programlar eşleşti, iki hizmet de aktifti. Yeni kit
seçildi ve önceki tam kit korundu. Dört DNS sorgusu ve ilk kurulum TLS'i tabanla
eşleşti. Bütün veritabanı karşılaştırması yalnız metrics nedeniyle **FARKLI** kaldı.
Bunlar normal kit geçişi/ileri tamamlama sonuçlarıdır; amaçlanan kesintiyi veya
otomatik geri almayı kanıtlamaz.

`7dcf5c0242ec37fc2fd5b494dd9a9af555b8cf43` düzeneği, sabit kökte doğru ada sahip tek
kalıcı sürümü tam manifest/envanterle doğrular; staging geri dönüşü yoktur.
Gözlem ve özet artık farklı dosyalara yazılır. Python testlerinin 255'i geçti;
üretim adayı değişmedi. Sonraki kesinti ve geçiş sonrası geri alma deneyleri aşağıda kaydedildi.

Bağımsız ana makine incelemesi Arch'ta 43, Debian'da 44 mühürlü dosyanın tamamını
tekrar doğruladı; bu inceleme konukta komut çalıştırmadı. K konukları kayıtlı
koruyucularla durduruldu, diskler ve ham kanıtlar saklandı. Metrics satır sayısı
Arch'ta 8→18, Debian'da 9→19 oldu. Eski satırların tek tek korunduğu bu deneyde
ayrıca kanıtlanmadı; bütün veritabanının korunduğu sonucu çıkarılmaz.

| K raporu | Arch SHA-256 | Debian 13 SHA-256 |
|---|---|---|
| Mühürlü dizin | `7dcd8caef5e01ca000673764584d76d55d725eaee9123cf1cb94b2e2adf56d19` | `afd53a624c074d9841847463d88f716c4725ba469186b7c74ae2b1a3b3ce972e` |
| Yerel sonuç | `26bddf8585fe1f1640d0472cbb7e585781a2cf9da6c5bbf16a264f9b422c91f2` | `4bd063eb1e988ad65512e0761ba2b1be1a7451f2bc3b081e87912b2827f88005` |

## L deneyi: gerçek kesinti sahip girişindeki kusuru gösterdi

Düzeltilmiş düzenek iki sistemde de ara çifti tam doğruladı, ardından güncelleyiciye
bir SIGKILL gönderdi. Yeni sabit girişten yapılan salt-okur komut eski seçili
okuyucuyu çift değişmeden başarıyla çalıştırdı. Sonraki açık sahip `recover`
komutu iki sistemde de exit 3 ve `runtime_unsafe_metadata` ile başarısız oldu.
Seçici eski kitte kaldı, durum `known/launcher_published` olarak korundu;
tamamlanma kaydı oluşmadı, işlem kökünde yalnız kanonik kilit vardı. İki kit ve
eski uygulama süreçleri sağlam kaldı. İkinci güncelleme veya kurtarma başlatılmadı.

Bu, **başarısız sahip devamı kabulüdür**; başarılı kurtarma sayılmaz.
`7dcf5c0242ec37fc2fd5b494dd9a9af555b8cf43` için
[CI 34895863261](https://github.com/celikbros/celikpanel/actions/runs/34895863261)
21 işi geçti; bu sonuç gerçek sahip sürecinin dosya tanıtıcısı düzenini
kapsamıyordu. Yerel Go 1.26.5 sürecinde salt-okur yol kanıtından sonra FD 9'un Go
olay izleyicisinde kalabildiği yeniden üretildi. Sahip yolunun sabit tanıtıcı
varsayımı, iki gerçek konuktaki salt-okur tanıyla da doğrulandı:
giriş kanıtından sonra FD 9, `anon_inode:[eventpoll]` idi. Tanı ve toplama çifti
değiştirmedi, kurtarmayı yeniden denemedi.

`ad782d8` commit'i sahip yolunda gerçekten alınan kanonik kilit tanıtıcısını taşır;
ilgisiz FD 9 korunur. Kabul edilmiş güncelleyicinin miras FD 9 sözleşmesi ayrıdır.
Üç regresyon önce eski kodda başarısız oldu; düzeltmeyle 34 root senaryosu, vet
ve iki kurtarma paketinin race kontrolleri geçti. Değişen adayın yeni gerçek sistem kabulü aşağıdaki M ve N deneylerindedir.

L başarısız kabulü korur. İki konuk koruyucularıyla durduruldu; Arch'ta 48,
Debian'da 49 kanıt dosyası mühürlendi ve bağımsız olarak yeniden özetlendi. Hata
gözlemcisinin `cleanup=thaw-unconfirmed` sonucu korundu; sonraki yerel gözlem
tam ilgili birimde `Result=signal`, çıkış sinyali 9 ve `MainPID=0` gösterdi. Bu,
K deneyindeki başarılı serbest bırakma değildir. Kesinti sonrası DNS/TLS
sorguları eşleşti; sürekli erişilebilirlik ölçülmedi. Bütün veritabanı yalnız
metrics nedeniyle farklıydı (Arch 8→11, Debian 7→12 satır). Eski metrics
satırlarının tek tek korunduğu ayrıca iddia edilmez.

| L raporu | Arch SHA-256 | Debian 13 SHA-256 |
|---|---|---|
| Mühürlü dizin | `f47d5bf4527eb47036667c25b50aa55c02d41084eda3b51677940e175c7f8215` | `9cd6c8aa851f275c2aacff25ca370979acd06dcb546248292e658a38ce9524e2` |
| Yerel sonuç | `440016113762f9b40428320ecfa4d78cbf0f2726f49101ac3f46c01f17ea7d53` | `b4f7a48895f6b91842072db5d28b11446843cdcc82bff1c7722c323c5018f219` |
| Sahip girişinde FD gözlemi | `927759aebdb011b4a3867be5af0da7e8138ed740e26d4739e721146d41d6d922` | `4df9a416bfe38742b5b9e4c8fb1b25f4e99c5ba7b5ecf85b75831765a473f6b6` |

## M deneyi: gerçek kit geçişi kesintisinden sonra düzeltilmiş sahip kurtarması

Yeni Arch ve Debian 13 konukları düzeltilmiş `ad782d8` adayını kullandı. İkisi de
yeni başlatıcı/eski seçici sınırını kanıtladı ve tam ilgili güncelleyiciyi bir kez
öldürdü. Gerçek sabit giriş daha sonra eski seçili veri yeteneği okuyucusunu
exit 0 ve değişmeyen çift ile çalıştırdı. Her sistemde bir açık sahip `recover`
komutu exit 0 ile tamamlandı.

Ayrı yerel gözlemler `runtime-status=known/committed`, ilk niyete bağlı kanonik
tamamlanma kaydı, yeni başlatıcı/seçici kimlikleri ve 13'er dosyalı tam eski/yeni
kitleri kanıtladı. Seçili hedef
`68e02c865f597ad030b95318fe58ca1782e7b607edbaae6f3ae7c0c1e5b9e4a3` oldu.
Saklanan adayın 292 dosyası doğrulandı. İlk Alpha75 süreç kimlikleri, diskteki ve
çalışan programların özetleri ile sahibin birim dosyaları değişmedi. Kurtarma
sonrası DNS sorguları, sunulan TLS ve panel web varlık ağacı gözlemleri eşleşti. Bütün
veritabanı yalnız metrics nedeniyle **FARKLI** kaldı (Arch 10→17, Debian 8→20 satır); ayrıca eski satır
koruma iddiası veya tablo dışlama yoktur. Gözlemcinin `cleanup=thaw-unconfirmed`
sonucu korundu; öldürülen sürecin artık çalışmadığı gözlendi. Test bu süreci yeniden başlatmadı.

Bu, başlatıcı yayımlandıktan sonra sınırlı **açık sahip devamını** kanıtlar;
eski uygulamanın altyapısıyla otomatik tamamlanmayı kanıtlamaz. Bağımsız ana
makine incelemesi Arch'taki 56 ve Debian'daki 57 mühürlü dosyanın tamamını ve
işlem zincirini doğruladı. İki konuk kayıtlı koruyucularıyla durduruldu ve
kanıtları saklandı. Geçiş sonrası ayrı uygulama geri alma deneyi aşağıda kaydedildi.

| M raporu | Arch SHA-256 | Debian 13 SHA-256 |
|---|---|---|
| Mühürlü dizin | `5bac0d4b7266ec2ad5aec5044322846a41f8cdf5d88eecc3d20accb451790641` | `7abb5b6fb499873390a661bf1a8b66a3f0462e722e47edb770086783fe2cd4a5` |
| Yerel sonuç | `ddd99b67ecd4225a115490152fa4b24be30d48110d29f706c7183f314f9cca99` | `744ee242aaa6b77e6a3c8d13c314cbd39d901a870fac899a9aee2688ec8b6963` |
| Tamamlanmış kit geçişi kanıtı | `f15627c6d5674a6e9b8d5a84af39090156ca8cc9f94adf5493db28e9705c828f` | `af667300868d79352082c3ff63980e307a7974e153d7efb3a4144610587978f7` |

## N deneyi: kit geçişinden sonra otomatik uygulama geri alma

Ayrı yeni Arch ve Debian 13 konukları aynı eski kit ve düzeltilmiş adayla
hazırlandı. Standart `candidate-installed` gözlemcisi gerçek snapshot ve aday
yerleştirmesini doğrulayıp yalnız ilgili güncelleyici grubunu SIGKILL ile keser.
Düzenek `recover`, `rollback.sh` veya kurtarma birimi için `systemctl start`
çağırmaz. Yerel iş birimine ürünün Agent başlatma yolunda da bulunan
`OnFailure=celikpanel-release-recovery.service` ilişkisini bağlar; dolayısıyla
bu deney yine imzalı Agent kabulü değildir. Ek kurtarma/veri kaybı hatası yoktur.

İki sistem de gerçek candidate-installed aşamasına ulaştı; tam ilgili süreç
sinyal 9 ile sonlandı. Yerel günlükler bunun `OnFailure` tetiklemesini aynı
yedeği doğrulayıp `Rollback complete` bildiren kurtarma çağrısına bağlar. Daha
önceki timer/kilit-meşgul gözlemleri ayrıdır ve bu sonuç yerine sayılmaz. Sahip
kurtarması veya elle geri alma çağrısı yapılmadı.

Son salt-okur kanıt, eski Alpha75'in diskteki ve çalışan dosya özetleriyle aktif
hizmetlerinin geri geldiğini, aktif işlem kalmadığını gösterdi. Yeni kit seçili
ve tamamlanmış kaldı, önceki kit korundu. Tam 292 dosyalı aday, 129 dosyalı
snapshot ve 15 dosyalı bağımsız veri kümesi doğrulandı. Sahip birim dosyaları,
yetkili DNS sorguları ve sunulan TLS eşleşti. Loopback panel HTTPS'i 200 döndürdü;
panelin web varlık ağacı (`/opt/celikpanel/web`) ayrıca incelendi. Barındırılan
sitelerin belge kökleri ve HTTP yanıtları sınanmadı.

Veritabanının 65 tablosunun tamamı dahil edildi. Bütün karşılaştırma yalnız
`metrics_samples` nedeniyle **FARKLI** kaldı; tablo dışlanmadı. İki konuktaki ek
salt-okur satır karşılaştırması, yedekteki 11 satırın tamamının aynı rowid ve
türlü değerlerle korunduğunu, eksik/değişmiş satır olmadığını ve yedek alındıktan
sonra sekiz yeni satır eklendiğini gösterdi (canlıda 19 satır). Bağımsız inceleme
sabitlenmiş karşılaştırma programını ve mühürlü toplam sonucu doğrular; ham
veritabanı satırları dışarı aktarılmadığından tek tek satırları toplam sonuçtan
yeniden üretemez.

Bu, kit geçişinden sonra yerel hata yolu üzerinden sınırlı otomatik yedek geri
almayı kanıtlar. Bu deney yeniden açılış kurtarmasını veya ek kurtarma kesintisi/
aday veri kaybı birleşimini kanıtlamaz.

Bağımsız ana makine incelemesi Arch'taki 42 ve Debian'daki 43 mühürlü dosyayı
yeniden özetledi; her sistemde tam kurtarma çağrısı ve snapshot/kit/veri bağları
dahil 24 kapsam kontrolü geçti. İki konuk kayıtlı koruyucularıyla durduruldu;
diskler ve kanıtlar saklanıyor.

| N raporu | Arch SHA-256 | Debian 13 SHA-256 |
|---|---|---|
| Mühürlü dizin | `a1962bb9ab9f463d0b2471ffa045c0bab27dfd4c3834da01fa3c1e5db745d686` | `df8e361079e6162853ef4cf5fa5b407a85e47b5c208d3a5bdcea134d775bb4c5` |
| Yerel sonuç | `ff7f060c2fad91340e3fbc4b8df15cfa5065082cd17092b8609e33120c85fbd1` | `1f32e03bb01ab1290e8dfdde0247d13ebb85796e2aaea88962d2adb84a079738` |
| Metrics satır koruma gözlemi | `485d8f1a35e2dd11cdc22114771d84696c76a722f257dc519087872bf8325e43` | `2fa6db27f038f6832fe004ef76e03d2085a368cb6fa5bc6505d7a66fce5d5a2b` |

Düzeltilmiş `ad782d803b1ba9531b1a3e92627ebdd748fe7c89` kaynağı bütün
[CI 34898469236](https://github.com/celikbros/celikpanel/actions/runs/34898469236)
işlerini geçti. Bu sonuç gerçek sistem kanıtını destekler; onun yerine geçmez.

## Kalan kapsam

P0.3 kısmidir. Bütün aşama birleşimleri, imzalı Agent kabulü, yeni tokenlı tarihsel
geri alma, metadata geçişleri, kanıt temizliği ve bağımsız iş yükü yenileme/açılış
matrisi açıktır. Adayın bütün kaynağını veya eski kiti kaldıran bileşen testleri
ayrı kanıttır; gerçek sistem deneyi aynı hatayı uygulamadan o kapsamı kazanmaz.
