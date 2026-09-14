# v0.1.0-alpha.79

[English](RELEASE-NOTES-v0.1.0-alpha.79.md)

Panel güncellemeleri, yönetilen TLS sürüm dizinlerinde tutulan sertifika üretim kaydını artık korur. Alpha78, yenileme sonrasında eski sertifika dizininde de bulunabilen bu geçerli dördüncü dosyayı reddediyor; panel ve agent durdurulduktan sonra yedek alma aşamasında güncelleme durabiliyordu.

Yedek doğrulayıcı yalnızca belirtilen addaki, boş olmayan, root:root sahipliğinde, 0600 izinli, tek bağlantılı ve en fazla 1024 baytlık kaydı kabul eder. Yedek manifesti ve geri yükleme, dosyanın baytlarını ve metaverisini aynen korur. Bilinmeyen dosyalar ve güvenli olmayan kayıtlar reddedilmeye devam eder.

Doğrulama; gerçek Go sertifika üretim ve yenileme kodunu, ardından shell yedekleme ve geri yükleme yolunu, güvenli olmayan kayıt örneklerini, kurtarma işlem testlerini ve Frankfurt olayına özel kullanıcı tarafından çalıştırılan kurtarma aracını kapsar. Eski doğrulayıcı gerçek üreticiyle yapılan regresyon testinde başarısız olur. Bu kontroller, iki sunucunun tüm güncelleme akışının veya barındırılan hizmetlerin denetimi değildir.

Frankfurt bu sürümden önce mevcut Alpha75 kurulumuna kurtarıldı; kurtarma sırasında güncelleme kurulmadı. Olaya özel araç genel kurtarma komutu olarak tekrar kullanılmamalıdır. [Olay kanıtları](UPDATE-TLS-RECOVERY-INCIDENT.tr.md).

Güncellemeyi CelikPanel güncelleme arayüzünden başlatın. Bu sürüm dış sağlayıcıdaki mail PTR gereksinimlerini çözmez ve kurulum planlarını sıfırlamaz.
