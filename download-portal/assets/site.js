"use strict";

const getNode = (id) => document.getElementById(id);
const setText = (id, value) => {
  const node = getNode(id);
  if (node) node.textContent = value;
};

// BEGIN DOWNLOAD COMMAND POLICY
const canonicalReleaseVersionPattern =
  /^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$/;

const isCanonicalReleaseVersion = (value) => {
  if (typeof value !== "string" || !canonicalReleaseVersionPattern.test(value))
    return false;
  const separator = value.indexOf("-");
  if (separator < 0) return true;
  return value
    .slice(separator + 1)
    .split(".")
    .every(
      (identifier) =>
        !/^[0-9]+$/.test(identifier) ||
        identifier === "0" ||
        !identifier.startsWith("0"),
    );
};

const buildInstallCommand = (version = "") => {
  if (version !== "" && !isCanonicalReleaseVersion(version))
    throw new Error("invalid release version");
  const versionArgument = version === "" ? "" : ` --version "${version}"`;
  return `(
  set -eu
  celikpanel_get=$(mktemp)
  cleanup_celikpanel_get() {
    rm -f -- "$celikpanel_get"
  }
  trap cleanup_celikpanel_get EXIT
  trap 'exit 129' HUP
  trap 'exit 130' INT
  trap 'exit 143' TERM
  curl --fail --show-error --location --proto '=https' --tlsv1.2 https://celikpanel.net/get.sh -o "$celikpanel_get"
  if [ "$(id -u)" -eq 0 ]; then
    sh "$celikpanel_get"${versionArgument}
  else
    sudo sh "$celikpanel_get"${versionArgument}
  fi
)`;
};
// END DOWNLOAD COMMAND POLICY

// Every visible Turkish string, keyed exactly as it appears in the markup
// (whitespace normalised), with its English. The page is authored in Turkish
// and switched in place, so the two languages can never drift apart in
// structure - only in words.
const englishText = new Map([
  ["Ana içeriğe geç", "Skip to main content"],
  ["Nasıl çalışır", "How it works"],
  ["Kanıt", "Proof"],
  ["Kurulum", "Install"],
  ["Sürüm", "Release"],
  ["Destek", "Support"],
  ["Güvenlik", "Security"],
  ["Sürümler", "Releases"],
  ["Kurulum komutunu al", "Get the install command"],
  ["Nasıl çalıştığını gör", "See how it works"],

  // hero
  ["Sunucunuzda ne yaptığını kanıtlayan hosting paneli.", "The hosting panel that proves what it did to your server."],
  ["Her değişiklik önce önizlenir, sonra deftere geçer. Kanıtlayamadığı hiçbir şeyi iddia etmez. Ölen bir sunucuyu tek mühürlü dosyadan 105 saniyede hizmete döndürür.", "Every change is previewed first and written to a ledger after. It claims nothing it cannot prove. It brings a dead server back into service from one sealed file in 105 seconds."],
  ["Dağıtımlar", "Distributions"],
  ["Lisans", "Licence"],
  ["Ücretsiz alfa, açık geri bildirim", "Free alpha, open feedback"],
  ["Debian 13, Ubuntu 24.04, Arch", "Debian 13, Ubuntu 24.04, Arch"],
  ["Dil", "Language"],
  ["Türkçe ve İngilizce", "Turkish and English"],

  // route board
  ["sunucu-01 · rota tablosu", "server-01 · route board"],
  ["canlı", "live"],
  ["Posta", "Mail"],
  ["Veritabanı (MariaDB)", "Database (MariaDB)"],
  ["Güvenlik duvarı", "Firewall"],
  ["açık", "clear"],
  ["bekliyor", "waiting"],
  ["kilitli", "locked"],
  ["Güvenlik duvarı rotası kurulamadı.", "The firewall route could not be set."],
  ["Bu sunucu 7.1.8 çekirdeğiyle çalışıyor ve modülleri artık diskte yok; yeniden başlatılana kadar nftables yüklenemez. Başka her şey kurulu ve çalışıyor.", "This server is running kernel 7.1.8 and its modules are no longer on disk; nftables cannot load until it restarts. Everything else is installed and running."],

  // how it works
  ["Bir sinyal kulesi gibi çalışır: rota iste, kilitlenmediğini kanıtla, sonra kur.", "It works like a signal box: request a route, prove it is not locked, then set it."],
  ["Demiryolunda bir rota kurulunca onunla çakışan her rota fiziksel olarak kilitlenir; yanlış kolu çekemezsiniz. CelikPanel sunucunuzu aynı disiplinle yönetir.", "On a railway, setting a route physically locks every route that conflicts with it; you cannot pull the wrong lever. CelikPanel manages your server with the same discipline."],
  ["Önizle", "Preview"],
  ["Panel, değişikliği yapmadan önce sunucuya sorar: bu rota kurulabilir mi? Cevap ya bir engel listesi ya da tek kullanımlık bir onay jetonudur. Engel varsa hiçbir şey değişmez.", "Before changing anything, the panel asks the server: can this route be set? The answer is either a list of blockers or a single-use token. If there is a blocker, nothing changes."],
  ["Taahhüt et", "Commit"],
  ["Yalnızca o jetonla değişiklik yapılır. Bu sırada sunucu tek bir değişiklik kirası tutar; ikinci bir değişiklik sıraya girer, üste binmez.", "Only that token makes the change. While it does, the server holds a single change lease; a second change waits its turn instead of stacking on top."],
  ["Deftere geç", "Record"],
  ["Her ayrıcalıklı değişiklik, sonucuyla birlikte kalıcı bir deftere yazılır. Elektrik kesilirse panel yarım işi bulur ve bitirir ya da geri alır; yarım kalmış bir şeyi başarılı diye göstermez.", "Every privileged change is written to a durable ledger with its outcome. If the power fails, the panel finds the unfinished work and finishes or reverses it; it never shows a half-done change as a success."],
  ["Kilitleme tablosu", "Interlocking table"],
  ["gösterim · kurallar gerçek", "demonstration · rules are real"],
  ["Hangi istek, sunucu hangi durumdayken reddedilir", "Which request is refused while the server is in which state"],
  ["İstediğiniz değişiklik", "The change you ask for"],
  ["Başka bir panel değişikliği sürüyor", "Another panel change is running"],
  ["Paket yöneticisi meşgul", "The package manager is busy"],
  ["Bitmemiş bir değişiklik kilidi tutuyor", "An unfinished change holds the lock"],
  ["Önizleme engel buldu", "The preview found a blocker"],
  ["Servis kur", "Install a service"],
  ["DNS motorunu değiştir", "Switch the DNS engine"],
  ["Güvenlik duvarını aç", "Turn on the firewall"],
  ["Veritabanı oluştur", "Create a database"],
  ["Sunucuyu oku", "Read the server"],
  ["Kilitli", "Locked"],
  ["Tablo yana kayar.", "The table scrolls sideways."],
  ["Serbest", "Free"],
  ["İstek reddedilir ve panel hangi durumun onu kilitlediğini söyler. Üç sebep üç farklı şey ister: paket yöneticisi için bir dakika beklersiniz, süren değişiklik için onun bitmesini; bitmemiş bir kilit ise beklemekle geçmez ve panel bunu açıkça söyler.", "The request is refused and the panel says which state locked it. The three reasons ask for three different things: for the package manager you wait a minute, for a running change you wait for it to finish; an unfinished lock does not clear by waiting, and the panel says so plainly."],
  ["İstek hemen yapılır. Veritabanı oluşturmak motorun içindeki bir işlemdir, sunucu kirası gerektirmez; okumak hiçbir zaman beklemez.", "The request is done at once. Creating a database is an operation inside the engine and needs no server lease; reading never waits."],
  ["Ne yönetir", "What it manages"],
  ["Alan adları ve siteler", "Domains and sites"],
  ["PHP, WordPress ve Node.js projeleri; sürüm, DNS ve TLS aynı yerden.", "PHP, WordPress and Node.js projects; runtime, DNS and TLS from one place."],
  ["DNS", "DNS"],
  ["BIND veya PowerDNS; iki sunuculu çift; çalışan bir sunucu hizmeti kesmeden devralınır.", "BIND or PowerDNS; a two-server pair; a running server is adopted without interrupting service."],
  ["Veritabanları", "Databases"],
  ["MariaDB ve PostgreSQL. Panel kendi hesabını açar; sizin kök parolanıza dokunmaz.", "MariaDB and PostgreSQL. The panel opens an account of its own; it never touches your root password."],
  ["E-posta", "Email"],
  ["Postfix ve Dovecot; DKIM, TLS ve spam koruması tek akışta.", "Postfix and Dovecot; DKIM, TLS and spam protection in one flow."],
  ["TLS", "TLS"],
  ["Let’s Encrypt sertifikaları; yenileme paneldedir.", "Let’s Encrypt certificates; renewal lives in the panel."],
  ["Yedek ve kurtarma", "Backup and recovery"],
  ["Planlı yedekler; kontrol düzlemi tek mühürlü arşiv olarak alınır ve temiz bir sunucuya geri yüklenir.", "Scheduled backups; the control plane is taken as one sealed archive and restored onto a clean server."],
  ["Kullanıcılar ve roller", "Users and roles"],
  ["Yönetici, bayi, müşteri ve ek kullanıcı; herkes yalnız kendi alanını görür.", "Administrator, reseller, customer and additional user; everyone sees only their own scope."],
  ["Güvenlik duvarı ve VPN", "Firewall and VPN"],
  ["nftables ile varsayılan-reddet; WireGuard eşleri panelden.", "Default-deny with nftables; WireGuard peers from the panel."],

  // proof
  ["Ölçüldü, kaydedildi, tarihlendi.", "Measured, recorded, dated."],
  ["Bu sayfadaki her sayı gerçek bir makinede ölçüldü ve ürünün risk kaydında tarihli kanıtıyla duruyor. Hiçbiri tahmin değil, hiçbiri yuvarlanmadı.", "Every number on this page was measured on a real machine and stands in the product's risk register with dated evidence. None is an estimate; none was rounded."],
  ["Gerçek makinelerde ölçülen değerler", "Values measured on real machines"],
  ["Ölçüm", "Measurement"],
  ["Değer", "Value"],
  ["Koşul", "Condition"],
  ["Tarih", "Date"],
  ["Felaketten hizmete dönüş", "Disaster to back in service"],
  ["105 sn", "105 s"],
  ["Birinci sunucu elektrik kesintisiyle öldü; ikincisi tek mühürlü arşivden kuruldu", "The first server died in a power cut; the second was built from one sealed archive"],
  ["6 Eyl 2026", "6 Sep 2026"],
  ["Arşiv yaşı, kayıp yok", "Archive age, nothing lost"],
  ["40,9 sn", "40.9 s"],
  ["Kesinti anında arşivin gerçek yaşı; hiçbir kayıt kaybolmadı", "The archive's real age at the moment of the cut; no record was lost"],
  ["Çalışan BIND devralınırken cevapsız sorgu", "Queries unanswered while adopting a running BIND"],
  ["Sunucu devralma boyunca hizmet vermeyi hiç kesmedi", "The server never stopped answering during adoption"],
  ["5 Eyl 2026", "5 Sep 2026"],
  ["Elle kurulmuş BIND devralınırken", "Adopting a hand-configured BIND"],
  ["Farklar gösterildi, sonra devralındı; yine sıfır kesinti", "Differences shown, then adopted; again zero interruption"],
  ["Elektrik kesintisinden sonra karar süresi", "Time to decide after a power cut"],
  ["≤ 7 sn", "≤ 7 s"],
  ["Dört kesinti; her defasında panel yarım işi kimse dokunmadan çözdü", "Four cuts; each time the panel resolved the unfinished work with nobody touching the machine"],
  ["Kabul turu", "Acceptance run"],
  ["3 dağıtım", "3 distributions"],
  ["Debian 13, Ubuntu 24.04, Arch: kurulum, veritabanı zinciri, yeniden başlatma", "Debian 13, Ubuntu 24.04, Arch: install, the database chain, restart"],
  ["Risk kaydı", "Risk register"],
  ["71 madde", "71 entries"],
  ["Her biri ne bulunduğunu, neden olduğunu ve nasıl kapandığını tarihli kanıtla anlatır", "Each says what was found, why, and how it was closed, with dated evidence"],
  ["sürekli", "ongoing"],
  ["Panel, kanıtlayamadığı bir şeyi asla başarılı olarak göstermez. Bir işlem yarım kaldıysa yarım kaldığını söyler ve sunucuyu kilitli tutar; bu, sessizce yanlış görünen bir yeşilden iyidir.", "The panel never shows as successful anything it cannot prove. If an operation was left half-done it says so and keeps the server locked; that is better than a green light that is quietly wrong."],

  // install
  ["Temiz bir sunucu, tek komut.", "A clean server, one command."],
  ["CelikPanel, desteklenen bir Linux sunucusuna imzalı ve önceden derlenmiş paket olarak kurulur. Hedefte Go, Node.js veya Git gerekmez. Kurucu paketi indirir, imzasını ve özetini doğrular, sonra kurar.", "CelikPanel installs on a supported Linux server as a signed, prebuilt package. The target needs no Go, Node.js or Git. The installer downloads the package, verifies its signature and digest, then installs."],
  ["Debian 13, Ubuntu 24.04 LTS, Arch Linux", "Debian 13, Ubuntu 24.04 LTS, Arch Linux"],
  ["Linux amd64", "Linux amd64"],
  ["Alfa önizleme: önce ayrı bir test sunucusunda deneyin", "Alpha preview: try it on a separate test server first"],
  ["Güncel sürümü kur", "Install the current release"],
  ["Sabit sürümü kur", "Install a pinned release"],
  ["Komut, betiği bir kez indirir ve doğrular; boru ile kabuğa akıtmaz. Tekrarlanabilir kurulum için sabit sürümü kullanın.", "The command downloads the script once and verifies it; it does not pipe it into a shell. Use the pinned release for a reproducible install."],

  // release
  ["Ne kurduğunuzu bilin.", "Know what you installed."],
  ["Her paket sürüm numarası, kaynak commit’i ve SHA-256 özetiyle yayımlanır ve Ed25519 ile imzalanır. Kurucu bu imzayı doğrulamadan tek bir dosya yazmaz.", "Every package is published with its version, source commit and SHA-256 digest, and signed with Ed25519. The installer writes nothing until that signature verifies."],
  ["HTTPS ile indirme", "Download over HTTPS"],
  ["İmzalı sürüm bildirimi", "Signed release manifest"],
  ["SHA-256 doğrulama", "SHA-256 verification"],
  ["Geriye alınamayan sürüm sırası", "A release sequence that cannot roll back"],
  ["Güncel alfa sürümü", "Current alpha release"],
  ["Yayın tarihi", "Published"],
  ["Kaynak commit", "Source commit"],
  ["SHA-256", "SHA-256"],
  ["Paketi indir", "Download the package"],
  ["SHA-256 dosyası", "SHA-256 file"],

  // support
  ["Bir sorun ya da bir fikir", "A problem or an idea"],
  ["Hataları ve önerileri herkese açık formlarla, güvenlik açıklarını yalnız bakım ekibinin göreceği gizli kanaldan bildirin.", "Report bugs and suggestions through the public forms, and security issues through the private channel only the maintainers can see."],
  ["Hata", "Bug"],
  ["Bir sorun mu buldunuz?", "Found a problem?"],
  ["Sürümünüzü, yeniden üretme adımlarını ve temizlenmiş hata çıktısını paylaşın.", "Share your version, the steps to reproduce it, and the sanitised error output."],
  ["Öneri", "Idea"],
  ["Paneli nasıl iyileştirebiliriz?", "How could the panel be better?"],
  ["İhtiyacınızı ve beklediğiniz davranışı kısa ve net anlatın.", "Describe what you need and the behaviour you expected, briefly and clearly."],
  ["Bir güvenlik açığı mı buldunuz?", "Found a security issue?"],
  ["Herkese açık issue olarak yazmayın; gizli bildirim yalnız bakım ekibine ulaşır.", "Do not write it as a public issue; a private report reaches only the maintainers."],
  ["Parola, token, özel anahtar, müşteri verisi, gerçek IP adresi veya özel alan adı paylaşmayın.", "Do not share passwords, tokens, private keys, customer data, real IP addresses or private domain names."],

  // final + footer
  ["Bir test sunucusunda deneyin. Ne yaptığını size gösterir.", "Try it on a test server. It will show you what it did."],
  ["Sunucunuzda ne yaptığını kanıtlayan hosting kontrol paneli.", "The hosting control panel that proves what it did to your server."],
  ["© 2026 CelikPanel", "© 2026 CelikPanel"],
  ["Alfa önizleme · Linux", "Alpha preview · Linux"],
]);

const uiText = {
  tr: {
    ready: "İndirmeye hazır",
    unavailable: "Sürüm bilgisi alınamadı",
    manifestUnavailable: "Manifest şu anda kullanılamıyor",
    exactUnavailable: "Sabit sürüm komutu için manifest bağlantısını kontrol edin.",
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
    exactUnavailable: "Check the manifest connection for the pinned release command.",
    copy: "Copy",
    copied: "Copied",
    select: "Select text",
    copyDone: "The installation command was copied to the clipboard.",
    copyFailed: "Clipboard access failed; select and copy the command.",
  },
};

// Text nodes the release reader owns are never translated by the map; their
// words come from the manifest or from uiText.
const ignoredDynamic =
  "#release-version,#release-status,#release-date,#release-commit,#release-sha,#exact-command,[data-copy-label],#copy-status";

// Bind every translatable text node once. Whitespace inside a node is
// normalised before lookup, so a string wrapped across source lines still
// matches its single-line key - the old walker compared raw text and quietly
// left wrapped paragraphs untranslated.
const localizedTextNodes = [];
const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
let textNode;
while ((textNode = walker.nextNode())) {
  if (textNode.parentElement && textNode.parentElement.closest(ignoredDynamic))
    continue;
  const raw = textNode.nodeValue || "";
  const match = raw.match(/^(\s*)([\s\S]*?)(\s*)$/);
  if (!match) continue;
  const key = match[2].replace(/\s+/g, " ");
  const translated = englishText.get(key);
  if (translated)
    localizedTextNodes.push({
      node: textNode,
      tr: key,
      en: translated,
      before: match[1],
      after: match[3],
    });
}

const localizedAttributes = [
  [document.querySelector(".brand"), "aria-label", "CelikPanel ana sayfa", "CelikPanel home"],
  [document.querySelector(".site-nav"), "aria-label", "Ana menü", "Main navigation"],
  [document.querySelector(".language-switch"), "aria-label", "Dil seçimi", "Language selection"],
  [document.querySelector('[data-copy="latest-command"]'), "aria-label", "Standart kurulum komutunu kopyala", "Copy the standard installation command"],
  [document.querySelector('[data-copy="exact-command"]'), "aria-label", "Sabit sürüm kurulum komutunu kopyala", "Copy the pinned release installation command"],
  [document.querySelector(".site-footer nav"), "aria-label", "Alt menü", "Footer navigation"],
];

// The interlocking marks describe themselves to a screen reader.
const localizedMarkLabels = { kilitli: "locked", serbest: "free" };

const languageButtons = {
  tr: document.querySelector('[data-language="tr"]'),
  en: document.querySelector('[data-language="en"]'),
};

let currentLanguage = "tr";
let releaseData = null;
let releaseFailure = null;

const formatPublishedAt = (value) => {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value || "—";
  return new Intl.DateTimeFormat(currentLanguage === "en" ? "en-GB" : "tr-TR", {
    day: "numeric",
    month: "long",
    year: "numeric",
    timeZone: "UTC",
  }).format(date);
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
  setText("release-version", currentLanguage === "en" ? "Loading…" : "Yükleniyor…");
  setText("release-status", currentLanguage === "en" ? "Reading manifest" : "Manifest okunuyor");
};

const applyLanguage = (language) => {
  currentLanguage = language === "en" ? "en" : "tr";
  document.documentElement.lang = currentLanguage;
  document.title =
    currentLanguage === "en"
      ? "CelikPanel | The hosting control panel that proves its work"
      : "CelikPanel | Kanıtla çalışan hosting kontrol paneli";
  const description = document.querySelector('meta[name="description"]');
  if (description)
    description.content =
      currentLanguage === "en"
        ? "CelikPanel is the hosting control panel that proves what it did to your server: every change is previewed first, recorded after, and nothing is claimed that cannot be proved."
        : "CelikPanel, sunucunuzda ne yaptığını kanıtlayan hosting kontrol panelidir: her değişiklik önce önizlenir, sonra deftere geçer, kanıtlanamayan hiçbir şey iddia edilmez.";
  localizedTextNodes.forEach((binding) => {
    binding.node.nodeValue = binding.before + binding[currentLanguage] + binding.after;
  });
  localizedAttributes.forEach(([node, attribute, tr, en]) => {
    if (node) node.setAttribute(attribute, currentLanguage === "en" ? en : tr);
  });
  document.querySelectorAll(".interlock-table .mark[aria-label]").forEach((mark) => {
    const key = mark.dataset.markKey || mark.getAttribute("aria-label");
    mark.dataset.markKey = key;
    mark.setAttribute(
      "aria-label",
      currentLanguage === "en" ? localizedMarkLabels[key] || key : key,
    );
  });
  if (languageButtons.tr)
    languageButtons.tr.setAttribute("aria-label", currentLanguage === "en" ? "Turkish" : "Türkçe");
  if (languageButtons.en) languageButtons.en.setAttribute("aria-label", "English");
  document.querySelectorAll("[data-language]").forEach((button) => {
    button.setAttribute("aria-pressed", String(button.dataset.language === currentLanguage));
  });
  document.querySelectorAll("[data-copy-label]").forEach((label) => {
    label.textContent = uiText[currentLanguage].copy;
  });
  document.querySelectorAll("[data-localized-href]").forEach((link) => {
    const target = currentLanguage === "en" ? link.dataset.hrefEn : link.dataset.hrefTr;
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
  button.addEventListener("click", () => applyLanguage(button.dataset.language));
});

// A scrolled table says so on its pinned column: the class drives a shadow
// on the sticky cells, the only place a left-edge signal can still paint.
document.querySelectorAll(".table-scroll").forEach((region) => {
  const mark = () => region.classList.toggle("is-scrolled", region.scrollLeft > 0);
  region.addEventListener("scroll", mark, { passive: true });
  mark();
});

const enableReleaseLink = (id, value) => {
  const node = getNode(id);
  if (!node) return;
  node.setAttribute("href", value);
  node.removeAttribute("aria-disabled");
  node.removeAttribute("tabindex");
};

const readStoredLanguage = () => {
  try {
    return window.localStorage.getItem("celikpanel-language");
  } catch {
    return null;
  }
};
const initialLanguage =
  readStoredLanguage() ||
  (navigator.language && navigator.language.toLowerCase().startsWith("tr") ? "tr" : "en");

setText("latest-command", buildInstallCommand());
applyLanguage(initialLanguage);

fetch("/releases/latest.json", { cache: "no-store", credentials: "omit" })
  .then((response) => {
    if (!response.ok) throw new Error("HTTP " + response.status);
    return response.json();
  })
  .then((release) => {
    if (!isCanonicalReleaseVersion(release.version))
      throw new Error("invalid release version");
    releaseData = release;
    enableReleaseLink("archive-link", release.archive_url);
    enableReleaseLink("checksum-link", release.checksum_url);
    setText("exact-command", buildInstallCommand(release.version));
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
