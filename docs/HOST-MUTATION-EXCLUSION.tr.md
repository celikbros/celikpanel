# Ortak sunucu değişiklik kilidi

*22 Eylül 2026 - D-025 ilkeleri 1-4; P0.3/P0.4/P0.5 altyapısı*

`internal/hostmutationlock`, gerçek Agent ve ayrı kurtarma denetleyicisinin
salt-okur kilit edinme, boşta olma sorgusu ve devralınan kilit kanıtını paylaştırır.
`/run/celikpanel/service-mutation.lock`, mevcut sayısal UID/GID ve kilit sırası
korunur. Dış sunucu kilidi, sertifika veya işlem kaydı yayın kilidinden önce alınır.

Okuyucu openat2 ile bağlantı içermeyen üst dizini sabitler; O_NONBLOCK sayesinde
FIFO ile değiştirilmiş dosyada takılmaz. Boş, tek bağlantılı, 0600 dosyanın
sahibini, dizin ve dosya kimliğini kilit alındıktan sonra da denetler. Sahip
müdahalesi dosyayı veya izinleri düzelterek gizlenmez; kanıt korunarak reddedilir.
Sistem çağrısı kullanılamazsa daha zayıf yola dönülmez. Eski özel sembolik
bağlantı yolları sahip incelemesi gerektirir; standart /run yolunda geçiş yoktur.

Devralınan tanıtıcı, aynı açık dosya nesnesinin özel flock kilidini zaten
elinde tutmalı ve başka açıcıyı engellemelidir. Denetim kilitsiz tanıtıcıyı
kilitlemez ve çağıranın kilidini bırakmaz. Edinmede eksik dosya hatadır;
boşta sorgusunda güvenilir mevcut dizindeki eksik dosyanın eski anlamı korunur.
Bu sonuçlar işlem yetkisi, sağlık, paket yöneticisinin boşta olması veya
belirsiz işlemi yeniden başlatma izni değildir. Çağıranın mevcut denetimleri sürer.

Şema veya kalıcı sahiplik değişmedi. Agent'ın oluşturucusu ve sınırlı ilk
oluşum kurtarması aynıdır. Bağımsız yenileme, kabul edilmiş sayısal kimliği ve
geçici dizinin açılışta hazırlanmasını ayrı sözleşmeyle tamamlamalıdır.
Bu değişiklik yeni yenileme kancası etkinleştirmez. Kilitleri yok sayan root
sahibin son denetim sonrası değişikliğine karşı koşulsuz atomiklik iddiası yoktur.

Yerel Linux dosya sisteminde race testleri iki süreç, SIGKILL sonrası yeniden
edinme, gerçek Agent oluşturucusuyla karşılıklı dışlama, devralınan kilit,
FIFO, eksik ve değiştirilmiş kanıt durumlarını geçti. Tüm Agent race testleri, vet
ve üretim kaynaklarından ayrı derlenen kurtarma denetleyicisi de geçti. Bu,
yeni VM güncelleme/geri alma veya yönetimsiz yenileme kanıtı değildir;
P0.3-P0.5 kısmi kalır. Kurulu kullanıcı paneli değiştirilmedi.
