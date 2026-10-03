# v0.1.0-alpha.81

[English](RELEASE-NOTES-v0.1.0-alpha.81.md)

Bu sürüm v0.1.0-alpha.80 sürümünü izler. DNS motoru değişikliklerinin, eşli DNS
sunucularının ve panel güncellemelerinin kesintiden nasıl kurtulduğunu değiştirir;
neyin ölçüldüğünü, neyin ölçülmediğini de açıkça belirtir.

## Sunucu sahibi için ne değişiyor

**DNS motoru değişiklikleri aynı işlem üzerinde kurtarılır.** İlk BIND ya da
PowerDNS kurulumu, PowerDNS'ten BIND'e geçiş ve mevcut bir PowerDNS'in devralınması
yarıda kesilirse aynı işlem üzerinde tamamlanır veya geri alınır: Agent'ın bir
sonraki başlangıcında ya da durum çıktısının adlandırdığı sahip komutuyla. Agent
çalışırken mevcut bir PowerDNS'in devralınması yalnız bileşen testleriyle
karşılanır. Hizmet veren bir BIND'i PowerDNS'e geçirmenin kurtarma sözleşmesi
yoktur; Panel'de, kurulumda ve Agent'ta reddedilir.

**Eşli iki sunucu üç birleşimde çalışır.** Birincil/ikincil olarak BIND/BIND,
BIND/PowerDNS ve PowerDNS/BIND; kurulumu, bölge eklemeyi, kayıt düzenlemeyi, bölge
silmeyi, yeniden eklemeyi ve Panel ile Agent kapalıyken yeniden açılışı (DNS yanıt
vermeyi sürdürürken) geçer. Eşli yeni bir PowerDNS birincil sunucusu artık,
henüz DNS motoru olmayan bir sunucuda sunuluyor (yalnız Debian 13, ölçülen PowerDNS
sürümüyle). Eşleştirme standart bölge aktarımı ve katalog bölgeleri kullanır; ikincil
sunucunun uzaktaki bir CelikPanel API'sine ihtiyacı yoktur.

**Eşli sunucuda bölge silme ikincil sunucuda kanıtlanır.** Panel, silmeyi
tamamlanmış bildirmeden önce ikincil sunucunun bölgeyi bıraktığını doğrular. Bunun
için iki sunucu arasında bir kez sahip kaydı gerekir (`dns-peer-enroll`, ikisinde de
çalıştırılır). O zamana kadar silme bekler; ekran kimin işlem yapması gerektiğini,
neyin çalıştırılacağını ve işin nasıl süreceğini söyler. Bu, sunucuda üst bölgesi
olmayan bir üst düzey bölgeyi de kapsar.

**Başarısız bir güncelleme geliştirici olmadan geri döner ya da sürer.** Geçiş
yapamayan ya da Panel'ini başlatamayan bir aday tamamlanmadan reddedilir ve önceki
sürüm otomatik olarak geri döner; bu, sıfırlama ya da sonlandırılmış bir kurtarma
işleminden sonra da geçerlidir. Tamamlandıktan sonra başarısız olan aday üç kez
yeniden denenir; ardından kurtarma duraklar, ilk nedeni korur ve nedeni
kaldırdıktan sonra sahibin çalıştıracağı tek bir komut yazdırır. Güncelleme kartı,
root komutu `/usr/libexec/celikpanel/recovery` ve kurtarma kayıtları aynı işlemi
anlatır; Panel çalışırken bu böyledir, durmuşken yalnız root komutu anlatır.

**Barındırılan hizmetler Panel'e bağlı değildir.** Panel ve Agent yeniden açılış
boyunca kapalıyken site, veritabanı, zamanlanmış işler, güvenlik duvarı kuralları ve
(Debian'da) SMTP, ölçülen hücrelerde çalışmayı sürdürdü. Bir güncellemenin tamamlanması üç
otomatik denemeden sonra duraklarsa, güncelleyicinin durdurduğu sertifika yenileme,
siz nedeni kaldırırken önceki durumuna döndürülür; sahibin yeniden denemesi
güncelleme bitene kadar onu yeniden durdurur.

**Kurulum, desteklenmeyeni başlamadan önce söyler.** Arch'ta posta plan
incelemesinde reddedilir. Yerel cron bir kurulum bileşenidir. Web sunucusunun
geçemediği bir barındırma kökü, yol ve çözümle birlikte bildirilir.

**Ubuntu'da kurulum artık kendini engellemez.** Standart bir Ubuntu 24.04 sunucu
imajında, bir paket yardımcı hizmeti (PackageKit) her paket işleminden sonra
yaklaşık beş dakika boyunca boşta çalışır durumda kalır. Önceki sürüm
(v0.1.0-alpha.80) bunu süren bir paket işi sanıp bir sonraki kurulum aşamasını
durduruyordu; testlerimizde kurulumu yaklaşık yedi kez başlatmak gerekti. Boştaki
hizmet artık sayılmaz. Gerçek bir paket işi hâlâ sayılır: kurulum ya da güncelleme
o zaman, sunucunun paket yöneticisinin meşgul olduğunu ve bitmesini beklemek
gerektiğini söyleyen iletiyle durur.

**Posta ayarları, güncelleme bittikten sonra yeniden uygulanır.** Yeni Panel,
güncelleme sunucuyu hâlâ tutarken başlarsa, o anda posta sertifikalarını posta
hizmetlerine yeniden yayımlayamaz ve posta süzgeçlerini yeniden bağlayamaz. Artık
bunu kendiliğinden, başladıktan sonra en çok 10 dakika boyunca her 30 saniyede bir
yeniden dener ve sunucu boşalır boşalmaz işlemi yapar; bir deneme bir şey
yaptığında Panel günlüğüne (`sudo journalctl -u celikpanel-panel`) bir satır
yazar. Genellikle görünür hiçbir şey buna bağlı değildir. Bir sertifika
etkinleştirmesi güncelleme yüzünden yarıda kaldıysa ya da posta süzgeçleri yeniden
bağlanana kadar posta teslimi reddediliyorsa önemlidir. Sunucu 10 dakikanın
tamamında meşgul kalırsa hiçbir şey değişmez ve posta çalışmaya devam eder: diğer
işlem bittikten sonra Paneli `sudo systemctl restart celikpanel-panel` ile yeniden
başlatın ya da alan adının SSL sayfasında "Etkinleştirmeyi yeniden dene"yi
kullanın.

**Müşteri arşivi test malzemesi taşımaz.** Sürüm arşivi artık kabul düzeneğini, test
betiklerini ya da saklanan kanıtı içermez; imzalama adımı içeren bir arşivi
reddeder.

## Bu sürümün sınırları

Bunlar bilinen ve bilinçli sınırlardır. Gizli kusur değildir.

- **Ölçülen platformlar:** Debian 13 ve Arch. Ubuntu 24.04'te yalnız üç şey
  denendi: v0.1.0-alpha.80'den güncelleme (bu adayın daha eski derlemeleriyle), ilk
  kurulum ve güncelleme başlatma. RHEL ailesi engelli önizleme olarak kalır.
- **Arch'ta posta** desteklenmez.
- Hizmet veren bir sunucuda **BIND'ten PowerDNS'e** geçiş reddedilir. Daha eski bir
  sürümün yapılandırdığı BIND ikincil sunucusu, yeni ikincil yapılandırmaya otomatik
  olarak yükseltilmez.
- **v0.1.0-alpha.80'den ilk güncelleme.** Bu yol Debian 13 ve Ubuntu 24.04'te,
  alpha.80 kaynağından bir deneme lisansıyla yeniden derlenerek ölçüldü; imzalı
  arşivden değil. Arch ölçülmedi. Bu aday başarısız olur ve sunucu otomatik olarak
  alpha.80'e dönerse alpha.80 Panel'i bunu anlatamaz: ham bir hata satırı gösterir
  ve aynı sürümü yeniden sunar. Durum o zaman SSH üzerinden
  `sudo /usr/libexec/celikpanel/recovery` ile okunur. Güncellemenin ilk
  saniyelerinde bu komut henüz yoktur.
- **Gerçek bir paket işi yüzünden duran kurulum aşaması kendiliğinden devam
  etmez.** İşin bitmesini bekleyin, planı gözden geçirip kurulumu yeniden başlatın;
  zaten kurulu hizmetler yeniden kurulmaz, durmuş olan aşama yeniden çalışır.
- **v0.1.0-alpha.80 çalıştıran bir Ubuntu sunucusunu güncelleme.** Güncelleme kartı,
  boştaki paket yardımcı hizmeti çalışırken "Sunucu değişiklikleri geçici olarak
  kullanılamıyor." gösterebilir. Yaklaşık beş dakika bekleyin, yeniden denetleyin,
  sonra güncellemeyi başlatın. Kart bunu gösterirken sunucuda hiçbir şey
  değişmez. Bu, alpha.80 kodundan okundu; bir testte yaşanmadı.
- **Tamamlandıktan sonra geri alma yok.** Yeni Panel başlayıp sonradan başarısız
  olursa güncelleme geri alınmaz, bitirilir.
- **Panel durmuşken** adresinde canlı kurtarma durumu görünmez; sahip bunu SSH
  üzerinden kurtarma komutuyla okur.
- **Duraklayan bir geri alma, sahip işlem yapana kadar sertifika yenilemeyi
  duraklatılmış tutar.**
- **Panel kaldırma yolu sunulmaz** ve CelikPanel'i kaldırmaya dair hiçbir iddia
  yoktur. Yalnız "yönetim kapalı" ölçüldü.
- **Kanıt kapsamı:** tek bir dizüstü ana makinedeki geçici sanal makineler, bir
  deneme imzalama anahtarı, bir loopback sürüm kaynağı ve yalnız test için lisans;
  her güncelleme durumu bu adayın kesin kodunda bir kez koşuldu (Debian 13,
  Arch, Ubuntu 24.04; "Panel başlıyor ama sonra başarısız oluyor" durumu yalnız
  Debian'da; bir durum bir düzenek hatasından sonra ikinci bir koşu gerektirdi);
  posta yeniden uygulamasına Debian ve Ubuntu'da 11 Panel'de ulaşıldı (duraklayan
  durumda, yönetimin kapalı olduğu durumlarda ve alpha.80'e dönüşten sonra
  ulaşılmadı), ancak yalnız ilk denemesine ulaşıldı ve posta sertifikası yoktu; güç kaybı denemesi
  yok; güncelleme koşularında hiçbir tarayıcı yer almadı. Üretim imzalama yolu, gerçek sürüm
  kaynağı ve lisans hizmeti bu koşularda denenmedi.
- **Denenmemiş iletiler:** dört güvenlik iletisi hiçbir deneme koşusunda ortaya
  çıkmadı; bu yüzden ekrandaki ifadeleri denenmedi: güncellemeden önce Panel'in meşgul
  olup olmadığının kısa ikinci denetimi, "güncelleme denetimi reddedildi" iletisi,
  başarısız bir güncelleme öncesi anlık görüntünün nedeni ve servis başlatma sınırı
  iletisi.
- **Yanıltıcı bir günlük satırı:** `postfix-lmdb` paketi olmayan Debian 13'te Panel
  posta sertifikalarını her yeniden yayımladığında Postfix "fatal: unsupported map
  type: lmdb" yazar. Adım yine de tamamlanır ve posta hash harita tipiyle
  çalışmaya devam eder; satır gürültüdür.
- **Arayüz borçları:** Bileşenler sayfası katalog adlarını İngilizce gösterir; cron
  Kaldır düğmesi gösterilir ve onaydan sonra reddeder; değişen ekranların görsel
  tarayıcı incelemesi bekliyor.
- **Açık kabul işleri:** dayanıklılık denetim listesi kısmen açık kalır. Bkz.
  [dayanıklılık sözleşmesi](RESILIENCE-CONTRACT.tr.md) ve
  [DNS kurtarma kayıt defteri](DNS-RECOVERY-ACCEPTANCE.tr.md).

## Yayımlamadan önce (sahip kararları ve kalan denetimler)

1. Sürüm: v0.1.0-alpha.81 (sahibin kararı).
2. root gerektiren paketleme sözleşmesi testleri (18 betik), bakımcı tarafından
   geçici bir makinede çalıştırılıyor; derleme ana makinesinde root olarak
   çalıştırılmadılar. Sonuç: bekleniyor (pending).
3. Üretim imzalaması, [imzalı sürüm sözleşmesinde](release-signing.tr.md)
   anlatıldığı gibi, sürüm etiketinde CI içinde yapılır; imzalama anahtarı CI
   dışında kullanılmaz. Ardından sahip, yayımlanan altı dosyayı o belgede
   anlatıldığı gibi doğrular; imzalı arşivin v0.1.0-alpha.80 ile kurtarma
   uyumluluğunu da doğrular.
4. Satıcı yayımlama araçları (indirme portalı ve üyelik betikleri) bu sürümün
   müşteri arşivinde kalır (sahibin kararı, 2026-10-03).
5. Kurulu herhangi bir panel güncellenmeden önce geçici bir sunucuda sahip denemesi
   yapılır.

Bu sürümü yalnız CelikPanel'in güncelleme arayüzünden kurun. Yayımlamak kurulu
sunucuları güncellemez.
