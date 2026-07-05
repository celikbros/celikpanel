# Kurallar — Dil ve İsimlendirme

*Proje standardı · 3 Temmuz 2026 · [English](CONVENTIONS.md)*

CelikPanel, uluslararası okunabilir bir kod tabanı üzerinde Türkçe-öncelikli bir kitleye hizmet eder. Bu belge, projede dilin nasıl kullanılacağının tek doğruluk kaynağıdır. Bağlayıcıdır: yeni işler buna uyar, kod incelemeleri bunu denetler.

---

## Tek cümlelik kural

**Teknik isimler İngilizcedir. Her açıklama ve tüm içerik iki dillidir (Türkçe + İngilizce). Ürünün kendisi çok dillidir; iki ana dili Türkçe ve İngilizcedir.**

---

## 1. Teknik isimler → yalnızca İngilizce

Bir makinenin okuduğu her şey istisnasız İngilizce yazılır:

- Dosya ve dizin adları
- Veritabanı tablo ve sütun adları
- Fonksiyon, metot, değişken, tip ve sabit adları
- API rotaları ve JSON alan adları
- Yapılandırma anahtarları ve ortam değişkenleri

Gerekçe: kod, uluslararası bir standart yüzeydir. İsimleri İngilizce tutmak; kod tabanını taşınabilir, aranabilir ve herhangi bir katkı sağlayıcı için okunabilir kılar.

## 2. Açıklamalar ve içerik → iki dilli (TR + EN)

Bir insanın anlamak için okuduğu her şey iki dilde de bulunur:

- **Dokümanlar** — paralel dosyalar olarak tutulur: `X.md` (İngilizce) ve `X.tr.md` (Türkçe). İkisi senkron tutulur; hiçbiri ikinci sınıf değildir.
- **Kod açıklamaları (comment)** — bir yorum gerektiğinde (niyeti ya da bir kısıtı belirtmek için, asla apaçık olanı anlatmak için değil) önce İngilizce, sonra Türkçe yazılır. Sırf çevrilecek bir şey olsun diye gürültü yorumu eklemeyiz.

Git commit mesajları tek pratik istisnadır: geçmişi okunur tutmak için tek dilde (ekibin çalışma dili Türkçe) yazılır ve asla yapay zekâ ortak-yazar imzası taşımaz.

Her commit, ekibi tam olarak şu ortak-yazar imzalarıyla anar:

```
Co-Authored-By: Mehmet Ömer Efe Çelik <293130995+momerefe@users.noreply.github.com>
Co-Authored-By: Alperen Çelik <89036584+celikalperen@users.noreply.github.com>
```

## 3. Ürün → çok dilli, TR + EN öncelikli

Panel arayüzü tümüyle uluslararasılaştırılır (i18n):

- Component'lerde sabit (hardcoded) doğal-dil metni olmaz. Kullanıcıya görünen her metin bir çeviri anahtarından gelir.
- İki öncelikli yerel ayar yayınlanır ve her zaman eksiksiz tutulur: **`tr`** ve **`en`**.
- Mimari, sonradan başka yerel ayarları kod değişikliği olmadan eklemeyi destekler — yalnızca yeni çeviri kataloğu eklenir.
- Yerel ayar kullanıcı başına seçilir (makul bir varsayılanla) ve hatırlanır.

Bu bir altyapıdır; yol haritasında planlanmıştır (Faz 1), bir Faz 0 özelliği değildir — ancak kural, bundan sonra yazılan her yeni UI metnine uygulanır.

---

## Hızlı başvuru

| Şey | Dil |
|---|---|
| Dosya / dizin adları | İngilizce |
| DB tabloları ve sütunları | İngilizce |
| Fonksiyon, değişken, tip | İngilizce |
| API rotaları ve JSON alanları | İngilizce |
| Config anahtarları, env değişkenleri | İngilizce |
| Dokümanlar | İki dilli (`.md` + `.tr.md`) |
| Kod açıklamaları (varsa) | İki dilli (önce EN, sonra TR) |
| UI metinleri | i18n anahtarları → `tr` + `en` (+ daha fazlası) |
| Commit mesajları | Türkçe, tek dil |
