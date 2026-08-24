# İşletim El Kitabı (Runbook)

*Son güncelleme: 28 Temmuz 2026 · [English](OPERATIONS.md)*

Bu belge CelikPanel sürüm dağıtımı ve geri alma işlemlerinin operasyonel tek
doğruluk kaynağıdır. Strateji [ROADMAP](../ROADMAP.tr.md)'te, mimari kararlar
[DECISIONS](DECISIONS.tr.md)'ta, katkı kuralları
[CONVENTIONS](CONVENTIONS.tr.md)'ta bulunur.

---

## 1. Yetki sınırı

Canlı paneldeki her değişikliğin sahibi panel kullanıcısıdır. DNS, nameserver,
DNSSEC, SSL, posta, güvenlik duvarı, servisler, eklentiler, alan adları,
kullanıcılar, veritabanları ve diğer panel ayarlarını kullanıcı CelikPanel
arayüzünden değiştirir.

Dağıtım araçları incelenmiş ve sürümlenmiş CelikPanel artefaktlarını, veritabanı
migration'larını ve CelikPanel'in sahibi olduğu systemd unit'lerini kurabilir
veya geri alabilir. Dağıtımın yan etkisi olarak panel ayar API'lerini çağıramaz,
arayüz işlemi yapamaz, operatörün seçtiği bir servisi kuramaz; canlı DNS, SSL,
posta, güvenlik duvarı veya servis yapılandırmasını yeniden yazamaz. SSH,
burada belgelenen dar kapsamlı ürün güncellemesi, bir kerelik bootstrap ve geri
alma yolları dışında teşhis için yalnız salt-okurdur.

Üretimdeki bir sorun panel değişikliği gerektiriyorsa kesin arayüz adımını
açıklayın ve kullanıcının uygulamasını bekleyin. Aynı işlemi arka planda
tekrarlamayın.

## 2. Değişmeyen dağıtım topolojisi

Dağıtım hedefleri ve zorunlu sıraları şöyledir:

| Sıra | Hedef | Adres | Rol |
|---|---|---|---|
| 1 | `boston.celikhost.com` | `2.25.80.4` | İlk üretim güncellemesi ve doğrulama |
| 2 | `frankfurt.celikhost.com` | `72.62.38.15` | Yalnız Boston geçtikten sonra ikinci güncelleme |

Bu runbook canlı component envanteri, güvenlik duvarı durumu, alan adı sayısı,
sertifika durumu veya kurulu sürüm varsayımı taşımaz. Bu bilgiler eskir; her
dağıtımdan hemen önce hedeften salt-okur yöntemle alınmalıdır. Gözlenen commit,
şema, işletim sistemi, unit durumları ve UTC zamanını sürüm kanıtına kaydedin;
bu runbook'u canlı durum önbelleğine çevirmeyin.

Ürünün değişmeyen yerleşimi:

- binary'ler: `/opt/celikpanel/bin/{agent,panel}`
- web artefaktları: `/opt/celikpanel/web/`
- panel veritabanı: `/var/lib/celikpanel/celikpanel.db`
- unit'ler: `celikpanel-agent` ve `celikpanel-panel`

Agent'ı durdurmak veya temiz biçimde yeniden başlatmak artık paneli durdurmaz.
Panel agent'tan sonra sıralanır, onu zayıf bağımlılık olarak ister ve agent geri
dönerken yeniden dener. Daha sıkı dondurma, güncelleme ve toparlama sırası yine
incelenmiş ürün scriptlerinin sorumluluğundadır; bu akışı doğaçlama SSH
komutlarıyla değiştirmeyin.

## 3. Sürüm kapıları

İki sunucu için tek, temiz ve push edilmiş release commit'ini sabitleyin. Her
dağıtımdan önce tam commit şu kontrolleri geçmelidir:

```bash
make test vet web
```

Make hedefleri incelenmiş Go 1.26.5 dışındaki tüm derleyicileri reddeder,
otomatik Go toolchain indirmesini kapatır ve test ile vet işlemlerini temiz bir
ortamda çalıştırır.

İncelenmiş commit push edildikten sonra sunucudaki checkout'u aşağıdaki tam
fast-forward kanıtıyla hazırlayın. İki yer tutucuyu da değiştirin; onaylanan
commit bir branch, tag veya kısaltılmış hash değil, tam nesne hash'i olmalıdır:

```bash
export CELIKPANEL_APPROVED_COMMIT='<incelenmis-tam-commit-hash>'
export CELIKPANEL_PREPARED_CHECKOUT='/root-sahipli/celikpanel-checkout/yolu'
cd "$CELIKPANEL_PREPARED_CHECKOUT"
[[ "$CELIKPANEL_APPROVED_COMMIT" =~ ^[0-9a-f]{40,64}$ ]]
git switch main
git fetch --prune origin main
test "$(git rev-parse origin/main^{commit})" = "$CELIKPANEL_APPROVED_COMMIT"
git merge --ff-only "$CELIKPANEL_APPROVED_COMMIT"
test "$(git rev-parse HEAD)" = "$CELIKPANEL_APPROVED_COMMIT"
test -z "$(git status --porcelain=v1 --untracked-files=all)"
```

Bu checkout edinimi operatörün release adımıdır. Ayrıcalıklı
`bootstrap-update.sh`/`update.sh` yolu Git fetch, merge veya ilerletme işlemi
yapmaz.

Güvenlik duvarı restore yolu açılış açısından kritiktir. Kurulum, systemd,
güvenlik duvarı kalıcılığı, update, bootstrap veya rollback davranışını
değiştiren sürüm; disposable gerçek sanal makinelerde **Debian 13**, **Ubuntu
24.04** ve güncel **Arch Linux** üzerinde de geçmelidir. Bunlar dağıtım adıyla
yetkilendirme listesi değil, uygulanan paket ekosistemlerinin kabul
fixture'larıdır. Her işletim sisteminde temiz kurulum, ilgili update modu,
rollback ve gerçek reboot için en az şu durumların kanıtını saklayın:

1. kayıtlı güvenlik duvarı snapshot'ı yokken restore unit'i kapalı kalır; reboot
   politika etkinleştirmez veya uygulamaz;
2. açık bir panel işlemiyle snapshot kaydedildiğinde reboot aynı politikayı geri
   yükler;
3. panelden açık **Turn off** sonrasında snapshot yoktur ve restore unit'i
   reboot sonrasında da kapalıdır.

Mock unit testleri gereklidir fakat bu açılış kapısını karşılamaz. SSH
kimlik doğrulamasının, VM erişiminin veya reboot/güvenlik duvarı çıktısının
olmaması dağıtım engelidir; başarı varsayma gerekçesi değildir. Bu kanıtlar
olmadan üretim dağıtımı başlatılamaz.

## 4. Güncelleme modları

### Herkese açık hazır paket yolu (normal kullanıcılar)

Desteklenen etiketli bir sürümde normal kullanıcı Git checkout hazırlamaz ve
sunucuda derleme yapmaz. `https://celikpanel.net/get.sh` dosyasını HTTPS ile
indirip root olarak çalıştırır. Betik, mod seçeneği verilmediğinde temiz sunucu
ile tamamlanmış CelikPanel kurulumunu ayırır. Yarım veya belirsiz yerleşimler,
bilerek dar tutulan tek istisna dışında, fail-closed biçimde durmaya devam eder:
`v0.1.0-alpha.4` panel TLS uyumluluk snapshot kusuruyla kesilmiş güncelleme.
Herkese açık seçeneksiz komut; transaction kaydı kesin alpha.4 hedefi
`8bbbac8b628fae4fca0e127e52c1c7835f56f8b8` ile beklenen sürüm, token,
operasyon ve snapshot metadata'sını gösteriyorsa, bütün dosya türü, sahiplik ve
kip kontrolleri geçiyorsa, çakışan faz marker'ı yoksa ve iki CelikPanel servisi
de durmuşsa kurtarma güncellemesini seçer. Bu eksiksiz parmak izini release
depolama veya indirmeden önce bir kez; arşiv doğrulamasından sonra, updater'ı
başlatmadan hemen önce ikinci kez doğrular. Her uyumsuzluk belirsiz sayılır ve
işlemi durdurur. Operatör özel bir kurtarma seçeneği kullanmaz; bu kurtarma yolu
panel ayarlarını değiştirmez ve onların değerlerine dayanmaz.

Güncellemede dış arşiv sağlaması doğrulanır; bağlantılar ve özel arşiv nesneleri
reddedilir; tam iç `SHA256SUMS` manifesti ile commit/tree kaynağı doğrulanır.
Ardından `/var/backups/celikpanel/releases/` altında yalnız root'a açık,
değişmez bir sürüm yayımlanır ve o sürümün mevcut işlem güvenli
`update.sh --normal` yoluna girilir. Derleyici veya değiştirilebilir Git checkout
kullanılmaz. `--install` ile `--update` rutin seçimler değil, tanılama amaçlı
açık geçersiz kılmalardır.

Aşağıdaki kaynaktan derleme modları sürüm mühendisliği, denetlenen geçişler ve
kurtarma için korunur; normal müşteri güncelleme yöntemi değildir.

### 4.0 Tam Go derleme önbelleği önkoşulu

Kaynaktan derlenen her güncelleme, /opt/celikpanel/.toolchain/go altındaki
mühürlü özel önbelleğin tam Go 1.26.5 olmasını gerektirir. Bunun dışında
güvenilir olan mevcut bir kurulumda daha eski bir Go ağacı varsa aşağıdaki
uygun güncelleme modunu seçmeden önce, aynı temiz ve incelenmiş checkout
içindeki geçiş betiğini çalıştırın:

~~~bash
cd "$CELIKPANEL_PREPARED_CHECKOUT"
test "$(git rev-parse HEAD)" = "$CELIKPANEL_APPROVED_COMMIT"
test -z "$(git status --porcelain=v1 --untracked-files=all)"
sudo /bin/bash ./deploy/migrate-go-toolchain.sh
~~~

Geçiş betiği yol veya sürüm geçersiz kılması kabul etmez. Eski ağacı emekliye
ayırmadan önce sabitlenmiş resmî arşiv SHA-256 değerini ve hazırlanmış ağacın
tamamını doğrular. Eski ağaç operatör incelemesi için korunur; yayın veya son
doğrulama hatası onu geri getirir. Hiçbir servisi, veritabanını, DNS kaydını
veya panel ayarını değiştirmez. Bu komutu güvenilmeyen, eksik ya da daha yeni
bir araç zinciri ağacını onarmak için kullanmayın; bunun yerine durumu
inceleyin.

### 4.1 Normal güncelleme

Normal yolu yalnız kalıcı servis işlem tablosu ile agent'ın özel mutasyon durumu
daha önce doğrulanmış bir sürüm tarafından kurulmuşsa kullanın. Bölüm 3'te
kanıtlanan hazırlanmış checkout'ta:

```bash
cd "$CELIKPANEL_PREPARED_CHECKOUT"
test "$(git rev-parse HEAD)" = "$CELIKPANEL_APPROVED_COMMIT"
test -z "$(git status --porcelain=v1 --untracked-files=all)"
sudo /bin/bash ./bootstrap-update.sh --normal
```

`bootstrap-update.sh` temiz ve incelenmiş commit'i dışa aktarmalı, derleyip
doğrulamalı ve
`/var/backups/celikpanel/releases/<commit>-<nonce>/` altında değişmez, root
sahipli, `0700` kipli tek bir release yayımlamalıdır. Ardından o release'in
`update.sh` dosyasını açık `--normal` moduyla çağırır. Değişebilir checkout
içindeki `update.sh` dosyasını doğrudan çağırmayın. Staged updater; panel
işlemleri, ayrıcalıklı mutasyon ledger'ı, ortak kilit veya host paket yöneticisi
idle değilse fail-closed durmalıdır.

### 4.2 Bir kerelik pre-ledger bootstrap

Kurulu sürümü kalıcı servis işlem ledger'ından eski olan bir sunucu normal
update yolunu kullanamaz. Bu genel bir kurtarma seçeneği değil, bir kerelik
geçiştir.

Bu yolu yalnız sürümde incelenmiş
[`bootstrap-update.sh`](../bootstrap-update.sh) bulunduğunda ve
[`update.sh`](../update.sh) `--bootstrap-pre-ledger` seçeneğini desteklediğinde
kullanın:

```bash
cd "$CELIKPANEL_PREPARED_CHECKOUT"
test "$(git rev-parse HEAD)" = "$CELIKPANEL_APPROVED_COMMIT"
test -z "$(git status --porcelain=v1 --untracked-files=all)"
sudo /bin/bash ./bootstrap-update.sh --bootstrap-pre-ledger
```

Normal modda olduğu gibi bootstrap temiz bir `git archive HEAD` hazırlamalı,
staged panel ve agent'ı derleyip doğrulamalı ve değişmez, yalnız root'a açık bir
release yayımlamalıdır. Şema v20'ye kadar kesintisiz tam migration geçmişini,
servis işlem tablosu ile indexlerinin bulunmadığını kanıtlamalı ve yalnız bu
eski sürüm için izin verilen eksik özel ledger/runtime durumunu kabul etmelidir.
Ardından o release'in `update.sh` dosyasını açık
`--bootstrap-pre-ledger` moduyla çağırır. Kısmi servis işlem nesnelerini, var
olan tutarsız durumu, etkin paket işlemini, kirli kaynağı veya güvenilmeyen
checkout'u reddetmelidir.

Mutasyon ledger'ını asla elle oluşturmayın, değiştirmeyin, boşaltmayın veya
uydurmayın; migration'larını elle çalıştırmayın. Pre-ledger kurulum akışında
ledger'ı yalnız ürünün kontrollü tek seferlik initializer'ı oluşturur; normal
panel veya agent başlangıcı bu initializer'ın yerini alamaz. Başarılı tek
geçişten sonraki bütün sürümlerde `--normal` kullanın.

### 4.3 Bir kerelik exact şema-17 köprüsü

`--bootstrap-schema17` modunu yalnız bilinen son pre-ledger veritabanı biçimi
için kullanın: migration ledger'ı kesintisiz ve tam olarak `1..17` olmalı,
18 ile 22 arasındaki migration'ların hiçbir nesnesi veya kolonu kısmen
bulunmamalı ve özel agent mutasyon ledger'ı olmamalıdır. Yalnız en büyük
migration numarası yeterli kanıt değildir. Bu modu seçmeden önce canlı sistemden
salt-okur kanıt alın. Aşağıdaki sorgu yalnız ilk elemedir; beklenen çıktı
`17|1|17|153` olmalıdır:

```bash
sudo /usr/bin/sqlite3 -readonly /var/lib/celikpanel/celikpanel.db \
  'PRAGMA query_only=ON; SELECT count(*), min(version), max(version), sum(version) FROM schema_migrations;'
sudo test ! -e /var/lib/celikpanel-agent-private/service-mutations.json
```

Sorgu çalıştırılamıyorsa, başka sonuç veriyorsa veya ledger varsa durun.
Uyumluluk varsaymayın ve SQL'i elle çalıştırmayın. Değişmez release içindeki
özel `schema17-bridge`, quiesce active olmadan önce authoritative salt-okur
ledger, nesne, kolon, bütünlük ve foreign-key kanıtını yapar. Bilinmeyen, daha
yeni, aralıklı veya kısmi biçimler veritabanı mutasyonundan önce fail-closed
reddedilir.

Hazırlanmış temiz checkout'tan çalıştırın:

```bash
cd "$CELIKPANEL_PREPARED_CHECKOUT"
test "$(git rev-parse HEAD)" = "$CELIKPANEL_APPROVED_COMMIT"
test -z "$(git status --porcelain=v1 --untracked-files=all)"
sudo /bin/bash ./bootstrap-update.sh --bootstrap-schema17
```

Updater exact şema 17'yi quiesce öncesinde, iki coordinator frozen iken ve
ikisi de durdurulduktan sonra yeniden kanıtlar. Ardından tam bir v4 exact-17
snapshot'ı oluşturup dayanıklı biçimde yayımlar. Özel yardımcı yalnız bu
yayımdan sonra allowlist içindeki 18, 19 ve 20 migration'larını uygulayabilir.
Sonra normal pre-ledger bootstrap agent ledger'ını oluşturur, doğrulanmış
release'i kurar ve kalan migration'ları offline çalıştırır. Başarılı köprüden
sonraki bütün sürümlerde `--normal` kullanılır; iki schema bootstrap modu da
onarım seçeneği değildir.

Mutasyon sonrası herhangi bir adım başarısız olursa updater release transaction'ı
bilerek active, iki coordinator'ı da stopped bırakır. Veritabanını değiştirmeyin,
transaction marker'larını silmeyin, servisleri elle başlatmayın ve kilidi başka
bir sürece devretmeyin. Yalnız başarısız update'in yazdırdığı exact trusted
rollback komutunu çalıştırın. Rollback tam snapshot manifestini doğrular ve
snapshot içindeki `schema17-bridge` ile exact şema 17'yi atomik olarak geri yükler.

### 4.4 Snapshot sözleşmesi

Pre-ledger ve exact-schema17 sürümleri; özel agent durumunu, eski sistemde
ledger'ın bulunmadığı bilgisini ve geçişe özel checker kümesini rollback
sırasında korumak için **snapshot sözleşmesi v4** gerektirir.
Bu sürüm dağıtılmadan önce `update.sh` ve `rollback.sh` birlikte v4 desteğini
bildirmelidir. Snapshot sözleşmesi sürümlerini karıştırmayın, snapshot içeriğini
elle kopyalamayın ve başka sürüme ait rollback scriptini kullanmayın. Root güven
zinciri doğrulaması, checksum'lar, unit durumu, eş
panel/agent/web/veritabanı durumu, saklama ve geri yükleme sırası scriptlerin
sorumluluğundadır.

## 5. İki sunuculu dağıtım ve doğrulama

Önce **Boston**'a dağıtın. Aşağıdaki Boston kontrollerinin tamamı geçmeden
Frankfurt'a dokunmayın:

1. update veya bir kerelik bootstrap başarıyla çıkar ve doğrulanmış snapshot'ın
   mutlak yolunu yazdırır;
2. `celikpanel-agent` ve `celikpanel-panel` active durumdadır;
3. kimliği doğrulanmış `/api/v1/panel/version` yanıtı hem panel hem agent için
   sabitlenen tam commit'i ve beklenen şemayı bildirir;
4. sunulan UI artefaktı bu sürüme aittir;
5. sürüm zaman aralığındaki panel ve agent journal'larında başarısız preflight,
   migration, restart veya reconciliation yoktur;
6. salt-okur servis işlemi ve mutasyon-ledger kontrolleri idle durumdadır.

Yalnız bundan sonra ilgili update modunu **Frankfurt** üzerinde tekrarlayın ve
aynı kontrolleri uygulayın. Herhangi bir kontrol başarısızsa dağıtımı durdurun;
peer'ı denemeden önce güncellenen sunucuyu geri alın.

Yalnız update çıktısında verilen root tarafından güvenilen rollback scriptini
ve `VERIFIED_SNAPSHOT` değerini kullanın. Yer tutucuyu çıktıda verilen kesin
release diziniyle değiştirin; checkout kopyasını veya başka bir release'in
rollback scriptini asla kullanmayın:

```bash
sudo /bin/bash /var/backups/celikpanel/releases/<commit>-<nonce>/rollback.sh "$VERIFIED_SNAPSHOT"
```

O release'in `rollback.sh` dosyası herhangi bir şeyi durdurmadan veya üzerine
yazmadan önce root güven zincirini, desteklenen snapshot sözleşmesini,
provenance bilgisini ve bütün checksum'ları doğrulamalıdır. Rollback sonrasında
bütün salt-okur doğrulamaları tekrarlayın.

## 6. Güvenlik duvarı kalıcılığı yetkisi

Kurulum CelikPanel güvenlik duvarı restore unit'ini yerleştirebilir; fakat
dağıtım ilk kayıtlı politikayı oluşturamaz, güvenlik duvarını açamaz veya
kaydedilmemiş runtime politikasını sessizce açılış politikasına çeviremez.

`install.sh`, açılış bağlantılarının uzlaştırılmasını
`deploy/systemd/enable-firewall-restore-if-saved.sh` dosyasına bırakır. Snapshot
yolu yoksa ve sembolik bağ değilse yardımcı, unit'i başlatmadan veya durdurmadan
hem kalıcı hem runtime etkinleştirme bağlantılarını kaldırır. Var olan snapshot,
sembolik bağ olmayan, boş olmayan normal dosya olmalıdır; yardımcı yalnız bu
durumda unit'i başlatmadan veya güvenlik duvarını uygulamadan mevcut kurulum
topolojisini yeniler. Boş, normal dosya olmayan veya sembolik bağ snapshot ile
disable/reenable hatası, güvenli kalıcılık varmış gibi davranmak yerine kurulumu
durdurur.

İlk dayanıklı snapshot'ı yalnız kullanıcının paneldeki açık **Save for reboot**
işlemi oluşturabilir; restore ancak yazma başarılı olduktan sonra
etkinleşebilir. Arka plan senkronizasyonu var olan snapshot'ı yenileyebilir
ancak ilk etkinleştirme yetkisi vermez. Açık **Turn off** snapshot'ı kaldırır ve
restore'u devre dışı bırakır. GET, rescan, monitoring, update, bootstrap ve
rollback unit'in yalnız var olmasını kullanıcı onayı olarak yorumlayamaz.

## 7. Yetkili DNS motoru yaşam döngüsü

PowerDNS ile BIND, paneldeki özel yetkili-DNS kartından seçilir. 53 numaralı
portun hangi daemon'a ait olduğunu değiştirmek için genel bileşen
başlat/durdur/kaldır eylemlerini veya doğrudan systemd komutlarını kullanmayın.

Yeni veya motor kimliği çözülmemiş bir düğümde önce kesin ortak NS adlarını,
yerel/eş IPv4 adreslerini, **Birincil/İkincil** rolünü ve **Eşli** topolojiyi
hazırlayın. Bu, yalnız veritabanını etkileyen incelenmiş bir adımdır; hiçbir şey
kurmaz veya yeniden başlatmaz. Bu kimlik olmadan ilk motor önizlemesi engellenir
ve kimlik motor etkinleşince değişmez olur. İki rol de BIND veya PowerDNS'i
doğrudan kurabilir; eş kurmak için geçici motor kullanmayın.

Sabit Frankfurt/Boston dağıtımında şu sırayı kullanın:

1. Debian 13 Frankfurt/NS1'i Boston'un sabit eş IPv4 adresiyle birincil olarak
   hazırlayın; ardından BIND'i inceleyip doğrudan etkinleştirin. Boston hazır
   olmadan kesin yönetilen ve çalışır olabilir; fakat eş-bekliyor görünmeli ve
   panelde yerel zone yazılarını henüz kabul etmemelidir.
2. Ubuntu 24.04 Boston/NS2'de PowerDNS'i yönlü ikincil olarak inceleyip doğrudan
   etkinleştirin. Temiz sunucuda işlem hedefi kapalı tutarken kesin paketleri
   kurar, katalog tüketicisini oluşturur ve commit öncesi hedefi kanıtlar.
   İşlem-öncesi kanıt bunun yerine boş ve kesin panel-yönetimli Tek-sunucu
   PowerDNS otoritesi bulursa yeniden yapılandırma yolunu seçer: veritabanı,
   config ve unit durumunu snapshot'lar; tüketiciyi yazar, bir sınırlı restart
   yapar, kanıtlar ve commit eder. Ara adım olarak Boston'a BIND kurmayın veya
   etkinleştirmeyin.
3. Frankfurt durumunu yenileyin. Yayın ancak agent kesin eş kataloğunu ve üye
   serilerini kanıtladığında hazır olur. Zone'ları yalnız bundan sonra oluşturun
   veya değiştirin.

İlk motor eylemi her zaman salt-okunurdur. Sunucudan; kesin işlem türünü, kaynak
ve hedef motorları, durum revizyonunu, topolojiyi, zone ve DNSSEC sayılarını,
beklenen kesintiyi, etkileri ve engelleri içeren bir önizleme alır. Hiçbir şey
kurmaz, başlatmaz, durdurmaz veya yeniden yazmaz. Ayrı **Bu DNS değişikliğini
başlat** eylemi aynı tek-kullanımlık önizleme yetkisini sunmak zorundadır.
Engelli, süresi dolmuş veya bayat önizleme işlem başlatamaz. Canlı motor geçişi
ayrıca açık kesinti onay kutusunu gerektirir; yazdırılan parola cümlesi yoktur.
Eski PowerDNS'i yalnız kayıt amacıyla devralmada kesinti beklenmez; işlem sadece
mevcut durumu kanıtlayıp kaydeder.

PowerDNS↔BIND değişimi hem **Tek sunucu** hem doğrulanmış **Eşli** topolojide
çift yönlüdür. Her düğümün motoru bağımsızdır; dolayısıyla PowerDNS/PowerDNS,
BIND/BIND, BIND/PowerDNS ve PowerDNS/BIND desteklenir. Birincil motor-bağımsız
Catalog Zone v2 yayınlar; ikincil bunu standart AXFR/NOTIFY ile tüketir. İkincil
her zaman yerel yazılara kapalıdır. Aşağıdaki kesin kontrollerin tümü geçene kadar
birincil eş-bekliyor kalır:

- yerel katalog AXFR serisi ve sıralı üyeleri kalıcı katalogla eşleşir;
- eş katalog AXFR'si kaynak olarak yapılandırılmış yerel eş IPv4 adresine
  bağlanır ve birebir aynı seri ile üyeliği döndürür;
- eş, kataloğun bu serideki yetkili SOA'sını hem UDP hem TCP üzerinden döndürür;
  ve
- her üye zone kalıcı beklenen yetkili SOA serisini yerelde ve eşte UDP/TCP
  üzerinden döndürür.

Katalogda üye olmasa bile eş-katalog kontrolü zorunludur; böylece olmayan eş
boşuna doğru sonuç veremez. Başarısız kanıt birincil motoru yönetilmeyen yapmaz
veya durdurmaz; yalnız panelde yerel DNS yayınını kapalı tutar. Silme sonrasında
tamamlama ayrıca silinen zone'un eşteki AXFR'sinin bulunmamasını gerektirir. Bu
aktarım başarılıysa eşte hâlâ bayat kopya vardır; işlem ve PairReady fail-closed
kalır.

Sabit ve özel eş IPv4 adresleri kullanın. Yönetilen BIND genel seçenekleri
normalde `allow-transfer { none; };` içerir. Yönlü birincilin panelce üretilen
katalog/üye zone'ları yalnız kesin yerel `LocalIP` ile kesin eşten AXFR kabul
eder ve yalnız o eşi bilgilendirir; ikincilin tek transfer girdisi kesin birincil
`/32` adresidir. Yönlü PowerDNS birincil exact
`allow-axfr-ips=LocalIP,PeerIP` ile `also-notify=PeerIP` kullanır; ikincil yalnız
eş AXFR'sine izin verir ve `also-notify` içermez. Yayımlanmış eski eşli politika
yalnız dar eski sürüm kanıtında bayt-birebir peer-only-plus-notify olarak kalır.
İlk BIND V3 yayını bunu kurtarılabilir pointer/durum işlemi içinde taşır. Eski
PowerDNS'i örtük biçimde yeniden yazmayın; açıkça incelenmiş switch veya yeniden
yapılandırmaya kadar uyumlulukta tutun.
Eski V2 mutasyonlarına yalnız exact tuple'sız üretici/standalone uyumluluğunda
izin verilir; yönlü receipt'ler ile tuple'sız consumer'lar salt okunurdur ve
incelenmiş V3 switch/yeniden yapılandırma yolunu kullanmalıdır. Yayımlanmış dolu
bir consumer, PowerDNS'ten ancak exact katalog-bağlı üye kümesi ile yerel/eş SOA
serileri kanıtlandıktan sonra ayrılabilir. TSIG
henüz yoktur; paylaşılan veya dinamik NAT uçları desteklenmez.

Katalog serisini motor-bağımsız topoloji durumu olarak ele alın. Birincili BIND
ile PowerDNS arasında değiştirirken değeri birebir koruyun. Üye ekleme, silme
veya yeniden eklemede bir kez ilerletin; yalnız kayıt güncellemesinde
ilerletmeyin. En büyük değerdeki üyelik değişikliği sarılmak yerine mutasyondan
önce reddedilir.

Yayımlanmış `v0.1.0-alpha.27` kaynak receipt'inde katalog-serisi alanı yoktur.
Daha sonraki motor geçişinde bu değeri yalnız kesin kalıcı katalog ile canlı
kaynak backend bağımsız olarak aynı pozitif seri ve üyeliği kanıtlarsa türetin.
Geçişi kabul etmeden önce türetilen seriyi yeni geçiş günlüğüne ve hedef
receipt'ine bağlayın. Taraflardan biri yoksa veya farklıysa durup kanıtı çözün;
seriyi tahmin etmeyin ya da sıfırlamayın.

Herhangi bir DNSSEC zone'u, bekleyen zone yayını, panel dışı DNS, TCP/UDP 53
portu çakışması, bozuk kaynak veya başka bir sunucu/DNS işlemi onayı engeller.
Engelleri kendi açık panel akışlarından giderip yeni önizleme alın. Önce
birincili, sonra ikincili etkinleştirin veya onarın; iki sunucunun motorunu aynı
anda değil sırayla değiştirin. Bir engeli aşmak için eş kimliğini, cluster,
DNSSEC, daemon durumunu veya işlem satırlarını elle değiştirmeyin.

İzin verilen kurulum veya değişim sırasında istenen tam zone kümesi, seçilen
hedef motora ve onun bir sonraki etkinleştirme dönemine bağlanarak dondurulur.
Gerekirse hedef paket başlamama bekçisi altında kurulur, tam zone durumu
hazırlanıp doğrulanır ve mevcut kaynak yalnız son geçiş için durdurulur. Başarı;
hedefin tek yönetilen genel otorite olmasını ve her zone'un beklenen SOA
yanıtını hem UDP hem TCP üzerinden vermesini gerektirir. Bir kaynak motor varsa
paketi kaldırılmaz; geri dönüş için kurulu fakat durmuş bekleme kopyası olarak
kalır.

Kalıcı motor kimliği eski sürümden dolayı çözümlenmemiş bir kurulum, yalnız
mevcut panel-yönetimli PowerDNS otoritesi için **Mevcut kurulumu devral**
seçeneğini sunabilir. Devralma yalnız kayıt amaçlıdır: yönetilen yapılandırmanın
baytlarını ve kiplerini, kesin unit durumunu ve topolojiyi doğrular; SQLite
veritabanını ve panelin sahibi olduğu her zone'u salt-okur sınar; TCP/UDP
otoritesini kanıtlar. Paket kurmaz, yapılandırma veya DNS verisini yeniden
yazmaz, servis başlatmaz ve DNSSEC'i değiştirmez. BIND devralınamaz.

Ayrı Boston yeniden yapılandırmasına ancak bu inceleme boş ve kesin yönetilen
Tek-sunucu PowerDNS durumunu kanıtlarsa izin verilir. Bu sıradan devralma değil,
kalıcı bir aynı-motor işlemidir: önce anlık görüntü alır, kesin yönlü tüketiciyi
yazar, yeniden başlatır, kanıtlar ve sonra tamamlar. Her hatada veritabanı,
config ve unit anlık görüntüsünü geri getirip kanıtlar. Boş olmayan, DNSSEC
taşıyan, sahipsiz, farklı veya eşzamanlı değişen durum fail-closed reddedilir.

Panel defteri ile agent'ın root sahipli host günlüğü aynı işlem kimliğini,
değişmez eş kimliğini, manifesti, işlem-öncesi anlık görüntüyü ve aşamayı taşır.
Yanıt kaybolursa DNS motoru durumunu yenileyin; yeni istek kimliği uydurmayın ve
işlemi SSH üzerinden tekrarlamayın. Başlangıç kurtarması önce kesin hedefi
kanıtlamaya çalışır. Bu kanıt başarısızsa işlem öncesi dosyaları, veritabanını,
nesil işaretçisini ve systemd durumunu geri getirip birebir kanıtlar. İki sonuç
da kanıtlanamazsa ikinci bir otoriteyi başlatmak veya tahmin yürütmek yerine DNS
mutasyonları açık kurtarma için kilitli kalır.

## 8. Geliştirme kontrolleri

- **Derleme:** `make test vet web` (tam Go 1.26.5 kapısı dahil).
- **Release sözleşmeleri:** `bash deploy/test-bootstrap-update-contract.sh` ve
  `bash deploy/test-schema17-bridge-contract.sh`.
- **Görsel doğrulama:** `tools/dev-preview/preview-server.py`, `web/dist`
  dizinini şemaya sadık bir stub arkasında sunar. `FRESH=1` ile
  `FIREWALL=on/off` yalnız geliştirme önizlemesinde kullanılır.
- **i18n:** `web/src/i18n/en.ts` anahtar kaynağıdır; `tr.ts` aynı anahtar
  kümesine birebir sahip olmalıdır.
- **Tasarım döngüsü:** `.design-sync/NOTES.md`, tasarım sistemi iş akışını ve
  zorunlu CSS-entry hash yenilemesini açıklar.

## 9. Gizli bilgiler politikası

Gizli bilgiler repoya, runbook'a, sürüm kanıtına veya sohbette paylaşılan komut
çıktısına girmez. Panel kimlik bilgileri ve SSH anahtarları operatörde kalır.
CelikPanel'in ürettiği servis kimlik bilgileri ürünün korumalı deposunda kalır.
Yeni mühendis erişimini operatör özel bir SSH açık anahtarı ekleyerek verir;
parola paylaşılmaz.
