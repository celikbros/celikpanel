# Kurtarma çalışma ortamının yükseltilmesi

*14 Eylül 2026 · [English](RECOVERY-RUNTIME-PROMOTION.md) · D-025 / P0.3*

**Kaynak uygulaması ve kabul çalışması sürüyor; yayımlanmış veya kurulu bir
yetenek değildir.** Bu dilim, daha önce seçilmiş bağımsız kurtarma kitinin
değiştirilmesini ele alır. Dayanıklılık sözleşmesinin tamamını kapatmaz.

## Neden ayrı bir geçiş gerekiyor?

Önceki kayıt işlemi, mevcut seçili kiti koruyordu. Böylece eski yürütücü
korunuyor, fakat güncellemenin gerektirdiği yeni kurtarma verisi okuyucusu
seçilemiyordu. Başlatıcı ile seçiciyi birbirinden bağımsız yazmak, kurtarmanın
kendi içinde ikinci bir kesinti aralığı oluştururdu.

Geçiş yalnız kabul edilmiş güncellemenin ön kontrolünde, FD 9 üzerinden yerel
sürüm kilidi tutulurken ve active, quiesce, completion veya scheduler işlem
işaretçisi yokken yapılır. Yeni kit bütünüyle saklanır, mevcut kurulum sabit
çevrimdışı okuyucularla denetlenir ve önceki kit korunur. Kit geçişi için panel
veya Agent servisi durdurulmaz. Okunamayan seçim, seçim yokmuş gibi yorumlanmaz.

Çalışma ortamı manifesti mevcut on iki dosyasıyla protokol 1 / snapshot 6 olarak
kalır. Geçişin ayrı sürümlü kanıtı vardır; eski yedek veya kurtarma verisi
kayıtlarına yeni anlam yüklemez. Uyumluluk denetimi mevcut durumun okunabildiğini
kanıtlar; ileride yapılacak geri yüklemenin başarısını kanıtlamaz.

## Kesinti davranışı

Amaçlanan kalıcı durumlar doğrulanmış eski başlatıcı ve seçim, yeni başlatıcı
ile eski seçim ve bütünüyle yeni çifttir. Yeni başlatıcı, bekleyen geçişi
doğrulayıp seçili kitin kurtarma programını sabitlenmiş dosya tanıtıcısından
çalıştırabilir. Eski program kendi seçili yürütücü doğrulamasını böylece korur.
Eski klasörün saklanması tek başına bu davranışı kanıtlamaz.

Başlatıcı yayımlanmadan kesinti olursa eski giriş kullanılabilir kalır. Eski
program geçiş kanıtını tanımadığından bu erken aşamanın otomatik tamamlanması
iddia edilmez. Başlatıcı yayımlandıktan sonra yeni saklanmış giriş, aday arşivin
kodunu çalıştırmadan aynı geçişi sürdürebilir. Uyumluluğu, kilidi ve işlem
kanıtlarını yeniden denetler; önceki başarılı kontrol sonraki değişikliğe tek
başına yetki vermez.

`recovery runtime-status [--json] [--lang en|tr]`, seçim değiştirmeden doğrulanmış
kit geçiş aşamasını veya kanıtın doğrulanamadığını bildirir. Türkçe/İngilizce
yönlendirme, erken aşamada aynı sürümü incelemek ile başlatıcı yayımlandıktan
sonra sahibin kurtarma komutunu çalıştırmasını ayırır. Değişmemiş eski başlatıcı
bu yeni komutu kendiliğinden kazanmaz.
Durum ve sürüm sorguları salt okunur kalır ve seçili kitin doğrulanmasından
bağımsızdır. Veri ve yetenek okumaları, seçim değiştirmeden doğrulanmış seçili
okuyucuya aktarılır. Bekleyen geçişi yalnız dar kurtarma veya kabul edilmiş ön
kontrol yolu sürdürebilir. Kabul edilmiş güncelleyici yine FD 9 üzerinden
doğrulanmış yerel kilidi almalıdır. Sahip kurtarması aynı kanonik kilit için
gerçekte aldığı tanıtıcıyı kullanır; Go, FD 9'u kendi olay izleyicisine ayırmış
olabilir. İlgisiz tanıtıcı korunur ve yetki sağlamaz; meşgul yerel kilit kurtarmayı
engeller. Miras FD 9 yalnız tam kilit kimliği ve özel sahipliği kanıtlanırsa
yeniden kullanılır. Sahibin değişiklikleri, yabancı nesneler, güvensiz
metadata ve açıklanamayan kanıt kaybı korunur ve ilgili işlem reddedilir.
Değiştirilecek girişlerde genişletilmiş öznitelikler desteklenmez; silinmek yerine
reddedilir. Geçiş tamamlandığında güncel kurtarma, doğrulanmış yeni çift ve hedef
kite dayanır. Önceki kit dosyaları korunan kanıttır; yeni çalışma ortamının ek
bağımlılığı değildir. Sonraki arşiv geçişinde bunların kanıtı yine gerekir.

Hazırlığı doğrulanamayan işlem, panel güncellemesi henüz başlamasa bile mevcut
hata iletisinde `recovery_required` olarak bildirilir; kurtarma dosyalarının
tamamının değişmeden kaldığı iddia edilmez. Kit geçişinin tamamlanması, panel
güncellemesinin veya güncel servis sağlığının başarılı olduğunu kanıtlamaz.
Doğrulanmış kit hazırlığından sonraki uygulama ön kontrolü hâlâ `state=unchanged`
bildirebilir: bu, uygulama dosyalarını anlatır; tamamlanmış kit geçişinin geri
alındığını söylemez. Kit ayrıca `runtime-status` üzerinden incelenir.

Değişmemiş eski kayıt programları sabit başlatıcının seçili programla aynı özete
sahip olmasını bekler. Ara durumdaki başlatıcıyı, normal servisler durmadan
reddedebilirler. Bu açık bir başlangıç uyumluluğu sınırıdır; eski programların
geçişi desteklediği anlamına gelmez.

## Bu dilimin kabul koşulları

Yerel testler tam kilit aktarımını, kapalı argüman kümesini, eski/yeni nesne
kimliğini, yayın sınırlarındaki gerçek süreç kesintisini, yeniden çalıştırmayı,
uyumsuzluğu ve sahip değişikliğinin reddini kapsamalıdır. Taklit denetleyicinin
başarılı çıkışı, bütün sunucunun kurtarıldığını kanıtlamaz.

Gerçek sistem kabulü, kayıtlı ve geçici Arch ile Debian 13 misafirlerinde gerçekten
kaydedilmiş önceki kit ile başlamalıdır. Test düzeneği seçici kaydı üretmek
yerine önceki programın gerçek kayıt komutunu çağırmalıdır. Ara durumda salt-okur veri yeteneği girişinden seçili yürütücünün
doğrulanmasını ve geçişten sonra gerçek başarısız güncellemenin geri
alınmasını sınamalıdır. Önceki kitin kaydı, daha önce bütünüyle başarılı bir
sürüm güncellemesi yapılmasından ayrı bir test koşuludur.

Tam kaynak commit'leri, hata sınırları, başarısız veya kaçırılmış enjeksiyonlar
ve korunan kanıt özetleri kaydedilir. Bu sonuçlar oluşana kadar belge gerçek
sistem geçiş kabulünün tamamlandığını iddia etmez. Tam aşama matrisi, yeni işlem
kimliğiyle tarihsel geri alma, metadata geçişleri, bağımsız servis yenileme ve
açılış davranışı ile temizleme ayrı açık işlerdir. Kurulu panel güncellemelerini
kullanıcı CelikPanel'in kendi arayüzünden başlatır.

Doğrulanmış kit hazırlığından sonraki uygulama ön kontrolü `state=unchanged`
bildirebilir. Bu, uygulama yükünün değişmediğini söyler; tamamlanmış kit geçişinin
geri alındığı anlamına gelmez. Kit durumu ayrıca `runtime-status` ile incelenir.
