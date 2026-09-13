# Güncellemede koordinatör kimliğinin kaydı

[English](UPDATE-QUIESCE-CAPTURE.md)

## Olay ve kanıt

13 Eylül 2026 tarihinde Frankfurt'taki iki alpha.76 güncellemesi
`state=unchanged` ve `coordinator cgroup changed during quiesce capture:
celikpanel-agent.service` hatasıyla durdu. Sonraki systemd ve ham `cgroup.procs`
okumalarında yalnızca MainPID 856154 vardı. İşçi günlüğü hata anındaki süreç
listesini kaydetmemişti. Bu kanıtlar ilk reddin hangi süreçten veya okuma hatasından
kaynaklandığını belirlemiyor. Asistan kurulu sunucuda güncelleme veya onarım yapmadı.

Önceki kontrol eşleşmeyen ilk süreç listesinde duruyordu. Agent'ın güncelleme
durumu sorgusu da bir `systemctl show` yardımcı süreci başlatıyor. Bu yardımcının
kayıt anına denk gelmesi olası bir çakışmadır; Frankfurt olayı için doğrulanmış
tanı değildir.

## Kaynak kod değişikliği

Yalnızca quiesce işaretçisi yayımlanmadan önceki ilk aktif koordinatör kaydı,
eşleşmeyen okumalar arasında 100 ms bekleyerek en fazla 20 okumaya izin veriyor.
Her okumada ilk ActiveState, MainPID ve süreç başlangıç zamanı korunmalıdır;
süreç kaybolmuş veya dondurulmuş olamaz. Yalnızca tam cgroup eşleşmesi başarılıdır.
Okuma hataları hemen durdurur. Okuma sayısı ve bekleme bütçesi sınırlıdır; bu,
systemd yanıtları için kesin bir duvar saati zaman aşımı değildir.

Hiçbir süreç öldürülmez, komut adına istisna tanınmaz, güncelleme isteği yeniden
başlatılmaz. Kalıcı ek süreçler güncellemeyi engellemeye devam eder. Sonraki donmuş
kimlik, mutasyon defteri, kilit ve kurtarma kontrolleri katı kalır. Hata ayrıntısı
beklenen PID ile uzunluğu sınırlı görülen PID listesini veya okunamadığı bilgisini
içerir; komut argümanları ve kimlik bilgileri kaydedilmez. Değişiklik ilk kayıt
kontrolünü ve tanıyı iyileştirir; sonraki bütün kontrollerin geçeceğini veya
Frankfurt'taki ilk nedenin çözüldüğünü garanti etmez.

## Doğrulama ve kullanıcı yolu

`deploy/test-update-quiesce-capture.sh`, gerçek kayıt işlevlerini yalıtılmış süreç
ve systemd test verileriyle sınar: temiz liste ve geçici yardımcı sonrası başarı;
kalıcı yardımcılar, boş veya okunamayan liste, kimlik değişimi, donmuş veya kayıp
süreçler ve değişmeyen pasif servis davranışı. Katı eşleştiricinin ek süreçleri
reddettiği de doğrulanır. Mevcut bootstrap-update sözleşmesi bu testleri çalıştırır.

Kaynak değişikliğinin kurulu panele ulaşması için yeni, test edilmiş ve imzalı
sürüm gerekir. İmzalı kurulu güncelleyiciye yama yapılmaz ve cgroup kanıtı atlanmaz.
Sonraki güncellemeyi kullanıcı panel arayüzünden başlatır. Tekrarlayan hatanın tam
işlem kimliği ve tanı bilgileri inceleme için korunur.
