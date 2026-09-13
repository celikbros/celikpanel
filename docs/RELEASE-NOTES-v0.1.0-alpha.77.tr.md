# CelikPanel v0.1.0-alpha.77

[English](RELEASE-NOTES-v0.1.0-alpha.77.md)

Güncellemenin ilk koordinatör kontrolü, eşleşmeyen ilk süreç listesinde hemen
hata vermek yerine geçici yardımcı süreçlerin çıkmasını kısa süre bekler. Her
okumada ilk servis durumu, PID ve süreç başlangıç zamanı korunmalıdır. Kalıcı
yardımcılar, okunamayan liste ve değişmiş kimlik güncellemeyi quiesce yayımından
önce durdurur. Sonraki donmuş süreç ve mutasyon kontrolleri katı kalır.

Kayıt hatalarında beklenen ve görülen süreç kimlikleri komut argümanları olmadan
saklanır. Bu, Frankfurt'taki alpha.76 güncelleme hatasının tanısını iyileştirir;
ilk günlük hatanın kesin nedenini göstermemiştir.

Doğrulama: on üç yalıtılmış kayıt/eşleştirici senaryosu ve mevcut bootstrap-update
sözleşmesi geçti. [Davranış ve sınırlar](UPDATE-QUIESCE-CAPTURE.tr.md).

Kurulu panelleri sahipleri panelin güncelleme arayüzünden günceller. Bu sürüm
kurulu sunucuda güncelleme yapmaz veya DNS/PTR verisini değiştirmez.
