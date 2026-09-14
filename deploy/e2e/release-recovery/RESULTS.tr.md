# Gerçek sürüm kurtarma gözlemleri — 2026-09-14

*[English](RESULTS.md) · [Deney düzeneği talimatları](README.tr.md)*

Bu kayıt ilk dört imzalı sürüm denemesini korur. Daha sonraki yayımlanmamış
aday, iki sistemde de sınırlı eski sürüme geri dönüş senaryosunu geçti;
[birim geçişi kabul kaydına](UNIT-TRANSITION.tr.md) bakın.

Dört geçici QEMU denemesinde gerçekten yayımlanmış dosyalar ve kurulu Agent'ın
imzalı güncelleme işçisi çalıştırıldı. Debian 13'te güncelleme hatası ve ardından
otomatik kurtarma hatası yeniden üretildi. Arch'ta bekleyen Alpha80 güncellemesi
tamamlanarak hizmetler çalışır duruma getirildi. Yeni Arch ve Debian konuklardaki
süreç öldürme denemeleri ise geri yüklemeden önce başarısız oldu.
**Bu dört denemenin hiçbirinde eski sürüme
tam geri yükleme çalışmadı. P0.1 açık kalıyor.**

İlk iki gözlem, yerel olarak `/var/tmp/cp-release-drill-20260914-b` altında saklanan
`release-recovery__35a6cb18dede09f7` deney hücresine aittir. Boston, Frankfurt veya
başka bir kurulu müşteri panelinde yapılan değişiklikler değildir. Aşağıdaki tüm
saatler UTC'dir. Raporda seçilmiş bulgular ve özetler bulunur; özel günlükler,
kimlik bilgileri, token'lar ve özel sertifika içerikleri yer almaz.

## Gerçekte ne çalıştı?

Tüm deneme konukları `5aa03fd5b6775b21834ff7b1ce0695d92f50ae93` commit'indeki gerçek
imzalı `v0.1.0-alpha.75` kurulumu ile başladı. Yayımlanmış, değiştirilmemiş başlangıç
betiğinin SHA-256 özeti
`82b2674c103e347df471ec7e3f2f091d50006c957c54ac946b7c021ed19e041d` değeridir.
Gerçek Agent, bağımsız BIND yönetimini devraldı; `recovery-fixture.test` bölgesini
oluşturdu ve ardından değiştirdi. Bu gerçek üreticiler, ilk devralma sahiplik
kaydını koruyarak yayımlanan nesli ilerletti. DNS kanıt kayıtları veya üretim
sunucusu durumu elle oluşturulmadı.

Denetleyici, her düğümde tek güncellemeyi başlatmadan önce tam istek kimliğini
kalıcı olarak sakladı. Kurulu Agent, istenen imzalı sürümü bağımsız olarak indirip
doğruladı. Gerçek `celikpanel-self-update-<istek>.service` işçisi ve sistemin
`OnFailure=celikpanel-release-recovery.service` yolu çalıştı. Geri yükleme gövdesi
değiştirilmedi, sahte kurtarma kullanılmadı, elle geri yükleme yapılmadı ve ikinci
bir güncelleme başlatılmadı.

| Deneme | İstek kimliği | Gözlenen sonuç |
| --- | --- | --- |
| Debian 13, Alpha75 → Alpha79 | `5a25a60dadd257dd6023eca4a988a54c` | **FAIL:** güncelleme ve otomatik kurtarma başarısız; aday dosyalar kurulu kaldı, panel ve Agent durdu. |
| Arch, Alpha75 → Alpha80, geçici TCP2083 çakışması | `4db8210bd09431d0e7b187bfedc4e858` | Bekleyen güncelleme tamamlanarak hizmetlerin kurtarılması **gözlendi**; tam geri yükleme hedefi **INCONCLUSIVE**. |
| Yeni Arch, Alpha75 → Alpha80, etkin aşamada SIGKILL | `8e79de5b2290688276d2ae8df056ba1c` | **FAIL:** otomatik kurtarma, systemd tarafından yüklenmemiş birim geçişini geri yüklemeden önce reddetti. |
| Yeni Debian 13, Alpha75 → Alpha80, etkin aşamada SIGKILL | `8ffc77c4e7a59bdfe175b87489de5f4a` | **FAIL:** aynı koruma önkoşulu otomatik geri yüklemeyi engelledi; iki koordinatör etkisiz kaldı. |

## Debian 13: hata yeniden üretildi

Güncelleme `07:58:14`'te başladı. `07:58:33`'te Alpha79 dosyaları kurulmuştu;
işçi, yönetilen BIND nesil kökünü hazırlarken
`BIND state and ownership receipts disagree` hatasıyla durdu. Gerçek `OnFailure`
yürütücüsü çalıştı. `07:58:34`'te saklanan sürümün alt süreci, devraldığı işlem
tanımlayıcısını reddetti:
`recovery transaction descriptor owns an unexpected lock`.

Kurtarma, geri yükleme gövdesine girmeden çıkış kodu 1 ile sonlandı. Saklanan
`07:58:41` gözleminde Agent etkisiz, panel hata durumundaydı; etkin işlem işaretçisi
duruyordu. Kurulu çalıştırılabilir dosyaların özetleri gerçek Alpha79 arşiviyle
eşleşiyordu; iki hizmetin de çalışan bir `MainPID` değeri yoktu.

Snapshot kimliği:

```text
20260914T075823Z-from-unknown-to-f3390addc85bbe92a0cc865448d9bb366e6b980a-4599eadccc696b6f9f35c939372596d5
```

`from-unknown`, kaydedilmiş snapshot adının parçasıdır; deney düzeneğinin başlangıç
sürümünü bilmediğini göstermez. Bu gözlemde snapshot'ın eksiksizliği bağımsız olarak
sınıflandırılmadı.

Kurulu TLS sertifikasının parmak izi değişmedi; ancak yerel TLS bağlantısı
reddedildi. BIND, PID 6292 ile etkin kaldı ve UDP/TCP53 dinleyicileri vardı.
Konukta `/usr/bin/dig` bulunmadığından DNS yanıt toplayıcısı çalışamadı. Önceki ve
sonraki yanıtlar **bilinmiyor**; doğrulanmış hata veya korunduğunun kanıtı değiller.
Başlangıç durumunu sonradan başarılı saymak için hata sonrasında tanı paketi
kurulmadı.

## Arch: bekleyen güncelleme tamamlanarak hizmetler kurtarıldı

Güncelleme `08:12:22`'de başladı. Deney aracı, ilk panel durduktan sonra
`127.0.0.1:2083` adresini meşgul etti. `08:12:47.821587`'de çakışma sürerken kurulu
aday dosyalarını doğruladı. `08:12:50`'de güncelleme işçisi
`saved active-like service is not active: celikpanel-panel.service` hatasıyla
çıktı. Araç, tam olarak bu güncelleme birimi sonlandığında, `08:12:50.857236`'da
portu serbest bıraktı. CelikPanel hizmetlerini başlatmadı veya geri yüklemedi.

Gerçek `OnFailure` kurtarma yürütücüsü ardından başarıyla tamamlandı. Sistemin
`08:12:55` günlüğünde açıkça
`Previous pending update finalized from verified snapshot` yazıyor.
`08:13:35`'te Agent ve panel etkindi; hem kurulu hem çalışan dosyalar **Alpha80**
özetleriyle eşleşiyordu. Etkin işlem işaretçisi yoktu. Bu, bekleyen güncellemenin
tamamlandığının kanıtıdır; Alpha75'in geri yüklendiğinin kanıtı değildir.

Snapshot kimliği:

```text
20260914T081230Z-from-unknown-to-bd14d97efc5cfd19acd70ddf0edb9c6343317e2b-85e1fd2f9c6dc7b0f040fc2aeb6adc07
```

Önceki ve sonraki yerel A ve SOA sorguları, hem UDP hem TCP üzerinden yetkili
`NOERROR` yanıtları verdi. Yanıtlar korundu: `recovery-fixture.test` için A
`192.0.2.11`, SOA seri numarası `2`. Sunulan TLS parmak izi de korundu:
`a52eaeb13389a65ac6f6e2b6829f173fa531f11e01d8a63e80123e529e4c1797`.
Sonraki kurtarma işlemi hizmet erişimini geri getirse de önceki güncelleme
işçisinin hatası kayıtlı bir hata olarak kalır.

## Yeni Arch: etkin aşamada süreç öldürme, geri yüklemeden önce kurtarmayı durdurdu

Üçüncü deneme, `/var/tmp/cp-release-drill-20260914-c` altındaki yeni
`release-recovery__a62839366acb9420` hücresinde yapıldı.
`8e79de5b2290688276d2ae8df056ba1c` isteği `08:27:22`'de başladı.
`08:27:56.681569`'da araç, tam olarak ilk işçiyi (PID 10557) dondurmuş, 129 snapshot
dosyasını doğrulamış ve işlem aşaması `active` iken kurulu Alpha80 dosyalarını
eşleştirmişti. `08:27:56.733642`'de SIGKILL yalnız bu güncelleme biriminin cgroup'una
gönderildi; sistemin systemd olayları çıkışı doğruluyor. Araç hizmet tanımlarını
veya kurtarma durumunu yeniden yazmadı.

Gerçek `OnFailure` kurtarması hemen başladı. `08:27:57`'de
`celikpanel-agent.service has unconsumed guard changes`, ardından
`release transaction service guards differ from the monotonic foundation`
bildirildi. Saklanan alt süreç, geri yüklemeden önce çıkış kodu 1 ile sonlandı.
Sistemin zamanlayıcı denemeleri aynı işlem altında reddi `08:28:33`, `08:29:07` ve
`08:29:39`'da tekrarladı. `08:29:55`'te Alpha80 dosyaları kurulu kalmıştı; iki
koordinatör de çalışmıyordu ve etkin işaretçi duruyordu. UDP/TCP A/SOA DNS yanıtları
başlangıçla eşleşti. TLS dosyasının parmak izi değişmedi; ancak sunulan TLS bağlantısı
reddedildi. Eski sürüme geri yükleme hedefi **bu kesinti için FAIL**.

```text
20260914T082735Z-from-unknown-to-bd14d97efc5cfd19acd70ddf0edb9c6343317e2b-beb02f79fc26e90507dbccecb65e4018
```

Salt-okur birim incelemesi, beklenen yüklü `FragmentPath` ve koruma `DropInPaths`
değerleriyle birlikte Agent ve panelde `NeedDaemonReload=yes` olduğunu doğruladı.
Diskteki her üretici biriminin içeriği **hem eski snapshot ile hem saklanan Alpha80
adayıyla aynıydı**: Agent
`d13caa039eb0515b5399ecece1df2620c289356fb0eaff66d7c524812a62a22b`, panel
`ee1fa28cf8532faee65ae6fae0437634e61aa5616326017eecb9558bb82f6fa3`.
Kurulu dosyaların değiştirilme saatleri yaklaşık `08:27:56.364` / `08:27:56.367`,
snapshot kopyalarınınki `08:27:51.331` / `08:27:51.351` idi. İki koruma drop-in'i de
`82e0fe894b3fa676c1b64e65ac964b74b27dcfe5a83ddf91761f44ad5971359f` özetini korudu.
Yönetici özellikleri ve disk özetleri ayrı gözlendi; geçmişte belleğe alınan birim
metninin özetinin elde edildiği iddia edilmez.

Kaynak, geçerli bir kesinti durumunu açıklıyor: Alpha80'de `install.sh:2940–2942`,
üretici birimlerini 2969. satırdaki `daemon-reload` öncesinde yeniden yayımlıyor.
Ancak `rollback.sh:1407–1410`, geri yüklemeden önce koruma doğrulaması istiyor;
`deploy/release-transaction-guard.sh:1361–1364`, `NeedDaemonReload=no` gerektiriyor.
Böylece kararlı durum önkoşulu, içerik değişmese bile güncelleyicinin oluşturduğu
geçişi reddediyor. Kurtarma, yayımlanmış fakat henüz yüklenmemiş birim tanımlarını
sahiplik kanıtıyla açıkça ele almalı; bu rapor düzeltmenin uygulandığını söylemez.
Denemeyi düzeltmek için daemon reload, elle geri yükleme veya ikinci güncelleme
yapılmadı.

Aşağıdaki kanıtlar `/var/tmp/cp-release-drill-20260914-c/evidence/arch/` altındadır.

| Kanıt | SHA-256 |
| --- | --- |
| `native-outcome.json` | `6145f55fa50c8ab7814dcda2de9cfca461b5840a7a99f81ee2079d58980c1a65` |
| `update-kill-collection-1789374510707201472.jsonl` | `585367355f231199c861ffc31fc4b30d42fbbb225bd391687243abd3deaf697d` |
| `update-observe-20260914T082953498183Z.native.json` | `7fa287dd4fcb1b45103dc0d1f86e66a6629d68e3d1b7168cb782bc3204a283df` |
| `update-observe-20260914T082953498183Z.json` | `ad2b04b67a6ffe029dce74930f96e0965c051e37b0b2f996bc3670783e315a22` |
| `unit-transition-observation.json` | `ce8a70b2d34d07982a4dff56b55e37085aad2b1a66f53d5c977ad41b809dfb43` |
| Alpha80 `install.sh` kaynağı | `1550d6134638ccb7b843c8d3cc891c3d9f95799528186d5a5150a562bec53baa` |
| Alpha80 `deploy/release-transaction-guard.sh` kaynağı | `666373620d35ef5f5826c38eab4732fdba5954aa6fa2a503b00e8a49ce752595` |

Öldürme sırası, port çakışması denemesinin saklanan düğümünde değil, ayrıca
hazırlanmış yeni deneyde çalışır. Bu komutlardan önce başlangıç kurulumu ve gerçek
üreticilerle DNS hazırlığı yapılır:

```sh
python3 deploy/e2e/release-recovery/update_trial.py --work-root "$KILL_LAB_ROOT" --node arch --sequence 80 --mode prepare --execute
python3 deploy/e2e/release-recovery/arm_update_kill.py --work-root "$KILL_LAB_ROOT" --node arch --mode arm --execute
python3 deploy/e2e/release-recovery/update_trial.py --work-root "$KILL_LAB_ROOT" --node arch --sequence 80 --mode start --execute
python3 deploy/e2e/release-recovery/arm_update_kill.py --work-root "$KILL_LAB_ROOT" --node arch --mode collect
python3 deploy/e2e/release-recovery/update_trial.py --work-root "$KILL_LAB_ROOT" --node arch --sequence 80 --mode observe
```

## Yeni Debian 13: etkin aşamadaki kurtarma hatası tekrarlandı

Dördüncü deneme, yeni lab-c'nin Debian düğümünde
`8ffc77c4e7a59bdfe175b87489de5f4a` isteğiyle yapıldı. Başlangıç gözlemlerinden önce
`bind9-dnsutils`, ayrı bir deney bağımlılığı olarak kuruldu; bu, önceki Debian
denemesindeki eksik DNS kanıtı sonucunu değiştirmez.

`08:37:33.523`'te araç, tam olarak ilk işçi (PID 10028) dondurulmuşken etkin aşama
kontrol noktasını, 129 snapshot dosyasını ve kurulu iki Alpha80 özetini doğruladı.
`08:37:33.586`'da yalnız bu birimin cgroup'una SIGKILL gönderildi. Gerçek
`OnFailure` kurtarması `08:37:37`'de aynı yüklenmemiş koruma reddiyle başarısız oldu;
zamanlayıcı denemeleri bunu `08:38:10`, `08:38:42` ve `08:39:14`'te tekrarladı.
Tam geri yükleme gövdesi çalışmadı. İki koordinatör de `MainPID=0` ile etkisizdi;
aday Alpha80 dosyaları kurulu, etkin işaretçi yerinde kaldı.

```text
20260914T083722Z-from-unknown-to-bd14d97efc5cfd19acd70ddf0edb9c6343317e2b-49f34c2c3e3c8ca04e884d28d98d0b78
```

Dört başlangıç ve sonuç DNS sorgusu da yetkili `NOERROR` verdi; A `192.0.2.11` ve
SOA seri numarası `2` korundu. Sunulan TLS bağlantısı reddedildi; sertifika dosyası
`d81106101d7e00f9eb4550bf0f1972a89f20ec7ebcdfd3dcbb9e56014ce95321`
parmak izini korudu. Sonuç veritabanının bütünlüğü şema 42'de `ok` idi; boş olmayan
başlangıç WAL'ı, anlamsal korunma iddiasını hâlâ engelliyor. Yukarıdaki ayrıntılı
disk/yönetici birim karşılaştırması Arch denemesine aittir; bu Debian sonucu,
sistemin koruma reddini bağımsız olarak doğrular. Elle kurtarma veya ikinci
güncelleme başlatma yapılmadı. Yukarıdaki öldürme denetleyicisi sırası yeni
`debian13` düğümünde, tüm işlem kimlikleri ayrıca üretilip saklanarak tekrarlandı.

Özel kanıt kökü: `/var/tmp/cp-release-drill-20260914-c/evidence/debian13/`.

| Kanıt | SHA-256 |
| --- | --- |
| `native-outcome.json` | `a11cc973e65c2d2b070c462d48a92d993f6635e50322f8b5529c7b85c70a8b09` |
| `update-kill-collection-1789375055854888567.jsonl` | `9a4d2686a472e489e2a3950b5dd4a21045d4422865d27a476189d3128b048ed1` |
| `update-observe-20260914T083933577712Z.native.json` | `f09446faeb2df9646ac75edbd4c194ed7b41a8dcb5eee038d80f8ea08a13a9c2` |
| `update-observe-20260914T083933577712Z.json` | `a8dff3f08f7031b4f67f35d50b49f912efe1578176262c310f62aa785235ac7c` |

## Dosya ve kaynak kimlikleri

Tüm özetler SHA-256'dır. Aşağıdaki Agent ve panel aday özetleri, imzalı arşivin
içeriği ve gözlenen kurulu dosyalarla eşleşir. Arch port çakışması denemesinde çalışan dosyalar da aynı
özetlerle eşleşti.

| Dosya | Debian Alpha79 | Arch Alpha80 |
| --- | --- | --- |
| Commit | `f3390addc85bbe92a0cc865448d9bb366e6b980a` | `bd14d97efc5cfd19acd70ddf0edb9c6343317e2b` |
| İmzalı release-manifest-v2 içeriği | `a58933f2e9df413967099e3e180ad0b761c12487866a5121286ba560abdb170a` | `04b20c2c0e3af195979cae0691a8b0266fe6df1245191e5cb1cd74c0f60f9fbe` |
| Arşiv | `cfc4ec5dd4f8b8d178d4b3bc328f62f0e541aadfe524abcedd605bc915b4ba5f` | `a29bd22d72dfd70d19811994999fda9b5d7d8d85873e2a65dc190d8034c70f10` |
| Agent | `ae3b563d65e24e4176b875a440c8e6a6b3bdea14213ac106b579701587a5782d` | `7f84ca2f03b82b51748a7a2c4f7bda8938f43a8e8bcac546171368d320f4c5ac` |
| Panel | `8abd262df639364ddfa274b303b5112f0eae25758f3118aa433e034154cad698` | `88d607bcdf160ab1d296803a5632d19e72cc7351365761de5d6847191537ce67` |
| Yayımlanmış `update.sh` | `2d787c590b7d0dfe0cfc59548aba957862faa13cb86d6a3d67d6cf5b0020658c` | `703263a66853503e960dd01ae13e01ced53278bc9f99f4282a7b111e12f218b1` |
| Yayımlanmış `rollback.sh` | `7063d2a5951fb6b7d0bc5b7eac320ec966a05a0bc58f818de7cfdbe1902ecb64` | `e559f741043c60615d67d829c0fb7769934334ef682bff3bdb7930125b76b1c7` |

Geçmiş kaynak `git show <commit>:<dosya>` ile okunabilir. Belirtilen Alpha79
sürümünde `update.sh:3220`, başarısız BIND hazırlığını bildirir;
`rollback.sh:707`, devralınan kilidi reddeder. Alpha80'de `update.sh:2154`, gözlenen
bekleyen güncelleme tamamlanmasını bildirir. Bunlar geçmiş sürümlerin satır
numaralarıdır; güncel çalışma ağacındaki konumlar için geçerli olmak zorunda
değildir.

Özel kanıt kökleri, saklanan deney kökünün altındaki `evidence/debian13/` ve
`evidence/arch/` dizinleridir. Özetler; başlangıç durumu, DNS hazırlığı, kalıcı
niyet kaydı, incelenmiş hedef değerleri, başlatma girişimi ve sistem gözlemlerinin
tam başvurularını ve dosya özetlerini içerir.

| Göreli kanıt yolu | SHA-256 |
| --- | --- |
| `debian13/native-outcome.json` | `430f0c2bb2ad67d2a9cf87a30e3e2c204f4fff2d5d7935539b7758391bbbeb54` |
| `debian13/update-status-20260914T075840122387Z.native.json` | `082c1af58dcaed83a0d478ddb8fd30d4f4d7ee2b51195520ad6224afea977d6f` |
| `debian13/update-status-20260914T075840122387Z.json` | `072359f74d178ade01f858617d428b36d5ba522851ba201f99fdc0b2df4d9f47` |
| `arch/native-outcome.json` | `415d06623513347e5ec9188bf40a509c163a16251fc657792b9c3e104d6dacd4` |
| `arch/update-observe-20260914T081333791576Z.native.json` | `a7914a73615c1216b1399517b18db4d26c79558221ea48d02ae196e2e49c662b` |
| `arch/update-observe-20260914T081333791576Z.json` | `4e5da4786e7107746d5c3f111239d0e1db4f47626990082432935428df36c245` |
| `arch/port-fault-collection-1789373605641389722.jsonl` | `d143d7d518c75bdf48fc3f2d5cc1483a491c76724dd51f80a699e8e73df02048` |

## Denetleyici sırası

[Deney düzeneği talimatları](README.tr.md) ile **yeni** bir kayıtlı deney oluşturun;
Alpha75'i kurun, iki düğümde DNS hazırlığını yapın ve gerekli başlangıç iş
yüklerini gözleyin. `dig` dahil tanı araçlarını başlangıç gözleminden önce kontrol
edin. Kanıtları saklanan hücrede başlatmayı tekrarlamayın; yeniden denemek için
niyet kaydını silmeyin. Yerel imzalı dosyalar `.tmp-release79/assets/` ve
`.tmp-release80/assets/` dizinlerindedir; güncelleme sürücüsü
`.tmp-release-recovery-update` dosyasıdır. Yeni bir tekrar için desteklenen araç
zinciriyle derleyin:

```sh
GOTOOLCHAIN=go1.26.5 go build -o .tmp-release-recovery-update ./deploy/e2e/release-recovery/driver-update
```

Seçilen denetleyici sıraları şunlardı:

```sh
python3 deploy/e2e/release-recovery/update_trial.py --work-root "$LAB_ROOT" --node debian13 --sequence 79 --mode start --execute
python3 deploy/e2e/release-recovery/update_trial.py --work-root "$LAB_ROOT" --node debian13 --sequence 79 --mode status

python3 deploy/e2e/release-recovery/update_trial.py --work-root "$LAB_ROOT" --node arch --sequence 80 --mode prepare --execute
python3 deploy/e2e/release-recovery/arm_port_fault.py --work-root "$LAB_ROOT" --node arch --mode arm --execute
python3 deploy/e2e/release-recovery/update_trial.py --work-root "$LAB_ROOT" --node arch --sequence 80 --mode start --execute
python3 deploy/e2e/release-recovery/arm_port_fault.py --work-root "$LAB_ROOT" --node arch --mode collect
python3 deploy/e2e/release-recovery/update_trial.py --work-root "$LAB_ROOT" --node arch --sequence 80 --mode observe
```

`prepare`, güncellemeyi başlatmadan incelemeyi saklar. `start`, bu tam niyet kaydı
altında bir kez kabul edilir. Zaman aşımı, sonucu belirsiz bırakır; `status`, aynı
isteği uzlaştırır ve ikinci bir değişiklik başlatmaz. Bu komutlar yalnız
nonce/DMI/süreç/SSH sunucu anahtarı denetimleriyle korunan geçici deney düzeneğinde
çalışır.

## Mimari sonuçlar ve açık kabul işleri

Bir `OnFailure` birimi yalnızca yürütmeye giriş noktasıdır. Alpha79 bu noktaya
ulaştı; ancak saklanan kurtarma alt süreci herhangi bir geri yüklemeden önce kilit
sözleşmesinde başarısız oldu. Güncelleyici, kurtarma yürütücüsü ve saklanan betik;
gerçek üst/alt süreç düzeninde devralınan tanımlayıcının kimliği ve sahipliği
konusunda aynı kurala uymalıdır. Geri yükleme gövdesinden önce duran testler bunu
kanıtlayamaz.

BIND gerilemesi de üretici/tüketici kapsamı gerektirir: ilk devralma sahipliği ile
güncel yayımlanan nesil, aynı yetki döneminin farklı anlarını anlatır. Geçerli
yayımlama ilerleyişi tanınmalı; geçmiş sahiplik kanıtı silinmemeli, ilgisiz bir
sahip değişikliği kabul edilmemelidir.

Bekleyen güncellemeyi tamamlamak yararlı bir kurtarma sonucudur; kabul ölçütü geri
yüklemeden ayrıdır. Güncel aday özetleri, etkin işlem işaretçisinin olmaması ve
sağlıklı hizmetler; eski dosyaların, verinin ve iş yüklerinin geri yüklendiğinin
kanıtı yerine geçmez. İlk hata korunmalı, ayrı olarak doğrulanan kurtarma sonucu
raporlanmalıdır.

Bu denemelerde aşağıdaki kanıt eksikleri devam eder:

- Etkin aşamada süreç öldürme otomatik kurtarmaya ulaştı; geri yüklemeden önce
  başarısız oldu. Başarılı tam eski sürüm geri yüklemesi ve güç kaybından kurtarma
  henüz kanıtlanmadı.
- Debian'da `dig` eksikliği DNS yanıtlarını belirsiz bıraktı. Arch, yerel bağımsız
  DNS'te UDP/TCP A/SOA kapsamını sağladı; gerçek ikincil sunucu, aktarım, TSIG veya
  delegasyon kapsamını sağlamadı.
- Canlı, boş olmayan WAL, tutarlı başlangıç veritabanı incelemesini engelledi;
  Arch'ın sonraki incelemesi de belirsiz kaldı. Debian'da sonradan
  `integrity_check` sonucunun `ok` olması, verinin anlamsal olarak korunduğunu
  kanıtlamaz.
- Tüm deneme konukları gerçek kurulumun başlangıç TLS sertifikasını kullandı. Sertifika
  edinimi, değişimi ve bağımsız yenileme test edilmedi.
- E-posta, barındırılan web istekleri, veritabanı uygulama trafiği, cron ve bağımsız
  açılış/yenileme iş yükleri için ayrı başlangıç/sonuç kabulü gerekir.

[Dayanıklılık sözleşmesi](../../../docs/RESILIENCE-CONTRACT.tr.md), kabul işleri
açık olan bir gereksinim olarak kalır. Bu gözlemler P0.1'i kapatmaz; her
güncellemenin, hatanın veya sunucu sahibi yapılandırmasının kurtarılabildiğini
kanıtlamaz.
