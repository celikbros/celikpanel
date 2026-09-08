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
  ["Özellikler", "Features"],
  ["Neden CelikPanel", "Why CelikPanel"],
  ["Kanıt", "Proof"],
  ["Kurulum", "Install"],
  ["Sürüm", "Release"],
  ["Destek", "Support"],
  ["Güvenlik", "Security"],
  ["Sürümler", "Releases"],
  ["Ücretsiz kur", "Install it free"],
  ["Neler yaptığını görün", "See what it does"],
  ["Web sitelerinizi, e-postanızı ve sunucunuzu tek panelden yönetin.", "Run your websites, your email and your server from one panel."],
  ["CelikPanel; siteleri, WordPress'i, alan adlarını, e-postayı, DNS'i, veritabanlarını, yedekleri ve güvenlik duvarını tek ekranda toplar. Farkı şudur: her değişikliği önce önizler, sonra deftere yazar. Sunucunuzda ne olduğunu her zaman bilirsiniz.", "CelikPanel brings sites, WordPress, domains, email, DNS, databases, backups and the firewall onto one screen. What sets it apart: it previews every change before making it and writes it to a ledger after. You always know what happened on your server."],
  ["Tek komut, birkaç dakika", "One command, a few minutes"],
  ["Sunucu", "Server"],
  ["Debian 13, Ubuntu 24.04, Arch", "Debian 13, Ubuntu 24.04, Arch"],
  ["Ücret", "Price"],
  ["Alfa boyunca ücretsiz", "Free for the whole alpha"],
  ["sunucu-01 · rota tablosu", "server-01 · route board"],
  ["canlı", "live"],
  ["Posta", "Mail"],
  ["Veritabanı (MariaDB)", "Database (MariaDB)"],
  ["Güvenlik duvarı", "Firewall"],
  ["açık", "clear"],
  ["bekliyor", "waiting"],
  ["kilitli", "locked"],
  ["Panel, bozmadan önce durdu ve sebebini yazdı.", "The panel stopped before it broke anything, and wrote down why."],
  ["Güvenlik duvarı rotası kurulamadı: bu sunucu 7.1.8 çekirdeğiyle çalışıyor ve modülleri artık diskte yok, yeniden başlatılana kadar nftables yüklenemez. Başka her şey kurulu ve çalışıyor.", "The firewall route could not be set: this server is running kernel 7.1.8 and its modules are no longer on disk, so nftables cannot load until it restarts. Everything else is installed and running."],
  ["Bir hosting işini yürütmek için gereken her şey.", "Everything it takes to run a hosting operation."],
  ["Dağınık araçlar, ezberlenen komutlar ve elle düzenlenen yapılandırma dosyaları yerine tek panel. Her biri kurulumdan sonra hazır gelir.", "One panel instead of scattered tools, memorised commands and hand-edited configuration files. Each of these is ready the moment the install finishes."],
  ["Web siteleri ve WordPress", "Websites and WordPress"],
  ["Alan adını bağlayın, WordPress'i tek adımda kurun ya da PHP, Laravel ve Node.js projelerinizi yayına alın. PHP sürümü, SSL ve DNS aynı ekranda.", "Connect a domain, install WordPress in one step, or publish your PHP, Laravel and Node.js projects. The PHP version, the certificate and DNS are on the same screen."],
  ["E-posta", "Email"],
  ["Alan adınıza posta kutuları açın. DKIM, SPF ve TLS panelden ayarlanır; spam koruması kurulumla birlikte gelir. Postfix ve Dovecot kullanır.", "Open mailboxes on your own domain. DKIM, SPF and TLS are set from the panel and spam protection ships with the install. It runs Postfix and Dovecot."],
  ["Veritabanları", "Databases"],
  ["MariaDB ve PostgreSQL veritabanı, kullanıcı ve yetki oluşturun. Panel kendi yönetici hesabını açar; sizin kök parolanıza dokunmaz, parolasını istediğinizde görürsünüz.", "Create MariaDB and PostgreSQL databases, users and grants. The panel opens an admin account of its own; it never touches your root password, and you can see its password whenever you want."],
  ["DNS", "DNS"],
  ["BIND veya PowerDNS; birincil ve ikincil iki sunuculu çift. Zaten çalışan bir DNS sunucunuz varsa hizmet kesilmeden devralınır.", "BIND or PowerDNS, as a primary and secondary pair. If you already run a DNS server, it is adopted without interrupting service."],
  ["SSL sertifikaları", "SSL certificates"],
  ["Let's Encrypt sertifikaları otomatik alınır ve yenilenir. Yenileme unutulmaz, çünkü panelin işidir.", "Let's Encrypt certificates are issued and renewed automatically. Renewal is never forgotten, because it is the panel's job."],
  ["Yedekleme ve geri yükleme", "Backup and restore"],
  ["Planlı yedekler. Sitelerinizi, veritabanlarınızı ve panelin kendi durumunu tek mühürlü arşivde alır; temiz bir sunucuya olduğu gibi geri yükler.", "Scheduled backups. Your sites, your databases and the panel's own state go into one sealed archive, and come back onto a clean server exactly as they were."],
  ["Kullanıcılar ve roller", "Users and roles"],
  ["Yönetici, bayi, müşteri ve ek kullanıcı hesapları. Herkes yalnız kendi sitelerini ve kutularını görür.", "Administrator, reseller, customer and additional user accounts. Everyone sees only their own sites and mailboxes."],
  ["Güvenlik duvarı ve VPN", "Firewall and VPN"],
  ["nftables ile varsayılan-reddet kurallar, WireGuard eşleri ve servis bazlı port açma; hepsi panelden, komut satırına inmeden.", "Default-deny rules with nftables, WireGuard peers and per-service ports, all from the panel and none from a shell."],
  ["Aynı işler, bambaşka bir güvenlik payı.", "The same jobs, a very different margin of safety."],
  ["Panellerin çoğu aynı özellik listesini sayar. Fark, bir şey ters gittiğinde ortaya çıkar. CelikPanel'i ayıran sekiz nokta:", "Most panels list the same features. The difference shows when something goes wrong. Eight places where CelikPanel differs:"],
  ["Alışılmış yöntem ile CelikPanel'in karşılaştırması", "The usual way compared with CelikPanel"],
  ["Durum", "Situation"],
  ["Alışılmış yöntem", "The usual way"],
  ["CelikPanel ile", "With CelikPanel"],
  ["Bir servis kurarken", "Installing a service"],
  ["Kur düğmesine basarsınız; olmazsa hata mesajını okuyup sunucuya SSH ile bakarsınız.", "You press install; if it fails you read the error and go look at the server over SSH."],
  ["Panel önce sorar: bu kurulabilir mi? Engel varsa hiçbir şey değişmez ve engeli düz Türkçe söyler.", "The panel asks first: can this be set? If something blocks it, nothing changes and the blocker is named in plain words."],
  ["Bir şey ters gidince", "When something goes wrong"],
  ["Neyin ne zaman değiştiğini bulmak için günlükleri elle karıştırırsınız.", "You dig through logs by hand to find what changed and when."],
  ["Her ayrıcalıklı değişiklik, sonucuyla birlikte kalıcı bir deftere yazılır. Ne olduğu yazılıdır.", "Every privileged change is written to a durable ledger with its outcome. What happened is on the record."],
  ["Elektrik kesilince", "When the power fails"],
  ["Yarım kalan iş yarım kalır; ekran yine de yeşil görünebilir.", "Half-finished work stays half-finished, and the screen can still look green."],
  ["Panel yarım işi bulur, bitirir ya da geri alır. Dört kesintide karar süresi 7 saniyenin altında kaldı.", "The panel finds the unfinished work and either finishes or reverses it. Across four cuts it decided in under 7 seconds."],
  ["Sunucu tamamen ölünce", "When the server dies outright"],
  ["Yeni sunucuyu elden kurar, yapılandırmayı yeniden yazarsınız. Saatler sürer.", "You build the new server by hand and write the configuration again. It takes hours."],
  ["Tek mühürlü arşivden temiz bir sunucuya 105 saniyede döner. 6 Eylül 2026'da ölçüldü.", "It comes back onto a clean server from one sealed archive in 105 seconds. Measured on 6 September 2026."],
  ["Veritabanı parolanız", "Your database password"],
  ["Çoğu panel kendi erişimini kök hesabın üzerinden kurar.", "Most panels build their own access on top of the root account."],
  ["Panel kendine ayrı bir yönetici hesabı açar. Kök parolanız sizde kalır.", "The panel opens a separate admin account for itself. Your root password stays yours."],
  ["Zaten çalışan bir DNS sunucunuz varsa", "If you already run a DNS server"],
  ["Genelde sıfırdan kurup kayıtları elle taşırsınız; geçişte kesinti olur.", "You usually start over and move the records by hand, and the cutover costs you downtime."],
  ["Çalışan BIND veya PowerDNS devralınır. Devralma boyunca 2508 sorgunun tamamı cevaplandı.", "A running BIND or PowerDNS is adopted. Through the adoption all 2508 queries were answered."],
  ["Depo ekleyin, bağımlılık derleyin, betikleri sırayla çalıştırın.", "Add repositories, compile dependencies, run scripts in order."],
  ["Tek komut. İmzalı ve önceden derlenmiş paket; hedefte Go, Node.js veya Git gerekmez.", "One command. A signed, prebuilt package; the target needs no Go, Node.js or Git."],
  ["Ticari panellerde sunucu ya da hesap başına lisans.", "Commercial panels charge a licence per server or per account."],
  ["Alfa boyunca ücretsiz, sunucu sayısı sınırsız.", "Free for the whole alpha, on as many servers as you like."],
  ["Sağ sütundaki her sayı gerçek bir makinede ölçüldü.", "Every number in the right-hand column was measured on a real machine."],
  ["Kanıt defterine bakın", "See the ledger"],
  ["Kimler kullanıyor", "Who uses it"],
  ["Ajanslar ve freelancer'lar", "Agencies and freelancers"],
  ["Müşteri sitelerini, e-postalarını ve yedeklerini tek hesaptan izleyin. Her müşteri yalnız kendi sitesini görür; siz hepsini.", "Watch client sites, their email and their backups from one account. Each client sees only their own site; you see all of them."],
  ["Hosting sağlayıcıları", "Hosting providers"],
  ["Bayi ve müşteri rolleri, çok kullanıcılı yapı ve modüler servis kataloğuyla ölçeklenebilir bir hosting hizmeti kurun.", "Build a hosting service that scales, with reseller and customer roles, a multi-user structure and a modular service catalogue."],
  ["Kendi sunucusunu yönetenler", "People running their own server"],
  ["Komut satırına inmeden düzenli bir sunucu. Yanlış bir tıkla bozulmaz, çünkü panel yapmadan önce önizler.", "A tidy server without dropping into a shell. One wrong click cannot break it, because the panel previews before it acts."],
  ["Peki bunu nasıl yapıyor?", "So how does it do that?"],
  ["Demiryolu sinyalizasyonundan alınmış tek bir kuralla: bir rota kurulunca onunla çakışan her rota kilitlenir, yani yanlış kolu çekemezsiniz. CelikPanel sunucunuzu aynı disiplinle yönetir.", "With one rule taken from railway signalling: setting a route locks every route that conflicts with it, so you cannot pull the wrong lever. CelikPanel manages your server with the same discipline."],
  ["Önizle", "Preview"],
  ["Panel, değişikliği yapmadan önce sunucuya sorar: bu rota kurulabilir mi? Cevap ya bir engel listesi ya da tek kullanımlık bir onay jetonudur. Engel varsa hiçbir şey değişmez.", "Before changing anything, the panel asks the server: can this route be set? The answer is either a list of blockers or a single-use token. If there is a blocker, nothing changes."],
  ["Taahhüt et", "Commit"],
  ["Yalnızca o jetonla değişiklik yapılır. Bu sırada sunucuda tek bir değişiklik hakkı tutulur; ikinci bir değişiklik sıraya girer, üste binmez.", "Only that token makes the change. While it does, the server holds a single right to change; a second change waits its turn instead of stacking on top."],
  ["Deftere yaz", "Record"],
  ["Her ayrıcalıklı değişiklik, sonucuyla birlikte kalıcı bir deftere yazılır. Elektrik kesilirse panel yarım işi bulur ve bitirir ya da geri alır; yarım kalmış bir şeyi başarılı diye göstermez.", "Every privileged change is written to a durable ledger with its outcome. If the power fails, the panel finds the unfinished work and finishes or reverses it; it never shows a half-done change as a success."],
  ["Kilitleme tablosu", "Interlocking table"],
  ["gösterim · kurallar gerçek", "demonstration · rules are real"],
  ["Hangi isteğin, sunucu hangi durumdayken reddedileceği baştan bellidir. Reddedilen istek sessizce beklemez; panel size hangi durumun onu kilitlediğini söyler.", "Which request is refused, and in which state, is settled in advance. A refused request does not wait in silence: the panel tells you which state locked it."],
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
  ["Sunucuyu oku", "Read server state"],
  ["Kilitli", "Locked"],
  ["Tablo yana kayar.", "The table scrolls sideways."],
  ["Serbest", "Clear"],
  ["İstek reddedilir. Panel, onu hangi durumun kilitlediğini söyler. Üç sebep üç farklı şey ister: paket yöneticisi için bir dakika beklersiniz, süren değişiklik için onun bitmesini; bitmemiş bir kilit ise beklemekle geçmez ve panel bunu açıkça söyler.", "The request is refused, and the panel says which state locked it. The three reasons ask for three different things: for the package manager you wait a minute, for a running change you wait for it to finish; an unfinished lock does not clear by waiting, and the panel says so plainly."],
  ["İstek hemen yapılır. Veritabanı oluşturmak motorun içindeki bir işlemdir, sunucuda değişiklik hakkı gerektirmez; okumak hiçbir zaman beklemez.", "The request is done at once. Creating a database is an operation inside the engine and needs no right to change the server; reading never waits."],
  ["Ölçüldü, kaydedildi, tarihlendi.", "Measured, recorded, dated."],
  ["Bu sayfadaki her sayı gerçek bir makinede ölçüldü ve ürünün risk kaydında tarihli kanıtıyla duruyor. Hiçbiri tahmin değil, hiçbiri yuvarlanmadı.", "Every number on this page was measured on a real machine and stands in the product's risk register with dated evidence. None is an estimate; none was rounded."],
  ["Gerçek makinelerde ölçülen değerler", "Values measured on real machines"],
  ["Ölçüm", "Measurement"],
  ["Değer", "Value"],
  ["Koşul", "Condition"],
  ["Tarih", "Date"],
  ["Felaketten hizmete dönüş", "From disaster back into service"],
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
  ["Temiz bir sunucu, tek komut.", "A clean server, one command."],
  ["CelikPanel, desteklenen bir Linux sunucusuna imzalı ve önceden derlenmiş paket olarak kurulur. Hedefte Go, Node.js veya Git gerekmez. Kurucu paketi indirir, imzasını ve özetini doğrular, sonra kurar.", "CelikPanel installs on a supported Linux server as a signed, prebuilt package. The target needs no Go, Node.js or Git. The installer downloads the package, verifies its signature and digest, then installs."],
  ["Debian 13, Ubuntu 24.04 LTS, Arch Linux", "Debian 13, Ubuntu 24.04 LTS, Arch Linux"],
  ["Linux amd64", "Linux amd64"],
  ["Alfa önizleme: önce ayrı bir test sunucusunda deneyin", "Alpha preview: try it on a separate test server first"],
  ["Güncel sürümü kur", "Install the current release"],
  ["Belirli bir sürümü kurmak istiyorum", "I want to install a specific version"],
  ["Sık sorulanlar", "Common questions"],
  ["Bilmediğimiz bir şeyi bildiğimizi söylemiyoruz. Cevabı henüz olmayan sorularda da bunu açıkça yazıyoruz.", "We do not claim to know what we do not. Where there is no answer yet, that is what it says."],
  ["Şimdi üretimde kullanabilir miyim?", "Can I use it in production today?"],
  ["Hayır. CelikPanel alfa aşamasında; önce ayrı bir test sunucusunda deneyin. Üretime hazır olduğunu söyleyeceğimiz gün, dayandığı kanıtla birlikte bu sayfada yazacak.", "No. CelikPanel is in alpha; try it on a separate test server first. The day we call it production-ready, this page will say so along with the evidence behind it."],
  ["Alfadan sonra ücretli mi olacak?", "Will it cost money after the alpha?"],
  ["Alfa boyunca ücretsiz ve kurabileceğiniz sunucu sayısı sınırsız. 1.0 fiyatlandırması henüz açıklanmadı; açıklandığında ilk burada duyurulacak.", "It is free for the whole alpha, on as many servers as you like. Pricing for 1.0 has not been announced; when it is, it will be announced here first."],
  ["cPanel veya Plesk'ten taşıyabilir miyim?", "Can I migrate from cPanel or Plesk?"],
  ["Otomatik taşıma aracı henüz yok. Çalışan bir DNS sunucusu kesinti olmadan devralınabiliyor; site dosyaları ve posta kutuları şimdilik elle taşınır.", "There is no automatic migration tool yet. A running DNS server can be adopted without downtime; site files and mailboxes are moved by hand for now."],
  ["Hangi sunucularda çalışır?", "Which servers does it run on?"],
  ["Debian 13, Ubuntu 24.04 LTS ve Arch Linux üzerinde, Linux amd64 mimarisinde. Kurulum üçünde de aynı komutla yapılır ve her sürüm üçünde birden sınanır.", "Debian 13, Ubuntu 24.04 LTS and Arch Linux, on Linux amd64. The install is the same command on all three, and every release is tested on all three."],
  ["Verilerim nerede duruyor?", "Where does my data live?"],
  ["Panel de veriler de kendi sunucunuzda. Panel dışarıyla yalnız sürüm bildirimini ve kurulum paketini indirmek için konuşur.", "The panel and your data both live on your own server. The only things it fetches from outside are the release manifest and the install package."],
  ["Vazgeçersem kaldırabilir miyim?", "Can I remove it if I change my mind?"],
  ["Evet. Kurulum gibi kaldırma da panelin kendi işidir ve ne bıraktığı deftere yazılır.", "Yes. Removal, like installation, is the panel's own job, and what it leaves behind is written to the ledger."],
  ["Sabit sürümü kur", "Install a pinned release"],
  ["Komut, betiği bir kez indirir ve doğrular; boru ile kabuğa akıtmaz. Tekrarlanabilir kurulum için sabit sürümü kullanın.", "The command downloads the script once and verifies it; it does not pipe it into a shell. Use the pinned release for a reproducible install."],
  ["Ne kurduğunuzu bilin.", "Know what you installed."],
  ["Her paket sürüm numarası, kaynak commit'i ve SHA-256 özetiyle yayımlanır ve Ed25519 ile imzalanır. Kurucu bu imzayı doğrulamadan tek bir dosya yazmaz.", "Every package is published with its version, source commit and SHA-256 digest, and signed with Ed25519. The installer writes nothing until that signature verifies."],
  ["HTTPS ile indirme", "Download over HTTPS"],
  ["İmzalı sürüm bildirimi", "Signed release manifest"],
  ["SHA-256", "SHA-256"],
  ["SHA-256 doğrulama", "SHA-256 verification"],
  ["Geriye alınamayan sürüm sırası", "A release sequence that cannot roll back"],
  ["Güncel alfa sürümü", "Current alpha release"],
  ["Yayın tarihi", "Published"],
  ["Kaynak commit", "Source commit"],
  ["Paketi indir", "Download the package"],
  ["SHA-256 dosyası", "SHA-256 file"],
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
    exactWaiting: "Sürüm bilgisi bekleniyor…",
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
    exactWaiting: "Waiting for release information…",
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
  setText("exact-command", uiText[currentLanguage].exactWaiting);
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
        ? "CelikPanel runs your websites, WordPress, email, DNS, databases and backups from one panel, and previews every change before making it."
        : "CelikPanel; web sitelerinizi, WordPress'i, e-postayı, DNS'i, veritabanlarını ve yedekleri tek panelden yönetir, her değişikliği yapmadan önce önizler.";
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
