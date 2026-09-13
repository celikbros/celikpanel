# Kurulum doğrulama geri bildirimi

Alpha76 sürüm kaynağı, 2026-09-13. Kurulu panelleri sahipleri günceller.

## Sorun

Son doğrulama adımı kontrol yapmadan başarılı kaydediliyordu; gerçek tamamlanma
kontrolleri daha sonra çalışıyordu. Bu nedenle gereksinim bekleyen kurulumda tüm
adımlar Tamamlandı görünüyordu. Elle yeniden kontrol, gereksinim değişmediğinde
görünür sonuç bildirmiyordu. İşlemin eski kontrolleri yeni yanıtı gölgeleyebiliyordu.

## Davranış

Son doğrulama, güncel hazırlık kontrolleri ve korumalı kurulum tamamlama başarılı
olana kadar bekler. Eski sürümden kalan bekleyen işlemlerin erken başarı kaydı
düzeltilir; tamamlanan servis adımları korunur. Plan düzenleme, yalnızca tüm kurulum
adımları başarılıysa ve son doğrulamaya bağlı alt işlem yoksa bekleyen son
doğrulamaya izin verir.

Arayüz eski kayıtları da doğru gösterir; servislerin kurulmasıyla sunucunun hazır
olmasını ayırır. Gereksinimleri tekrar kontrol et düğmesi işlem sırasında ve
sonrasında yanında erişilebilir bir sonuç gösterir: eksik gereksinim, bilinmeyen
sonuç, bağlantı hatası veya son tamamlanmayı bekleyen başarılı kontrol. Güncel
kontroller işlemin eski kanıtından önceliklidir. Yalnızca işlemin başarılı olması
sunucuyu hazır ilan etmez. Bu okuma istekleri servis kurmaz veya DNS/PTR değiştirmez.

## Doğrulama

- Linux Go 1.26.5: cmd/panel TestServerSetup testlerinin tümü geçti; eski kayıtların
  düzeltilmesi, servislerin tekrar kurulmaması ve plan düzenleme korumaları dahil.
- Arayüz: 442 test, üretim derlemesi ve paket boyutu kontrolleri geçti.
- Yerel Chrome, taklit API: TR/EN, 390/1440 piksel, dört kontrol sonucu
  (16 senaryo); yatay taşma, JS hatası, yazma veya dış ağ isteği yok.
- Mobil eksik gereksinim ve masaüstü bilinmeyen kontrol görüntüleri incelendi.

Kurulu paneller güncellenmedi veya yeniden yapılandırılmadı. mail_identity
gereksinimi gerçek posta sunucusu adı ve ileri/ters DNS eşleşmesinin kanıtını
gerektirir; bu arayüz düzeltmesi o gereksinimi doğrulamaz veya onarmaz.

## Salt okunur olay kanıtı

2026-09-13 tarihinde 1.1.1.1 üzerinden DNS sorguları şu sonucu verdi:
- 72.62.38.15 PTR: server1.celikhost.com.
- mail.frankfurt.celikhost.com A: 72.62.38.15.

Sihirbazda kayıtlı posta adı mail.frankfurt.celikhost.com. Bu,
setupMailHostIdentityReady için somut bir ters DNS uyuşmazlığıdır. Başka
kontrollerin de başarısız olup olmadığını kanıtlamaz. PTR değişikliğini sunucu
sahibinin sağlayıcısı uygular; bu incelemede DNS kaydı değiştirilmedi.
