# Yerel test izleyicisinde çıkan thread’lerin uzlaştırılması

*16 Eylül 2026 · [English](EXIT-THREADS.md)*

Bu çalışma D-025/P0.1 ve P0.3 kanıtları için test altyapısını düzeltir. Kurulu
panel yaşam döngüsü, kalıcı ürün şeması, kurtarma protokolü, veritabanı, lisans
politikası ve kesinti denetleyicisinin yetkisi değişmez. Açılış hazır olma
kontrolü için yeni bir gerçek sistem deneyi hâlâ gereklidir; bu kayıt o kabulü
kapatmaz.

## Yeniden üretim ve sınırlı düzeltme

[AA deneyi](../NATIVE-EXCHANGE-RECOVERY.tr.md) istenen iki kesintiye de ulaşamadı:
çıkmakta olan Go kontrol sürecinin ana thread’i zombi durumundaydı; izleyiciye
bağlı, duraklamış başka bir thread ise kabul edilmiş TID listesinde yoktu.
Yalnız bu listeyi sorgulamak, eksik thread’in durma olayını tüketemediği için
ana thread’in çıkışı tamamlanamıyordu.

Özel bir Go alt süreci aynı durumu yerelde yeniden üretti. Daha doğrudan
karşılaştırmada, `ee70fce` sürümünün `native_trace.py` dosyası yeni iki-goroutine
örneğinde ilk denemede `trace-detach-incomplete-controller-watchdog-required`
sonucunu verdi. Düzeltmeli deneyle aynı ikili kullanıldı. Özgün iz ve sıfırdan
farklı test sonucu `/var/tmp/celikpanel-exit-before.tDhmIWNF` altında korunuyor.
Son temizliği testin kendi sabitlenmiş alt sürecine uygulayan test sonlandırıcısı
oldu; bu temizlik yerel izleyicinin başarısı olarak gösterilmez.

Yerel adaptör mevcut bekleme süresini 100 ms’lik sorgulara böler. Bir kardeş
thread ancak daha önce kabul edilen ana thread’in EXIT olayı tüketilmiş ve
zombi olduğu doğrulanmışsa uzlaştırılır. İki kimlik yeniden okunur; TGID,
başlangıç zamanı, izleyici sahipliği ve kayıtlı cgroup aynı olmalıdır. Yeni kayıt
`exit-thread-admitted` adını ve çekirdek thread grubu yetkisini taşır; uydurulmuş
CLONE olayı veya ebeveyn ilişkisi değildir.

Bu thread yalnız gerçek durma/çıkış olaylarını tüketmek için kabul edilir.
Syscall, exec veya fork kanıtı üretemez. Gerçek wait çıkışı görülene kadar son
tüm-thread durdurma/kesinti kanıtını engeller. Envanterden yeni süreç bulunup
bağlanılmaz. Yerel kod `waitpid(-1)` kullanmaz; bellek/register değiştirmez,
EXITKILL açmaz veya öldürme sinyali göndermez. Denetleyici yetkisi ve toplam
izleme/temizleme süre sınırları değişmez.

## Kanıt ve kalan belirsizlik

WSL2 Linux `6.18.33.2-microsoft-standard-WSL2` üzerinde sabit Go 1.26.5 ile
`a60954608232edca0aaa3ae43117eb72fbca2ae6084edce996d9a5b8001f77d7`
SHA256 değerli örnek ikili üretildi. Yalıtılmış
`/var/tmp/celikpanel-exit-trace.BSsii90t` deneyi 50 izin tümünü korudu:
47’si gerçek wait çıkışlarıyla tamamlandı; bunlarda 42 thread uzlaştırıldı.
Üç iz ayrı `task-id` belirsizliğiyle, kesinti uygulamadan izleme bağını bıraktı.
Bunlar **sonuçsuzdur**, doğrulanmış çıkış yarışı başarıları değildir. Çekirdek
olay-kimliği durumu açık kalır. Hiçbir deneme kesinti denetleyicisini çağırmadı.

| Kanıt | SHA256 |
|---|---|
| `evidence/summary.json` | `7fad1c2d8d8d116e46b48316a2d6b80c1646f649f7d09eb9f0a7f43908faddbc` |
| `source.sha256` (çalışma kaynağının tam kopyası) | `c0136a2045d988507ebfedc9850a0d2f29d846e4e6f09067714213f9a730df75` |
| `tests.log` | `88cdd5e8acc1d17042c6a3cac586d0b5cd8f8fdb03b3d0a212c76ecb927eae90` |

Önceki araştırma denemeleri de korunuyor: `task-id` sonucu veren yoğun
32-goroutine örneği, yarışı hiç tetiklemeyen en küçük örnek ve 45 tamamlanma /
beş sonuçsuz iz üreten iki-goroutine koşusu. Bunlar yukarıdaki yalıtılmış deney
olarak yeniden adlandırılmadı. Yerel yeniden üretim AA’nın tam çekirdek olay
sırasını henüz kanıtlamaz.

Python adaptör testleri; eksik EXIT kanıtı, yaşayan/yabancı ana thread, değişen
PID/başlangıç zamanı, yanlış izleyici/cgroup, durmamış thread, çıkış dışı olay,
reddedilen kesinti kanıtı, değişmeyen süre sınırı ve öldürmeden temizlemeyi
kapsar. İzleyici kümesinde 73 test koştu (68 geçti, isteğe bağlı beş çekirdek
testi atlandı); ayrı kurtarma altyapısında 517 test koştu (516 geçti, gerçek
ikili gerektiren bir test açıkça atlandı). Yukarıdaki çekirdek deneyi ayrıca
çalıştırıldı.

Önceden kurulmuş tam Go sürümüyle yalıtılmış örneği çalıştırma:

```sh
CELIKPANEL_WALTRACE_GO=/path/to/go1.26.5/bin/go \
  bash deploy/e2e/release-recovery/waltrace/run-exit-local.sh
```

Çalıştırıcı yeni özel dizin açar, kaynak/araç zinciri/ikili özetlerini kaydeder,
araç zinciri/modül indirmelerini kapatır, sonunda kaynak özetlerini yeniden
kontrol eder ve başarısız kanıtları korur. Test yalnız kendi yeni oluşturduğu,
başlangıç kapısındaki alt sürece bağlanır; çekirdek adaptörü gerçek VM kaydı
yerine örneğin cgroup’unu kullanır. Kurulu panel, VM, güncelleme veya ağa
dokunulmaz. Olasılıksal test gerçekten bir thread uzlaştırmalıdır; yarışı hiç
tetiklemeyen koşu geçemez. Gerçek exchange/yeniden başlatma kabulü ve diğer P0
matrisi açık kalır.
