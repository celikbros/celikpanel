# v0.1.0-alpha.80

[English](RELEASE-NOTES-v0.1.0-alpha.80.md)

Normal BIND alan yayımı, motorun edinim kaydını koruyarak güncel nesli değiştirir. Güncelleme ve kurulumun son doğrulaması bu geçerli farkı sahiplik çakışması sayıyordu. Artık güncel yayım durumu korunup etkin nesil ve çalışma yapılandırması doğrulanıyor; edinim kimliği, DNS çiftinin yetkisi ve katalog ilerleme kontrolleri sürüyor. Farkları gizlemek için sahiplik kayıtları yeniden yazılmıyor.

İmzalı güncelleyici kararlı BIND durumunu servis başlangıç engeli yayımlanmadan kontrol ediyor; koordinatörleri askıya almadan önce mutasyon kilidi altında yeniden doğruluyor. Mevcut kurtarma ve geçiş yollarının son kontrolleri korunuyor. Bu erken kontrol önlenebilir panel kesintilerini azaltır; kurulumun sonraki her hatasının önceden bilinebileceği anlamına gelmez.

Otomatik geri alma artık Linux kilit alanlarını değişken boşluklarla ayrıştırıyor. Önceden geçerli devralınmış özel kilit reddedilip kurtarma beklemede kalabiliyordu. Descriptor kimliği, özel sahiplik ve bağımsız kilit dışlama kontrolleri korunuyor.

Regresyonlar gerçek BIND kayıt oluşturma ve düzenlemesini, güncel ağaç bozulmasını ve sahiplik değişimlerini, erken shell hata yolunu, güncelleme ve geri alma kilit girişlerini kapsıyor. Orijinal Alpha79 kodu bildirilen BIND ve kilit örneklerinde hata veriyor. Bunlar yerel Linux testleridir; iki sunucuda tam güncelleme veya barındırılan hizmet denetimi değildir.

Kullanıcı Frankfurt'u doğrulanmış güncelleme öncesi yedeğiyle geri yükledi; bağımsız HTTPS kontrolü panel erişimini doğruladı. [Olay kaydı](UPDATE-TLS-RECOVERY-INCIDENT.tr.md). Bu sürüm panelin güncelleme arayüzünden kurulur. Yayımlamak kurulu sunucuları güncellemez ve dış sağlayıcıdaki mail PTR gereksinimlerini çözmez.
