# Olay Raporu Şablonu

*[English](INCIDENT-TEMPLATE.md) · Belirli bir olay için bu dosyayı kopyalayın; şablonu değiştirmeyin*

Bu şablon sır içermeyen operasyon, release, DNS veya güvenlik olayını kaydeder.
Gerçek kişiler, kimlik bilgileri, özel kanıt konumları ve müşteri verisi onaylı
harici olay sisteminde kalır.

## 1. Metadata

| Alan | Değer |
|---|---|
| Olay ID | `<harici-id-veya-sir-icermeyen-id>` |
| Başlık | `<kisa-nesnel-baslik>` |
| Durum | İnceleniyor / Azaltıldı / İzleniyor / Kapalı |
| Önem | `<haricen-onaylanmis-severity>` |
| Başlangıç UTC | `<YYYY-MM-DDTHH:MM:SSZ>` |
| Algılama UTC | `<YYYY-MM-DDTHH:MM:SSZ>` |
| Azaltılma UTC | `<deger-veya-BILINMIYOR>` |
| Kapanış UTC | `<deger-veya-ACIK>` |
| Olay komutanı | REPO DIŞI / ATA |
| Teknik sorumlu | REPO DIŞI / ATA |
| İletişim sorumlusu | REPO DIŞI / ATA |
| Harici olay kaydı | REPO DIŞI / REFERANS ATA |

## 2. Kanıt sözlüğü

- **DOĞRULANMIŞ:** Korunmuş ürün çıktısı, değişmez release metadata'sı,
  incelenmiş kod veya yeniden üretilebilir test ifadeyi kanıtlar.
- **KULLANICI GÖZLEMİ:** Operatör veya müşteri belirtiyi bildirmiş ya da
  kaydetmiştir.
- **ÇIKARIM:** Kanıtla uyumludur fakat bağımsız kanıtlanmamıştır.
- **BİLİNMİYOR:** Yeterli kanıt yoktur.

Her önemli ifade ve zaman çizelgesi satırı kanıt sınıfını göstermelidir.
Çıkarımı sessizce gerçeğe dönüştürmeyin.

## 3. Yönetici özeti

Neyin bozulduğunu, ne zaman başladığını, etkilenen müşteri yolculuğunu ve
güncel doğrulanmış durumu en fazla üç kısa paragrafta anlatın. Yalnız patch veya
release var diye çözüm ilan etmeyin.

## 4. Etki

| Konu | Sonuç | Kanıt sınıfı |
|---|---|---|
| Etkilenen düğüm/servisler | `<deger>` | `<sinif>` |
| Etkilenen release/commit'ler | `<deger>` | `<sinif>` |
| Müşteriye görünür etki | `<deger>` | `<sinif>` |
| DNS/veri erişimi | `<deger-veya-BILINMIYOR>` | `<sinif>` |
| Veri bütünlüğü veya kaybı | `<deger-veya-BILINMIYOR>` | `<sinif>` |
| Süre | `<deger-veya-BILINMIYOR>` | `<sinif>` |

Ortam sınıfı ve müşteri-verisi durumu haricen kararlaştırılmadıysa BİLİNMİYOR
olarak kaydedin.

## 5. UTC zaman çizelgesi

| UTC zamanı | Kanıt sınıfı | Olay | Kanıt referansı |
|---|---|---|---|
| `<zaman-damgasi>` | `<sinif>` | `<gercek-gozlem-veya-islem>` | `<sir-icermeyen-referans>` |

UTC kullanın, sıralamayı koruyun ve olay zamanı ile algılama zamanını ayırın.
Ham logları ve özel URL'leri repo dışında tutun.

## 6. Algılama ve belirtiler

- Olayın nasıl algılandığı.
- Kullanıcının ne gördüğü.
- Sunucunun yetkili olarak ne bildirdiği.
- Hangi sinyallerin eksik, yanıltıcı veya gecikmiş olduğu.
- İzleme ya da destek escalation'ının çalışıp çalışmadığı.

## 7. Sınırlama

Ek etkiyi önlemek için kullanılan exact ve sınırlı işlemleri yazın. Her canlı
mutasyona kimin yetki verdiğini harici kayıtta belirtin. Ad-hoc veritabanı
düzenlemesi, marker silme, release-floor değişimi veya daemon müdahalesini kabul
edilmiş kurtarma prosedürü olarak yazmayın.

## 8. Teknik teşhis

### Doğrulanmış kök neden

Her nedeni kod, değişmez release kanıtı, yeniden üretilebilir test veya sınırlı
canlı kanıta bağlayın.

### Katkıda bulunan etkenler

Etkiyi artıran süreç, gözlenebilirlik, test, belge ve sahiplik boşluklarını kök
neden diye adlandırmadan belirtin.

### Reddedilen hipotezler

Kanıtla çürütülen önemli hipotezleri tekrar edilmemeleri için kaydedin.

## 9. Kurtarma ve veri bütünlüğü

- Exact kurtarma yolu ve otoritesi.
- Özel yol veya sır vermeden snapshot/rollback kimliği.
- Panel, agent, UI, şema ve servis durumu doğrulaması.
- DNS katalog, AXFR, UDP/TCP SOA ve harici çözümleme doğrulaması.
- Veritabanı bütünlüğü ve veri kaybının kanıtlanıp kanıtlanmadığı ya da
  BİLİNMİYOR kaldığı.
- Kalan bozuk veya doğrulanmamış davranış.

## 10. Düzeltici işlemler

| ID | İşlem | Öncelik | Durum | Kabul kanıtı | Sorumlu / hedef |
|---|---|---|---|---|---|
| `<id>` | `<sinirli-islem>` | `<oncelik>` | AÇIK | `<nesnel-cikis-testi>` | REPO DIŞI / ATA |

Her işlem tek hesap verebilir harici sorumlu, hedef tarih ve nesnel çıkış testi
gerektirir. Kod yayımlamak canlı kabulü tamamlamakla aynı şey değildir.

## 11. Müşteri ve paydaş iletişimi

Her bildirim, güncelleme ve kapanış mesajının UTC zamanını ve sır içermeyen
konusunu harici olay sistemine kaydedin. Kimlik bilgisi, kişisel veri, özel
bildirim gerektiren exploit ayrıntısı veya dayanaksız garanti paylaşmayın.

## 12. Kanıt indeksi ve gizleme

| Kanıt | Kapsam | Repoya uygun sonuç | Harici referans |
|---|---|---|---|
| `<oge>` | `<sinirli-aralik-konu>` | `<sir-icermeyen-sonuc>` | REPO DIŞI |

Private key, token, parola, ham veritabanı, sınırsız log, müşteri kaydı veya
kişisel bilgi içeren ekran görüntüsünü asla eklemeyin.

## 13. Kapanış ve değerlendirme

Olay yalnız şu koşullarda kapanır:

- terminal canlı durum bağımsız doğrulanır;
- etki ve veri bütünlüğü BİLİNMİYOR değerleri dahil açıktır;
- düzeltici işlemler tamamlanır veya resmen devredilir/risk-kabul edilir;
- risk sicili ve tarihli canlı durum kaydı güncellenir;
- harici olay rolleri ve kabul kaydedilir; ve
- takip öğrenimi test, runbook kuralı veya sahipli işe dönüşür.

| Onay | Değer |
|---|---|
| Teknik doğrulama | REPO DIŞI / ATA |
| Operasyonel kabul | REPO DIŞI / ATA |
| Uygunsa güvenlik incelemesi | REPO DIŞI / ATA |
| Kapanış UTC | `<zaman-damgasi-veya-ACIK>` |
