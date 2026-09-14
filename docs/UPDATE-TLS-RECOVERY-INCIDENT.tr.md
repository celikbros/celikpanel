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

## Alpha78 sonrası: saklanan sertifika işlem kaydı

Kullanıcının 13 Eylül 2026 22:17 UTC Alpha78 denemesi TLS snapshot yayımlanmadan
durdu. Kurtarma sürekli `existing managed TLS tree failed strict validation`
hatasına ulaştı. Paylaşılan dosya listesinde eski sertifika sürümünde
`.panel-certificate-issue-receipt.json` (root:root, 0600, tek link, 325 bayt)
ve yanında üç dosyalı daha yeni bir `current` sürümü bulunuyor.

Sertifika üreticisi bu dördüncü dosyayı işlem kanıtı olarak özellikle koruyor.
Shell snapshot doğrulayıcısı ise eski sürümler dahil her dizinde tam üç dosya
istiyordu. Paylaşılan düzenin yerel örneği hatayı yeniden üretiyor. Alpha78 testleri
elle hazırlanmış üç dosyalı sürümler kullandığından gerçek üretici çıktısı bu
kontrollerde sınanmadı. CI başarısı bu güncelleme yolunu doğrulamaya yetmedi.

Kaynak düzeltmesi yalnız bu adı taşıyan isteğe bağlı kaydı, root erişimiyle
sınırlı metaverisi ve 1–1024 bayt boyut koşuluyla kabul eder. Snapshot ve geri
yükleme baytları ve metaveriyi aynen korur; işlem kimliği ve canonical JSON
yorumlaması agent'ın sorumluluğunda kalır. Bilinmeyen dosyalar ve güvensiz izinler
reddedilir. İşlem kaydını silmek kurtarma yöntemi değildir.

Bu ek düzeltme Alpha79 kapsamındadır. Yayımdan önce kullanıcı gözden geçirilmiş kurtarma
aracını (SHA256
`13f03ebe790a3b9af9b01207a22393ab287a9c938b87b697859873ac2b19763c`)
aktarıp hash değerini doğruladı ve çalıştırdı. Araç, kilit altında program/veri
kontrollerinden sonra mevcut Alpha75 servislerinin aktif olduğunu bildirdi;
güncelleme kurulmadı. Kanıtlar
`/var/backups/celikpanel/frankfurt-recovery-20260913T221708Z` yolunda saklandı.
Ardından bağımsız salt-okur HTTPS giriş isteği geçerli sertifika ile HTTP 200
aldı. Bu panel erişimini doğrular; barındırılan hizmetlerin denetimi değildir.
Önceki olay kanıtları, kurtarma kopyaları ve TLS dosyaları aracın değişiklik
kapsamı dışındaydı.

## Alpha79 sonrası: BIND yayımı ve otomatik geri alma

Kullanıcı 14 Eylül'de `971510e51549422a7cd8c4878dc714af` Alpha79 güncellemesini
başlattı. Güncelleyici 04:31:58 UTC'de hedef dosyaları kurup doğruladıktan sonra
`BIND state and ownership receipts disagree` hatası verdi. Ardından kurtarma
servisi sürekli `recovery transaction descriptor owns an unexpected lock`
hatasıyla durdu. Günlükte eski başarısız systemd tetikleyicilerinin listelenmesi
eşzamanlı güncelleme yapıldığını kanıtlamaz.

Paylaşılan mevcut BIND durumu ile motor edinim kaydında motor, epoch 1, primary
rolü, yerel/eş IP, kaynak revizyonu ve işlem kimliği aynı. Yalnız nesil ve primary
katalog sayacı farklı: güncel durum
`ea6ea68b1b7caa4fa9b0a4260e567842353f3667aa5ad4df98ba5b70cb1f1469`
ve sayaç 2; edinim kaydı
`b19ee90a81d87342cba30751f1aede6342706643c243130f37c3f164006926c7`
ve sayaç 1 gösteriyor. Normal alan yayımı, motorun edinim kanıtını değiştirmeden
bu güncel alanları ilerletiyor. Güncelleyici hatalı biçimde bütün alanların eşit
olmasını istiyor ve sonra eski edinim neslini seçiyordu. Gerçek alan üreticisi
ve durum yazıcısını kullanan regresyon testi aynı hatayı üretiyor. Düzeltme,
edinim kanıtını koruyup güncel nesil ve çalışma yapılandırmasını doğruluyor.

Otomatik geri almanın ayrı nedeni, Alpha78'in fdinfo alan ayrıştırma düzeltmesinin
`update.sh` içinde yapılıp `rollback.sh` içindeki tek boşluğa bağlı karşılaştırmanın
kalması. Gerçek Linux devralınmış özel flock testi Alpha79 geri alma girişinde
`exclusive status=41 expected=0` sonucunu üretiyor. Yerel düzeltme iki girişte de
alanları ayrıştırıyor; tek kayıt, inode kimliği ve bağımsız kilit dışlama
kontrollerini koruyor. Paylaşımlı, kilitsiz, kapalı, yanlış kimlikli ve başka
sahibe ait kilitler reddedilmeye devam ediyor.

### Kullanıcının tamamladığı Alpha79 geri alması

Hata dosyalar uygulandıktan sonra oluştuğundan eski değişiklik öncesi iptal
araçları uygun değildi. Kullanıcı, saklanan ve doğrulanmış Alpha79 sürümünün
standart geri alma girişini kullandı:

- Sürüm: `/var/backups/celikpanel/releases/f3390addc85b-1f7e1f714db463724f795ebb`
- Yedek: `/var/backups/celikpanel/update-snapshots/20260914T043142Z-from-unknown-to-f3390addc85bbe92a0cc865448d9bb366e6b980a-db8813bd340936d749913d55dbcda68d`

Kullanıcının normal çağrısı işlem kilidini kendisi alarak hatalı devralınmış
kilit ayrıştırıcısına girmez. Geri yüklemeden önce sürüm, tam yedek, eşleşen işlem
ve boşta işlem defteri doğrulanır. Komut verilmeden önce orijinal Alpha79 girişinin
normal kilit edinimi ve meşgul kilitte durması yerelde sınandı. Saklanan imzalı
dosyalar veya işlem işaretçileri elle değiştirilmedi.

Kullanıcı çıktısı 05:53:39 UTC'de veritabanı, dosyalar, TLS ve servis düzeni geri
yüklendikten sonra `Rollback complete`, `Panel: active`, `Agent: active` bildirdi.
Ardından bağımsız salt-okur `https://frankfurt.celikhost.com:2083/login` isteği
`72.62.38.15` adresinden geçerli sertifikayla HTTP 200 aldı. Bu yönetim erişimini
doğrular; barındırılan hizmetlerin sağlığını veya BIND kayıtlarını yorumlayan
kodun düzeldiğini kanıtlamaz. Kurtarma yeni güncelleme kurmadı. Kalıcı düzeltmeler
ayrıca yayımlanana kadar yerel çalışma olarak duruyor.
