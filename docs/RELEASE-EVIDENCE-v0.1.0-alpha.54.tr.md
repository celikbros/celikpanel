# Alpha54 Sürüm Kanıtı

*Doğrulama: 8 Eylül 2026 · [English](RELEASE-EVIDENCE-v0.1.0-alpha.54.md)*

Bu kayıt, yarım kalan çalışma devralınırken doğrulanan sürüm ve web sitesi
durumunu belgeler. Boston veya Frankfurt'un güncel kurulum durumunu kanıtlamaz.

| Kanıt | Doğrulanan sonuç |
|---|---|
| Sürüm | [v0.1.0-alpha.54](https://github.com/celikbros/celikpanel/releases/tag/v0.1.0-alpha.54), altı ürünle yayımlanmış ön sürüm |
| Commit | `956d54fdd226483b776bdb84e29c029fab36e043` |
| Sıra | `54` |
| Sürüm CI'ı | [34202049674](https://github.com/celikbros/celikpanel/actions/runs/34202049674), sürüm commit'inde başarıyla tamamlandı |
| Manifest zamanı | `2026-09-08T07:58:14Z` |
| GitHub yayın zamanı | `2026-09-08T08:09:51Z` |
| Platform | `linux/amd64` |
| Arşiv boyutu | `23322968` bayt |
| Arşiv SHA-256 | `62d3a589c316a0f2dee5e5881a1bb4fc81489da7676cb3cecbd9002ae665b7fd` |
| Ed25519 imzası | `deploy/release-signing-ed25519.pem` açık anahtarıyla bağımsız doğrulandı |
| Canlı portal | `https://celikpanel.net`, Alpha54 HTTPS ve salt okunur SSH ile doğrulandı |

`deploy/verify-download-portal-public.py`, saklanan Alpha54 yayın adayına karşı
başarıyla tamamlandı: 15 istek, bir tam arşiv indirmesi ve toplam 23.487.882 bayt.
HTML, CSS, JavaScript, bootstrap, açık anahtar, güvenlik iletişimi, sürüm
seçicileri, manifestler, imza ve arşiv adayla eşleşti. Önceki yayın dökümü,
desteklenen yayın aracının `2026-09-08T08:12:37Z` zamanında işlemi tamamladığını
ve yedek oluşturduğunu kaydediyor; yedek ayrıca SSH ile gözlendi. Bu devralma
kontrolünde Alpha54 yeniden yayımlanmadı.

Lacivert-beyaz tasarım canlıda. Masaüstü ve mobil kontrollerde Türkçe/İngilizce
geçişi, sürüm bilgileri, indirme bağlantıları ve sayfa içi bağlantılar çalıştı.
Test pano adaptörüyle kopyalanan metinler güncel ve sabit sürüm komutlarıyla
eşleşti; komutlar çalıştırılmadı. Sabit sürüm kutusu açıldığında bir grid boyutlama
hatası bulundu: 390 piksel genişliğindeki sayfa 975 piksele taşıyordu. Alpha55,
komut ve mobil grid sütunlarının genişliğini sınırlayarak bu hatayı giderir.

30 Ağustos devir ve sunucu kayıtları tarihsel durumu anlatır. Operatör, Boston
ve Frankfurt'ta kurulumların kendi kullanıcıları tarafından yapılmasını bekliyor.
Bu web sitesi incelemesinde iki sunucunun güncel kurulum durumu doğrulanmadı;
eski Alpha52 receipt'leri bugünkü durumun kanıtı olarak kullanılmamalıdır.
