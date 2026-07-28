# CelikPanel AI Agent

*[English](CELIKPANEL-AI-AGENT.md) · Ürün ve güvenlik yol haritası*

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
4. Plan değişiklik içeriyorsa kullanıcıdan onay istenir.
5. İstemci istek kimliği ve idempotency anahtarıyla tipli işlemler gönderilir.
6. Deftere dayalı ilerleme, elle yapılan işlemlerle aynı tüm-sayfa işlem
   katmanında gösterilir.
7. Başarı bildirilmeden önce otoriter durum yeniden okunur.
8. Kullanıcı, plan, onay, araç girdileri ve sonuç; gizli bilgiler ayıklanarak
   denetim günlüğüne yazılır.

Agent yalnızca bir komut veya istek döndü diye başarı bildiremez. Son durum
doğrulaması işlemin parçasıdır.

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
- Denetim günlüğü her işlemi kimin istediğini, onayladığını ve yürüttüğünü yeniden
  kurabilir.
- Operatör, normal panel çalışmasını etkilemeden özelliği global olarak
  kapatabilir.
