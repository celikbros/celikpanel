# Posta sunucusu sertifikası doğrulaması — 2026-09-11

*[English](README.md)*

Bu kanıtlar, `web_mail` kurulum profilinin kullandığı ayrı posta sunucusu
sertifikasının yaşam döngüsünü kapsar. Profilin tamamının veya genel ACME
sertifika alımının kabul testi olarak değerlendirilmez.

## Geçici Debian test ortamı

Test, localhost SSH 2241 üzerinden erişilen yalıtılmış Debian 13 QEMU ortamında
çalıştı. Test ortamı işaretini zorunlu tuttu; panel ikili dosyası varsa çalışmayı
reddetti. Kurulu müşteri panelleri güncellenmedi. Gerçek Postfix ve Dovecot,
sistem güven deposuna eklenen geçici test CA'sı ve yalnızca
`mail.setup.celikpanel.test` adı kullanıldı. Doğrulama bitince VM kapatıldı.

Test; üretim kodundaki korumalı Certbot kaynak okuyucusunu, değişmez sertifika
nesillerini, kalıcı işlem kaydını, servis yapılandırma/yüklemesini ve kuyruklu
yenileme aktarımını kullandı. Gerçek SMTP submission STARTTLS 587 ve IMAPS 993
bağlantılarında sistem güveni, sunucu adı ve sunulan yaprak sertifikanın tam
özeti denetlendi. Posta gönderilmedi, posta kutusuyla oturum açılmadı.

- [İlk yayın](initial-publication-before-queue-fix.log), ilk güvenilir
  sertifikanın iki dinleyicide de sunulduğunu doğruladı; ardından yenileme
  kuyruğundaki sahiplik uyuşmazlığını ortaya çıkardı. Başarısız sonuç bilerek
  korunmuştur.
- Kuyruğu yazma ve kaldırma işlemleri artık korumalı okuyucuyla aynı servis
  grubu sahiplik sözleşmesini kullanır. Regresyon testi sıfırdan farklı servis
  GID değeriyle çalışır.
- [Düzeltme sonrası yenileme](debian13-renewal.log), gerçek servisleri ikinci
  sertifikaya geçirdi; yedek sertifika baytlarını korudu, kuyruğu temizledi ve
  aynı deploy-hook kaynağının tekrarını sertifikayı yeniden değiştirmeden
  tamamladı. Panel veya lisans yöneticisi bu işleme katılmadı.
- [Son okuma](debian13-receipt.log), tamamlanan işlemin kalıcı kaydını korumalı
  mevcut sertifika kaydı ve gerçek dinleyici sertifikasıyla eşleştirdi.
  Yenileme isteği: `404c857313331b277931e62abda1fcce`.

İlk ve yenilenen yaprak sertifikaların SHA-256 özetleri yenileme günlüğündedir.
Düzeltmeden sonra ilk test sıfırdan bir VM üzerinde bütünüyle tekrarlanmadı;
aynı ortamda bekleyen ikinci nesil tamamlandı ve ayrıca son işlem kaydı
denetlendi. Son test ikilisinin özeti:
[acceptance-binary.sha256](acceptance-binary.sha256).

## Başarısız yapılandırma regresyonu

Başarısız bir posta TLS niyet kaydı, eskiden gözetimsiz yenilemede son başarılı
sunucu adı/SNI yapılandırması sanılabiliyordu. Artık normal çalışmada veya
yeniden başlatma kurtarmasında başarıyla doğrulanan yapılandırma ayrı, korumalı
bir kayıt olarak saklanır. Başarısız öneriler bu kaydı değiştiremez; sertifika
yayını, işlem yetkisi alındıktan sonra ve hazırlama başlamadan önce kimliği
denetler. Eski işlem geçmişinin temizlenmesi bu kaydı geçersiz kılmaz.

Bağımsız regresyon, önceki başarı bulunmayan durumu ve başarılı yapılandırmanın
ardından başarısız değişikliği; yönetici yeniden başlatılmadan önce ve sonra
denetler: [düzeltme öncesi](snapshot-regression-before-fix.log),
[düzeltme sonrası yarış denetimi](snapshot-regression-after-fix.log).

## Son kaynak kontrolleri

- [Agent ve komut çalıştırma testleri](final-agent-hostcmd.log).
- [Posta yaşam döngüsü ve kapalı sözleşme yarış testleri](final-mail-race.log).
- [Agent, panel ve komut çalıştırma vet sonucu](final-vet.log).
- [İlgili son kaynak özetleri](source.sha256).

Bu kontroller önceki tam depo test/vet çalışmasını tamamlar. Geçici CA testi;
genel ACME sertifikası alımını, gerçek Certbot zamanlayıcısıyla yenilemeyi,
İnternet üzerinden posta teslimini, DNS/PTR önkoşullarını veya `web_mail`
kurulumunun tamamını kanıtlamaz. Bunlar kendi dış önkoşulları ve kabul
kanıtlarıyla ayrıca doğrulanmalıdır.
