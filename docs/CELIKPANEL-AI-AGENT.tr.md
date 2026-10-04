# CelikPanel AI Agent

*[English](CELIKPANEL-AI-AGENT.md) · Ürün ve güvenlik yol haritası*

## Durum ve kullanıcı yönlendirmesi — 14 Eylül 2026

**Planlanan yetenek; bu belge yayımlanmış bir AI entegrasyonu iddiası değildir.**
Kullanıcı; yardımcı olan, isteği üzerine paneli kullanan ve sorun çözen bir ajan
istedi. Bu, [D-025](DECISIONS.md#d-025--resilience-is-a-core-contract-not-an-incident-patch)
ve [dayanıklılık sözleşmesindeki](RESILIENCE-CONTRACT.tr.md) operatör mimarisinin
parçasıdır. Model sağlayıcısı, dağıtım, fiyatlandırma ve üretim verilerinin saklanması
uygulama aşamasında kararlaştırılacaktır.

Model önerir ve açıklar; kuralları belirli işlem katmanı yetkilendirir, yürütür,
doğrular ve kurtarır. Model kesintisi kabul edilmiş işleri, kurtarmayı veya
barındırılan hizmetleri durduramaz. Bilinen, kesilmiş bir işlemin otomatik
kurtarması, modelin kalıcı kanıtları tahmin etmesini veya yeniden kurmasını
gerektiremez.

## Amaç

CelikPanel AI Agent; mevcut durumu açıklayan, plan hazırlayan ve gerekli onay
alındıktan sonra web arayüzüyle aynı kimlik doğrulamalı API'ler üzerinden
CelikPanel işlemleri yapan, yalnızca panele özel bir operatördür.

Genel amaçlı bir asistan değildir. CelikPanel dışındaki istekleri reddetmeli;
sınırsız shell, SSH, keyfi ağ veya doğrudan veritabanı erişimi hiçbir zaman
almamalıdır.

## Değişmez sınırlar

- Her araç, tipli ve izin listesine alınmış bir CelikPanel API işlemidir.
- Yetki ve tenant kapsamı her araç çağrısında yeniden değerlendirilir.
- Agent; kota, abonelik hakkı, çakışma veya güvenlik ön kontrolünü aşamaz.
- Salt okunur teşhis hemen çalışabilir. Her değişiklik görünür bir planla başlar
  ve paneldeki eşdeğer işlemin onay politikasını izler.
- Kaldırma, silme, firewall değişikliği, geri yükleme, sertifika değiştirme ve
  DNSSEC gibi yüksek etkili işlemler açık onay gerektirir.
- İşlemler normal kalıcı işlem defterini, kilitleri, ilerleme olaylarını, iptali
  ve denetim günlüğünü kullanır. Model komut çalıştırmaz.
- Gizli bilgiler kısa ömürlü referanslarla temsil edilir; prompt, konuşma veya
  modelin görebildiği araç sonuçlarına konmaz.
- CelikPanel dışındaki istek başka asistana veya araca aktarılmadan reddedilir.

## Etkileşim modeli

1. Oturumdaki kullanıcı, rol, abonelik ve seçili sunucu/domain çözülür.
2. Salt okunur panel API'leriyle güncel durum toplanır.
3. Beklenen değişiklikleri, riskleri ve geri alma bilgisini içeren somut plan
   gösterilir.
4. Paneldeki eşdeğer işlemin onay politikası uygulanır. Kabul edilmiş, sınırları
   belli planın kaydedilen kapsamı yetkili kalır; yeniden bağlanmak, ilerleme
   izlemek veya aynı işlemi sürdürmek için tekrar onay istenmez. Değişen kapsam
   veya yeni gereken yüksek etkili eylem ayrıca değerlendirilir.
5. İstemci istek kimliği ve idempotency anahtarıyla tipli işlemler gönderilir.
6. Deftere dayalı ilerleme elle yapılan işlemlerle aynı işlem görünümünde
   gösterilir. Gezinme ve yetkili durum/kurtarma yüzeyi erişilebilir kalır;
   model akışının veya tarayıcı bağlantısının kesilmesi kullanıcıyı kilitlemez.
7. Başarı bildirilmeden önce otoriter durum yeniden okunur.
8. Kullanıcı, plan, onay, araç girdileri ve sonuç; gizli bilgiler ayıklanarak
   denetim günlüğüne yazılır.

Agent yalnızca bir komut veya istek döndü diye başarı bildiremez. Son durum
doğrulaması işlemin parçasıdır.

## Ortak yürütme ve kurtarma sözleşmesi

AI adaptörü ekran görüntüsündeki etiketlerden sonuç çıkarmak yerine sürümlenmiş,
tipli panel yeteneklerini kullanır. Her araç; sunucu/kaynak, kabul edilmiş plan
ve işlem kimlikleri, gözlemin kaynağı ve zamanı, bilinen sonuç, izinli sonraki
eylemler ve kimin işlem yapacağını bildirir. Arayüz ve AI adaptörü aynı backend
kararlarını kullanır.

- `unknown`, `unavailable`, `waiting`, `failed`, `recovered` ve `verified` ayrı
  tutulur. “İstek kabul edildi”, “servis sağlıklı” anlamına gelmez.
- Değişiklik gönderilmeden istek/idempotency kimliği kalıcı kaydedilir. Yanıt
  kaybolursa yeni istek düşünülmeden aynı işlemin sonucu uzlaştırılır.
- Kurtarma, işlemin tanımlı ve sürüm uyumlu kurtarma yolunu kullanır. Hata
  kaybolsun diye sahiplik kanıtı yeniden yazılmaz, adım tamamlandı işaretlenmez.
- Günlükler, DNS kayıtları, site içeriği ve araç metinleri güvenilmeyen kanıttır.
  Yetki veremez, planı değiştiremez veya çalıştırılacak araç talimatı olamaz.
- Gizli bilgi referansları güvenilir yürütücüde çözülür. Harici model isteğinden
  önce hem teşhis girdileri hem sonuçlar ayıklanır.
- Dış önkoşul somut açıklanır: örneğin gereken PTR değeri, gözlenen değer ve
  sağlayıcıda yapılacak işlem. Engellenmiş kurulum döngüde yeniden denenmez;
  bağlı olmayan sağlayıcıyı modelin değiştirebildiği iddia edilmez.
- Normal Agent erişilemiyorsa yalnız gerçekten uygulanmış, ayrıca kimlik
  doğrulanan durum/kurtarma yetenekleri sunulur. Sınırsız root shell'e geçilmez.
- Kurulu panel güncellemelerinde [AGENTS.md](../AGENTS.md) içindeki yalnız
  kullanıcının başlatması kuralı korunur. AI yardımı istemek, ajanın dolaylı
  yoldan panel güncellemesi başlatmasına izin vermez.

Değişiklik yapabilen önizleme ilgili P0 kabul işlerine bağlıdır: gerçek
güncelleme/geri alma kanıtı (P0.1), tipli erişim ve kurtarma erişilebilirliği
(P0.2), bağımsız kurtarma (P0.3), ortak kanıt şemaları (P0.4) ve yerel hizmet
bağımsızlığı (P0.5). Salt okunur danışman bu çalışmalarla birlikte geliştirilebilir;
sınırları görünür kalmalıdır. Model eklemek hiçbir P0 işini kapatmaz.

## Ürün ve abonelik kapısı

Yetenek yalnızca arayüzde gizlenerek değil, sunucu özellik bayrağı ve abonelik
hakkıyla kontrol edilir.

- Erken önizleme: güvenlik ve kullanılabilirlik ölçülürken özellik bayrağı tüm
  planlara erişim verebilir.
- Ticari sürüm: `ai_agent` hakkı yalnızca seçilen Pro/Premium planlara verilir.
- Hakkın kaldırılması yeni konuşma ve değişiklikleri engeller; agent'ın daha önce
  oluşturduğu kaynaklara zarar vermez.
- Kullanım sınırı, model seçimi ve maliyet hesabı abonelik politikasına aittir;
  yetkilendirme bütün planlarda aynıdır.

## Teslim aşamaları

### Aşama 0 — sözleşme ve tehdit modeli

- İzinli araç şeması tanımlanır; her araç salt okunur, geri alınabilir değişiklik,
  yüksek etkili değişiklik veya desteklenmiyor olarak sınıflandırılır.
- Prompt injection, tenant aşımı, gizli bilgi sızıntısı ve confused-deputy
  testleri eklenir.
- Konuşma ve denetim olayları için saklama ve ayıklama kuralları belirlenir.

### Aşama 1 — salt okunur danışman

- Mevcut panel verileriyle DNS, SSL, posta, servis ve yedek durumunu açıklar.
- Her öneriyi paneldeki tam ekrana bağlar.
- Model değişiklik istese bile sunucu bütün değişiklikleri reddeder.

### Aşama 2 — onaylı işlemler

- Önce küçük ve geri alınabilir bir araç kümesi açılır.
- Panelin yetkilendirme, ön kontrol, işlem defteri ve denetim yolları tekrar
  kullanılır.
- Görünür onay gerekir; otoriter durum doğrulanana kadar ilerleme gösterilir.

### Aşama 3 — abonelik ürünü

- Abonelik hakkı ve kullanım kotası uygulanır.
- Model sağlayıcısı, bütçe, saklama ve acil kapatma için operatör kontrolleri
  eklenir.
- Araç izin listesi yalnızca önceki küme saldırgan testlerden ve üretim
  telemetrisinden güvenli sonuç aldıktan sonra genişletilir.

## Değişiklik yapabilen önizleme için çıkış ölçütleri

- Hiçbir araç, oturumdaki kullanıcının elle ulaşamadığı kaynağa ulaşamaz.
- Hiçbir değişiklik panel API'sini, onay politikasını veya kalıcı işlem defterini
  atlayamaz.
- Tenant aşımı, prompt injection ve gizli bilgi ayıklama testleri geçer.
- Kesilen işlemler panel veya agent yeniden başladığında dürüstçe uzlaştırılır.
- Kabul edilen değişiklikten sonra model zaman aşımı, tekrarlanan araç teslimi
  ve tarayıcının yeniden bağlanması ikinci değişiklik başlatmaz; aynı işlem
  kimliğini korur.
- Model/sağlayıcı kaybı yürütmeyi veya yerel kurtarmayı kesmez; ilerleme ve son
  doğrulanmış sonuç konuşma olmadan da incelenebilir.
- Bildirilen sahiplik çakışması, desteklenen geçiş veya kullanıcı eylemi ilişkiyi
  kanıtlayana kadar engelli kalır; AI kanıtı değiştiremez.
- Aynı senaryo elle kullanılan arayüzde ve AI aracında aynı yetki, önkoşul ve
  kurtarma kararlarını üretir.
- Denetim günlüğü her işlemi kimin istediğini, onayladığını ve yürüttüğünü yeniden
  kurabilir.
- Operatör, normal panel çalışmasını etkilemeden özelliği global olarak
  kapatabilir.
