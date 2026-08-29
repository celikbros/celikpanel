# Frontend Mimarisi — Roller ve Yerleşim

*Tasarım belgesi · 3 Temmuz 2026 · [English](UI_ARCHITECTURE.md)*

Bu belge tek bir soruyu yanıtlar: dört kullanıcı rolüyle (Yönetici, Bayi, Müşteri, Ek Kullanıcı — bkz. [ROLES.tr.md](ROLES.tr.md)) arayüz nasıl yapılandırılır? Ayrı uygulamalar mı? Rol başına bir masterpage mi? Yoksa tek bir kalıtımsal (inherited) kabuk mu?

---

## Karar: yetkilere göre yönlendirilen tek bir kalıtımsal kabuk

**Tek bir uygulama kabuğu vardır. Navigasyon, rotalar ve eylemler, giriş yapan kullanıcının rolü ve yetkilerine göre render edilir. Ayrı uygulamalar ve elle sürdürülen rol-başına masterpage yoktur.**

```
        ┌───────────────────────────────┐
        │   AppShell (tek yerleşim)     │
        │  ┌─────────┬────────────────┐ │
        │  │ Kenar   │  Sayfa alanı   │ │
        │  │ çubuğu  │  (paylaşılan   │ │
        │  │(yetki-  │   özellik      │ │
        │  │ lerden) │   bileşenleri) │ │
        │  └─────────┴────────────────┘ │
        └───────────────────────────────┘
                    ▲
                    │ okur
        ┌───────────────────────────┐
        │ AuthContext: { role,      │
        │   capabilities[] }        │
        └───────────────────────────┘
```

Giriş yapan kullanıcı bir **yetki kümesi** taşır. Kenar çubuğu, rotalar ve sayfa içi eylem butonlarının hepsi bu kümeden türetilir. Yöneticinin kabuğu Servisleri, Kullanıcıları ve sunucu araçlarını gösterir; müşterinin aynı kabuğu yalnızca kendi domainlerini, veritabanlarını ve postasını gösterir; ek kullanıcının kabuğu yalnızca ebeveyninin devrettiği dilimleri gösterir. Aynı kabuk, aynı bileşenler — farklı yetki girer, farklı arayüz çıkar.

---

## Diğer iki seçenek neden değil

### ❌ Tamamen ayrı sayfalar / uygulamalar (cPanel modeli)
cPanel, WHM'yi (yönetici/bayi) cPanel'den (son kullanıcı) fiziksel olarak ayırır — iki arayüz, iki kod tabanı kadar tekrar. Bu, [anayasamızın](../ROADMAP.tr.md) tam *tersidir*: bir domain ekranını iki kez yazmak, bir hatayı iki kez düzeltmek, öğrenilecek iki zihinsel model demektir. Reddedildi.

### ❌ Rol başına bir masterpage
Dört paralel yerleşim (AdminLayout, ResellerLayout, CustomerLayout, UserLayout) ilk başta düzenli görünür ama hemen birbirinden uzaklaşır: başlığa, bildirim ziline, temaya ya da kabuğa yapılan bir değişiklik dört kez yapılmalı ve kaçınılmaz olarak üçünde yapılır. Ayrıca "tek bir ek yetkiye sahip müşteri"yi beşinci bir yerleşim olmadan ifade edemez. Reddedildi.

### ✅ Tek kalıtımsal kabuk, yetkiye göre
- **Sadelik (Google ilkesi):** tek kabuk, tek zihinsel model, çerçeveyi değiştirmek için tek yer. *Aynı* domain-yönetimi bileşeni, sitesini yöneten bir müşteriye de onu inceleyen bir yöneticiye de hizmet eder.
- **Backend ile tutarlılık:** [ROLES.tr.md](ROLES.tr.md) zaten şöyle der: *"Arayüz, API'nin reddedeceği şeyi gizler."* Bu mimari, o cümlenin hayata geçmiş halidir — arayüzün render ettiği yetki kümesi, backend'in uyguladığı kümenin aynısıdır.
- **Ek kullanıcılar bedavaya gelir:** onlar yalnızca daha küçük bir yetki kümesine sahip bir müşteri kabuğudur. Yeni yerleşim yok, yeni sayfa yok.
- **Genişletilebilir:** ileride bir "dağıtıcı" katmanı ya da yeni bir yetki, yeni bir uygulama değil, bir veri değişikliğidir (yetki kümesine yeni girdi).

---

## Nasıl çalışır

1. **AuthContext** — yüklemede `GET /api/v1/auth/me`, `{ username, role, capabilities }` döndürür. Bu, router'ın üstünde, kökte bir React context'inde yaşar.
2. **Navigasyon kaydı** — her navigasyon öğesini / rotayı gerektirdiği yetkiye eşleyen tek bir bildirimsel liste. Kenar çubuğu, bu listeyi kullanıcının yetkilerine göre süzerek oluşturulur. Rol-başına hiçbir şey sabit kodlanmaz.
3. **Rota koruyucuları** — her rota gerektirdiği yetkiyi kontrol eder. `/services` yazan bir müşteri bozuk bir sayfa görmek yerine yönlendirilir. (Derinlemesine savunma — API zaten çağrıları reddederdi.)
4. **Eylem kısıtlama** — sayfa içi butonlar ("Domain sil", "SSL al") yetkileri aynı şekilde kontrol eder; böylece salt-okunur bir ek kullanıcı veriyi değiştiren kontroller olmadan görür.
5. **Backend doğruluğun kaynağıdır.** Arayüz kısıtlaması kullanılabilirlik içindir, asla güvenlik için değil; her istek yine sunucu tarafında yetkilendirilir (bkz. [ROLES.tr.md](ROLES.tr.md) uygulama bölümü). Kurcalanmış bir frontend hiçbir şey kazanmaz.

---

## Her rol ne görür (aynı kabuk, farklı navigasyon)

| Bölüm | Yönetici | Bayi | Müşteri | Ek Kullanıcı |
|---|:---:|:---:|:---:|:---:|
| Sunucu panosu (CPU/RAM/disk) | ✅ | — | — | — |
| Servisler (kur/başlat/durdur) | ✅ | — | — | — |
| Kullanıcılar & bayiler | ✅ | kendi müşterileri | — | — |
| Kaynak havuzu / planlar | ✅ | ✅ | — | — |
| Domainler & siteler | hepsi | müşterilerininki | kendi | devredilen |
| Veritabanı · Posta · DNS · Dosya | hepsi | müşterilerininki | kendi | devredilen dilimler |
| Hesap ayarları | ✅ | ✅ | ✅ | sınırlı |

---

## Tema (karar verildi)

Panel **hem açık hem koyu temayı** destekler; kullanıcı bir tema değiştiriciyle seçer. Varsayılan, işletim sistemi tercihini (`prefers-color-scheme`) izler, yoksa açık temaya düşer. Mekanik olarak: mevcut `web/src/theme.ts` token'ları CSS özel değişkenlerine bağlanır ve iki tema, *aynı* değişkenlerin iki değer kümesidir — bileşenler değişkenlere başvurur, asla sabit renklere değil. Bu, kabuğun üstündeki dış görünümdür; hiçbir yapıyı değiştirmez.

## Kapsam sınırı

Bu belge **yapı** (iskelet) hakkındadır: tek kabuk, yetkiye göre render. Yukarıdaki tema kararının ötesinde, daha ince **görsel tasarım** (tipografi ölçeği, boşluk ritmi, bileşen stili) konusunda sessiz kalır. Bunlar yeniden tasarım işi başladığında seçilir ve bu mimariyi değiştirmeden üzerine giydirilir — bir yeniden tasarım kabuğu yeniden boyar, yeniden mimarileştirmez.

Mevcut kod bu yapıyı izler. `web/src/nav.ts`, role duyarlı tek navigasyon
registry'sidir; sidebar ve rota erişim kontrolleri aynı girdilerden türetilirken
`Layout.tsx` ortak kabuk olarak kalır. Bu registry bir arayüz sınırıdır,
güvenlik otoritesi değildir: backend kimlik doğrulama, sahiplik ve yetkilendirme
kontrolleri her endpoint için zorunlu kalır.
