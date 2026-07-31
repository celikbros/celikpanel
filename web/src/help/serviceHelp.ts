// In-panel help content, one entry per component, fully bilingual.
//
// GENERATED CONTENT, HAND-CURATED SHAPE: the entries below were drafted per
// component and reviewed; edit them freely — this file is the single source.
// Why this lives here and not in the i18n key files: help is CONTENT, not
// chrome. It is long-form, per-component, and edited as prose; smearing it
// over hundreds of `help.nginx.tip3` keys would make both the keys and the
// prose unreadable. Both locales sit side by side in one entry so a content
// edit cannot update one language and silently forget the other.
//
// Every component always has help: a specific entry here, or the generic
// entry for its kind. The operator asked for exactly this (25 Jul): pages
// that help instead of scare — "Korkutmasın yardımcı olsun."
//
// Panel içi yardım içeriği, bileşen başına bir kayıt, tamamen iki dilli.
// Yardım, arayüz iskeleti değil İÇERİKtir; iki dil aynı kayıtta yan yana
// durur ki bir düzeltme tek dili güncelleyip öbürünü sessizce unutamasın.
// Her bileşenin her zaman yardımı vardır: özel kayıt ya da türünün geneli.

export interface HelpContent {
    /** What is this component, in 2-3 plain sentences. */
    what: string;
    /** 3-5 short, actionable, panel-first tips. */
    tips: string[];
    /** Common failure modes: what you see -> what to do. */
    troubleshoot: { symptom: string; fix: string }[];
}

export interface LocalizedHelp {
    tr: HelpContent;
    en: HelpContent;
}

export const SERVICE_HELP: Record<string, LocalizedHelp> = {
    'bind': {
        tr: {
            what: 'BIND, internetin en eski ve en yaygın DNS sunucusudur. Bu panelde PowerDNS\'in alternatifidir: barındırdığınız alan adları için DNS sorgularını yanıtlar. İkisi aynı işi aynı portta yaptığından bir sunucuda yalnızca biri çalışır — BIND\'i, ona aşinaysanız ya da özel bir özelliğine ihtiyacınız varsa seçin.',
            tips: [
                'Mümkün oldukça kayıtları DNS sayfasından yönetin — DNS kazalarının çoğu bölge (zone) dosyalarını elle düzenlerken olur.',
                'Listeden bir yapılandırma ya da bölge dosyası düzenlediyseniz, yeniden başlattıktan hemen sonra aşağıdaki günlükten tüm bölgelerin sorunsuz yüklendiğini doğrulayın.',
                'Bölge dosyasındaki tek bir yazım hatası o alan adını tamamen erişilmez yapabilir; bu yüzden her seferinde tek bir şeyi değiştirin.',
                'BIND ile PowerDNS\'in birlikte çalışamayacağını unutmayın — birini kurmak, diğerinin kaldırılması demektir.',
            ],
            troubleshoot: [
                { symptom: 'Bir yapılandırma değişikliğinden sonra başlamıyor (ya da bir alan adı kayboldu).', fix: 'Aşağıdaki günlük, takıldığı dosyayı ve satırı tam olarak yazar. Düzenleyicide son değişikliğinizi geri alın ve yeniden başlatın. Mesaj anlaşılmazsa tahmin yürütmeyin — bir uzmana danışmak için doğru an.' },
                { symptom: 'Barındırılan bir alan adı çözülmüyor.', fix: 'Servisin çalıştığını kontrol edin, DNS sayfasındaki kayıtları doğrulayın ve kayıt firmasının ad sunucularını bu sunucuya yönlendirdiğinden emin olun. Değişikliklerin yayılmasının kaydın TTL süresi kadar zaman alabildiğini de unutmayın.' },
                { symptom: 'Başlamıyor; günlük 53 numaralı portun kullanımda olduğunu söylüyor.', fix: 'PowerDNS (ya da başka bir DNS sunucusu) hâlâ çalışıyor. Bir sunucuya yalnızca bir DNS sunucusu sığar — diğerini durdurun ya da kaldırın.' },
            ],
        },
        en: {
            what: 'BIND is the internet\'s oldest and most widely used DNS server. On this panel it\'s the alternative to PowerDNS: it answers DNS queries for your hosted domains, and since both do the same job on the same port, only one of the two runs on a server. Pick BIND if you know it well or need one of its specific features.',
            tips: [
                'Manage records on the DNS page whenever possible — hand-editing zone files is where most DNS accidents happen.',
                'If you do edit a config or zone file from the list, restart afterwards and immediately check the log below to confirm every zone loaded cleanly.',
                'One typo in a zone file can take that whole domain offline, so change one thing at a time.',
                'Remember that BIND and PowerDNS can\'t run together — installing one means the other has to go.',
            ],
            troubleshoot: [
                { symptom: 'It won\'t start (or a domain vanished) after a config edit.', fix: 'The log below names the exact file and line it choked on. Undo your last change in the editor and restart. If the message is cryptic, don\'t guess — this is a reasonable moment to involve an expert.' },
                { symptom: 'A hosted domain doesn\'t resolve.', fix: 'Check the service is running, confirm the records on the DNS page, and verify the registrar points the domain\'s nameservers at this server. Remember changes can take up to the record\'s TTL to spread.' },
                { symptom: 'It won\'t start, and the log says port 53 is in use.', fix: 'PowerDNS (or another DNS server) is still running. Only one DNS server fits per server — stop or uninstall the other.' },
            ],
        },
    },
    'clamav': {
        tr: {
            what: 'ClamAV bir antivirüs tarayıcısıdır. Dosyaları virüslere ve diğer zararlı yazılımlara karşı kontrol eder — en çok e-posta ekleri ve sunucuya yüklenen dosyalar için kullanılır. Virüs tanımlarını kendi kendine güncel tutar; esas olarak bu sunucu e-posta barındırıyorsa ya da kullanıcı dosya yüklemeleri alıyorsa gerekir.',
            tips: [
                'Açılışta sabırlı olun: ilk başlatma (ve her yeniden başlatma), virüs tanımları belleğe yüklenirken birkaç dakika sürebilir. Bu normaldir.',
                'ClamAV bellek düşkünüdür — boşta otururken bile çoğu zaman 1 GB\'tan fazla RAM ister. Küçük bir sunucuda yalnızca gerçekten kullanıyorsanız tutun.',
                'Tanımların güncel kaldığını doğrulamak için aşağıdaki günlükte veritabanı güncelleme mesajlarını arayın; güncelleyici tarayıcıyla birlikte çalışır.',
                'Bu sunucu e-posta ya da kullanıcı yüklemesi barındırmıyorsa ClamAV\'ı kaldırmak güvenlidir ve epey bellek kazandırır.',
            ],
            troubleshoot: [
                { symptom: 'Uzun süredir \'başlatılıyor\' durumunda takılı görünüyor.', fix: 'Birkaç dakika tanıyın — virüs veritabanını yüklemek gerçekten yavaştır. Yalnızca aşağıdaki günlükte gerçek bir hata görürseniz yeniden başlatın.' },
                { symptom: 'Kurulumdan sonra sunucu yavaşladı ya da bellek yetmedi.', fix: 'ClamAV çok RAM ister. Yaklaşık 2 GB\'tan az boş belleği olan sunucularda ya kaldırın ya da sunucuyu büyütün.' },
                { symptom: 'Günlük, virüs veritabanının eski olduğu uyarısını veriyor.', fix: 'Servisi yeniden başlatın; güncelleyici tekrar çalışır. Uyarı geri gelmeye devam ederse sunucu güncelleme sunucularına ulaşamıyor olabilir — dışa giden bağlantıyı bir uzmana kontrol ettirmekte fayda var.' },
            ],
        },
        en: {
            what: 'ClamAV is an antivirus scanner. It checks files for viruses and other malware — most commonly email attachments and files that users upload to this server. It keeps its virus definitions updated on its own; you mainly need it if this server handles email or accepts file uploads.',
            tips: [
                'Be patient at startup: the first start (and every restart) can take a few minutes while it loads virus definitions into memory. That\'s normal.',
                'ClamAV is memory-hungry — it often needs over 1 GB of RAM just sitting idle. On a small server, only keep it if you really use it.',
                'Look for database update messages in the log below to confirm definitions are staying fresh; the updater runs alongside the scanner.',
                'If this server doesn\'t handle mail or user uploads, uninstalling ClamAV is a safe way to free a lot of memory.',
            ],
            troubleshoot: [
                { symptom: 'It seems stuck on \'starting\' for a long time.', fix: 'Give it several minutes — loading the virus database is genuinely slow. Only restart if the log below shows an actual error.' },
                { symptom: 'The server slowed down or ran out of memory after installing it.', fix: 'ClamAV needs a lot of RAM. On servers with less than about 2 GB to spare, either uninstall it or upgrade the server.' },
                { symptom: 'The log warns that the virus database is outdated.', fix: 'Restart the service so the updater runs again. If the warning keeps returning, the server may not be reaching the update servers — worth having an expert check the outbound connection.' },
            ],
        },
    },
    'domain-connection': {
        tr: {
            what: 'Bir alan adı, "bu adı kim yanıtlıyor?" sorusunun cevabı internette kayıtlı olmadan çalışmaz. Bu bölüm o cevabın şu an ne olduğunu canlı olarak gösterir ve doğru olması için kayıtçınızda tam olarak ne yazmanız gerektiğini söyler. Alan adını satın aldığınız firmada bir kez yapılan bir ayardır.',
            tips: [
                'İki yol vardır ve ikisi de meşrudur: DNS yönetimini bu sunucuya vermek (A yolu) ya da DNS\'i olduğu yerde bırakıp yalnız adresi buraya yöneltmek (B yolu).',
                'E-posta da barındıracaksanız A yolunu seçin: SPF, DKIM ve DMARC gibi posta kayıtları ancak DNS burada yönetilirken otomatik tutulabilir.',
                'Yalnız bir web sitesi yayınlayacaksanız B yolu yeterlidir ve daha az adımdır.',
                'Değişiklikten sonra hemen sonuç beklemeyin: DNS\'in dünyaya yayılması 15 dakikadan 24 saate kadar sürebilir. "Tekrar kontrol et" düğmesi gerçek durumu gösterir, tahmin etmez.',
                'Bu ekrandaki her değer kopyalanabilir; kayıtçınızın formuna elle yazmanıza gerek yok.',
            ],
            troubleshoot: [
                { symptom: '"Başka bir yeri gösteriyor" yazıyor.', fix: 'Alan adı şu an başka bir sunucuya bakıyor. Kayıtçınızdaki eski A kaydını bu sunucunun adresiyle değiştirin ya da A yoluna geçin. Eski adresi hatırlamıyorsanız yukarıdaki "Şu an çözüldüğü adres" satırı size onu söyler.' },
                { symptom: '"Henüz çözülmüyor" yazıyor.', fix: 'Alan adı yeni alınmış ya da ad sunucuları yeni değişmiş olabilir; yayılması birkaç saat sürer. Bir gün sonra hâlâ aynıysa kayıtçınızda ad sunucularının gerçekten kaydedildiğini kontrol edin.' },
                { symptom: '"Ad sunucuları buraya bakıyor ama adres uyuşmuyor" yazıyor.', fix: 'DNS yönetimi bizde ama alan adının A kaydı başka bir adresi gösteriyor. DNS sekmesinden A kaydını bu sunucunun adresiyle güncelleyin; ya da değişiklik yeni yapıldıysa yayılmasını bekleyin.' },
                { symptom: 'Sertifika alınamıyor.', fix: 'Let\'s Encrypt sertifika vermeden önce alan adının bu sunucuya çözüldüğünü kendi kontrol eder. Bu bölüm "Bağlı" diyene kadar sertifika adımı beklemek zorundadır — bu bir panel kısıtı değil, sertifika sağlayıcısının kuralıdır.' },
            ],
        },
        en: {
            what: 'A domain does not work until the internet knows who answers for that name. This section shows what that answer is right now, live, and tells you exactly what to enter at your registrar to make it correct. It is a one-time setting at the company you bought the domain from.',
            tips: [
                'There are two legitimate routes: hand DNS management to this server (Route A), or leave DNS where it is and simply point the address here (Route B).',
                'Choose Route A if you will also host e-mail: mail records such as SPF, DKIM and DMARC can only be maintained automatically while DNS lives here.',
                'If you only need a website published, Route B is enough and has fewer steps.',
                'Do not expect an instant result: DNS changes take anywhere from 15 minutes to 24 hours to spread. The "Check again" button reports reality rather than guessing.',
                'Every value on this screen is copyable — you never have to retype it into your registrar\'s form.',
            ],
            troubleshoot: [
                { symptom: 'It says "Points somewhere else".', fix: 'The domain currently points at another server. Replace the old A record at your registrar with this server\'s address, or switch to Route A. If you do not remember the old address, the "Resolves to right now" line above tells you what it is.' },
                { symptom: 'It says "Does not resolve yet".', fix: 'The domain may be newly registered, or its nameservers were just changed; spreading takes a few hours. If it still says this a day later, check at your registrar that the nameservers were actually saved.' },
                { symptom: 'It says the nameservers point here but the address does not match.', fix: 'DNS is managed here, but the domain\'s A record shows a different address. Update the A record from the DNS tab to this server\'s address — or, if you changed it just now, wait for it to spread.' },
                { symptom: 'A certificate cannot be issued.', fix: 'Let\'s Encrypt verifies for itself that the domain resolves to this server before issuing anything. The certificate step has to wait until this section says "Connected" — that is the certificate provider\'s rule, not a panel limitation.' },
            ],
        },
    },
    'dns-server-settings': {
        tr: {
            what: 'Bu sayfa barındırılan tüm alan adlarının kullanacağı ortak ad sunucusu çiftini ve bu makinenin tek başına mı yoksa başka bir DNS makinesiyle eşlenmiş mi çalışacağını belirler. Eşlenmiş düzende iki panel de aynı iki adı kullanır; her makine kendi panelinde oluşturulan zone’ların sahibi olur ve eşindeki zone’ların canlı kopyasını tutar.',
            tips: [
                'Eşlenmiş modda adlardan biri bu makinenin, diğeri eş makinenin IP adresine çözülmelidir. İki ad da aynı IP’yi gösterirse yedeklilik yoktur.',
                'İki panelde de aynı ad sunucusu çiftini kullanın. Sihirbaz bu makinenin adını ve eş sunucunun adını genel DNS’ten algıladığında forma yerleştirir; son adımda adları ve topolojiyi tek işlemle kaydeder.',
				'Ad sunucusu adlarının sahibi olan üst domain’de iki eşleşmeyi de yapın: yetkili zone’da A/AAAA, kayıtçıda child nameserver/glue. Barındırılan müşteri alan adı yalnız “ad sunucularını değiştir” ekranında bu çifti seçer; kendi altında child nameserver oluşturmaz.',
                'İki makinede de Eşlenmiş düğüm modunu kaydedin. Böylece Frankfurt’ta oluşturulan zone Boston’a, Boston’da oluşturulan zone Frankfurt’a kopyalanabilir.',
				'Paneller hesap veya API anahtarı paylaşmaz; PowerDNS normalde NOTIFY için UDP 53, zone aktarımı (AXFR) için TCP 53 kullanır. İki protokol de iki yönde açık olmalıdır.',
            ],
            troubleshoot: [
                { symptom: 'Adların panel sunucu adından türetildiği yazıyor.', fix: 'Doğru ortak çift bunlarsa değiştirmeniz gerekmez. Sunucu eşleşmesini kontrol edip son adıma ilerleyin; tek kayıt düğmesi adları ve çalışma biçimini birlikte uygular.' },
                { symptom: 'İki ad da aynı makineyi gösteriyor.', fix: 'Bu bir DNS çifti değildir. Adlardan birinin A/AAAA kaydını bu sunucuya, diğerinin kaydını eş sunucuya yöneltin ve genel DNS yayılımından sonra tekrar kontrol edin.' },
                { symptom: '“TCP/53 erişilemiyor” yazıyor.', fix: 'Eş sunucuda PowerDNS çalışmıyor olabilir ya da sunucu/sağlayıcı güvenlik duvarı TCP 53’ü engelliyordur. İki yönde de TCP 53 erişimine izin verin; UDP 53 de genel DNS sorguları için açık olmalıdır.' },
                { symptom: 'Zone eş sunucuda görünmüyor.', fix: 'İki panelde de aynı ad çiftinin ve Eşlenmiş düğüm modunun kayıtlı olduğunu, her panelin doğru eş IP’yi ve doğru eş adını seçtiğini doğrulayın. Sonra yerel zone’da bir değişiklik yapıp TCP 53 durumunu yeniden kontrol edin.' },
            ],
        },
        en: {
            what: 'This page sets the shared nameserver pair used by every hosted domain and whether this machine works alone or is paired with another DNS machine. In a pair, both panels use the same two names; each machine owns zones created on its own panel and keeps live copies of zones created on its peer.',
            tips: [
                'In paired mode, one name must resolve to this machine and the other to the peer machine. If both names show one IP, there is no redundancy.',
                'Use the same nameserver pair on both panels. When public DNS identifies this machine and its peer, the wizard stages that assignment and the final action saves the names and topology together.',
				'Configure both mappings under the parent domain that owns the names: A/AAAA in its authoritative zone and child-nameserver/glue at its registrar. A hosted customer domain only selects this pair under “change nameservers”; it does not create child nameservers of its own.',
                'Save Paired node mode on both machines. Zones created in Frankfurt can then be copied to Boston, and zones created in Boston can be copied to Frankfurt.',
				'The panels do not share accounts or API keys. PowerDNS normally uses UDP 53 for NOTIFY and TCP 53 for zone transfer (AXFR), so both protocols must be open in both directions.',
            ],
            troubleshoot: [
                { symptom: 'The names are marked as inferred from the panel hostname.', fix: 'If they are the intended shared pair, you do not need to change them. Review the server assignment and continue; one final action saves both the names and operating mode.' },
                { symptom: 'Both names point at the same machine.', fix: 'That is not a DNS pair. Point one name’s A/AAAA record at this server and the other name’s record at the peer, then check again after public DNS has updated.' },
                { symptom: 'It says TCP/53 cannot be reached.', fix: 'PowerDNS may not be running on the peer, or a server/provider firewall may be blocking TCP port 53. Allow TCP 53 in both directions; UDP 53 must also be open for normal public DNS queries.' },
                { symptom: 'A zone does not appear on the peer.', fix: 'Confirm both panels saved the same names, Paired node mode, the correct peer IP, and the correct peer name. Then change the local zone once and recheck TCP port 53.' },
            ],
        },
    },
    'dovecot': {
        tr: {
            what: 'Dovecot, posta uygulamalarının — Outlook, Thunderbird, telefonlar — bu sunucudaki posta kutularını açmasını sağlayan hizmettir. IMAP ve POP3 girişlerini karşılar, iletileri uygulamaya aktarır ve klasörleri eşitli tutar. Birileri buradaki postasını okuduğu sürece gereklidir; webmail de buna dahildir.',
            tips: [
                'Kullanıcılar aniden giriş yapamaz olduysa üstteki düğmelerden yeniden başlatma çoğu takılmayı çözer — yeniden başlatma asla posta silmez.',
                'Açık portlar bölümüne bakın: posta uygulamalarının bağlanabilmesi için 143/993 (IMAP) ve 110/995 (POP3) listede olmalı.',
                'Başarısız girişler aşağıdaki günlükte görünür — şifre mi yanlış, başka bir sorun mu var; e-posta adresine göre filtreleyip görebilirsiniz.',
                'Panelden posta kutusu şifresi değiştirdiğinizde yeniden başlatma gerekmez; Dovecot yeni şifreyi bir sonraki girişte kullanır.',
                'Webmail (Roundcube) de postaları Dovecot üzerinden okur — webmail hata veriyorsa önce buraya bakın.',
            ],
            troubleshoot: [
                { symptom: 'Posta uygulaması giriş başarısız diyor ama şifre doğru.', fix: 'Aşağıdaki günlüğü e-posta adresine göre filtreleyin. Kimlik doğrulama hatası görüyorsanız panelden posta kutusunun şifresini yeniden belirleyip deneyin. Uygulamada kullanıcı adı olarak e-posta adresinin tamamının yazıldığından da emin olun.' },
                { symptom: 'Posta uygulamaları hiç bağlanamıyor ya da zaman aşımı veriyor.', fix: 'Dovecot\'un çalıştığını ve açık portlarda 993 ile 995\'in göründüğünü doğrulayın. Çalışıyor ama portlar listede yoksa yeniden başlatın; düzelmezse günlükteki ilk hata satırları nedenini söyleyecektir.' },
                { symptom: 'Posta uygulaması sertifika uyarısı gösteriyor.', fix: 'Uygulama büyük olasılıkla sertifikayla eşleşmeyen bir sunucu adıyla bağlanıyor. Uygulamayı posta sunucu adınıza (örn. mail.alanadiniz.com) yönlendirin ve panelden bu ad için sertifika çıkarıldığından emin olun.' },
            ],
        },
        en: {
            what: 'Dovecot is the service that lets mail apps — Outlook, Thunderbird, phones — open the mailboxes stored on this server. It handles IMAP and POP3 logins, hands messages to the app, and keeps folders in sync. You need it whenever anyone reads mail hosted here, including through webmail.',
            tips: [
                'If users suddenly can\'t log in, a restart from the buttons above clears most glitches — a restart never deletes any mail.',
                'Check the open-ports display: 143/993 (IMAP) and 110/995 (POP3) should be listed so mail apps can connect.',
                'Failed logins appear in the journal below — filter by the email address to see whether it\'s a wrong password or something else.',
                'Changing a mailbox password in the panel needs no restart; Dovecot uses the new password on the next login.',
                'Webmail (Roundcube) also reads mail through Dovecot — if webmail is failing, check here first.',
            ],
            troubleshoot: [
                { symptom: 'A mail app says the login failed, but the password is correct.', fix: 'Filter the journal below by the email address. If it shows an authentication failure, set the mailbox password again from the panel and retry. Also make sure the app uses the full email address as the username.' },
                { symptom: 'Mail apps can\'t connect at all or time out.', fix: 'Confirm Dovecot is running and that ports 993 and 995 appear in the open-ports display. If it\'s running but the ports are missing, restart it; if that doesn\'t help, the first error lines in the journal will say why.' },
                { symptom: 'Mail apps show certificate warnings.', fix: 'The app is probably connecting with a name that doesn\'t match the server\'s certificate. Point it at your mail hostname (e.g. mail.yourdomain.com) and make sure a certificate has been issued for that name in the panel.' },
            ],
        },
    },
    'fail2ban': {
        tr: {
            what: 'Fail2ban, sunucunun kayıtlarını izler ve art arda hatalı giriş deneyen internet adreslerini geçici olarak engeller — örneğin SSH ya da e-posta şifresi deneyen botları. Arka planda sessizce çalışır ve sunucudaki tüm servisleri korur. Ona nadiren dokunmanız gerekir; bütün görevi çalışır kalmaktır.',
            tips: [
                'Ara sıra aşağıdaki günlüğe göz atın — her \'Ban\' satırı durdurulmuş bir saldırıdır. Çok olması kötü değil, iyi işarettir.',
                'Engeller tasarım gereği geçicidir. Şifresini yanlış yazıp kendini kilitleyen biri için çoğu zaman biraz beklemek yeterlidir.',
                'Listeden bir yapılandırma dosyasını düzenlerseniz, değişikliğin geçerli olması için servisi yeniden başlatın.',
                'Her zaman çalışır tutun. Durdurmak gözle görülür bir şey değiştirmez ama sunucuyu aralıksız şifre denemelerine açık bırakır.',
            ],
            troubleshoot: [
                { symptom: 'Siz ya da bir müşteri birkaç yanlış şifreden sonra engellendiniz.', fix: 'Bekleyin — engeller kendiliğinden kalkar; genelde dakikalar ile saatler içinde. Başka bir ağdan (örneğin mobil veriden) bağlanmak da işe yarar. Acilse bir uzman engeli elle kaldırabilir.' },
                { symptom: 'Yapılandırma değişikliğinden sonra servis \'başarısız\' görünüyor.', fix: 'Aşağıdaki günlüğü açın — beğenmediği ayarı tam olarak yazar. Düzenleyicide son değişikliğinizi geri alın ve yeniden başlatın.' },
                { symptom: 'Günlükte aynı adres tekrar tekrar engelleniyor.', fix: 'Bu normaldir: botlar geri gelir, fail2ban da her seferinde yeniden engeller. Yapmanız gereken bir şey yok.' },
            ],
        },
        en: {
            what: 'Fail2ban watches this server\'s logs and temporarily blocks any internet address that keeps failing to log in — for example, bots guessing SSH or email passwords. It works quietly in the background and protects every service on the server. You rarely need to touch it; its whole job is simply to stay running.',
            tips: [
                'Glance at the log below occasionally — every \'Ban\' line is an attack that was stopped. Seeing lots of them is a good sign, not a bad one.',
                'Blocks are temporary by design. If someone locks themselves out by mistyping a password, waiting a while usually solves it on its own.',
                'If you edit one of its config files from the list, restart the service afterwards so the change takes effect.',
                'Keep it running at all times. Stopping it changes nothing visible, but it leaves the server open to nonstop password guessing.',
            ],
            troubleshoot: [
                { symptom: 'You or a customer got blocked after a few wrong passwords.', fix: 'Wait it out — bans expire on their own, usually within minutes to hours. Connecting from another network (like mobile data) also works. If it\'s urgent, an expert can lift the ban manually.' },
                { symptom: 'The service shows as failed after a config change.', fix: 'Open the log below — it names the exact setting it didn\'t like. Undo your last edit in the config editor and restart.' },
                { symptom: 'The same address appears banned over and over in the log.', fix: 'That\'s normal: bots come back, and fail2ban blocks them again each time. No action needed.' },
            ],
        },
    },
    'mariadb': {
        tr: {
            what: 'MariaDB, MySQL ile uyumlu bir veritabanı sunucusudur; WordPress dahil en yaygın web uygulamalarının ve çoğu e-ticaret yazılımının verilerini o saklar. PHP ile çalışan sitelerin neredeyse tamamı ona ihtiyaç duyar. Bu tür siteler barındırıyorsanız MariaDB kurulu ve çalışır durumda olmalı.',
            tips: [
                'Durdur yerine Yeniden Başlat\'ı tercih edin; MariaDB duruyken veritabanı kullanan her site hata gösterir.',
                'Veritabanlarını ve kullanıcıları panelin veritabanı sayfalarından oluşturun; panel onları sitelerinize bağlar ve bilgiler tutarlı kalır.',
                'MariaDB 3306 portunu dinler ve normalde yalnızca bu sunucunun kendisine açıktır. Uzaktaki bir uygulama gerçekten gerektirmedikçe bu portu internete kapalı tutun.',
                'Yeni kurulan bir site veritabanına ulaşamıyorsa veritabanı adını, kullanıcıyı ve parolayı panelin oluşturduklarıyla karşılaştırın.',
                'Yeniden başlattıktan sonra günlüğü izleyin; MariaDB bağlantı kabul etmeden önce bazen veri dosyalarını kontrol etmek için kısa bir süre bekler.',
            ],
            troubleshoot: [
                { symptom: 'Siteler \'veritabanı bağlantısı kurulamadı\' hatası gösteriyor.', fix: 'Önce bu sayfadan MariaDB\'nin çalıştığına bakın; durmuşsa Başlat\'a basıp günlüğü izleyin. Çalışıyorsa sitenin veritabanı bilgileri büyük olasılıkla yanlış; veritabanı sayfasındakilerle karşılaştırın.' },
                { symptom: 'MariaDB birkaç günde bir kendi kendine duruyor.', fix: 'En sık nedeni sunucu belleğinin dolmasıdır; duruştan hemen önceki günlük satırları genellikle bunu söyler. Yeniden başlatmak geri getirir ama sorun tekrarlıyorsa sunucuya büyük olasılıkla daha fazla bellek gerekir; bir uzmana danışmaya ya da sunucuyu büyütmeye değer.' },
                { symptom: 'Başlamıyor ve günlükte bozuk (corrupted/crashed) tablolardan söz ediliyor.', fix: 'Üst üste zorla başlatmayı denemeyin. Bu çoğu zaman onarılabilir ama hassas bir iştir: güncel bir yedeğiniz varsa onu geri yükleyin; yoksa onarımı bir uzmanın yapması en güvenlisidir.' },
                { symptom: 'Veritabanı kullanan her şey yavaşladı.', fix: 'Önce MariaDB\'yi yeniden başlatın; çoğu zaman bir süreliğine düzelir. Yavaşlık geri geliyorsa bir uygulama ağır sorgular çalıştırıyor ya da sunucuya daha fazla bellek gerekiyor olabilir; kaynağı bulmak bir uzmanın işidir.' },
            ],
        },
        en: {
            what: 'MariaDB is a database server compatible with MySQL — it stores the data behind most popular web apps, including WordPress and most shop software. Almost every PHP-based site needs it. If you host that kind of site, MariaDB should be installed and running.',
            tips: [
                'Prefer Restart over Stop — while MariaDB is stopped, every site that uses a database shows errors.',
                'Create databases and users from the panel\'s database pages; the panel connects them to your sites so the details stay consistent.',
                'MariaDB listens on port 3306, normally only for this server itself. Keep it closed to the internet unless a remote app genuinely needs it.',
                'If a freshly installed site can\'t reach its database, double-check the database name, user, and password against what the panel created.',
                'Watch the log after a restart — MariaDB sometimes takes a moment to check its data files before it accepts connections.',
            ],
            troubleshoot: [
                { symptom: 'Sites show \'Error establishing a database connection\'.', fix: 'First check on this page that MariaDB is running; if it stopped, press Start and watch the log. If it is running, the site\'s database details are probably wrong — compare them with the database page.' },
                { symptom: 'MariaDB stops on its own every few days.', fix: 'The most common cause is the server running out of memory; the log lines right before the stop usually say so. Restarting brings it back, but if it keeps happening the server most likely needs more memory — worth an expert\'s look or a bigger server.' },
                { symptom: 'It won\'t start, and the log mentions corrupted or crashed tables.', fix: 'Don\'t keep force-starting it. This can usually be repaired, but it\'s delicate work: restore a recent backup if you have one, and if not, it\'s safest to have an expert run the repair.' },
                { symptom: 'Everything that uses the database has become slow.', fix: 'Restart MariaDB first — that usually helps for a while. If the slowness keeps coming back, one app may be running heavy queries or the server may need more memory; pinpointing it is a job for an expert.' },
            ],
        },
    },
    'memcached': {
        tr: {
            what: 'Memcached, basit ve çok hızlı bir bellek önbelleğidir — Redis ile aynı işi yapar: sık kullanılan verileri RAM\'de tutarak siteleri hızlandırır; sadece daha az özelliği ve daha az derdi vardır. Bazı uygulamalar özellikle Redis yerine Memcached ister. Farklı uygulamalar ikisini birden istemedikçe normalde yalnızca birine ihtiyacınız olur.',
            tips: [
                'Uygulamanızın belgeleri hangi önbelleği istiyorsa onu kurun; \'ne olur ne olmaz\' diye hem Redis hem Memcached çalıştırmak yalnızca bellek israfıdır.',
                'Gönül rahatlığıyla yeniden başlatın — Memcached\'teki her şey tasarım gereği geçicidir ve uygulamalar önbelleği kendiliğinden yeniden doldurur.',
                'Uygulamalar ona 127.0.0.1 adresinden, 11211 numaralı porttan ulaşır. Açık portlar ekranından bu portun internete açık OLMADIĞINI doğrulayın.',
                'Başlarken sabit bir bellek miktarı ayırır; bunu yapılandırma dosyasından artırıp azaltabilir, sonra yeniden başlatabilirsiniz.',
            ],
            troubleshoot: [
                { symptom: 'Bir uygulama Memcached\'e ulaşamadığını söylüyor.', fix: 'Çalıştığını kontrol edin, gerekirse yeniden başlatın; uygulama 127.0.0.1 adresine ve 11211 portuna yönlenmiş olmalı. Bazı uygulamaların ayrıca kendi Memcached eklentisine (örneğin bir PHP eklentisi) ihtiyacı vardır.' },
                { symptom: 'Önbellekteki veriler çok çabuk kayboluyor gibi.', fix: 'Ayrılan bellek muhtemelen çok küçük; eski kayıtlar yenilere yer açmak için erkenden siliniyor. Yapılandırma dosyasındaki bellek ayarını artırın ve yeniden başlatın.' },
                { symptom: '11211 numaralı port internete açık görünüyor.', fix: 'Bunu vakit kaybetmeden düzeltin — internete açık Memcached sunucuları saldırılarda kötüye kullanılır. Yalnızca 127.0.0.1\'i dinlemeli; yapılandırmayı düzeltip yeniden başlatın, emin değilseniz bir uzmana danışın.' },
            ],
        },
        en: {
            what: 'Memcached is a simple, very fast memory cache — the same job as Redis, speeding up sites by keeping frequently used data in RAM, just with fewer features and even less fuss. Some applications specifically want Memcached rather than Redis. You normally need only one of the two, unless different apps demand both.',
            tips: [
                'Install whichever cache your application\'s documentation asks for; running both Redis and Memcached \'just in case\' only wastes memory.',
                'Restart freely — everything in Memcached is temporary by design, and apps rebuild the cache on their own.',
                'Apps reach it at 127.0.0.1, port 11211. Check the open-ports display to make sure that port is NOT open to the internet.',
                'It reserves a fixed amount of memory at startup; you can raise or lower that in the config file, then restart.',
            ],
            troubleshoot: [
                { symptom: 'An app says it can\'t reach Memcached.', fix: 'Check it\'s running and restart if needed; the app should point at 127.0.0.1, port 11211. Some apps also need their own Memcached add-on (such as a PHP extension) installed.' },
                { symptom: 'Cached data seems to disappear too quickly.', fix: 'Its memory allowance is probably too small, so old entries get pushed out early. Increase the memory setting in the config file and restart.' },
                { symptom: 'Port 11211 shows as open to the internet.', fix: 'Fix this promptly — publicly reachable Memcached servers get abused in attacks. It should listen only on 127.0.0.1; adjust the config and restart, or ask an expert if you\'re unsure.' },
            ],
        },
    },
    'netdata': {
        tr: {
            what: 'Netdata, sunucunun o an ne yaptığını — işlemci, bellek, disk, ağ ve daha fazlasını — saniyede bir yenilenen canlı grafiklerle çizer ve yakın geçmişi saklar. Kendi web panosu vardır ve bu panelden açılır. \'Sunucu saat üçte neden yavaştı?\' gibi soruların yanıtını veren araçtır.',
            tips: [
                'Panoyu bu sayfadan açıp bir dakika izleyin — bu sunucu için \'normal\'in neye benzediğini çabuk öğrenirsiniz; böylece tuhaf günler hemen göze çarpar.',
                'Yalnızca gözlemler; hiçbir şeyi değiştirmez. Kurmak, durdurmak ya da kaldırmak her zaman güvenlidir.',
                'İzlemenin kendisi de biraz işlemci ve RAM harcar. Çok küçük bir sunucuda Netdata\'yı durdurup yalnızca bir sorunu incelerken başlatmak gayet mantıklıdır.',
                'Panosu tüm internete açık olmamalı — açık portlar ekranını kontrol edin ve erişimi sınırlı tutun.',
            ],
            troubleshoot: [
                { symptom: 'Pano açılmıyor.', fix: 'Servisin çalıştığını kontrol edip yeniden başlatın, sonra pano bağlantısını tekrar deneyin. Açılışta bir şey ters gittiyse aşağıdaki günlük söyler.' },
                { symptom: 'Netdata kurulduğundan beri sunucu daha yavaş hissettiriyor.', fix: 'İzlemenin küçük ama gerçek bir maliyeti vardır. Dar sunucularda Netdata\'yı aktif kullanmadığınız zamanlarda durdurun ya da kaldırın.' },
                { symptom: 'Grafiklerde boşluklar var.', fix: 'Boşluk, Netdata\'nın durdurulduğu ya da sunucunun kayıt tutamayacak kadar yüklendiği anlamına gelir. Boşluğun olduğu saat civarında aşağıdaki günlüğe bakın — boşluğun kendisi çoğu zaman başka bir şeyin zorlandığının ipucudur.' },
            ],
        },
        en: {
            what: 'Netdata draws live charts of everything this server is doing — CPU, memory, disk, network and more, refreshed every second — and keeps recent history. It comes with its own web dashboard, opened from this panel. It\'s the tool for answering questions like \'why was the server slow at three o\'clock?\'',
            tips: [
                'Open the dashboard from this page and just watch for a minute — you\'ll quickly learn what \'normal\' looks like for this server, which makes odd days obvious.',
                'It only observes; it never changes anything. Installing, stopping, or uninstalling it is always safe.',
                'Monitoring itself costs a little CPU and RAM. On a very small server it\'s fine to keep Netdata stopped and start it only when investigating a problem.',
                'Its dashboard shouldn\'t be open to the whole internet — check the open-ports display and keep access limited.',
            ],
            troubleshoot: [
                { symptom: 'The dashboard won\'t open.', fix: 'Check the service is running and restart it, then try the dashboard link again. The log below will say if something failed at startup.' },
                { symptom: 'The server feels slower since installing Netdata.', fix: 'Monitoring has a small but real cost. On tight servers, stop Netdata when you\'re not actively using it, or uninstall it.' },
                { symptom: 'The charts have gaps in them.', fix: 'Gaps mean Netdata was stopped, or the server was so overloaded it couldn\'t record. Check the log below around the gap\'s time — the gap itself is often a clue that something else was struggling.' },
            ],
        },
    },
    'nginx': {
        tr: {
            what: 'Nginx, ziyaretçilerin tarayıcı isteklerini karşılayan web sunucusudur. Bu sunucuda barındırdığınız tüm siteleri o yayınlar; panelin kendi sayfaları da yine onun üzerinden gelir. Sitelerinizin erişilebilir olması için nginx\'in çalışıyor olması gerekir.',
            tips: [
                'Yeniden başlatma birkaç saniye sürer ve ziyaretçiler çoğu zaman fark etmez; bir yapılandırma dosyasını düzenledikten sonra Yeniden Başlat düğmesini kullanmaktan çekinmeyin.',
                'Bir site, yapılandırma dosyasını düzenledikten hemen sonra açılmaz olduysa ilk şüpheli o düzenlemedir; dosyayı yapılandırma listesinden yeniden açıp değişikliği geri alın.',
                'Açık portlar bölümüne bakın: nginx 80 ve 443 portlarını dinliyor olmalı. Bu portlar görünmüyorsa sunucu web trafiği kabul etmiyor demektir.',
                'Günlük görüntüleyicinin filtresine alan adını yazarak yalnızca o sitenin trafiğini ve hatalarını görebilirsiniz.',
                'Siteler yayındayken nginx\'i kaldırmayın; geri kurulana kadar bu sunucudaki tüm siteler kapalı kalır.',
            ],
            troubleshoot: [
                { symptom: 'Hiçbir site açılmıyor; tarayıcı bağlantının reddedildiğini söylüyor.', fix: 'Önce bu sayfanın üstünden nginx\'in çalışıp çalışmadığına bakın. Durmuşsa Başlat\'a basın ve alttaki günlüğü izleyin; başlamama nedeni genellikle kırmızı bir hata satırında açıkça yazar.' },
                { symptom: 'Yapılandırma değişikliğinden sonra nginx başlamıyor.', fix: 'Günlük çoğu zaman hatalı dosyayı ve satırı adıyla söyler. O dosyayı yapılandırma listesinden açın, düzenlemeyi düzeltin ya da geri alın, sonra nginx\'i tekrar başlatın.' },
                { symptom: 'Bir site başka bir sitenin içeriğini ya da varsayılan bir sayfayı gösteriyor.', fix: 'Bu genellikle bir alan adı karışıklığıdır. Sitenin kendi sayfasından alan adını kontrol edin ve DNS kayıtları sayfasından kayıtların bu sunucuyu gösterdiğinden emin olun. Sorun sürerse site yapılandırmasına bir uzmanın bakması gerekebilir.' },
                { symptom: 'Siteler açılıyor ama \'502 Bad Gateway\' hatası veriyor.', fix: 'Nginx\'in kendisi çalışıyor demektir; yanıt vermeyen, arkasındaki uygulamadır (PHP ya da Node). İlgili servisin sayfasına gidip onu oradan yeniden başlatın.' },
            ],
        },
        en: {
            what: 'Nginx is the web server that answers visitors\' browser requests. On this server it delivers every website you host here, and the panel\'s own pages are served through it as well. It needs to be running whenever you want your sites to be reachable.',
            tips: [
                'A restart takes only a moment and visitors rarely notice, so don\'t hesitate to use the Restart button after editing a config file.',
                'If a site stops loading right after you edited a config file, that edit is the first suspect — reopen the file from the config list and undo the change.',
                'Check the open-ports display: nginx should be listening on ports 80 and 443. If they\'re missing, the server isn\'t accepting web traffic.',
                'Type a domain name into the log viewer\'s filter to see only that site\'s traffic and errors.',
                'Don\'t uninstall nginx while sites are live — every site on this server stays offline until it\'s back.',
            ],
            troubleshoot: [
                { symptom: 'No site loads at all; browsers say the connection was refused.', fix: 'Check at the top of this page whether nginx is running. If it stopped, press Start and watch the log below — the reason it wouldn\'t start is usually spelled out in a red error line.' },
                { symptom: 'Nginx refuses to start after a config change.', fix: 'The log usually names the exact file and line with the mistake. Open that file from the config list, fix or undo the edit, then start nginx again.' },
                { symptom: 'A site shows another site\'s content or a default page.', fix: 'This is usually a domain mix-up. Check the domain on the site\'s own page and confirm on the DNS records page that the records point to this server. If it persists, the site\'s config may need an expert\'s review.' },
                { symptom: 'Sites load but show a \'502 Bad Gateway\' error.', fix: 'Nginx itself is fine — the app behind it (PHP or Node) isn\'t answering. Go to that service\'s page and restart it there.' },
            ],
        },
    },
    'node': {
        tr: {
            what: 'Node.js, sunucuda JavaScript uygulamaları çalıştırır; Next.js veya Express gibi araçlarla yazılmış modern web uygulamaları ve API\'ler ona ihtiyaç duyar. Her site kendi Node sürümünü kullanabilir; uygulamalar nginx\'in arkasında çalışır ve ziyaretçi trafiğini onlara nginx iletir. Node yalnızca bu tür uygulamalar barındırıyorsanız gerekir; düz PHP veya statik siteler onu kullanmaz.',
            tips: [
                'Node uygulamaları internete hiçbir zaman doğrudan açılmaz; önlerinde nginx durur. Bir Node sitesi çalışmıyorsa hem bu sayfaya hem nginx\'e bakın.',
                'Her sitenin uygulaması kendi süreci olarak çalışır; birini yeniden başlatmak diğerlerine dokunmaz.',
                'Node sürümünü her site için kendi sayfasından seçin; uygulamalar çoğu zaman belirli bir ana sürüm ister, uygulamanın belgelerine bakın.',
                'Bir Node sitesine yeni kod yükledikten sonra o sitenin uygulamasını yeniden başlatın; yeni kod ancak o zaman devreye girer.',
                'Günlük filtresine sitenin adını yazıp göz kulak olun; Node uygulamaları çökerken hatayı oraya yazar.',
            ],
            troubleshoot: [
                { symptom: 'Site \'502 Bad Gateway\' gösteriyor.', fix: 'Nginx uygulamaya ulaşamıyor; uygulama büyük olasılıkla çökmüş. Uygulamayı yeniden başlatın ve günlüğünü okuyun; çökmeden önceki son satırlar genellikle hatanın adını verir.' },
                { symptom: 'Uygulama sürekli yeniden başlıyor ya da başlar başlamaz duruyor.', fix: 'Genellikle bir başlangıç hatası vardır: eksik bir ayar ya da yanlış Node sürümü. Tam mesaj günlükte yazar; düzeltmek için uygulamanın geliştiricisine ihtiyaç olabilir.' },
                { symptom: 'Uygulama çalışıyor ama yüklediğiniz değişiklikler görünmüyor.', fix: 'Sitenin uygulamasını yeniden başlatın; Node, yeniden başlatılana kadar eski kodu bellekte tutar. Tarayıcınız da sayfayı önbelleğe almış olabilir, bir de zorla yenileme deneyin.' },
                { symptom: 'Site bir süre çalışıyor, giderek yavaşlıyor ve sonunda duruyor.', fix: 'Bu tablo genellikle uygulamanın belleği yavaş yavaş tüketmesi (bellek sızıntısı) demektir. Yeniden başlatmak şimdilik düzeltir ama kalıcı çözüm için sızıntıyı uygulamanın geliştiricisinin gidermesi gerekir.' },
            ],
        },
        en: {
            what: 'Node.js runs JavaScript apps on the server — modern web apps and APIs built with tools like Next.js or Express need it. Each site can use its own Node version, and the apps run behind nginx, which passes visitor traffic to them. You only need Node if you host this kind of app; plain PHP or static sites don\'t use it.',
            tips: [
                'Node apps never face the internet directly — nginx sits in front of them. If a Node site is down, check both this page and nginx.',
                'Each site\'s app runs as its own process, so restarting one app doesn\'t touch the others.',
                'Choose the Node version per site on the site\'s own page; apps often require a specific major version, so check the app\'s documentation.',
                'After deploying new code to a Node site, restart that site\'s app — the new code only takes over after a restart.',
                'Keep an eye on the log with the site\'s name in the filter; Node apps write their error there when they crash.',
            ],
            troubleshoot: [
                { symptom: 'The site shows \'502 Bad Gateway\'.', fix: 'Nginx can\'t reach the app — it has probably crashed. Restart the app and read its log; the last lines before the crash usually name the exact error.' },
                { symptom: 'The app keeps restarting, or stops right after starting.', fix: 'There\'s usually a startup error — a missing setting or the wrong Node version. The exact message is in the log; fixing it may need the app\'s developer.' },
                { symptom: 'The app runs, but changes you deployed don\'t show up.', fix: 'Restart the site\'s app — Node keeps the old code in memory until it\'s restarted. Your browser may also be caching pages, so try a hard refresh too.' },
                { symptom: 'The site works for a while, gets slower, and eventually dies.', fix: 'That pattern usually means the app is slowly eating memory (a memory leak). Restarting fixes it for now, but the lasting fix has to come from the app\'s developer.' },
            ],
        },
    },
    'pdns': {
        tr: {
            what: 'PowerDNS, burada barındırılan alan adları için \'bu site hangi adreste?\' sorusunu yanıtlayan DNS sunucusudur. DNS sayfasında kayıt eklediğinizde ya da düzenlediğinizde, o yanıtları internete sunan PowerDNS\'tir. Uzun süre kapalı kalırsa barındırdığınız alan adları yavaş yavaş çözülmez olur.',
            tips: [
                'Kayıtları yapılandırma dosyalarından değil, her zaman DNS sayfasından yönetin — panel, PowerDNS\'i ve veritabanını sizin için eşzamanlı tutar.',
                'DNS değişikliklerinin internete yayılması zaman alır (her kaydın TTL süresi kadar). Henüz görünmeyen bir değişiklik genelde bozuk değildir — sadece erkendir.',
                'Açık portlar ekranında 53 numaralı portu hem TCP hem UDP için kontrol edin; DNS ikisine de ihtiyaç duyar.',
                'PowerDNS ile BIND aynı işi aynı portta yapar; bu yüzden aynı anda yalnızca biri kurulu ve çalışır olabilir.',
                'Günün her saati çalışır tutun. Kısa kesintileri başka sunucuların önbelleği gizler ama uzun kesintiler alan adlarınızı erişilmez yapar.',
            ],
            troubleshoot: [
                { symptom: 'Barındırılan bir alan adı hiçbir yerden çözülmüyor.', fix: 'Önce servisin çalıştığından emin olun (gerekirse yeniden başlatın). Sonra DNS sayfasında alan adının kayıtlarının var olduğunu ve alan adı kayıt firmasının ad sunucularını gerçekten bu sunucuya yönlendirdiğini doğrulayın.' },
                { symptom: 'Değiştirdiğiniz bir kayıt dışarıdan hâlâ eski değeri gösteriyor.', fix: 'Bu genelde önbelleklemedir — kaydın TTL süresini bekleyin. Hiç güncellenmiyorsa aşağıdaki günlükte hata arayın ve servisi yeniden başlatın.' },
                { symptom: 'Başlamıyor; günlük, adresin ya da portun kullanımda olduğunu söylüyor.', fix: 'Başka bir DNS sunucusu (genelde BIND) 53 numaralı portu tutuyor. Burada yalnızca bir DNS sunucusu çalışabilir — diğerini durdurun ya da kaldırın.' },
            ],
        },
        en: {
            what: 'PowerDNS is the DNS server that answers the question \'what address is this domain at?\' for the domains hosted here. When you add or edit records on the DNS page, PowerDNS is what serves those answers to the rest of the internet. If it stays down for long, your hosted domains gradually stop resolving.',
            tips: [
                'Always manage records on the DNS page rather than in config files — the panel keeps PowerDNS and its database in sync for you.',
                'DNS changes take time to spread across the internet (up to each record\'s TTL). A change that isn\'t visible yet usually isn\'t broken — just early.',
                'Check the open-ports display for port 53 on both TCP and UDP; DNS needs both.',
                'PowerDNS and BIND do the same job on the same port, so only one of them can be installed and running at a time.',
                'Keep it running around the clock. Short outages are hidden by caching elsewhere, but long ones take your domains offline.',
            ],
            troubleshoot: [
                { symptom: 'A hosted domain doesn\'t resolve from anywhere.', fix: 'First make sure the service is running (restart if needed). Then confirm the domain\'s records exist on the DNS page, and that the domain\'s registrar actually points its nameservers at this server.' },
                { symptom: 'A record you changed still shows the old value from outside.', fix: 'That\'s usually caching — wait out the record\'s TTL. If it never updates, check the log below for errors and restart the service.' },
                { symptom: 'It won\'t start, and the log says the address or port is already in use.', fix: 'Another DNS server (usually BIND) is holding port 53. Only one DNS server can run here — stop or uninstall the other one.' },
            ],
        },
    },
    'php-fpm': {
        tr: {
            what: 'PHP-FPM, sitelerinizin arkasındaki PHP kodunu çalıştırır; WordPress, çoğu e-ticaret ve forum yazılımı ve pek çok özel uygulama ona ihtiyaç duyar. Bu sunucuda birden fazla PHP sürümü yan yana kurulu olabilir ve her site kendine uygun sürümü seçer. Hiçbir siteniz PHP kullanmıyorsa kurmadan bırakabilirsiniz.',
            tips: [
                'Her PHP sürümü ayrı çalışır; bir sürümü yeniden başlatmak yalnızca o sürümü kullanan siteleri etkiler.',
                'Bir uygulama daha yeni ya da daha eski bir PHP istiyorsa o sürümü buradan kurun ve siteyi kendi sayfasından yeni sürüme geçirin; diğer siteler kendi sürümlerinde kalır.',
                'Yapılandırma dosyasında bir PHP ayarını (örneğin yükleme boyutu ya da bellek sınırı) değiştirdikten sonra o sürümü yeniden başlatın ki ayar geçerli olsun.',
                'Günlük filtresine site adını yazarak yalnızca o siteden gelen PHP hatalarını görebilirsiniz.',
                'Bir PHP sürümünü kaldırmadan önce hiçbir sitenin onu kullanmadığından emin olun; önce sitelerin ayarlarına bakın.',
            ],
            troubleshoot: [
                { symptom: 'Bir PHP sitesi \'502 Bad Gateway\' ya da bembeyaz bir sayfa gösteriyor.', fix: 'O sitenin kullandığı PHP sürümü büyük olasılıkla durmuş. Bu sayfada o sürümü bulun, Başlat ya da Yeniden Başlat\'a basın ve nedenini alttaki günlükten okuyun.' },
                { symptom: 'Dosya yüklemeleri başarısız oluyor, büyük dosyalar reddediliyor.', fix: 'Bu genellikle PHP\'nin yükleme boyutu sınırıdır. İlgili sürümün yapılandırma dosyasını listeden açın, upload_max_filesize ve post_max_size değerlerini yükseltin, sonra o sürümü yeniden başlatın.' },
                { symptom: 'PHP sürümünü değiştirdikten hemen sonra site bozuldu.', fix: 'Uygulama yeni sürümü desteklemiyor olabilir. Siteyi kendi sayfasından eski sürüme geri alın; hangi sürümlerin desteklendiğini uygulamanın belgelerinden kontrol edin.' },
                { symptom: 'Sunucu yoğun görünmediği halde sayfalar çok yavaş.', fix: 'Önce o PHP sürümünü yeniden başlatın. Yavaşlık sürerse günlükte çoğu zaman \'max_children\' uyarısı görünür; bu sınırı yapılandırma dosyasından yükseltmek işe yarar ama emin değilseniz değiştirmeden önce bir uzmana danışın.' },
            ],
        },
        en: {
            what: 'PHP-FPM runs the PHP code behind your sites — WordPress, most shop and forum software, and many custom apps depend on it. On this server several PHP versions can be installed side by side, and each site picks the version it needs. If none of your sites use PHP, you can leave it uninstalled.',
            tips: [
                'Each PHP version runs separately, so restarting one version only affects the sites that use that version.',
                'When an app needs a newer or older PHP, install that version here and switch the site to it on the site\'s own page — every other site keeps its current version.',
                'After changing a PHP setting (like upload size or memory limit) in a config file, restart that PHP version so the change takes effect.',
                'Type a site\'s name into the log filter to see PHP errors from just that site.',
                'Before removing a PHP version, make sure no site is still set to use it — check the sites\' settings first.',
            ],
            troubleshoot: [
                { symptom: 'A PHP site shows \'502 Bad Gateway\' or a completely blank page.', fix: 'The PHP version that site uses has probably stopped. Find that version on this page, press Start or Restart, and read the log below for the reason.' },
                { symptom: 'File uploads fail or large files are rejected.', fix: 'That\'s usually PHP\'s upload size limit. Open that version\'s config file from the list, raise upload_max_filesize and post_max_size, then restart that version.' },
                { symptom: 'A site broke right after switching its PHP version.', fix: 'The app likely doesn\'t support the new version. Switch the site back to the old version on its site page, and check the app\'s documentation for which versions it supports.' },
                { symptom: 'Pages are very slow even though the server doesn\'t look busy.', fix: 'Restart that PHP version first. If it stays slow, the log often shows a \'max_children\' warning — raising that limit in the config file helps, but if you\'re unsure, ask an expert before changing it.' },
            ],
        },
    },
    'phpmyadmin': {
        tr: {
            what: 'phpMyAdmin, MariaDB veritabanlarınızın içinde çalışmanızı sağlayan bir web sayfasıdır: tablolara göz atın, satırları düzenleyin, sorgu çalıştırın, yedek alın ya da geri yükleyin — hepsi tarayıcıdan. Bir arka plan servisi değildir; onu web sunucusu ve PHP sunar, bu yüzden burada başlatılacak ya da durdurulacak bir şey yoktur. Ara sıra yapılacak veritabanı işleri için komut satırı gerektirmeyen rahat bir yoldur.',
            tips: [
                'Giriş için panel hesabınızı değil, panelin veritabanı sayfalarında oluşturduğunuz veritabanı kullanıcı adı ve şifresini kullanın.',
                'Riskli değişikliklerden önce Dışa Aktar (Export) sekmesinden yedek indirmek en kolay sigortadır. Alışkanlık edinin.',
                'Drop ve Delete\'e saygılı yaklaşın: veritabanında çöp kutusu yoktur. Yedeğiniz yoksa silinen, gitmiştir.',
                'Sayfa açılmıyorsa sorun genelde phpMyAdmin\'in kendisi değil, web sunucusu ya da PHP\'dir — panelden o servisleri kontrol edin.',
            ],
            troubleshoot: [
                { symptom: 'Sayfa hata veriyor ya da hiç açılmıyor.', fix: 'phpMyAdmin\'in yeniden başlatılacak kendi servisi yoktur. Panelden web sunucusu ve PHP servislerinin çalıştığını kontrol edip onları yeniden başlatın; asıl nedeni onların günlükleri gösterir.' },
                { symptom: 'Girişte \'Access denied\' (erişim reddedildi) hatası.', fix: 'Panel bilgileriyle değil, veritabanı bilgileriyle giriş yaptığınızdan emin olun. Veritabanı kullanıcısının şifresini panelin veritabanı sayfalarından sıfırlayabilirsiniz.' },
                { symptom: 'Büyük bir yedek dosyası içe aktarılırken yarıda kesiliyor.', fix: 'Dosya, PHP\'nin yükleme sınırından büyük. Yapılandırma dosyası listesinden PHP\'nin yükleme sınırlarını artırın (sonra PHP\'yi yeniden başlatın) ya da yedeği daha küçük parçalara bölerek aktarın.' },
            ],
        },
        en: {
            what: 'phpMyAdmin is a web page for working inside your MariaDB databases: browse tables, edit rows, run queries, and import or export backups, all from a browser. It isn\'t a background service — the web server and PHP serve it — so there\'s nothing here to start or stop. It\'s the comfortable way to do occasional database work without a command line.',
            tips: [
                'Log in with a database username and password (the ones from the panel\'s database pages) — not your panel login.',
                'The Export tab is the easiest way to download a backup copy of a database before risky changes. Make it a habit.',
                'Treat Drop and Delete with respect: a database has no trash bin. Deleted means gone, unless you have a backup.',
                'If the page won\'t load, the problem is usually the web server or PHP, not phpMyAdmin itself — check those services in the panel.',
            ],
            troubleshoot: [
                { symptom: 'The page shows an error or won\'t load at all.', fix: 'phpMyAdmin has no service of its own to restart. Check that the web server and PHP services are running in the panel and restart those; their logs will show the real cause.' },
                { symptom: '\'Access denied\' when logging in.', fix: 'Make sure you\'re using database credentials, not your panel ones. You can reset the database user\'s password from the panel\'s database pages.' },
                { symptom: 'Importing a large backup file fails partway.', fix: 'The file is bigger than PHP\'s upload limit. Raise the upload and post size limits in the PHP config from the config file list (then restart PHP), or split the import into smaller files.' },
            ],
        },
    },
    'phppgadmin': {
        tr: {
            what: 'phpPgAdmin, PostgreSQL veritabanlarınızın içinde çalışmanızı sağlayan bir web sayfasıdır — tablolara göz atın, SQL çalıştırın, veri alıp verin; hepsi tarayıcıdan. MariaDB\'deki karşılığı phpMyAdmin gibi o da bir arka plan servisi değildir: onu web sunucusu ve PHP sunar, burada başlatılacak ya da durdurulacak bir şey yoktur. Komut satırı olmadan ara sıra PostgreSQL işi yapmak için kullanışlıdır.',
            tips: [
                'Giriş için panel hesabınızı değil, panelin veritabanı sayfalarından oluşturulmuş PostgreSQL kullanıcı adı ve şifresini kullanın.',
                'Büyük değişikliklerden önce veritabanının bir kopyasını dışa aktarın — bir dakikalık yedek, çok kötü bir günü kurtarabilir.',
                'Tablo ya da satır silmek kalıcıdır; veritabanlarında geri al yoktur. Tereddütteyseniz önce dışa aktarın.',
                'Sayfa açılmıyorsa panelden web sunucusu, PHP ve PostgreSQL servislerine bakın — neden neredeyse her zaman bu üçünden biridir.',
            ],
            troubleshoot: [
                { symptom: 'Sayfa hata veriyor ya da açılmıyor.', fix: 'Yeniden başlatılacak bir phpPgAdmin servisi yoktur. Panelden web sunucusu ve PHP servislerini kontrol edip yeniden başlatın; gerçek hatayı onların günlüklerinde okuyun.' },
                { symptom: 'Giriş reddediliyor.', fix: 'Panelin veritabanı sayfalarındaki PostgreSQL bilgilerini kullanın ve PostgreSQL servisinin çalıştığından emin olun. Şifre sıfırlama da yine o sayfalardan yapılır.' },
                { symptom: 'Sayfa açılıyor ama veritabanı sunucusuna bağlanamadığını söylüyor.', fix: 'PostgreSQL büyük olasılıkla durmuş — panelin servis listesinden bulun, günlüğüne bakın ve başlatın.' },
            ],
        },
        en: {
            what: 'phpPgAdmin is a web page for working inside your PostgreSQL databases — browse tables, run SQL, import and export data, all from a browser. Like phpMyAdmin (its MariaDB counterpart), it isn\'t a background service: the web server and PHP serve it, so there\'s nothing to start or stop here. Handy for occasional PostgreSQL work without a command line.',
            tips: [
                'Log in with a PostgreSQL username and password created through the panel\'s database pages, not your panel login.',
                'Export a copy of a database before big changes — a minute of exporting can save a very bad day.',
                'Deleting tables or rows is permanent; databases have no undo. When in doubt, export first.',
                'If the page won\'t open, look at the web server, PHP, and PostgreSQL services in the panel — one of those three is nearly always the cause.',
            ],
            troubleshoot: [
                { symptom: 'The page shows an error or won\'t load.', fix: 'There\'s no phpPgAdmin service to restart. Check the web server and PHP services in the panel, restart them, and read their logs for the actual error.' },
                { symptom: 'Login is rejected.', fix: 'Use PostgreSQL credentials from the panel\'s database pages, and make sure the PostgreSQL service itself is running. Password resets are done from those pages too.' },
                { symptom: 'It loads, but says it can\'t connect to the database server.', fix: 'PostgreSQL is likely stopped — find it in the panel\'s service list, check its log, and start it.' },
            ],
        },
    },
    'postfix': {
        tr: {
            what: 'Postfix, sunucunuzun posta taşıyıcısıdır: barındırdığınız alan adları için giden e-postaları gönderir, gelenleri teslim alır. Bu sunucudaki bir site ya da posta kutusu ne zaman e-posta gönderse veya alsa, işi Postfix yapar. Alan adlarınızdan herhangi biri e-posta kullandığı sürece çalışır durumda olmalı.',
            tips: [
                'Üstteki başlat/durdur/yeniden başlat düğmeleri güvenlidir — yeniden başlatma saniyeler sürer, kuyruktaki iletiler kaybolmaz.',
                'Aşağıdaki listeden bir yapılandırma dosyasını düzenledikten sonra değişikliğin etkili olması için Postfix\'i yeniden başlatın.',
                'Ara sıra açık portlar bölümüne bakın: 25 numaralı port listede olmalı, yoksa diğer sunucular size posta ulaştıramaz.',
                'Aşağıdaki günlük her teslim denemesini kaydeder — tek bir iletinin yolculuğunu izlemek için alıcı adresine veya alan adına göre filtreleyin.',
                'Yeni bir alan adının e-postasını bu sunucuya yönlendirmeden önce DNS kayıtları sayfasından MX ve SPF kayıtlarını ekleyin.',
            ],
            troubleshoot: [
                { symptom: 'Giden postalar reddediliyor ya da spam klasörüne düşüyor.', fix: 'DNS kayıtları sayfasından alan adının SPF, DKIM ve DMARC kayıtlarını kontrol edin — en sık neden eksik kayıtlardır. Karşı sunucunun tam ret mesajını aşağıdaki günlükte görebilirsiniz.' },
                { symptom: 'Hiç posta gelmiyor.', fix: 'Postfix\'in çalıştığından ve açık portlarda 25\'in göründüğünden emin olun. DNS kayıtları sayfasında alan adının MX kaydının bu sunucuyu gösterdiğini de kontrol edin. Bazı sağlayıcılar 25 numaralı portu kapalı tutar — öyleyse açtırmak için sunucu sağlayıcınıza başvurmanız gerekir.' },
                { symptom: 'Bir ayar değişikliğinden sonra Postfix başlamıyor.', fix: 'Aşağıdaki günlüğe bakın — ilk hata satırı genellikle hatalı dosyayı ve satırı söyler. Son düzenlemenizi editörden geri alıp yeniden başlatın. Mesaj anlaşılmıyorsa tahmin etmek yerine önceki hâle dönmek daha güvenlidir.' },
            ],
        },
        en: {
            what: 'Postfix is this server\'s mail carrier: it sends outgoing email and receives incoming email for the domains hosted here. Any time a website or mailbox on this server sends or gets a message, Postfix handles the delivery. You need it running for as long as any of your domains use email.',
            tips: [
                'The start/stop/restart buttons above are safe to use — a restart takes seconds and messages waiting in the queue are not lost.',
                'After editing any config file from the list below, restart Postfix so the change takes effect.',
                'Glance at the open-ports display now and then: port 25 must be listed, or other servers can\'t deliver mail to you.',
                'The journal below records every delivery attempt — filter by a recipient address or domain to follow a single message\'s journey.',
                'Before pointing a new domain\'s email at this server, add its MX and SPF records on the DNS records page.',
            ],
            troubleshoot: [
                { symptom: 'Outgoing mail is rejected or lands in the spam folder.', fix: 'Check the domain\'s SPF, DKIM and DMARC records on the DNS records page — missing records are the most common cause. The journal below shows the exact rejection message from the receiving server.' },
                { symptom: 'No incoming mail arrives.', fix: 'Confirm Postfix is running and that port 25 appears in the open-ports display. Also check on the DNS records page that the domain\'s MX record points at this server. Note that some hosting providers block port 25 — if so, you\'ll need to ask your provider to open it.' },
                { symptom: 'Postfix won\'t start after a config change.', fix: 'Open the journal below — the first error line usually names the file and line with the mistake. Undo your last edit in the config editor and restart. If the message is unclear, going back to the previous version is safer than guessing.' },
            ],
        },
    },
    'postgresql': {
        tr: {
            what: 'PostgreSQL bir veritabanı sunucusudur; uygulamalarınızın dayandığı verileri — kullanıcı hesapları, siparişler, içerikler — o saklar. Bazı uygulamalar MariaDB/MySQL yerine özellikle PostgreSQL ister. Yalnızca bu sunucudaki bir uygulama onu gerektiriyorsa kurmanız gerekir; kurmadan önce uygulamanın gereksinimlerine bakın.',
            tips: [
                'Veritabanında canlı veri durur; Durdur yerine Yeniden Başlat\'ı tercih edin ve onu kullanan siteler yayındayken durdurmaktan kaçının.',
                'Veritabanlarını ve kullanıcıları yapılandırma dosyalarını elle düzenlemek yerine panelin veritabanı sayfalarından oluşturun; panel siteleri ve erişim bilgilerini birbiriyle uyumlu tutar.',
                'Açık portlar bölümünde PostgreSQL normalde 5432 portunu, çoğunlukla da yalnızca bu sunucunun kendisi için dinler. Gerçekten uzaktan erişim gerekmedikçe bu portu internete açmayın.',
                'Yapılandırma dosyasında bir ayarı değiştirirseniz geçerli olması için PostgreSQL\'i yeniden başlatın ve ne değiştirdiğinizi bir kenara not edin.',
                'Büyük bir değişiklikten — örneğin uygulama güncellemesi ya da sürüm yükseltmesi — önce güncel bir yedeğiniz olduğundan emin olun.',
            ],
            troubleshoot: [
                { symptom: 'Uygulamalar veritabanına bağlanamadığını söylüyor.', fix: 'Önce bu sayfanın üstünden PostgreSQL\'in çalışıp çalışmadığına bakın; durmuşsa Başlat\'a basıp günlüğü izleyin. Çalışıyorsa uygulamanın bağlantı bilgileri (veritabanı adı, kullanıcı, parola) yanlış olabilir; veritabanı sayfasındakilerle karşılaştırın.' },
                { symptom: 'Yapılandırma değişikliğinden sonra PostgreSQL başlamıyor.', fix: 'Günlük, kabul etmediği ayarın adını verir. Yapılandırma dosyasını yeniden açıp değişikliği geri alın, sonra tekrar başlatın.' },
                { symptom: 'Kendi kendine durdu; günlükte bellek ya da disk sorunlarından söz ediliyor.', fix: 'Sunucunun belleği ya da diski dolmuş olabilir. Önce boş disk alanını kontrol edin; sorun tekrarlıyorsa sunucuya büyük olasılıkla daha fazla bellek gerekiyordur — bu noktada bir uzmana danışmak iyi olur.' },
            ],
        },
        en: {
            what: 'PostgreSQL is a database server — it stores the data your apps rely on, like user accounts, orders, and content. Some apps specifically require PostgreSQL instead of MariaDB/MySQL. You only need it if an app on this server calls for it, so check the app\'s requirements before installing.',
            tips: [
                'A database holds live data — prefer Restart over Stop, and avoid stopping it while sites that use it are serving visitors.',
                'Create databases and users from the panel\'s database pages rather than editing config files by hand; the panel keeps sites and access details in sync.',
                'In the open-ports display, PostgreSQL normally listens on port 5432, and usually only for this server itself. Don\'t open it to the internet unless you truly need remote access.',
                'If you change a setting in a config file, restart PostgreSQL for it to take effect — and keep a note of what you changed.',
                'Make sure you have a recent backup before big changes, like an app update or a version upgrade.',
            ],
            troubleshoot: [
                { symptom: 'Apps say they can\'t connect to the database.', fix: 'Check at the top of this page whether PostgreSQL is running; if it stopped, press Start and watch the log. If it is running, the app\'s connection details (database name, user, password) may be wrong — compare them with what\'s on the database page.' },
                { symptom: 'PostgreSQL won\'t start after a config change.', fix: 'The log names the setting it didn\'t accept. Reopen the config file, undo the change, then start it again.' },
                { symptom: 'It stopped on its own, and the log mentions memory or disk problems.', fix: 'The server may have run out of memory or disk space. Check free disk space first; if this keeps happening, the server probably needs more memory — worth getting an expert\'s opinion.' },
            ],
        },
    },
    'redis': {
        tr: {
            what: 'Redis, bellekte yaşayan yıldırım hızında bir veri deposudur. Web siteleri ve uygulamalar onu çoğunlukla önbellek olarak kullanır: aynı bilgiyi tekrar tekrar veritabanından istemek yerine Redis\'ten çok daha kısa sürede alırlar. Barındırdığınız bir uygulama destekliyor ya da istiyorsa kurun — Redis eklentili WordPress tipik bir örnektir.',
            tips: [
                'Redis\'i yeniden başlatmak güvenlidir: önbellek kendiliğinden yeniden dolar; tek bedeli, kısa bir süre sayfaların biraz yavaş açılmasıdır.',
                'Bu sunucudaki uygulamalar ona 127.0.0.1 adresinden, 6379 numaralı porttan ulaşır — uygulama ayarlarına genelde tam olarak bunu yazarsınız.',
                'Açık portlar ekranına göz atın: Redis internetten erişilebilir OLMAMALI. 6379 dışarıya açık görünüyorsa bunu acil kabul edin.',
                'Sunucu belleği sürekli doluyorsa yapılandırma dosyasında bir bellek sınırı (\'maxmemory\' ayarı) belirleyin ve yeniden başlatın.',
            ],
            troubleshoot: [
                { symptom: 'Bir web sitesi Redis\'e bağlanamadığını söylüyor.', fix: 'Servisin çalıştığını kontrol edin, gerekirse yeniden başlatın. Sonra uygulamanın 127.0.0.1 adresine ve 6379 portuna ayarlı olduğunu doğrulayın; siz ayarlamadıysanız şifre de olmamalı.' },
                { symptom: 'Sunucunun bellek kullanımı sürekli artıyor.', fix: 'Redis, aksi söylenmedikçe büyümeye devam eder. Listeden yapılandırma dosyasını açın, makul bir \'maxmemory\' sınırı belirleyin ve yeniden başlatın.' },
                { symptom: 'Günlükte \'memory overcommit\' ile ilgili bir uyarı var.', fix: 'Bu uyarı yeni sunucularda yaygındır ve önbellek kullanımı için zararsızdır. Bir sistem ayarıyla susturulabilir — bir uzman için ufak bir iş, ama ortada bozuk bir şey yok.' },
            ],
        },
        en: {
            what: 'Redis is a lightning-fast data store that lives in memory. Websites and apps use it mostly as a cache: instead of asking the database for the same information again and again, they grab it from Redis in a fraction of the time. Install it when an app you host supports or requires it — WordPress with a Redis plugin is a typical example.',
            tips: [
                'Restarting Redis is safe: a cache rebuilds itself automatically, and the only cost is slightly slower pages for a short while.',
                'Apps on this server reach it at 127.0.0.1, port 6379 — that\'s usually exactly what you type into the app\'s settings.',
                'Glance at the open-ports display: Redis should NOT be reachable from the internet. If 6379 shows as publicly open, treat it as urgent.',
                'If server memory keeps filling up, set a memory cap (the \'maxmemory\' setting) in the config file, then restart.',
            ],
            troubleshoot: [
                { symptom: 'A website reports it can\'t connect to Redis.', fix: 'Check the service is running and restart it if needed. Then verify the app is set to 127.0.0.1, port 6379, with no password unless you configured one.' },
                { symptom: 'Server memory usage keeps climbing.', fix: 'Redis will happily grow until told otherwise. Open the config file from the list, set a sensible \'maxmemory\' limit, and restart.' },
                { symptom: 'The log shows a warning about \'memory overcommit\'.', fix: 'That warning is common on fresh servers and harmless for cache use. It can be silenced with a system setting — a fine little job for an expert, but nothing is broken.' },
            ],
        },
    },
    'roundcube': {
        tr: {
            what: 'Roundcube, /webmail/ adresindeki web postasıdır — tarayıcıda açılan, kullanıcı tarafında hiçbir kurulum istemeyen bir gelen kutusu. Postaları Dovecot üzerinden okur, Postfix üzerinden gönderir; bu ikisiyle birlikte web sunucusu ve PHP de çalışıyor olmalı. Kullanıcılarınızın posta uygulaması ayarlamadan her cihazdan e-postalarına bakabilmesi için sunun.',
            tips: [
                'Roundcube kendi başına bir arka plan hizmeti değildir, bu yüzden başlat/durdur düğmesi yoktur — web sunucusu ve PHP içinde çalışır.',
                '/webmail/ sorun çıkarırsa Roundcube\'a ait bir süreç aramak yerine web sunucusunu ve PHP\'yi kendi sayfalarından yeniden başlatın.',
                '/webmail/ üzerinden bir deneme girişi, tüm posta düzeneğinin en hızlı sağlık kontrolüdür — tek seferde hem Dovecot\'u hem web sunucusunu yoklar.',
                'Ayarları aşağıda listelenen yapılandırma dosyalarındadır; oradaki veritabanı bağlantı bilgilerine dokunmanız nadiren gerekir.',
                '/webmail/ sayfasını mutlaka HTTPS ile sunun — kullanıcılar bu sayfaya posta şifrelerini yazıyor.',
            ],
            troubleshoot: [
                { symptom: '/webmail/ boş sayfa ya da hata gösteriyor.', fix: 'Web sunucusunu ve PHP\'yi kendi sayfalarından yeniden başlatın, sonra web sunucusunun günlüğüne bakın. Boş sayfa neredeyse her zaman PHP tarafındaki bir sorundan kaynaklanır, Roundcube\'un kendisinden değil.' },
                { symptom: 'Webmail\'de giriş olmuyor ama aynı şifre posta uygulamasında çalışıyor.', fix: 'Roundcube girişi bu sunucudaki Dovecot üzerinden yapar. Dovecot\'un çalıştığını kontrol edin — başarısız denemeyi ve nedenini Dovecot\'un günlüğünde görürsünüz.' },
                { symptom: 'Webmail\'den posta okunuyor ama gönderilemiyor.', fix: 'Gönderme Postfix üzerinden gider. Postfix\'in çalıştığını kontrol edin ve bir gönderme denemesinin hemen ardından günlüğüne bakın — ret nedeni orada görünür.' },
                { symptom: '/webmail/ \'bulunamadı\' (404) hatası veriyor.', fix: 'Web sunucusuna webmail yolu henüz tanıtılmamış demektir. Roundcube\'u panelden kaldırıp yeniden kurmak bu bağlantıyı yeniden oluşturur; sorun sürerse bir uzmana danışmak gerekir.' },
            ],
        },
        en: {
            what: 'Roundcube is the webmail app at /webmail/ — an inbox in the browser, with nothing to set up on the user\'s side. It reads mailboxes through Dovecot and sends through Postfix, so both must be running, along with the web server and PHP. Offer it so people can check their mail from any device without configuring a mail app.',
            tips: [
                'Roundcube isn\'t a background service of its own, so there are no start/stop buttons for it — it runs inside the web server and PHP.',
                'If /webmail/ acts up, restart the web server and PHP from their pages instead of looking for a Roundcube process.',
                'A test login at /webmail/ is the quickest health check for the whole mail setup — it exercises Dovecot and the web server in one go.',
                'Its settings live in the config files listed below; the database connection details there rarely need touching.',
                'Always serve /webmail/ over HTTPS — users type their mail passwords into this page.',
            ],
            troubleshoot: [
                { symptom: '/webmail/ shows a blank page or an error.', fix: 'Restart the web server and PHP from their own pages, then check the web server\'s log. A blank page is almost always a PHP-side problem, not Roundcube itself.' },
                { symptom: 'Webmail login fails, but the same password works in a mail app.', fix: 'Roundcube logs in through Dovecot on this same server. Check that Dovecot is running — its journal will show the failed attempt and the reason.' },
                { symptom: 'Mail can be read in webmail but not sent.', fix: 'Sending goes through Postfix. Check that Postfix is running and read its journal right after a send attempt — the rejection reason shows up there.' },
                { symptom: '/webmail/ returns \'not found\' (404).', fix: 'The web server hasn\'t been wired up to the webmail path. Reinstalling Roundcube from the panel recreates that link; if the problem stays, it\'s one for an expert.' },
            ],
        },
    },
    'rspamd': {
        tr: {
            what: 'Rspamd, Postfix ile el ele çalışan istenmeyen posta (spam) filtresidir. Gelen her iletiye bir puan verir: bariz çöp daha kapıda reddedilir, şüpheli olanlar işaretlenir ve gereksiz klasörüne düşer. Filtre çalışırken posta kutularınız kullanılabilir kalır — hiç filtre olmazsa kısa sürede çöple dolar.',
            tips: [
                'Rspamd ile SpamAssassin aynı işi yapar — ikisinden yalnızca birini kurup çalıştırın.',
                'İlk günlerde aşağıdaki günlüğü izleyin: her satırda iletinin aldığı puan ve nedenleri görünür.',
                'Meşru bir gönderici sürekli engelleniyorsa aşağıdaki yapılandırma dosyalarından izin listesine ekleyin, sonra yeniden başlatın.',
                'Rspamd sorun çıkarırsa kaldırmak yerine yeniden başlatmayı veya ayar düzeltmeyi tercih edin — spam filtresiz posta işletmek kısa sürede çekilmez olur.',
                'Rspamd yalnızca gelen postayı süzer; gönderdiklerinizi yavaşlatmaz, değiştirmez.',
            ],
            troubleshoot: [
                { symptom: 'Meşru postalar reddediliyor ya da gereksiz klasörüne düşüyor.', fix: 'Günlüğü gönderenin adresine göre filtreleyip puanı ve nedenleri görün. Neden eksik SPF/DKIM kayıtlarıysa düzeltmeyi gönderene iletin; değilse göndereni editörden izin listesine ekleyip yeniden başlatın.' },
                { symptom: 'Spam hâlâ geçiyor.', fix: 'Yeni kurulmuş bir filtreden her zaman bir miktar spam sızar — filtre zamanla öğrenip iyileşir. Rspamd\'nin gerçekten çalıştığından emin olun: posta gelirken günlükte hiç hareket yoksa bağlantının tazelenmesi için hem Rspamd\'yi hem Postfix\'i yeniden başlatın.' },
                { symptom: 'Rspamd bozulunca posta akışı yavaşladı ya da durdu.', fix: 'Postfix filtrenin kararını bekler; takılan bir filtre her şeyi geciktirir. Üstteki düğmeden Rspamd\'yi yeniden başlatın; başlamıyorsa günlükteki ilk hata nedenini söyler. Son çare olarak bir uzman, Postfix ayarındaki filtre bağlantısını geçici olarak kapatabilir.' },
            ],
        },
        en: {
            what: 'Rspamd is the spam filter that works hand in hand with Postfix. It gives every incoming message a score: obvious junk is rejected at the door, and borderline mail is marked so it lands in the Junk folder. With it running your mailboxes stay usable — without any spam filter they fill up with junk fast.',
            tips: [
                'Rspamd and SpamAssassin do the same job — install and run only one of them at a time.',
                'For the first few days, watch the journal below: each line shows the score a message received and the reasons behind it.',
                'If a legitimate sender keeps getting blocked, add them to the allow list in the config files below, then restart.',
                'If Rspamd misbehaves, prefer a restart or a config fix over uninstalling — running mail with no spam filter gets unpleasant quickly.',
                'Rspamd only screens incoming mail; it doesn\'t slow down or alter what you send.',
            ],
            troubleshoot: [
                { symptom: 'Legitimate mail is rejected or filed as junk.', fix: 'Filter the journal by the sender\'s address to see the score and the reasons. If the reasons are missing SPF/DKIM records, ask the sender to fix them on their side; otherwise add the sender to your allow list in the config editor and restart.' },
                { symptom: 'Spam still gets through.', fix: 'A fresh filter always lets some spam past — it improves as it learns. Make sure Rspamd is actually running: if mail is arriving but the journal shows no activity, restart both Rspamd and Postfix so they reconnect.' },
                { symptom: 'Mail flow slowed down or stopped after Rspamd broke.', fix: 'Postfix waits for the filter\'s verdict, so a stuck filter delays everything. Restart Rspamd with the button above; if it won\'t start, the first error in the journal explains why. As a last resort, an expert can temporarily unhook the filter in Postfix\'s configuration.' },
            ],
        },
    },
    'spamassassin': {
        tr: {
            what: 'SpamAssassin, Rspamd ile aynı koltuğa oturabilen köklü bir istenmeyen posta filtresidir: Postfix\'e gelen iletileri puanlar ve çöpü posta kutularından uzak tutar. Kural setini tercih ediyorsanız ya da onu zaten iyi tanıyorsanız seçin; aksi halde daha modern olan Rspamd genelde ilk tercihtir. Aynı anda yalnızca bir spam filtresi etkin olmalı.',
            tips: [
                'Ya SpamAssassin ya Rspamd — asla ikisi birden; birini seçin, diğerini durdurun ya da kaldırın.',
                'Deneyimden öğrenir; doğruluğunu değerlendirmeden önce birkaç gün gerçek posta akışı tanıyın.',
                'Her iletinin puanı aşağıdaki günlükte görünür — bir şeyin neden işaretlendiğini görmek için göndericiye göre filtreleyin.',
                'İzin ve engel listeleri aşağıdaki yapılandırma dosyalarındadır; düzenledikten sonra yeni kuralların geçerli olması için yeniden başlatın.',
                'Rspamd\'ye göre hatırı sayılır ölçüde fazla bellek kullanır — küçük sunucularda bunu göz önünde bulundurun.',
            ],
            troubleshoot: [
                { symptom: 'İyi postalar spam olarak işaretleniyor.', fix: 'İletiyi günlükte bulup hangi kuralların tetiklendiğine bakın. Göndereni editörden izin listesine ekleyip yeniden başlatın. Sorun birçok gönderende tekrarlıyorsa puan eşiğini biraz gevşetmek yardımcı olur.' },
                { symptom: 'Gelen postalar gecikiyor.', fix: 'Puanlama, yoğun ya da küçük sunucularda zaman alabilir. Gecikme dakikaları buluyorsa günlükte zaman aşımı hatası arayın ve hizmeti yeniden başlatın; sürerse daha hafif olan Rspamd\'ye geçmeyi düşünün.' },
                { symptom: 'Hizmet başlamıyor.', fix: 'Günlükteki ilk hata satırları genellikle bozuk bir kural ya da ayar dosyasını gösterir — son düzenlemenizi geri alıp yeniden başlatın. Hiçbir şey düzenlemediyseniz sunucunun belleği yetmiyor olabilir; daha hafif olan Rspamd\'ye geçmek en pratik çözümdür.' },
            ],
        },
        en: {
            what: 'SpamAssassin is a long-established spam filter that can take the same seat as Rspamd: it scores mail arriving through Postfix and keeps junk out of the mailboxes. Choose it if you prefer its rule set or already know it well; otherwise the more modern Rspamd is the usual pick. Only one spam filter should be active at a time.',
            tips: [
                'Run either SpamAssassin or Rspamd, never both — pick one and stop or uninstall the other.',
                'It learns from experience; give it a few days of real mail before judging its accuracy.',
                'Each message\'s score appears in the journal below — filter by sender to see why something was flagged.',
                'Allow and block lists live in the config files below; restart after editing so the new rules apply.',
                'It uses noticeably more memory than Rspamd — worth keeping in mind on a small server.',
            ],
            troubleshoot: [
                { symptom: 'Good mail is being marked as spam.', fix: 'Find the message in the journal to see which rules fired. Add the sender to the allow list in the config editor and restart. If it happens across many senders, relaxing the score threshold a little helps.' },
                { symptom: 'Incoming mail is delayed.', fix: 'Scoring can take a moment on busy or small servers. If delays reach minutes, look for timeouts in the journal and restart the service; if it keeps happening, consider switching to the lighter Rspamd.' },
                { symptom: 'The service won\'t start.', fix: 'The first error lines in the journal usually point at a broken rule or config file — undo your last edit and restart. If you haven\'t edited anything, the server may be short on memory; switching to the lighter Rspamd is the most practical fix.' },
            ],
        },
    },
    'valkey': {
        tr: {
            what: 'Valkey, sık kullanılan verileri diskten çok daha hızlı olan bellekte tutan bir önbellek sunucusudur. WordPress, Laravel gibi uygulamalar oturum ve sorgu sonuçlarını burada saklayarak belirgin biçimde hızlanır. Redis\'in topluluk çatalıdır ve onunla aynı işi yapar; bu yüzden panel ikisini aynı koltukta tutar — aynı anda yalnız biri kurulabilir.',
            tips: [
                'Uygulamanız ona bağlanmadıkça kurmak tek başına siteyi hızlandırmaz — uygulamanın önbellek ayarında sunucu olarak 127.0.0.1:6379 seçilmiş olmalı.',
                'Redis zaten kuruluysa Valkey satırı "Redis ile çakışıyor" der; geçiş yapmak isterseniz önce Redis\'i kaldırın. Bu bir ekleme değil, bir değiştirmedir.',
                'Yalnız sunucunun kendisinden erişilir; açık portlar ekranında 6379 görünmemesi doğrudur. Kimlik doğrulamasız bir önbelleği internete açmak, bulunup boşaltılmasının en kısa yoludur.',
                'Önbellekteki veri kalıcı değildir: yeniden başlatmak onu boşaltabilir. Bu bir kayıp değildir — uygulama veriyi gerektiğinde yeniden üretir.',
                'Bir şey ters gittiğinde ilk bakılacak yer bu sayfadaki günlüktür; son birkaç satır sorunu genelde adıyla söyler.',
            ],
            troubleshoot: [
                { symptom: 'Kurdum ama sitede hiçbir hız farkı yok.', fix: 'Beklenen durum: uygulama henüz ona bağlanmıyor olabilir. Uygulamanızın önbellek ayarını kontrol edin; çoğu sistemde önbelleği ayrıca etkinleştirmek gerekir.' },
                { symptom: 'Servis çalışmıyor ya da başlar başlamaz duruyor.', fix: 'Genellikle 6379 portunu başka bir program tutuyordur — çoğu zaman Redis. Bileşenler sayfasında Redis kurulu mu bakın; aşağıdaki günlük "address already in use" diyorsa sebep budur.' },
                { symptom: 'Uygulama "bağlanamadı" hatası veriyor.', fix: 'Servisin çalıştığını doğrulayın ve yeniden başlatın. Bağlantı adresi 127.0.0.1:6379 olmalı; uygulama başka bir makineyi işaret ediyorsa oraya erişim yoktur.' },
                { symptom: 'Bellek kullanımı sürekli artıyor.', fix: 'Önbellek, sınır koyulmadıkça büyümeye devam eder. Yapılandırma dosyasından bir bellek üst sınırı belirleyip servisi yeniden başlatın; emin değilseniz bir uzmana danışın.' },
            ],
        },
        en: {
            what: 'Valkey is a cache server: it keeps frequently used data in memory, which is far faster than reading it from disk. Applications like WordPress or Laravel get noticeably quicker by storing sessions and query results here. It is the community fork of Redis and does the same job, which is why the panel gives them one seat — only one of the two can be installed at a time.',
            tips: [
                'Installing it does not speed up a site on its own — your application has to be pointed at it, usually as 127.0.0.1:6379 in its cache settings.',
                'If Redis is already installed, the Valkey row will say it conflicts with it. Switching means removing Redis first: this is a swap, not an addition.',
                'It is reachable only from this server, so seeing no port for it in the open-ports display is correct. Exposing an unauthenticated cache to the internet is the shortest path to having it found and emptied.',
                'Cached data is not permanent: a restart can empty it. That is not data loss — the application rebuilds what it needs.',
                'When something looks wrong, the log on this page is the first place to look; the last few lines usually name the problem.',
            ],
            troubleshoot: [
                { symptom: 'I installed it but the site is no faster.', fix: 'That is expected until the application actually uses it. Check your application\'s cache settings — most systems need caching switched on separately.' },
                { symptom: 'The service will not run, or stops right after starting.', fix: 'Usually another program already holds port 6379 — most often Redis. Check whether Redis is installed on the Components page; if the log below says "address already in use", that is the reason.' },
                { symptom: 'The application reports it cannot connect.', fix: 'Confirm the service is running and restart it. The address should be 127.0.0.1:6379; if the application points at another machine, it has no route to it.' },
                { symptom: 'Memory use keeps climbing.', fix: 'A cache grows until it is given a limit. Set a maximum memory value in the config file and restart the service; if you are unsure, ask an expert.' },
            ],
        },
    },
    'vsftpd': {
        tr: {
            what: 'vsftpd bir FTP sunucusudur: insanların FileZilla gibi bir FTP programıyla bu sunucuya dosya yükleyip indirmesini sağlar. Site sahiplerine dosya erişimi vermenin klasik yoludur. Yalnız şunu bilin: düz FTP şifreleri şifrelemeden gönderir; bu yüzden yalnızca kullanıcılarınız gerçekten FTP istiyorsa çalıştırın — bugünlerde çoğu kişi SFTP\'yi tercih ediyor.',
            tips: [
                'FTP\'nin erişilebilir olduğunu doğrulamak için açık portlar ekranında 21 numaralı portu kontrol edin.',
                'FTP, asıl dosya aktarımı için ayrıca bir \'pasif\' port aralığına ihtiyaç duyar — bu portlar sağlayıcınızın güvenlik duvarında açık değilse giriş çalışır ama aktarımlar takılır.',
                'Listeden bir yapılandırma dosyasını düzenledikten sonra değişikliğin uygulanması için servisi yeniden başlatın.',
                'FTP\'yi gerçekten kullanan yoksa durdurun ya da kaldırın — ihtiyaç duymadığınız her servis, korumak zorunda olmadığınız bir kapıdır.',
            ],
            troubleshoot: [
                { symptom: 'Giriş oluyor ama dosya listesi hiç gelmiyor ya da aktarımlar takılıyor.', fix: 'Bu klasik pasif mod sorunudur: yapılandırmadaki pasif port aralığının sunucu güvenlik duvarında ya da sağlayıcı panelinde de açık olması gerekir. Portlar ve güvenlik duvarları size yabancıysa bu işi bir uzmana bırakmak yerinde olur.' },
                { symptom: 'Şifre doğru olduğu halde giriş reddediliyor.', fix: 'Başarısız bir denemenin hemen ardından aşağıdaki günlüğe bakın — nedeni genelde yazar; çoğu zaman o kullanıcının ev klasörü eksik ya da yanlış ayarlıdır.' },
                { symptom: 'FTP programı bağlantının güvensiz olduğu uyarısını veriyor.', fix: 'Haklı: düz FTP şifrelenmemiştir. Kullanıcılara SFTP\'ye geçmelerini önerin ya da bir uzmana yapılandırmada FTPS\'yi (şifreli FTP) açtırın.' },
            ],
        },
        en: {
            what: 'vsftpd is an FTP server: it lets people upload and download files here using an FTP program such as FileZilla. It\'s the classic way to give website owners access to their files. Keep in mind that plain FTP sends passwords unencrypted, so only run it if your users really need FTP — many people prefer SFTP these days.',
            tips: [
                'Check the open-ports display for port 21 to confirm FTP is reachable.',
                'FTP also needs a range of extra \'passive\' ports for the actual file transfers — if those aren\'t open in your provider\'s firewall, logins work but transfers hang.',
                'After editing a config file from the list, restart the service to apply the change.',
                'If nobody actually uses FTP, stop or uninstall it — every service you don\'t need is a door you don\'t have to guard.',
            ],
            troubleshoot: [
                { symptom: 'Clients log in fine, but the file list never appears or transfers stall.', fix: 'This is the classic passive-mode problem: the passive port range in the config must also be open in the server firewall or your provider\'s panel. If ports and firewalls are unfamiliar territory, this one is worth handing to an expert.' },
                { symptom: 'Login is refused even with the correct password.', fix: 'Check the log below right after a failed attempt — it usually states the reason, often a missing or misconfigured home folder for that user.' },
                { symptom: 'The FTP program warns that the connection is insecure.', fix: 'It\'s right: plain FTP is unencrypted. Advise users to switch to SFTP, or have an expert enable FTPS (encrypted FTP) in the config.' },
            ],
        },
    },
    'wireguard': {
        tr: {
            what: 'WireGuard, hızlı ve modern bir VPN sunucusudur. Nerede olursanız olun, cihazlarınızın bu sunucuya şifreli bir tünelle — sanki yanı başındaymışsınız gibi — bağlanmasını sağlar. Panel kurulum sırasında her şeyi ayarladı; kimlerin bağlanabileceğini VPN sayfasından yönetirsiniz.',
            tips: [
                'Her kişi ya da cihaz için VPN sayfasında ayrı bir istemci (peer) oluşturun — böylece ileride birini silmek diğerlerini etkilemez.',
                'Telefonlarda en pratik yol, resmi WireGuard uygulamasıyla QR kodu okutmak; bilgisayarlarda yapılandırma dosyasını indirip içe aktarın.',
                'Bir cihaz bağlanamaz olduysa çoğu zaman en hızlı çözüm o istemciyi silip yenisini oluşturmaktır — saniyeler sürer ve yepyeni anahtarlar üretir.',
                'WireGuard UDP kullanır; erişimi kontrol ederken açık portlar ekranında UDP portuna bakın.',
                'Günlüğün sessiz olması burada normaldir — WireGuard her şey yolundayken bile neredeyse hiçbir şey yazmaz.',
            ],
            troubleshoot: [
                { symptom: 'İstemci \'bağlı\' görünüyor ama veri akmıyor.', fix: 'WireGuard, karşı taraf yanıt vermese bile \'bağlı\' gösterebilir. Yukarıdaki düğmeyle servisi yeniden başlatın ve açık portlar ekranındaki UDP portunun sunucu sağlayıcınızın güvenlik duvarında engellenmediğinden emin olun.' },
                { symptom: 'Daha önce çalışan bir cihaz artık bağlanamıyor.', fix: 'VPN sayfasında o cihazın istemcisini silip yenisini oluşturun; yeni yapılandırma dosyasını ya da QR kodu cihaza yükleyin.' },
                { symptom: 'Hiç kimse bağlanamıyor.', fix: 'Servisi yeniden başlatın ve aşağıdaki günlükte hata olup olmadığına bakın. Sorun sürerse UDP portu dışarıda engelleniyor olabilir — bu genelde sunucu sağlayıcının güvenlik duvarı ayarıdır; sağlayıcı desteği ya da bir uzman doğrulayabilir.' },
            ],
        },
        en: {
            what: 'WireGuard is a fast, modern VPN server. It lets you and your devices connect to this server through an encrypted tunnel from anywhere, as if you were sitting right next to it. The panel set it up during installation, and you manage who can connect on the VPN page.',
            tips: [
                'Create a separate peer on the VPN page for each person or device — that way you can remove one later without affecting the others.',
                'On phones, the quickest setup is scanning the QR code with the official WireGuard app; on computers, download the config file and import it.',
                'If a device stops connecting, the fastest fix is often deleting its peer and creating a fresh one — it takes seconds and issues brand-new keys.',
                'WireGuard uses UDP, so look for its UDP port in the open-ports display when checking that it\'s reachable.',
                'A quiet log is normal here — WireGuard writes almost nothing even when everything is working perfectly.',
            ],
            troubleshoot: [
                { symptom: 'A client says it\'s connected, but no traffic actually flows.', fix: 'WireGuard can show \'connected\' even when the other side isn\'t answering. Restart the service with the button above, and check that the UDP port shown in the open-ports display isn\'t blocked by your hosting provider\'s firewall.' },
                { symptom: 'A device that used to work suddenly can\'t connect.', fix: 'Delete that device\'s peer on the VPN page and create a new one, then load the new config file or QR code onto the device.' },
                { symptom: 'Nobody can connect at all.', fix: 'Restart the service and look for errors in the log below. If it still fails, the UDP port may be blocked further upstream — that\'s usually a hosting provider firewall setting, and their support (or an expert) can confirm it.' },
            ],
        },
    },
};

// Generic fallbacks by kind, so a component added to the catalogue tomorrow
// opens with sensible help before anyone writes its specific entry.
// Türe göre genel yedekler; yarın kataloğa eklenen bileşen, özel kaydı
// yazılmadan önce de makul bir yardımla açılır.
export const GENERIC_HELP: Record<string, LocalizedHelp> = {
    'runtime': {
        tr: {
            what: 'Bu bir çalışma ortamıdır (runtime): belirli bir dilde yazılmış uygulamaları çalıştıran motor — PHP, Node.js, Python gibi — ve belirli bir sürüm olarak kurulur. Bu sunucudaki siteler ve uygulamalar kodlarını çalıştırmak için ona bağımlıdır. Birden fazla sürüm yan yana kurulabilir; böylece her uygulama, yapıldığı sürümde kalabilir.',
            tips: [
                'Bir sürümü kaldırmadan önce hiçbir sitenin ya da uygulamanın onu kullanmadığından emin olun — motoru kaybolan uygulama, çalışmayı bırakır.',
                'Yükseltme mi yapıyorsunuz? Yeni sürümü yanına kurun, uygulamaları teker teker taşıyın; her şeyin çalıştığı doğrulanmadan eski sürümü kaldırmayın.',
                'Çalışma ortamının bir arka plan servisi varsa (PHP\'nin FPM\'i gibi), ayar değişikliklerinden sonra yeni ayarların geçerli olması için onu yeniden başlatın.',
                'Bir uygulama sürüm değişikliğinin hemen ardından tuhaflaşırsa, araştırırken eski sürüme geri dönmek gayet doğru bir hamledir.',
            ],
            troubleshoot: [
                { symptom: 'Sürüm yükseltme ya da değişikliğinden hemen sonra siteler bozuldu.', fix: 'O siteleri önceki sürüme geri alın — hizmet anında geri gelir. Yeniden denemeden önce uygulamanın resmî olarak hangi sürümleri desteklediğine bakın.' },
                { symptom: 'Bir uygulama, bir modülün ya da eklentinin eksik olduğundan yakınıyor.', fix: 'Eklentiler belirli bir sürüme aittir; uygulamanın kullandığı sürüm için kurulmaları gerekir. Panel o eklentiyi sunmuyorsa bu bir uzman işidir.' },
                { symptom: 'Bir sürüm kaldırıldıktan sonra bir şeyler çalışmaz oldu.', fix: 'O sürümü panelden yeniden kurun — ona bağlı uygulamalar toparlanır. Sonra tekrar kaldırmadan önce onları düzgünce taşıyın.' },
            ],
        },
        en: {
            what: 'This is a runtime: the engine that executes applications written in a particular language — PHP, Node.js, Python and the like — installed as a specific version. Websites and apps on this server depend on it to run their code. Several versions can live side by side, so each app can stay on the version it was built for.',
            tips: [
                'Before uninstalling a version, make sure no site or app still uses it — an app whose engine disappears simply stops working.',
                'Upgrading? Install the new version alongside, move apps over one by one, and only remove the old version once everything is confirmed working.',
                'If the runtime has a background service (like PHP\'s FPM), restart it after config changes so the new settings apply.',
                'When an app misbehaves right after a version switch, switching it back is a perfectly good move while you investigate.',
            ],
            troubleshoot: [
                { symptom: 'Sites broke right after a version upgrade or switch.', fix: 'Switch those sites back to the previous version — that restores service immediately. Then check which versions the app officially supports before trying again.' },
                { symptom: 'An app complains a module or extension is missing.', fix: 'Extensions belong to a specific version, so they must be installed for the exact version the app uses. If the panel doesn\'t offer that extension, this is a job for an expert.' },
                { symptom: 'Something stopped working after a version was removed.', fix: 'Reinstall that version from the panel — apps pointing at it will recover. Then migrate them properly before removing it again.' },
            ],
        },
    },
    'service': {
        tr: {
            what: 'Bu bir arka plan servisidir: sunucuyla birlikte başlayan ve kendi kendine çalışmaya devam eden bir program (sistem tarafından bir birim olarak yönetilir — systemd). Panelde ona özel bir sayfa yok ama temel her şeyi buradan yapabilirsiniz: başlatın, durdurun, yeniden başlatın, günlüğünü okuyun ve yapılandırma dosyalarını açın.',
            tips: [
                'Aşağıdaki günlük en iyi dostunuzdur: bir servisin yaşadığı hemen her sorun orada, çoğu zaman son birkaç satırda yazar.',
                'Yeniden başlatmak çoğu servis için güvenli bir ilk yardımdır — ama uzun süre durdurmadan önce buna nelerin bağlı olabileceğini bir saniye düşünün.',
                'Adını tanımıyorsunuz diye bir servisi durdurmayın. Gösterişsiz isimli birçok servis sunucuyu sessizce ayakta tutar — önce ne olduğunu araştırın.',
                'Ağ servisiyse, açık portlar ekranı beklediğiniz portu gerçekten dinleyip dinlemediğini gösterir.',
                'Yapılandırma dosyalarından birini düzenledikten sonra servisi yeniden başlatın — çoğu program ayarlarını yalnızca açılışta okur.',
            ],
            troubleshoot: [
                { symptom: 'Servis \'başarısız\' görünüyor.', fix: 'Aşağıdaki günlüğün son satırlarını okuyun — neden hemen her zaman orada açıkça yazar. Onu düzeltin (çoğu zaman yakın tarihli bir yapılandırma değişikliğini geri almak yeter) ve yeniden başlatın. Mesaj size bir şey ifade etmiyorsa kopyalayıp bir uzmana sorun; görmek isteyecekleri şey tam olarak budur.' },
                { symptom: 'Sürekli durup yeniden başlıyor.', fix: 'Bu bir çökme döngüsüdür; günlükte aynı hata tekrarlanır. Baş şüpheli yakın tarihli bir yapılandırma değişikliğidir — geri alın ve yeniden başlatın.' },
                { symptom: 'Çalışıyor görünüyor ama yaptığı iş gerçekleşmiyor.', fix: 'Yeniden başlatın, sonra tekrar denerken günlüğü izleyin. Ağ servislerinde ayrıca portunun açık portlar ekranında göründüğünü doğrulayın.' },
            ],
        },
        en: {
            what: 'This is a background service: a program that starts with the server and keeps running on its own (managed by the system as a unit, via systemd). The panel doesn\'t have a dedicated page for it, but you can still do the essentials right here — start it, stop it, restart it, read its log, and open its config files.',
            tips: [
                'The log below is your best friend: almost every problem a service has is written there, usually in its last few lines.',
                'A restart is safe first aid for most services — but think for a second about what might depend on this one before stopping it for long.',
                'Don\'t stop a service just because you don\'t recognize its name. Many unglamorous-sounding services quietly keep the server alive — look it up first.',
                'If it\'s a network service, the open-ports display shows whether it\'s actually listening where you expect.',
                'After editing one of its config files, restart the service — most programs only read their settings at startup.',
            ],
            troubleshoot: [
                { symptom: 'The service shows as failed.', fix: 'Read the last lines of the log below — the reason is nearly always spelled out there. Fix that (often by undoing a recent config change) and restart. If the message means nothing to you, copy it and ask an expert; it\'s exactly what they\'ll want to see.' },
                { symptom: 'It keeps stopping and starting in a loop.', fix: 'That\'s a crash loop, and the log will show the same error repeating. A recent config edit is the usual suspect — revert it and restart.' },
                { symptom: 'It shows as running, but whatever it does isn\'t happening.', fix: 'Restart it, then watch the log while you try again. For network services, also confirm its port appears in the open-ports display.' },
            ],
        },
    },
    'tool': {
        tr: {
            what: 'Bu bir yardımcı araçtır: yalnızca bir şey onu çağırdığında çalışan, kendine ait arka plan süreci olmayan bir program. Yani başlatılacak ya da durdurulacak bir şey ve canlı bir durum göstergesi yoktur — bu bir sorun değil, normalin ta kendisidir. Panel onu kurabilir, güncelleyebilir ve kaldırabilir; sunucudaki başka yazılımlar da onu perde arkasında sessizce kullanıyor olabilir.',
            tips: [
                'Burada yeşil bir \'çalışıyor\' ışığı aramayın — bu tür araçların çalışır durumu yoktur ve bu gayet normaldir.',
                'Kaldırmadan önce iki kez düşünün: yedekleme, e-posta ya da web bileşenleri bu aracı siz hiç görmeden çağırıyor olabilir. Emin değilseniz bırakın — boştaki bir aracın maliyeti yok denecek kadar azdır.',
                'Kendi günlüğü yoktur. Onu kullanan bir servis sorun çıkarırsa, o servisin günlüğüne bakın.',
            ],
            troubleshoot: [
                { symptom: 'Başlat düğmesi ya da durum göstergesi yok.', fix: 'Beklenen durum bu. Bu bir arka plan servisi değil — yalnızca çağrıldığında çalışır; başlatılacak ya da izlenecek bir şey yok.' },
                { symptom: 'Başka bir servis bu aracın eksik olduğundan yakınıyor.', fix: 'Aracı panelden yeniden kurun, sonra yakınan servisi yeniden başlatın.' },
                { symptom: 'Aracın eski kaldığından şüpheleniyorsunuz.', fix: 'Panelden yeniden kurun ya da güncelleyin — sisteminiz için paketlenmiş güncel sürümü getirir.' },
            ],
        },
        en: {
            what: 'This is a helper tool: a program that runs only when something calls it, with no background process of its own. That means there\'s nothing to start or stop and no live status — and that\'s normal, not a problem. The panel can install, update, or remove it, and other software on this server may rely on it quietly behind the scenes.',
            tips: [
                'Don\'t look for a green \'running\' light here — tools like this have no running state, and that\'s fine.',
                'Think twice before uninstalling: backup, mail, or web components may call this tool without you ever seeing it. When unsure, leave it — an idle tool costs almost nothing.',
                'It writes no log of its own. If a service that uses it misbehaves, read that service\'s log instead.',
            ],
            troubleshoot: [
                { symptom: 'There\'s no start button or status for it.', fix: 'That\'s expected. This isn\'t a background service — it only runs when called, so there\'s nothing to start or monitor.' },
                { symptom: 'Another service complains this tool is missing.', fix: 'Reinstall the tool from the panel, then restart the service that complained.' },
                { symptom: 'You suspect it\'s outdated.', fix: 'Reinstall it from the panel — that fetches the current version packaged for this system.' },
            ],
        },
    },
};

export function getServiceHelp(serviceId: string, kind: string, locale: string): HelpContent | null {
    const entry = SERVICE_HELP[serviceId] ?? GENERIC_HELP[kind] ?? GENERIC_HELP['service'];
    if (!entry) return null;
    return locale === 'tr' ? entry.tr : entry.en;
}
