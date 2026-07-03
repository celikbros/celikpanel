import type { TranslationKey } from './en';

// Turkish catalog. Must cover exactly the keys defined by the English
// source; the Record type enforces that at compile time.
// Türkçe katalog. İngilizce kaynağın tanımladığı anahtarların tamamını
// kapsamalıdır; Record tipi bunu derleme zamanında zorlar.
export const tr: Record<TranslationKey, string> = {
    'app.name': 'CelikPanel',
    'app.tagline': 'Kontrol Paneli',

    'nav.dashboard': 'Panel',
    'nav.domains': 'Alan Adları',
    'nav.databases': 'Veritabanları',
    'nav.services': 'Servisler',
    'nav.users': 'Kullanıcılar',
    'nav.settings': 'Ayarlar',

    'login.subtitle': 'Kontrol panelinize giriş yapın',
    'login.username': 'Kullanıcı adı',
    'login.password': 'Parola',
    'login.signIn': 'Giriş yap',
    'login.signingIn': 'Giriş yapılıyor…',
    'login.invalid': 'Kullanıcı adı ya da parola hatalı.',
    'login.failed': 'Giriş başarısız. Lütfen tekrar deneyin.',
    'login.tooMany': 'Çok fazla deneme. Lütfen biraz bekleyip tekrar deneyin.',

    'theme.label': 'Tema',
    'theme.light': 'Açık',
    'theme.dark': 'Koyu',
    'theme.system': 'Sistem',

    'lang.label': 'Dil',

    'user.logout': 'Çıkış yap',
    'role.admin': 'Yönetici',
    'role.reseller': 'Bayi',
    'role.customer': 'Müşteri',
    'role.additional_user': 'Kullanıcı',

    'dashboard.title': 'Panel',
    'dashboard.welcome': 'Tekrar hoş geldiniz, {name}',
    'dashboard.services': 'Servisler',
    'dashboard.servicesRunning': '{total} servisin {running} tanesi çalışıyor',
    'dashboard.domains': 'Alan Adları',
    'dashboard.databases': 'Veritabanları',
    'dashboard.quickActions': 'Hızlı işlemler',
    'dashboard.addDomain': 'Alan adı ekle',
    'dashboard.manageServices': 'Servisleri yönet',
    'dashboard.viewDatabases': 'Veritabanlarını gör',

    'common.loading': 'Yükleniyor…',
    'common.error': 'Bir şeyler ters gitti.',
    'common.retry': 'Tekrar dene',
    'common.back': 'Geri',
};
