# Sunucu kurulumu kaynak ve tarayıcı kontrolleri

*11 Eylül 2026 · [English](README.md)*

Bunlar geliştirme kabul sonuçlarıdır; sürüm yayımlandığını veya kurulu panelin
güncellendiğini göstermez. Takip edilen dosyalarla kurulum çalışmasının yeni
kaynakları temiz bir kopyada test edildi; yerel geçmiş ve eklenti çıktıları dışarıda
tutuldu. Kaynak kopyasının SHA-256 değeri:
`fdbb0de27a0c4f4fd56e525df23c8ecfccaa10f0075fbe2fdb13d5fd1bd61459`.

- `make test vet`: Linux üzerinde Go 1.26.5 ve projenin temizlenmiş araç ortamıyla
  tüm Go paketleri geçti; vet başarıyla tamamlandı.
- Race denetimi: `cmd/panel`, `cmd/agent` ve `internal/db` paketlerinde
  `TestServerSetup|TestSetup|TestMailHostCertificate|TestSQLite` seçimi geçti.
  Bu, bütün deponun race denetimiyle çalıştırıldığı iddiası değildir.
- `npm test`: 339 test geçti. `npm run build`: TypeScript, üretim derlemesi ve
  paket boyutu sınırları geçti.
- Üretim arayüzü gerçek Chrome'da kontrollü API test verileriyle on senaryoda
  doğrulandı: Türkçe/İngilizce yeni kurulumda 1440/390 piksel; eski kurulum, hazır,
  bekleyen, erişilemiyor, müşteri ve lisans kilidi. İlk okuma yazma yapmadı;
  müşteri ve lisanssız kullanıcı kurulum durumunu okumadı; onaylanan kurulum
  tek başlatma isteği gönderdi. Çalışma zamanı hatası ve yatay taşma görülmedi.
  Ekran görüntüleri sınırlı bir görsel incelemeden geçti.

İlgili günlükler ve tarayıcı sonuç JSON'u bu dizindedir. Gerçek güvenlik duvarı ve
yeniden başlatma kanıtı [ayrı kaydedilmiştir](../server-setup-firewall-20260911/README.tr.md).
Kontrollü tarayıcı verileri ve birim testleri; genel ACME sertifikası alınmasını,
bütün amaç profillerinin uçtan uca çalışmasını veya henüz uygulanmayan uzak DNS
bağlantısını kanıtlamaz. [Geçerli uygulama durumuna](../../SERVER-SETUP-STATUS.tr.md) bakın.

## Eş IPv6 mesajı için ek doğrulama

Yukarıdaki kaynak görüntüsünden sonra kurulum, `dns_peer_ipv6_unverified` için
özel Türkçe/İngilizce açıklama kazandı. Bu açıklama eşin AAAA kaydının yanlış
olduğunu söylemeden ve DNS kayıtlarını değiştirmeden doğrulama sınırını bildirir.

Geliştirme oturumunda şu komutlar başarıyla tamamlandı:

- `node --test tests/server-setup-runtime.test.mjs tests/external-dns-ui-runtime.test.mjs`: 17 geçti, 0 başarısız, çıkış durumu 0.
- `npm run build`: TypeScript, Vite ve paket boyutu sınırı kontrolleri geçti, çıkış durumu 0.

Bu iki ek çalıştırmanın sonuçları komut aracının çıktısında gözlendi; ayrı tam
günlük dosyaları kaydedilmedi. Bu dizindeki `web-tests.log` ve `web-build.log`
yukarıdaki önceki tam çalıştırmaya aittir. Bu not, o dosyaları sonraki mesaj
değişikliğinin kanıtı olarak yeniden adlandırmaz.
