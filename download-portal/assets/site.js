"use strict";

const getNode = (id) => document.getElementById(id);
const setText = (id, value) => {
  const node = getNode(id);
  if (node) node.textContent = value;
};

const englishText = new Map([
  ["Ana içeriğe geç", "Skip to main content"],
  ["Özellikler", "Features"],
  ["Paneli keşfet", "Explore the panel"],
  ["Kurulum", "Install"],
  ["Sürüm", "Release"],
  ["Destek", "Support"],
  ["Güvenlik", "Security"],
  ["Ücretsiz kur", "Install free"],
  [
    "Linux sunucular için modern hosting paneli",
    "A modern hosting panel for Linux servers",
  ],
  ["Hosting yönetimi", "Hosting management"],
  ["tek ekranda kolay.", "made simple in one place."],
  [
    "Web siteleri, WordPress, alan adları, e-posta, DNS, veritabanları ve yedekler. CelikPanel, günlük hosting işlerini anlaşılır bir arayüzde bir araya getirir.",
    "Websites, WordPress, domains, email, DNS, databases and backups. CelikPanel brings everyday hosting work together in one clear interface.",
  ],
  ["CelikPanel’i kur", "Install CelikPanel"],
  ["Paneli incele", "See the panel"],
  ["Ücretsiz alpha", "Free alpha"],
  ["Tek komutla kurulum", "One-command setup"],
  ["Türkçe arayüz", "Turkish and English"],
  ["GENEL", "GENERAL"],
  ["Genel bakış", "Overview"],
  ["Siteler", "Websites"],
  ["Uygulamalar", "Applications"],
  ["E-posta", "Email"],
  ["Veritabanları", "Databases"],
  ["Yedekler", "Backups"],
  ["Servisler", "Services"],
  ["Sunucu çevrimiçi", "Server online"],
  ["12 Ağustos, Çarşamba", "Wednesday, August 12"],
  ["Günaydın, Ali 👋", "Good morning, Ali 👋"],
  ["Sunucunuz sağlıklı çalışıyor.", "Your server is running smoothly."],
  ["+ Yeni site", "+ New website"],
  ["Web siteleri", "Websites"],
  ["5 yayında", "5 online"],
  ["Kaynak kullanımı", "Resource usage"],
  ["Bugün alındı", "Created today"],
  ["Web sitelerim", "My websites"],
  ["Son eklenen projeler", "Recently added projects"],
  ["Tümünü gör", "View all"],
  ["Yayında", "Online"],
  ["Hazırlanıyor", "Preparing"],
  ["Sunucu durumu", "Server health"],
  ["Canlı kaynak kullanımı", "Live resource usage"],
  ["Canlı", "Live"],
  ["Çalışma süresi", "Uptime"],
  ["Bellek", "Memory"],
  ["WordPress hazır", "WordPress is ready"],
  ["Kurulum tamamlandı", "Installation complete"],
  ["SSL aktif", "SSL active"],
  ["Otomatik yenilenir", "Renews automatically"],
  ["Ajanslar için", "For agencies"],
  ["Freelancer’lar için", "For freelancers"],
  ["Hosting sağlayıcıları için", "For hosting providers"],
  ["Kendi sunucusunu yönetenler için", "For server owners"],
  ["TEK PANEL, TÜM HOSTING İŞLERİ", "ONE PANEL FOR ALL YOUR HOSTING WORK"],
  ["Sunucunuzu yönetmek için", "Everything you need"],
  ["ihtiyacınız olan her şey.", "to manage your server."],
  [
    "Dağınık araçlar ve ezberlenen komutlar yerine, günlük işlerinizi tek ve anlaşılır bir panelden yönetin.",
    "Replace scattered tools and memorized commands with one clear panel for your daily work.",
  ],
  ["Web siteleri ve uygulamalar", "Websites and applications"],
  [
    "PHP, WordPress, Laravel ve Node.js projelerini oluşturun; alan adı, SSL ve çalışma sürümünü aynı yerden yönetin.",
    "Create PHP, WordPress, Laravel and Node.js projects, then manage domains, SSL and runtime versions in one place.",
  ],
  ["Profesyonel e-posta", "Professional email"],
  [
    "Alan adınıza bağlı posta kutuları oluşturun; teslimat, kimlik doğrulama ve spam korumasını yönetin.",
    "Create mailboxes for your domains and manage delivery, authentication and spam protection.",
  ],
  ["Aktif", "Active"],
  [
    "MariaDB ve PostgreSQL veritabanlarını, kullanıcılarını ve erişim izinlerini birkaç tıklamayla hazırlayın.",
    "Set up MariaDB and PostgreSQL databases, users and access permissions in a few clicks.",
  ],
  ["Alan adı, DNS ve SSL", "Domains, DNS and SSL"],
  [
    "DNS kayıtlarını düzenleyin, ücretsiz TLS sertifikası alın ve yönlendirmeleri tek ekrandan yönetin.",
    "Edit DNS records, issue free TLS certificates and manage redirects from one screen.",
  ],
  ["SSL ile korunuyor", "Protected with SSL"],
  ["Yedekleme ve geri yükleme", "Backup and restore"],
  [
    "Site ve veritabanı yedeklerini planlayın. Gerektiğinde doğru noktaya güvenle geri dönün.",
    "Schedule website and database backups, then restore the right recovery point when needed.",
  ],
  ["Kullanıcılar ve ekipler", "Users and teams"],
  [
    "Yönetici, müşteri ve ekip üyelerine yalnızca ihtiyaç duydukları alanları gösterin.",
    "Show administrators, customers and team members only the areas they need.",
  ],
  ["SİTENİZİ HIZLA YAYINA ALIN", "LAUNCH YOUR WEBSITE FASTER"],
  [
    "Fikirden çalışan siteye, birkaç adımda.",
    "From idea to a live website in a few steps.",
  ],
  [
    "Sunucu detaylarında kaybolmadan projenize odaklanın. CelikPanel gerekli servisleri sizin seçiminize göre hazırlar.",
    "Focus on your project instead of server details. CelikPanel prepares the services that match your choices.",
  ],
  ["Alan adınızı ekleyin", "Add your domain"],
  [
    "Yeni veya mevcut alan adınızı bağlayın.",
    "Connect a new or existing domain.",
  ],
  ["Uygulamanızı seçin", "Choose your application"],
  [
    "WordPress, PHP veya Node.js ile başlayın.",
    "Start with WordPress, PHP or Node.js.",
  ],
  ["Yayına alın", "Go live"],
  [
    "DNS ve SSL tamamlandığında siteniz hazır.",
    "Your website is ready when DNS and SSL are complete.",
  ],
  ["Yeni web sitesi", "New website"],
  ["Alan adı", "Domain"],
  ["Uygulama", "Application"],
  ["Yayınla", "Publish"],
  ["Ne kurmak istiyorsunuz?", "What would you like to install?"],
  ["Blog, kurumsal site veya mağaza", "Blog, business website or store"],
  ["Özel PHP sitesi", "Custom PHP website"],
  ["Laravel veya kendi uygulamanız", "Laravel or your own application"],
  ["✓ Uygun", "✓ Available"],
  ["Devam et →", "Continue →"],
  ["HERKES İÇİN ANLAŞILIR", "CLEAR FOR EVERYONE"],
  ["Tek ürün.", "One product."],
  ["Farklı çalışma biçimleri.", "Different ways of working."],
  [
    "İster birkaç müşteri sitesi yönetin, ister büyüyen bir hosting operasyonu kurun; CelikPanel işinize uyum sağlar.",
    "Whether you manage a few client websites or a growing hosting operation, CelikPanel adapts to your work.",
  ],
  ["Ajanslar ve freelancer’lar", "Agencies and freelancers"],
  [
    "Müşteri sitelerini, e-postaları ve yedekleri tek hesaptan izleyin.",
    "Manage client websites, email and backups from one account.",
  ],
  ["Hızlı site kurulumu", "Fast website setup"],
  ["Müşteri erişimi", "Customer access"],
  ["Toplu sunucu görünümü", "Unified server overview"],
  ["En popüler kullanım", "Most popular use case"],
  ["Hosting sağlayıcıları", "Hosting providers"],
  [
    "Paketler, kullanıcılar ve servislerle ölçeklenebilir hosting deneyimi sunun.",
    "Deliver a scalable hosting experience with packages, users and services.",
  ],
  ["Çok kullanıcılı yapı", "Multi-user architecture"],
  ["Rol ve yetki yönetimi", "Roles and permissions"],
  ["Modüler servis kataloğu", "Modular service catalog"],
  ["Sunucu sahipleri", "Server owners"],
  [
    "Komut satırına ihtiyaç duymadan kişisel sunucunuzu düzenli tutun.",
    "Keep your personal server organized without living in the command line.",
  ],
  ["Canlı kaynak takibi", "Live resource monitoring"],
  ["Kolay servis yönetimi", "Simple service management"],
  ["Planlı yedekler", "Scheduled backups"],
  ["HEMEN DENEYİN", "TRY IT NOW"],
  ["Temiz bir sunucu.", "A clean server."],
  ["İki kısa komut.", "Two short commands."],
  [
    "CelikPanel, desteklenen bir Linux test sunucusuna önceden derlenmiş paket olarak kurulur. Hedefte Go, Node.js veya Git gerekmez.",
    "CelikPanel installs as a prebuilt package on a supported Linux test server. The target does not need Go, Node.js or Git.",
  ],
  ["ÖNERİLEN", "RECOMMENDED"],
  ["Güncel sürümü kur", "Install the latest release"],
  ["Kopyala", "Copy"],
  ["SABİT SÜRÜM", "PINNED RELEASE"],
  ["Tekrarlanabilir kurulum", "Reproducible installation"],
  ["Sürüm bilgisi bekleniyor…", "Waiting for release information…"],
  [
    "Alpha sürümü üretim ortamı için hazır değildir. Önce izole bir test sunucusunda değerlendirin.",
    "The alpha release is not production-ready. Evaluate it on an isolated test server first.",
  ],
  ["ŞEFFAF SÜRÜM KANALI", "TRANSPARENT RELEASE CHANNEL"],
  ["Ne kurduğunuzu bilin.", "Know exactly what you install."],
  [
    "Her CelikPanel paketi sürüm, kaynak commit ve SHA-256 özetiyle yayımlanır. İsterseniz güncel paketi, isterseniz sabit bir sürümü kullanın.",
    "Every CelikPanel package is published with its version, source commit and SHA-256 digest. Use the latest package or pin an exact release.",
  ],
  ["HTTPS indirme", "HTTPS download"],
  ["SHA-256 doğrulama", "SHA-256 verification"],
  ["Paket içi manifest", "In-package manifest"],
  ["GÜNCEL ALPHA SÜRÜMÜ", "LATEST ALPHA RELEASE"],
  ["Yükleniyor…", "Loading…"],
  ["Manifest okunuyor", "Reading manifest"],
  ["Yayın tarihi", "Published"],
  ["Kaynak commit", "Source commit"],
  ["Paketi indir", "Download package"],
  ["SHA-256 dosyası", "SHA-256 file"],
  ["GERİ BİLDİRİM VE DESTEK", "FEEDBACK AND SUPPORT"],
  [
    "Bir fikriniz veya karşılaştığınız bir sorun mu var?",
    "Have an idea or found a problem?",
  ],
  [
    "CelikPanel’i birlikte geliştirelim. Hataları ve özellik önerilerini herkese açık formlarla, güvenlik açıklarını ise gizli kanaldan bildirin.",
    "Help us improve CelikPanel. Use the public forms for bugs and feature requests, and the private channel for vulnerabilities.",
  ],
  ["HATA BİLDİR", "REPORT A BUG"],
  ["Bir sorun mu buldunuz?", "Found a problem?"],
  [
    "Sürümünüzü, yeniden üretme adımlarını ve temizlenmiş hata çıktısını paylaşın.",
    "Share your version, reproduction steps and sanitized error output.",
  ],
  ["Hata formunu aç →", "Open the bug form →"],
  ["ÖZELLİK ÖNER", "REQUEST A FEATURE"],
  ["Paneli nasıl iyileştirebiliriz?", "How can we improve the panel?"],
  [
    "İhtiyacınızı ve beklediğiniz kullanıcı deneyimini kısa ve net biçimde anlatın.",
    "Briefly describe your need and the experience you expect.",
  ],
  ["Öneri formunu aç →", "Open the feature form →"],
  ["GİZLİ GÜVENLİK BİLDİRİMİ", "PRIVATE SECURITY REPORT"],
  ["Bir güvenlik açığı mı buldunuz?", "Found a vulnerability?"],
  [
    "Açığı herkese açık issue olarak yazmayın. GitHub üzerinden yalnız bakım ekibinin görebileceği şekilde bildirin.",
    "Do not post it as a public issue. Report it privately to the maintainers through GitHub.",
  ],
  ["Gizli bildirim aç →", "Open a private report →"],
  [
    "Parola, token, özel anahtar, müşteri verisi, gerçek IP adresi veya özel alan adı paylaşmayın.",
    "Never share passwords, tokens, private keys, customer data, real IP addresses or private domains.",
  ],
  ["YENİ NESİL HOSTING PANELİ", "A NEW-GENERATION HOSTING PANEL"],
  [
    "Sunucunuzu daha kolay yönetmeye başlayın.",
    "Start managing your server more easily.",
  ],
  [
    "CelikPanel’i ücretsiz bir test sunucusunda keşfedin.",
    "Explore CelikPanel on a free test server.",
  ],
  ["Kurulum komutunu al →", "Get the install command →"],
  [
    "Web siteleri ve sunucular için sade, modern kontrol paneli.",
    "A simple, modern control panel for websites and servers.",
  ],
  ["Sürümler", "Releases"],
]);

const uiText = {
  tr: {
    ready: "İndirmeye hazır",
    unavailable: "Sürüm bilgisi alınamadı",
    manifestUnavailable: "Manifest şu anda kullanılamıyor",
    exactUnavailable:
      "Sabit sürüm komutu için manifest bağlantısını kontrol edin.",
    copy: "Kopyala",
    copied: "Kopyalandı",
    select: "Metni seçin",
    copyDone: "Kurulum komutu panoya kopyalandı.",
    copyFailed: "Pano kullanılamadı; komutu seçerek kopyalayın.",
  },
  en: {
    ready: "Ready to download",
    unavailable: "Release information unavailable",
    manifestUnavailable: "The manifest is currently unavailable",
    exactUnavailable:
      "Check the manifest connection for the pinned release command.",
    copy: "Copy",
    copied: "Copied",
    select: "Select text",
    copyDone: "The installation command was copied to the clipboard.",
    copyFailed: "Clipboard access failed; select and copy the command.",
  },
};

const ignoredDynamic =
  "#release-version,#release-status,#release-date,#release-commit,#release-sha,#exact-command,[data-copy-label],#copy-status";
const localizedTextNodes = [];
const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
let textNode;
while ((textNode = walker.nextNode())) {
  if (textNode.parentElement && textNode.parentElement.closest(ignoredDynamic))
    continue;
  const raw = textNode.nodeValue || "";
  const match = raw.match(/^(\s*)([\s\S]*?)(\s*)$/);
  const translated = match && englishText.get(match[2]);
  if (translated)
    localizedTextNodes.push({
      node: textNode,
      tr: match[2],
      en: translated,
      before: match[1],
      after: match[3],
    });
}

const localizedAttributes = [
  [
    document.querySelector(".brand"),
    "aria-label",
    "CelikPanel ana sayfa",
    "CelikPanel home",
  ],
  [
    document.querySelector(".site-nav"),
    "aria-label",
    "Ana menü",
    "Main navigation",
  ],
  [
    document.querySelector(".language-switch"),
    "aria-label",
    "Dil seçimi",
    "Language selection",
  ],
  [
    document.querySelector(".product-stage"),
    "aria-label",
    "CelikPanel ürün arayüzü önizlemesi",
    "CelikPanel product interface preview",
  ],
  [
    document.querySelector('[data-copy="latest-command"]'),
    "aria-label",
    "Standart kurulum komutunu kopyala",
    "Copy the standard installation command",
  ],
  [
    document.querySelector('[data-copy="exact-command"]'),
    "aria-label",
    "Sabit sürüm kurulum komutunu kopyala",
    "Copy the pinned release installation command",
  ],
  [
    document.querySelector(".site-footer nav"),
    "aria-label",
    "Alt menü",
    "Footer navigation",
  ],
];

const languageButtons = {
  tr: document.querySelector('[data-language="tr"]'),
  en: document.querySelector('[data-language="en"]'),
};

let currentLanguage = "tr";
let releaseData = null;
let releaseFailure = null;

const storedLanguage = (() => {
  try {
    return window.localStorage.getItem("celikpanel-language");
  } catch {
    return null;
  }
})();
const initialLanguage =
  storedLanguage === "en" || storedLanguage === "tr"
    ? storedLanguage
    : (navigator.language || "tr").toLowerCase().startsWith("tr")
      ? "tr"
      : "en";

const formatPublishedAt = (value) => {
  const publishedAt = new Date(value);
  if (Number.isNaN(publishedAt.getTime())) return value;
  return new Intl.DateTimeFormat(currentLanguage === "en" ? "en-GB" : "tr-TR", {
    dateStyle: "long",
    timeStyle: "short",
    timeZone: "Europe/Istanbul",
  }).format(publishedAt);
};

const renderReleaseState = () => {
  const panel = document.querySelector(".release-panel");
  if (releaseData) {
    if (panel) {
      panel.setAttribute("aria-busy", "false");
      panel.classList.remove("release-error");
    }
    setText("release-status", uiText[currentLanguage].ready);
    setText("release-version", releaseData.version);
    setText("release-date", formatPublishedAt(releaseData.published_at));
    setText("release-commit", releaseData.commit);
    setText("release-sha", releaseData.sha256);
    return;
  }
  if (releaseFailure) {
    if (panel) {
      panel.setAttribute("aria-busy", "false");
      panel.classList.add("release-error");
    }
    setText("release-version", uiText[currentLanguage].unavailable);
    setText("release-status", uiText[currentLanguage].manifestUnavailable);
    setText("release-date", releaseFailure.message);
    setText("exact-command", uiText[currentLanguage].exactUnavailable);
    return;
  }
  setText(
    "release-version",
    currentLanguage === "en" ? "Loading…" : "Yükleniyor…",
  );
  setText(
    "release-status",
    currentLanguage === "en" ? "Reading manifest" : "Manifest okunuyor",
  );
};

const applyLanguage = (language) => {
  currentLanguage = language === "en" ? "en" : "tr";
  document.documentElement.lang = currentLanguage;
  document.title =
    currentLanguage === "en"
      ? "CelikPanel | Simple and Modern Hosting Control Panel"
      : "CelikPanel | Kolay ve Modern Hosting Kontrol Paneli";
  const description = document.querySelector('meta[name="description"]');
  if (description)
    description.content =
      currentLanguage === "en"
        ? "Manage websites, domains, email, databases and your server easily from one CelikPanel interface."
        : "CelikPanel ile web sitelerinizi, alan adlarınızı, e-postalarınızı, veritabanlarınızı ve sunucunuzu tek panelden kolayca yönetin.";
  localizedTextNodes.forEach((binding) => {
    binding.node.nodeValue =
      binding.before + binding[currentLanguage] + binding.after;
  });
  localizedAttributes.forEach(([node, attribute, tr, en]) => {
    if (node) node.setAttribute(attribute, currentLanguage === "en" ? en : tr);
  });
  if (languageButtons.tr)
    languageButtons.tr.setAttribute(
      "aria-label",
      currentLanguage === "en" ? "Turkish" : "Türkçe",
    );
  if (languageButtons.en)
    languageButtons.en.setAttribute("aria-label", "English");
  document.querySelectorAll("[data-language]").forEach((button) => {
    button.setAttribute(
      "aria-pressed",
      String(button.dataset.language === currentLanguage),
    );
  });
  document.querySelectorAll("[data-copy-label]").forEach((label) => {
    label.textContent = uiText[currentLanguage].copy;
  });
  document.querySelectorAll("[data-localized-href]").forEach((link) => {
    const target =
      currentLanguage === "en" ? link.dataset.hrefEn : link.dataset.hrefTr;
    if (target) link.setAttribute("href", target);
  });
  try {
    window.localStorage.setItem("celikpanel-language", currentLanguage);
  } catch {
    /* optional preference */
  }
  renderReleaseState();
};

document.querySelectorAll("[data-language]").forEach((button) => {
  button.addEventListener("click", () =>
    applyLanguage(button.dataset.language),
  );
});

const enableReleaseLink = (id, value) => {
  const node = getNode(id);
  if (!node) return;
  node.setAttribute("href", value);
  node.removeAttribute("aria-disabled");
  node.removeAttribute("tabindex");
};

applyLanguage(initialLanguage);

fetch("/releases/latest.json", { cache: "no-store", credentials: "omit" })
  .then((response) => {
    if (!response.ok) throw new Error("HTTP " + response.status);
    return response.json();
  })
  .then((release) => {
    releaseData = release;
    enableReleaseLink("archive-link", release.archive_url);
    enableReleaseLink("checksum-link", release.checksum_url);
    setText(
      "exact-command",
      "curl --fail --show-error --location --proto '=https' --tlsv1.2 https://celikpanel.net/get.sh -o /tmp/celikpanel-get.sh\n" +
        "sh /tmp/celikpanel-get.sh --version " +
        release.version,
    );
    const exactCopy = getNode("exact-copy");
    if (exactCopy) exactCopy.disabled = false;
    renderReleaseState();
  })
  .catch((error) => {
    releaseFailure = error;
    renderReleaseState();
  });

document.addEventListener("click", async (event) => {
  const target = event.target;
  if (!(target instanceof Element)) return;
  const button = target.closest("[data-copy]");
  if (!(button instanceof HTMLButtonElement) || button.disabled) return;
  const source = getNode(button.dataset.copy);
  if (!source) return;
  const label = button.querySelector("[data-copy-label]");
  const status = getNode("copy-status");
  const resetLabel = () => {
    if (label) label.textContent = uiText[currentLanguage].copy;
    button.classList.remove("copied");
  };
  try {
    await navigator.clipboard.writeText(source.textContent || "");
    if (label) label.textContent = uiText[currentLanguage].copied;
    if (status) status.textContent = uiText[currentLanguage].copyDone;
    button.classList.add("copied");
  } catch {
    if (label) label.textContent = uiText[currentLanguage].select;
    if (status) status.textContent = uiText[currentLanguage].copyFailed;
  }
  window.setTimeout(resetLabel, 1800);
});
