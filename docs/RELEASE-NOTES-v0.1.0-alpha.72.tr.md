# CelikPanel v0.1.0-alpha.72

*Yerel ikincil DNS ve sihirbazda kaldığınız adıma dönüş · [English](RELEASE-NOTES-v0.1.0-alpha.72.md)*

Barındırma yapan bir ikincil DNS sunucusu artık uzak CelikPanel adresi veya kayıt bağlantısı gerektirmeden, sahibinin yönettiği DNS kayıtlarını kullanabilir. Yerel DNS çoğaltması ile isteğe bağlı panel üzerinden kayıt yönetimi ayrı seçimlerdir. Elle yönetimde yeni alan adları için harici DNS talimatları gösterilir ve kayıt doğrulaması gerekir; sunucu sahibi kayıtları birincilde kendi araçlarıyla yayımlar. Mevcut alan adlarının DNS sahipliği ve önceden incelenmiş planlar korunur. Yetkilendirilmiş birincil panel üzerinden otomatik yayımlama seçeneği de devam eder.

Kurulum sihirbazı, sayfa yenilendiğinde veya arayüz yeniden açıldığında aynı tarayıcı sekmesindeki düzenlenebilir alanları ve mevcut adımı korur. İnceleme adımına dönüşte güncel plan yeniden alınır ve başlatma onayı temizlenir. Kurulum kendiliğinden başlamaz. Sekme kapatılınca yerel kurtarma kaydı sona erer; sunucudaki taslağın sürümü değişmişse eski alanlar geri yüklenmez.

Doğrulama: 399 arayüz testi, üretim derlemesi ve paket boyutu kontrolleri, panel ve BIND testleri, odaklı yarış durumu testleri, masaüstü ve mobil genişliklerde Türkçe/İngilizce tarayıcı kontrolleri. Geçici BIND birincil/PowerDNS ikincil çiftinde panel ve agent kapatılıp çalıştırılabilir dosyaları kaldırılmışken katalog keşfi, bölge aktarımı, sahibinin kayıt değişiklikleri, servis yeniden başlatmaları ve katalogdan çıkarma çalıştı. Bu bir DNS servis testidir; tüm sunucunun yeniden başlatılması veya panel kaldırma güvencesi değildir.

[Panelden bağımsız çalışma incelemesi](OWNER-INDEPENDENCE.tr.md), posta sertifikası yenilemesi, açılışta güvenlik duvarının geri yüklenmesi ve veri/çalışma ortamlarının korunması için kalan bağımlılıkları kaydeder. Bu sürüm, CelikPanel ve agent kaldırıldığında bütün iş yüklerinin güvenle çalışacağının tamamlandığını iddia etmez. [Yerel DNS kanıtlarına](validation/native-dns-independence-20260912/README.tr.md) ve [sihirbaz kurtarma kanıtlarına](validation/setup-editor-recovery-20260912/README.tr.md) bakabilirsiniz.

## Güncelleme

Her sunucuda **Ayarlar → CelikPanel güncellemeleri → Güncellemeleri kontrol et** yolundan **v0.1.0-alpha.72** sürümünü kendiniz yükleyin. DNS yönetimini ve kayıtlı planı incelemek için **Ayarlar → Sunucu kurulumu** bölümüne dönün. Sürüm yayımlama kurulu panelleri güncellemez veya sunucu kurulumunu başlatmaz. Yayımlanan paket Linux amd64 içindir.
