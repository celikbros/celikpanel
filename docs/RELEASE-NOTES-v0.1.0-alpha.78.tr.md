# CelikPanel v0.1.0-alpha.78

[English](RELEASE-NOTES-v0.1.0-alpha.78.md)

Güncelleme, başlangıç sertifikası çifti bulunsa da bulunmasa da sıkı doğrulamadan
geçen yönetilen panel sertifikası ağacını kabul eder. Önceden bu desteklenen düzen,
panel ve agent durdurulduktan sonra reddediliyordu. Sertifika içeriği, sahiplik,
izinler ve bağlantılar korunur; güvensiz düzenler güncellemeyi engellemeye devam eder.

Otomatik kurtarma girişinde Linux kilit bilgisi alanlarına göre okunur.
Sütunlar arasındaki değişken boşluklar geçerli devralınmış özel kilidi artık
reddettirmez. Dosya tanıtıcısı sahipliği, inode kimliği ve bağımsız kilit denetimi sürer.

Frankfurt olayına özel kurtarma aracı ve testleri denetim için saklanır. Genel
onarım komutu değildir; kurtarılan sunucuda tekrar çalıştırılmamalıdır. Başlangıç
kontrolü tam çalıştırılabilir dosyayı ve kararlı servis PID değerini bekler.

Doğrulama; atomik ve karma TLS düzenlerini, değiştirmeden reddetmeyi, altı gerçek
Linux kilit durumunu ve on sekiz kurtarma/başlangıç senaryosunu kapsar. Mevcut
bootstrap güncelleme ve kurtarma sözleşmeleri de denetlenir. Bunlar tüm kurulu
sunucu yapılandırmalarının sınandığı anlamına gelmez.

Kurulu panel güncellemesini sunucu sahibi panelin güncelleme ekranından başlatır.
Kanıt ve sınırlar için [olay kaydına](UPDATE-TLS-RECOVERY-INCIDENT.tr.md) bakın.
