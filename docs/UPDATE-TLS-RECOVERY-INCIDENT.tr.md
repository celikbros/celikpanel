# Yönetilen TLS güncellemesi ve kurtarma kilidi hatası

13 Eylül 2026 · [English](UPDATE-TLS-RECOVERY-INCIDENT.md)

Durum: kaynak düzeltmeleri yerelde test edildi. Kullanıcı Frankfurt’u mevcut
Alpha75 programlarıyla kurtardı; süreç kimlikleri ve HTTPS erişimi doğrulandı.
Bu kayıt tek başına yeni sürümün yayımlandığını doğrulamaz.

Bildirilen Alpha77 denemesi iki koordinatörü durdurduktan sonra
`legacy self-signed panel TLS ownership normalization failed` hatası verdi.
Paylaşılan TLS dizininde `current` bağlantısı, onun `.panel-cert-*` dizini ve
başlangıç sertifikası çifti bulunuyor. Eski normalleştirme işlevi her durumda
yalnız iki başlangıç dosyası beklediğinden desteklenen bu düzeni reddediyordu.
Hata yerel örnekle yeniden üretildi; özel anahtar içeriğini okumak gerekmedi.

Otomatik kurtarmanın alt süreci ayrıca
`recovery transaction descriptor owns an unexpected lock` hatasında durdu.
İlk kilit kontrolü tam olarak ` FLOCK ADVISORY WRITE ` metnini arıyordu. Linux
fdinfo sütun hizalaması `FLOCK  ADVISORY  WRITE` biçiminde çift boşluk
kullanabiliyor. Aynı geçerli kilit, kurtarma çalıştırıcısının alan ayrıştırmasını
geçerken güncelleyicinin metin eşleştirmesinden geçemiyordu. Bu uyuşmazlık gerçek
Linux dosya kilidiyle yeniden üretildi.

Kaynak düzeltmesi atomik TLS ağacının tamamını sıkı biçimde doğrulayıp korur;
başlangıç çifti de bulunabilir. Sertifika baytları, sahiplik, izinler, bağlantılar
ve değiştirilme zamanları değiştirilmez. Yalnız eski iki dosyalı düzen
normalleştirilir. Tanınmayan nesneler, güvenilmeyen atomik dosya sahipliği ve
dizin dışına çıkan bağlantılar reddedilmeye devam eder. İlk kilit kontrolü
alanları ayrıştırır; tek kayıt, inode kimliği ve bağımsız açılışın dışlanması
kontrolleri korunur.

Doğrulama: TLS snapshot/geri dönüş testleri atomik ve karma düzeni, tekrarlı
çağrıyı ve hatalı düzenlerin değiştirilmeden reddini kapsar. Gerçek miras alınan
exclusive flock kabul edilir; paylaşımlı, kilitsiz, kapalı, kimliği uyuşmayan veya
ayrı descriptor tarafından tutulan kilit reddedilir. Güncelleme ön kontrol ve
kurtarma temeli/son durum testleri de yerelde geçti.

Canlı kurtarma öncesinde kalıcı işlem aşaması, geçici snapshot içeriği ve kurulu
dosyaların değişmediği doğrulanmalıdır. Önceki olaya özel pre-ledger iptal aracı
bu normal-ledger hatasına kendiliğinden uygulanamaz. İşlem işaretçilerini
silmek, saklanan imzalı sürümü düzenlemek veya yeni güncelleme başlatmak geçici
onarım yolu değildir. Kurulu panel güncellemesini kullanıcı panelden başlatır.

## Kullanıcının uyguladığı kurtarma sonucu

Kullanıcı, durmuş servisler, sürüm dosyaları, veritabanı ve aşama kayıtları
kontrol edildikten sonra olaya özel kurtarma aracını çalıştırdı. İlk araç,
değişiklik öncesi işaretçiyi kaldırıp eksik snapshot'ı kanıt olarak sakladıktan
sonra program kimliğinin hazır olmadığını bildirdi. Araç tekrar çalıştırılmamalıdır.

Sonraki çıktıda agent PID 1156220 ve panel PID 1156319 ile iki servis de aktifti.
Çalışan program hash değerleri diskteki dosyalarla ve yayımlanan Alpha75 paketiyle
eşleşti. Salt-okur HTTPS kontrolü geçerli sertifika ile HTTP 200 döndürdü.
Barındırılan diğer hizmetlerin sağlığı bu kontrolde denetlenmedi.

Gerçek fork/exec testi, başlangıç sırasında geçici program kimliğini yeniden üretir.
Araç artık tam program, kararlı PID ve agent soketini bekler; kalıcı yanlış programı
reddeder. Frankfurt'taki ara program yakalanmadığından bu test olası nedeni
gösterir, canlı geçişin kesin kanıtı değildir. On sekiz test dosya sistemi, kilit,
işaretçi ve başlangıç davranışını kapsar. systemctl yanıtları taklit edilir;
fork/exec ve kilit denetimleri gerçek yerel Linux süreçleri kullanır.
