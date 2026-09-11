# Sunucu kurulumu güvenlik duvarı kabulü — 2026-09-11

*[English](README.md)*

Temiz ve geçici Debian 13, Ubuntu 24.04 ve Arch Linux QEMU imajlarında geçti.
Bütün hedeflerde SSH yönlendirmesi yalnız localhost üzerinden yapıldı. Kurulu
hiçbir panel güncellenmedi; test düzeneklerinde aday ayrıcalıklı agent ile
korumalı Go test programı bulunur, panel uygulama ikilisi bulunmaz.

Test, imzalı ve yalnız teste ait lisansla `runServerSetupFirewall` işlemini,
gerçek kimliği doğrulanmış agent RPC'sini ve ardından panelin normal Kapat
işleyicisini çalıştırır. Sürücü `deploy/test-server-setup-firewall-vm.py`,
korumalı test ise `cmd/panel/server_setup_vm_test.go` içindeki
`TestServerSetupDisposableVMFirewall` testidir.

| Kanıt | Debian 13 | Ubuntu 24.04 | Arch Linux |
|---|---|---|---|
| Kayıtlı görüntü yok: gerçek yeniden başlatma öncesi ve sonrası kapalı | Geçti | Geçti | Geçti |
| İncelenen kurulum güvenlik duvarını açar ve kaydeder | Geçti | Geçti | Geçti |
| Aynı alt işlem isteği ikinci değişiklik olmadan uzlaştırılır | Geçti | Geçti | Geçti |
| Kaydedilen politika gerçek yeniden başlatmadan sonra değişmeden geri yüklenir | Geçti | Geçti | Geçti |
| Kapat işlemi görüntüyü kaldırır ve geri yükleme birimini devre dışı bırakır | Geçti | Geçti | Geçti |
| Sonraki yeniden başlatmada kapalı kalır | Geçti | Geçti | Geçti |
| Her gerçek yeniden başlatmadan sonra SSH yeniden bağlanır | Geçti | Geçti | Geçti |

Her işletim sistemi dizininde başarılı aşama günlükleri, önceki/sonraki açılış
kimlikleri, aday ikili dosyaların SHA-256 özetleri ve kaydedilmiş politikanın
SHA-256 özeti bulunur. Kaydedilmiş tam görüntünün özeti:
`789f4fbf006df6db821614b137c85e73541fea968ab34e0cbf8e1bb30a1d733b`.

Gerçek test ilk yeniden denemede bir hata ortaya çıkardı: otomatik korunan SSH
portları canlı politikada görünüyor ve aynı incelenmiş isteği yanlışlıkla
geçersiz kılıyordu. Yürütücü artık önce tam eşleşen başarılı işlem makbuzunu
uzlaştırır; güncel olarak doğrulanmış SSH portlarını yeni hizmet politikası
gereksinimlerinden ayrı değerlendirir. Başarılı kanıt, bu düzeltme yeniden
derlendikten sonra toplandı.

Arch'ta paket güncellemesi çekirdek modüllerini değiştirdiğinden ayrıca hazırlık
yeniden başlatması gerekti. Mevcut çekirdek hazırlık koruması bu yeniden
başlatmadan önce nftables işlemini doğru biçimde reddetti; hiçbir koruma aşılmadı.

Bu sonuç, güvenlik duvarı ve kurulum alt işlemi kabulüdür. Uçtan uca ACME alma,
bütün amaç profilleri, uzak DNS eşleştirmesi, tam temiz kurulum aracı, kurulu panel
güncellemesi veya sürüm geri alma kabulü anlamına gelmez. Bunların yayımdan önce
ayrı koşulları vardır.
