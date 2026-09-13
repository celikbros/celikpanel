# CelikPanel v0.1.0-alpha.75

*Altyapı DNS kayıtlarını sunucu kurulumu içinde hazırlayın · [English](RELEASE-NOTES-v0.1.0-alpha.75.md)*

Kurulum sihirbazı artık genel DNS çözümlemesini denetlemeden ve panel ya da posta
TLS sertifikasını istemeden önce açıkça incelenen altyapı DNS kayıtlarını
hazırlayabilir. DNS motorunun kurulu olması tek başına sunucu adının çözüldüğü
anlamına gelmez. Bu kayıtlar için sihirbazdan çıkıp müşteri alan adı oluşturmak
gerekmez.

Yerel birincil sunucuda sahibine ait bölgenin tam adını seçip asgari SOA, NS ve A
kayıtlarını inceleyin. İsteğe bağlı olarak diğer sunucunun panel adını ekleyin.
İlgisiz mevcut kayıtlar korunur; sahiplik çakışmaları ve tespit edilen yerel sahip
değişiklikleri yayını durdurur. Bu adım web sitesi, posta kutusu veya müşteri
alan adı oluşturmaz.

Standart birincil/ikincil DNS aktarımı uzak panel HTTPS erişiminden bağımsızdır.
Birincil katalog veya genel erişim kayıtları hazır değilse sihirbaz eksikliği ve
sonraki işlemi açıklar. Bekleyen işlemler kimliklerini korur; kesinleşmiş hatalar
kurtarma ve yeniden incelenen plan gerektirir.

## Yarım kalan kuruluma devam etme

Yayımlandıktan sonra her sunucuda **Ayarlar → CelikPanel güncellemeleri** üzerinden
**v0.1.0-alpha.75** güncellemesini kendiniz başlatın. Sürüm yayını kurulu panelleri
güncellemez.

Tamamlanan servis kurulumları korunur. Güncelleme başarısız kurulumu yeniden
başlatmaz ve kabul edilmiş planı değiştirmez. **Ayarlar → Sunucu kurulumu**
bölümünden düzeltilmiş planı inceleyerek yeni DNS hazırlama adımlarını plana alın.
Başlatmadan önce sahip olunan bölgeyi, sunucu adreslerini ve isteğe bağlı diğer
panel adını doğrulayın. Önce birincil DNS hazırlığını, ardından ikincili başlatın;
ikincil için birincilin bütün barındırma adımlarının veya sertifikasının
bitmesini beklemeyin.

Mevcut paket yöneticisi veya sunucu işlemi çakışmaları kendi kurtarma adımlarını
gerektirir. Bu sürüm güncelleme kilitlerini aşmaz; Boston'da ayrıca bildirilen
Nginx meşgul hatasının düzeldiğini iddia etmez.

## Doğrulama ve sınırlar

Tam yerel Linux testleri geçti: panel 1.258 üst düzey test, agent 1.341 ve DNS
wire 5. Son odaklı kurulum grubu 104 üst düzey testi; ön yüz 436 testi, üretim
derlemesini ve değişmeyen paket boyutu sınırlarını geçti. Sekiz yerel tarayıcı
örneği Türkçe/İngilizce masaüstü/mobil kurulum ve bekleme durumlarını kapsar.
Bunlar canlı Frankfurt/Boston DNS aktarımını veya genel sertifika otoritesi
başarısını kanıtlamaz. [Uygulama sınırlarına bakın](SERVER-SETUP-ACCESS-DNS.tr.md).
