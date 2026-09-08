# İlk kurulum kurtarması

[English](FIRST-INSTALL-RECOVERY.md)

İlk kurulumda SSH veya sağlayıcının tarayıcı terminali koparsa yeniden bağlanıp
aynı genel kurulum komutunu çalıştırın. Bağlantı kaybı işlemi hâlâ kesebilir.
Kurucu artık doğrulanmış sürüm bilgisini saklar; tekrar çalıştırma özgün sürümü
güvenle tamamlayabilir.

Devam kaydı `/var/lib/celikpanel-release-state/install.pending` dizinindedir.
Dizin root sahipli ve 0700 izinlidir. Yalnız imzalı manifesti, imzayı ve sabit
açık anahtarı içerir; her biri 0600 izinli, tek bağlantılı normal dosyadır.
Yönetici parolası bu kayda kopyalanmaz. Kayıt, kalıcı güncelleme kilidi eldeyken,
arşiv indirilmeden ve kurulum değişiklikleri başlamadan önce atomik yayımlanıp
diske eşitlenir.

Devam sırasında imza, manifestin tam biçimi, platform, sürüm tabanı, izinler ve
dizin kimliği doğrulanır. Aynı değişmez arşiv yeniden indirilip doğrulanır ve
kurucunun tekrarlanabilir adımları yeniden uygulanır. Bir ilerleme sayacına göre
rastgele adım atlanmaz. Veritabanı içeriği, mevcut yapılandırma ve önceden
oluşturulmuş yönetici korunur.

Etkin kurulum kalıcı kilidi tutar ve ikinci komutu reddeder. Çalışan yarım
servisler üzerinde devam etmeden önce agent'ın ortak değişiklik kilidi alınır,
kalıcı işlem kaydı denetlenir, panel durdurulur ve panelin işlem kuyruğu
denetlenir; ardından agent durdurulur. Meşgul veya tutarsız durum devamı engeller.
Panel durdurulduktan sonra bekleyen işlem bulunursa yarım panel durmuş kalabilir;
yeniden denemeden önce işlemin sonuçlanması gerekir.

Kurucu başarılı döndüğünde boş, root sahipli tamamlanma işareti doğrulanır ve
devam kaydı kaldırılır. Tamamlanmış yerleşimde varsayılan komut yeniden kurulum
veya güncelleme yapmadan tamamlanmayı bildirir. Güncellemeler panelin kimliği
doğrulanmış güncelleme akışından yapılır.

Alpha55 devam kayıtlarından önce yayımlandı. Bu sürümün dar uyumluluk yolu
yalnız özgün imzalı Alpha55 sürümünü ve sürüm tabanını, baytları birebir eşleşen
kurulu panel ve agent'ı, çalışmayan paneli, sıfır giriş yapabilen yöneticiyi ve
başlangıç agent işlem kaydını kabul eder. Bilinmeyen veya çelişkili kısmi
yerleşimler reddedilir. Veritabanı içeriği korunur; sıfır yönetici bulunması tek
başına veritabanının boş olduğunun kanıtı sayılmaz.

8 Eylül Frankfurt SSH günlüğünde sağlayıcı terminal istemcisinden gelen
`Disconnected by application` kaydı vardı. Bu, oturumu hangi tarafın kapattığını
gösterir; istemcinin neden bağlantıyı kapattığını açıklamaz. Sunucu açık kalmış,
agent başlamış, yönetici oluşturma ve panel başlatma tamamlanmamıştı. Düzeltme
yarım kurulumun devamını sağlar; dış tarayıcı terminalinin bağlantısının hiç
kopmayacağını iddia etmez.
