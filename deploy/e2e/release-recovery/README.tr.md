# Geçici sürüm ve kurtarma laboratuvarı

*[English](README.md) · P0.1 kanıt araçları*

Bu dizin temiz Debian 13 ve Arch QEMU konukları hazırlar, gerçek Alpha75 sürümünü
kurar, kurulu Agent üzerinden sınırları belirli bir DNS test durumu oluşturur ve
yerel gözlemler toplar. **Araçların bulunması, konukların açılması veya çevrimdışı
testlerin geçmesi gerçek ortamda PASS sonucu oluşturmaz; P0.1'i kapatmaz.**
[Dayanıklılık sözleşmesi](../../../docs/RESILIENCE-CONTRACT.tr.md), gerçek
güncelleme/otomatik geri alma ve yerel iş yükü kabulünün tamamlanmasını gerektirir.

## Kaydedilmiş gerçek ortam sonuçları

[2026-09-14 sonuç kaydı](RESULTS.tr.md), gerçek QEMU denemelerinin tam işlem,
ürün, yedek ve özel kanıt özetlerini içerir:

| Deneme | Gözlenen sonuç | Eski sürüme tam geri alma |
| --- | --- | --- |
| Debian 13, Alpha75 → Alpha79 | Güncelleme BIND sahiplik uyumluluğunda durdu; otomatik kurtarma da devralınan kilit kontrolünde başarısız oldu. Aday dosyalar kurulu kaldı; panel/Agent durdu. | Geri yükleme gövdesi çalışmadı; kurtarma girişimi başarısız oldu. |
| Arch, Alpha75 → Alpha80, geçici port çakışması | Gerçek kurtarma bekleyen güncellemeyi tamamladı. Kurulu ve çalışan programlar Alpha80 idi; DNS A/SOA yanıtları ve sunulan başlangıç TLS parmak izi korundu. | **INCONCLUSIVE:** güncelleme tamamlanarak erişim geri geldi; Alpha75 geri yüklenmedi. |
| Arch, Alpha75 → Alpha80, etkin güncelleme sırasında kesinti | Tam güncelleme işçisi öldürülmeden önce aday ve tamamlanmış yedek doğrulandı. Gerçek kurtarma, değişmemiş üretici servis dosyalarının yayımlanması ile yeniden yükleme arasındaki `NeedDaemonReload=yes` durumunu reddetti. | **FAIL:** kurtarma tam geri yükleme gövdesinden önce durdu. |
| Debian 13, Alpha75 → Alpha80, etkin güncelleme sırasında kesinti | Gerçek `OnFailure` kurtarması aynı yeniden yükleme durumu kontrolünde başarısız oldu. UDP/TCP DNS A/SOA yanıtları korundu; HTTPS bağlantısı reddedildi. | **FAIL:** kurtarma tam geri yükleme gövdesinden önce durdu. |

Bekleyen güncellemeyi tamamlamak yararlı ve gözlenmiş bir kurtarma sonucudur.
Eski programları, veriyi ve iş yüklerini geri yüklemekten farklı bir kabul
ölçütüne sahiptir; tam geri alma PASS sonucu veya yeni bir işlev hatası değildir.
İlk güncelleme hatası ayrıca kayıtlı kalır. **P0.1 hâlâ açıktır.** Etkin işçi
kesintisi gerçek kurtarmaya ulaştı; ancak olağan çalışma durumunu arayan kontrol,
kesilmiş geçişi geri yüklemeden önce reddetti. Bu, açık P0.3 hata kanıtıdır;
düzeltilmiş bir kusur veya tamamlanmış geri alma kabulü değildir.

Rapor kanıt sınırlarını da korur: ilk Debian denemesinde `dig` yoktu, canlı WAL tam veritabanı
karşılaştırmasını engelledi; TLS edinimi/yenilemesi ve gerçek DNS eş sunucu
aktarımı test edilmedi. Diğer bağımsız iş yükleri de henüz ölçülmedi.

## Sınır ve önkoşullar

- [DNS test düzeneğinde](../dns-kill-matrix/README.md) açıklanan bağımlılıkları ve
  sabitlenmiş temel imaj önbelleğini içeren Linux QEMU sunucusunda çalıştırılır.
  Başlatıcı her konuk için KVM, iki işlemci, 3 GiB RAM ve yeni 24 GiB yazılabilir
  disk katmanı kullanır. Windows QEMU bu başlatıcının çalışma ortamı değildir.
- Çalışma kökü yeni bir `/var/tmp/cp-release-drill-AD` olmalıdır. Mevcut sunucu
  veya keyfi SSH hedefi parametresi yoktur. Her laboratuvar yeni anahtar, nonce,
  özeti sabitlenmiş test planı ve cloud-init işaretçisi alır. Yönetim SSH
  yönlendirmeleri yalnız `127.0.0.1` adresine bağlanır; sonraki işlemler öğrenilen
  sunucu anahtarını zorunlu tutar.
- Sunucu, kayıtlı QEMU komutunu ve PID'yi kontrol eder. Konuk yürütmesi; root
  sahipli işaretçiyi, tam nonce/hücre/düğüm/UUID kimliğini, eşleşen DMI UUID'sini
  ve gerçek systemd'yi doğrular. Hazırlama/güncelleme sürücüleri ve toplayıcı ayrıca
  QEMU kimliğini kontrol eder. Bu kontroller geçici test ortamını tanımlar; hem
  sunucuyu hem kanıtını denetleyen kötü niyetli yöneticiye karşı güvence değildir.
- **Laboratuvarın internet bağlantısı vardır.** NAT yönetim ağı genel paket ve
  imzalı sürüm indirmelerine, gerçek Agent'ın genel manifesto sorgusuna da izin
  verir. Diğer ağ kartı yalıtılmış `192.0.2.0/24` test bağlantısını kullanır.
  Loopback SSH'yi dış ağ erişiminin engellenmesi olarak tanımlamayın. Konuklara
  üretim kimlik bilgisi, lisans, yapılandırma, veritabanı veya özel sertifika
  kopyalanmaz.
- Kurulu müşteri panellerinde güncellemeyi yalnız kullanıcı başlatır. Bu test
  sürücüleri Boston, Frankfurt veya başka bir mevcut kurulum için yönetim ya da
  dağıtım aracı değildir.

## Mevcut komut satırı

Depo kökünden çalıştırın. Yeni bir laboratuvar adı kullanın; imaj önbelleği tam
olarak sabitlenmiş imajları önceden içermelidir. Değişiklik yapan başlatıcı
komutları `--execute` gerektirir. `status` bu bayrak olmadan çalışan konukları okur.

```sh
LAB_ROOT=/var/tmp/cp-release-drill-example
NODE=debian13
python3 deploy/e2e/release-recovery/lab.py prepare --work-root "$LAB_ROOT" --ssh-port 2261
python3 deploy/e2e/release-recovery/lab.py prepare --work-root "$LAB_ROOT" --ssh-port 2261 --execute
python3 deploy/e2e/release-recovery/lab.py start --work-root "$LAB_ROOT" --execute
python3 deploy/e2e/release-recovery/lab.py status --work-root "$LAB_ROOT"
```

`--image-cache`, varsayılan `/var/tmp/cp-install-vm/images` yolunu değiştirir.
Örnekteki `2261`, `2262` ve `2263` portları iki SSH yönlendirmesi ve düğümler arası
taşıma içindir; başlatıcı hazırlık sırasında dosya yazmadan önce aralığı doğrular.

Başlangıcı kaydetmeden önce `/usr/bin/dig` dahil toplayıcı önkoşullarını kontrol
edin. Eksik araç bilinmeyen kanıt üretir; arızadan sonra eklemek önceki başlangıç
kanıtını düzeltmez. Özgün sürümü kurun ve sonucunu inceleyin:

```sh
python3 deploy/e2e/release-recovery/install_baseline.py start --work-root "$LAB_ROOT" --node all
python3 deploy/e2e/release-recovery/install_baseline.py start --work-root "$LAB_ROOT" --node all --execute
python3 deploy/e2e/release-recovery/install_baseline.py status --work-root "$LAB_ROOT" --node all
python3 deploy/e2e/release-recovery/install_baseline.py collect --work-root "$LAB_ROOT" --node all
```

Burada `--node`, `debian13`, `arch` veya `all` kabul eder. Başlatma komutu konuğun
`celikpanel-lab-alpha75-install.service` birimini başlattıktan sonra döner; sonucu
status ile inceleyin. Yarım girişim incelenecek kanıttır; yeniden kurulum izni
değildir. Konuk kendi yönetici kimlik bilgilerini üretir ve yalnız özel test
dizininde saklar. Kurucu günlüğü özeldir; gizli bilgiler açısından incelemeden
yayımlamayın. `collect`, kuruluma ait sonucun kaydedilmiş olmasını gerektirir;
özel sonuç/günlük dosyalarını özetleriyle birlikte sunucuya kopyalar. Ayrı kimlik
bilgisi dosyasını kopyalamaz.

Başlangıç kurulumu, `5aa03fd5b6775b21834ff7b1ce0695d92f50ae93` commit'indeki
değiştirilmemiş `download-portal/get.sh` dosyasını kullanır. SHA-256 özeti
`82b2674c103e347df471ec7e3f2f091d50006c957c54ac946b7c021ed19e041d` ile sabittir.
Yayımlanmış bu başlatıcı, 75 sıra numarasını / `v0.1.0-alpha.75` sürümünü ve genel
`https://celikpanel.net` kaynağını seçer. Sabitlenmiş sürüm anahtarı, imzalı
manifesto, arşiv denetimleri ve gerçek kurucu akışta kalır. Eski şema, servis
birimleri, soket ve işlem defteri gerçek sürüm tarafından oluşturulur. Bu;
güncel programları kopyalamaktan, üretim veritabanını içe aktarmaktan veya kanıt
dosyalarını elle yazmaktan ayrıdır. Başlangıçtaki loopback `curl --insecure`
kontrolü HTTP erişilebilirliğini gösterir; **güvenilir sertifika üretimini
kanıtlamaz**.

Yalnız test için olan durum üreticisini Linux'ta deponun desteklediği Go araç
zinciriyle derleyin; seçilen konukta bir kez çalıştırın:

```sh
ARTIFACT_ROOT=/var/tmp/cp-release-drill-artifacts
mkdir -p "$ARTIFACT_ROOT"
GOTOOLCHAIN=go1.26.5 go build -o "$ARTIFACT_ROOT/release-recovery-seed" ./deploy/e2e/release-recovery/driver
python3 deploy/e2e/release-recovery/exercise.py seed --work-root "$LAB_ROOT" --node "$NODE" --binary "$ARTIFACT_ROOT/release-recovery-seed"
python3 deploy/e2e/release-recovery/exercise.py seed --work-root "$LAB_ROOT" --node "$NODE" --binary "$ARTIFACT_ROOT/release-recovery-seed" --execute
```

Hazırlama; eski Agent'ın kimlik doğrulamalı yerel soketini ve gerçek
Begin/heartbeat/finish/status işlemlerini kullanır: önce DNS motoru yokken BIND
yönetimi alınır, ardından `recovery-fixture.test` bölgesi oluşturulur ve değiştirilir.
Yönetimi devralma kanıtının baytları korunurken yayın ilerlemesi doğrulanır. DNS
sahipliği, motor durumu, yapılandırma veya lisans kanıtı elle yazılmaz. Kapsam
**bağımsız çalışan Agent üreticisidir**; tarayıcı sihirbazı, panel veritabanı veya
lisanslı kullanıcı kabul yolu değildir; birincil/ikincil çoğaltma testi de değildir.

`seed-intent.json`, hazırlama komutundan önce saklanır. Dosya oluştuktan sonra
denetleyici zaman aşımı sonrasında da ikinci hazırlama girişimini reddeder.
Kısmi çıktıyı koruyup aynı girişimi inceleyin; yeniden denemek için niyet kaydını
silmeyin. Kabul edilen son üretici olayı da bağımsız yerel DNS gözlemleri gerektirir.

Benzersiz bir etiket ve son 24 saat içindeki UTC başlangıç zamanı ile gözlem alın:

```sh
SINCE_UTC=$(date -u +%Y-%m-%dT%H:%M:%SZ)
python3 deploy/e2e/release-recovery/exercise.py observe --work-root "$LAB_ROOT" --node "$NODE" --label before-update --since "$SINCE_UTC"
```

Güncelleme sonrası gözlemde daha önceki güncelleme başlangıç zamanını kullanın;
isterseniz `--operation-id` ile tam 32 karakterli küçük harf onaltılık işlem
kimliğini verin. `observe`, `--execute` istemez: toplayıcıyı özel test dizinine
yükler ve sunucuda kanıt dosyası oluşturur. Toplayıcının kendisi yönetilen durumu
okur ve yalnız loopback DNS/TLS sorgular; iş yüklerini onarmaz veya değiştirmez.
Kanıt etiketlerinin üzerine yazılmaz.

`driver-update`, konuk kontrolü olan ayrı bir gerçek güncelleme RPC test
sürücüsüdür. Güncel bayrakları `--nonce`, `--manifest`, `--signature`, isteğe bağlı
`--request-id` ve `--mode preview|start|status` biçimindedir; varsayılan mod
`preview` olur. Değiştirilmemiş yayımlanmış manifesto/imzayı doğrular, istemci
inceleme kimliğini kalıcı kaydeder ve kurulu Agent'ın imzalı güncelleme yolunu
kullanır. Tek başına arıza/geri alma matrisini tamamlamaz; denetleyici tam aday
sürüm, işlem, anlık görüntü, arıza ve kurtarma gözlemlerini korumalıdır. Mevcut
müşteri panelini güncellemek için desteklenen bir komut değildir.

### Tek bir imzalı güncelleme denemesi hazırlayın

`update_trial.py`; denemeyi kaydedilmiş konuğa, toplanmış Alpha75 kurulum
sonucuna ve hazırlama işlemlerinin tam sırasına bağlar. Yalnız `--sequence 79`
veya `80` kabul eder. Değiştirilmemiş yayımlanmış manifesto/imza önceden
`.tmp-release79/assets` veya `.tmp-release80/assets` içinde bulunmalıdır. Arıza
testleri ayrıca sabitlenmiş Alpha80 arşivini gerektirir. Bunlar yayımlanmış
ürünlerin yerel kopyalarıdır; test için yeniden imzalanmış manifestolar değildir.

Test sürücüsünü derleyip yukarıda hazırlanan düğümde Alpha80 önizlemesini alın.
`NODE=arch` seçeneğini ancak o düğümü kurup hazırladıktan sonra kullanın.

```sh
GOTOOLCHAIN=go1.26.5 go build -o "$ARTIFACT_ROOT/release-recovery-update" ./deploy/e2e/release-recovery/driver-update
python3 deploy/e2e/release-recovery/update_trial.py --work-root "$LAB_ROOT" --node "$NODE" --sequence 80 --mode prepare --binary "$ARTIFACT_ROOT/release-recovery-update" --execute
```

`prepare`, `update-intent.json` kaydını saklar; `before-update.json` gözlemini
alır veya kontrol eder ve gerçek Agent'tan önizleme ister. Güncellemeyi başlatmaz.
İncelenen istek hâlâ mevcut sürüm olarak Alpha75'i ve tam imzalı hedefi göstermelidir.
Var olan niyet kaydı sessizce değiştirilmez.

Aşağıdaki başlatmadan önce arıza olmamasını veya alternatiflerden **birini** seçin.
Aynı denemede ikisini birden kurmayın. Her yeni hedef/arıza denemesi temiz bir
başlangıç konuğu ve kanıt dizini gerektirir; yeniden başlatmak için niyet kaydını
silmeyin.

### İsteğe bağlı arıza: geçici panel portu çakışması

```sh
python3 deploy/e2e/release-recovery/arm_port_fault.py --work-root "$LAB_ROOT" --node "$NODE" --mode arm --execute
```

Denetleyici, kayıtlı Alpha80 önizlemesini ve daha önce başlatma denenmemiş
olmasını gerektirir. Kurmadan önce incelenen arşivi ve iki aday programın
özetlerini doğrular. `guest_port_fault.py`, tam güncelleyicinin eski paneli
durdurmasını bekler; sonra yalnız `127.0.0.1:2083` adresini tutar. Güncelleyici
çıkınca, kurtarma başlayınca, işlem değişince, gözlem hata verince veya 600 saniye
dolunca soketi bırakır. Ayrı systemd çalışma süresi sınırı da yardımcıyı sınırlar.
Aday kontrol noktası, arıza sürerken kurulu aday özetlerini ve tamamlanmış yedeğin
sağlama toplamı envanterini kaydeder. Bu, geri alma sonucu değildir.

### Alternatif arıza: doğrulanmış etkin güncelleyiciyi kesme

```sh
python3 deploy/e2e/release-recovery/arm_update_kill.py --work-root "$LAB_ROOT" --node "$NODE" --mode arm --execute
```

Bu seçenek temiz ve önizlemesi hazırlanmış ayrı bir Alpha80 denemesi içindir.
Konuk yardımcısı `guest_update_kill.py`, özgün Alpha75 işçi programını; tam istek,
PID/başlangıç sayacı, systemd çalıştırma kimliği, cgroup ve komut satırıyla birlikte
doğrular. Yalnız bu güncelleyici birimini dondurur; etkin işlemi, kurulu aday
özetlerini ve yedeğin tam sağlama toplamı envanterini doğrular. İşçiyi yeniden
kontrol ettikten sonra yalnız o birimin cgroup'una SIGKILL gönderir. Olağan
panel/Agent servislerine sinyal göndermez; kurtarmayı kendisi başlatmaz. Kontrol
noktası kaçırılırsa süreç öldürülmez. Yardımcının 600 saniyelik sınırı, donmuş
kontrol noktası için 30 saniyelik bütçesi ve hem `finally` hem systemd
`ExecStopPost` içinde çözme temizliği vardır. `kill_sent` yalnız uygulanan
kesintiyi kanıtlar; gerçek otomatik kurtarma ayrıca gözlenmelidir.

### Bir kez başlatın, sonra aynı isteği inceleyin

İsteğe bağlı arıza hazır olduğunu bildirdikten sonra gecikmeden başlatın. Arıza
seçilmediyse hazırlıktan sonra aynı başlatma komutunu kullanın:

```sh
python3 deploy/e2e/release-recovery/update_trial.py --work-root "$LAB_ROOT" --node "$NODE" --sequence 80 --mode start --execute
python3 deploy/e2e/release-recovery/update_trial.py --work-root "$LAB_ROOT" --node "$NODE" --sequence 80 --mode status
python3 deploy/e2e/release-recovery/update_trial.py --work-root "$LAB_ROOT" --node "$NODE" --sequence 80 --mode observe
```

Tek gerçek Start RPC çağrısından önce başlatma denemesi kalıcı kaydedilir.
Bağlantı hatası veya zaman aşımı bu kimliği korur; ardından yalnız
`status`/`observe` kullanılabilir. Bunlar güncellemeyi yeniden başlatmaz.
Ham sürücü çıktısı ve sınırlı gerçek günlükler özel kanıt dosyalarında kalır;
konsol özetleri durum bilgileri ve kanıt referansları içerir. Sürücü çağrısının
sıfır koduyla bitmesi tek başına kurulumun veya kurtarmanın tamamlandığını
kanıtlamaz.

Yalnız yukarıda seçilen arızanın kanıtını toplayın; bunlar alternatiftir:

```sh
python3 deploy/e2e/release-recovery/arm_port_fault.py --work-root "$LAB_ROOT" --node "$NODE" --mode collect
```

```sh
python3 deploy/e2e/release-recovery/arm_update_kill.py --work-root "$LAB_ROOT" --node "$NODE" --mode collect
```

Arıza kanıtı toplama, tam konuk/işlem olaylarını ve birim durumunu korur. Konuğu
onarmaz, arızayı tekrar kurmaz veya geri alma kabulünün tamamlandığını ilan etmez.

### Yayımlanmamış commit adayı: gerçek kurtarma regresyonu

`local_candidate_trial.py`, yayımlanmamış yerel derlemeyi mevcut
`bootstrap-prebuilt-update.sh` giriş noktası ve korunan sürümün gerçek kurtarma
betiği üzerinden sınar. Yukarıdaki gerçek Alpha75 kurulumu ve hazırlama işlemleri
bulunan yeni, kaydedilmiş bir VM gerektirir. Aynı konukta imzalı denemeyle
birleştirmeyin. Arşivi temiz ve commit edilmiş kaynak dışa aktarımından `make dist`
ile derleyin; tam SHA256 değerini verin. Denetleyici arşivin tüm envanterini ve
paketlenmiş her sabit kaynak dosyasını o Git commit/ağaç kaydıyla doğrular.
Bu yol açıkça **imzasız yerel derleme kanıtıdır**; imzalı Agent kabulünü sınamaz.
Sürüm imzalamaz, anahtar tanıtmaz veya üretim güven politikasını değiştirmez.

Varsayılan `require-unit-reload` kontrol noktası, koordinatör birimlerinde gerçek
bir eski-yeni sürüm geçişi gerektirir. Örneğin başlangıç gözleminden önce eski
test konuğunun koordinatör birimlerine zararsız bir sahip yorumu ekleyin,
systemd yapılandırmasını yeniden yükleyip doğrulayın; yalnız test ortamındaki bu
değişikliği `evidence/<node>/owner-unit-baseline.json` dosyasında saklayın.
Hazırlık bu dosyanın özetini kaydeder. Hazırlıktan sonra yapay bir bekleyen yeniden
yükleme durumu üretmeyin. Baytları aynı birimleri değiştirmemek doğru davranış
olabilir; kontrol noktası oluşmazsa süreç öldürülmez. `--boundary candidate-installed`
yalnız eski systemd görünümü regresyonunu kanıtladığı iddia edilmeyen, ayrıca
açıklanmış bir denemede seçilmelidir.

`CANDIDATE_ARCHIVE` ve `CANDIDATE_SHA256` değişkenlerini doğrulanmış yerel arşive
ayarlayın. Her değişiklik komutunu bir kez çalıştırıp sonucunu inceleyin; tam arıza
hazır olduğunu bildirdikten sonra gecikmeden başlatın:

```sh
python3 deploy/e2e/release-recovery/local_candidate_trial.py --work-root "$LAB_ROOT" --node "$NODE" --mode prepare --archive "$CANDIDATE_ARCHIVE" --archive-sha256 "$CANDIDATE_SHA256" --boundary require-unit-reload --execute
python3 deploy/e2e/release-recovery/local_candidate_trial.py --work-root "$LAB_ROOT" --node "$NODE" --mode arm --execute
python3 deploy/e2e/release-recovery/local_candidate_trial.py --work-root "$LAB_ROOT" --node "$NODE" --mode start --execute
python3 deploy/e2e/release-recovery/local_candidate_trial.py --work-root "$LAB_ROOT" --node "$NODE" --mode collect
```

Hazırlık, dosyaları yerleştirmeden önce tam arşiv ve konuk niyetini kalıcı kaydeder.
Arızayı kurmanın ve tek gerçek başlatmanın ayrı kalıcı deneme kayıtları vardır.
Belirsiz yanıttan sonra yalnız aynı işlemin kanıtı toplanabilir; ikinci başlatma
veya arızayı yeniden kurma yapılmaz. Toplama, yeni kanıt etiketleriyle tekrarlanabilir.
Arıza; özgün Bash programını, tam bootstrap komutunu, PID/başlangıç sayacını,
çalıştırma kimliğini ve cgroup'u doğrular. SIGKILL öncesinde o güncelleyiciyi
dondurur; aday programları ve kurulu birim/yardımcıları kanıtlar; hem tam yedeği
hem korunan aday envanterini doğrular. Gerçek systemd `OnFailure`, olağan kurtarmayı
çağırır; test, geri alma betiğini taklitle değiştirmez veya olağan panel/Agent
servislerine sinyal göndermez. Toplanan olaylar ve günlükler kanıttır; bunlardan
otomatik olarak kurtarma PASS sonucu çıkarılmaz.

Bu yol eklenirken `test_local_candidate_trial.py` içindeki 22 odaklı çevrimdışı
test geçti. Bu sayı bütün CI kapsamını veya gerçek geri almayı kanıtlamaz;
gerçek önce/kontrol noktası/kurtarma/sonra iş yükü kanıtları hâlâ gereklidir.

Kanıtları koruyarak konukları durdurun:

```sh
python3 deploy/e2e/release-recovery/lab.py stop --work-root "$LAB_ROOT"
python3 deploy/e2e/release-recovery/lab.py stop --work-root "$LAB_ROOT" --execute
```

Durduğu bilinen düğümler QMP durdurma kümesine alınmaz. Açıklanamayan bir soketle
birlikte eksik kimlik veya başka süreç tarafından kullanılan PID reddedilir.
Canlı düğümler kayıtlı QEMU süreçleriyle eşleşmelidir. Durdurma; yazılabilir disk
katmanlarını, günlükleri ve kanıtları korur. Bu sarmalayıcıda özyinelemeli silme
komutu yoktur.

## Kanıt ve kalan kabul çalışmaları

`guest_probe.py`, `celikpanel/release-recovery-observation/v1` üretir: kurulu ve
çalışan program özetleri, web ağacı özeti, servis/zamanlayıcı durumu, veritabanı
bütünlüğü ve geçiş sürümleri, kurulu/sunulan genel TLS özellikleri, loopback
UDP/TCP DNS yanıtları, sınırlı işlem alanları ve izin verilen günlük olayları.
Özel anahtar, parola veya belirteç içeriğini okumaz. Bir gözlem hatası diğer
gözlemleri silmeden `unknown` kalır. Gerçek `Rollback complete` günlük satırı
metin kanıtıdır; kurtarmanın başarılı olduğunu bildiren bir bayrak değildir.

`evidence.py`, ayrı olarak birleştirilmiş
`celikpanel/release-recovery-evidence/v1` kaydını okur. Gerekli başlangıç, aday,
güncelleme, arıza, kurtarma, son durum ve iş yükü bölümleri için
[şema örneğine](test_evidence.py) bakın. Çalıştırma:

```sh
python3 deploy/e2e/release-recovery/evidence.py /absolute/path/to/assembled-evidence.json
```

Çıkış kodları: 0 `PASS`, 1 `FAIL`, 2 `INCONCLUSIVE`. İsteğe bağlı
`--expected-regression CODE`, hatanın yeniden üretilmesini ayrı raporlar; bilinen
bir kurtarma hatasını üretmek kurtarma sonucunu PASS yapmaz. Sınıflandırıcı,
verilen bilgilerin tutarlılığını ve tamlığını kontrol eder. Kaynaklarının
gerçekliğini doğrulayamaz; elle hazırlanmış JSON'u gerçek çalışma kanıtına
dönüştüremez. Denetleyici referansları, ürün imzaları ve gerçek yürütme izleri
bu bilgileri desteklemelidir.

Gerekli gerçek güncelleme/korunan sürüme otomatik geri alma ve iş yükü matrisi
ölçülene kadar P0.1 açıktır. Güncel sınırlar:

- Dolu veya güvensiz SQLite WAL, `unknown` üretir; toplayıcı WAL'ı yok saymaz,
  checkpoint yapmaz veya SHM oluşturmaz. Gerektiğinde tutarlılığı ayrıca
  kanıtlanmış anlık görüntü alın. Canlı veritabanının bayt özeti, anlamsal veri
  denetimi değildir.
- Başlangıç HTTPS'si ve genel sertifika özetleri; güvenilir üretimi, yenilemenin
  çalışmasını, ACME/DNS doğrulamasını veya panel/Agent kaldırıldıktan sonra
  bağımsız yenilemeyi kanıtlamaz. Zamanlayıcı durumu tek başına yeterli değildir.
- Bağımsız loopback DNS; gerçek ikincil sunucuyu, katalog aktarımını, TSIG'yi,
  eş düğümün kaybı/kurtarılmasını veya dış delegasyonu kanıtlamaz.
- Yerel posta, barındırılan web trafiği, veritabanı uygulamaları, cron ve güvenlik
  duvarının açılış/yenileme davranışı kendi canlı önce/sonra iş yüklerini
  gerektirir. Panel erişiminden veya servis etiketlerinden çıkarılamaz.
- Güncellemeyi başlatmak, değişim öncesinde eski özetleri görmek, bağlantı zaman
  aşımı veya `recovery-required` görmek gerçek geri almayı kanıtlamaz. Adayın
  uygulanmasını ve eşleşen gerçek kurtarma işlemini kaydedin; ardından geri
  yüklenmiş eski çalışmayı, veriyi, DNS'yi ve bağımsız iş yüklerini doğrulayın.

Çevrimdışı kontroller; gerçek no-follow dosya davranışı için Linux'ta çalıştırın:

```sh
python3 -m unittest discover -s deploy/e2e/release-recovery -p 'test_*.py' -v
GOTOOLCHAIN=go1.26.5 go test ./deploy/e2e/release-recovery/driver ./deploy/e2e/release-recovery/driver-update -count=1
```

Bu kontroller iletişimi taklit eder veya geçici yerel dosyalar kullanır. Gerçek
konuk yaşam döngüsünün sonucunu raporlamaz.
