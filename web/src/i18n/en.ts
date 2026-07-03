// English catalog. This file is the source of truth for translation keys:
// its shape defines the TranslationKey type, and every other locale must
// provide the same keys.
//
// İngilizce katalog. Bu dosya çeviri anahtarlarının doğruluk kaynağıdır:
// şekli TranslationKey tipini tanımlar ve diğer her yerel ayar aynı
// anahtarları sağlamalıdır.
export const en = {
    'app.name': 'CelikPanel',
    'app.tagline': 'Control Panel',

    'nav.dashboard': 'Dashboard',
    'nav.domains': 'Domains',
    'nav.databases': 'Databases',
    'nav.services': 'Services',
    'nav.users': 'Users',
    'nav.settings': 'Settings',
    'nav.group.hosting': 'Hosting',
    'nav.group.server': 'Server',

    'login.subtitle': 'Sign in to your control panel',
    'login.username': 'Username',
    'login.password': 'Password',
    'login.signIn': 'Sign in',
    'login.signingIn': 'Signing in…',
    'login.invalid': 'Invalid username or password.',
    'login.failed': 'Sign-in failed. Please try again.',
    'login.tooMany': 'Too many attempts. Please wait a moment and try again.',

    'theme.label': 'Theme',
    'theme.light': 'Light',
    'theme.dark': 'Dark',
    'theme.system': 'System',

    'lang.label': 'Language',

    'user.logout': 'Sign out',
    'role.admin': 'Administrator',
    'role.reseller': 'Reseller',
    'role.customer': 'Customer',
    'role.additional_user': 'User',

    'dashboard.title': 'Dashboard',
    'dashboard.welcome': 'Welcome back, {name}',
    'dashboard.services': 'Services',
    'dashboard.servicesRunning': '{running} of {total} running',
    'dashboard.domains': 'Domains',
    'dashboard.databases': 'Databases',
    'dashboard.quickActions': 'Quick actions',
    'dashboard.addDomain': 'Add domain',
    'dashboard.manageServices': 'Manage services',
    'dashboard.viewDatabases': 'View databases',
    'dashboard.serverInfo': 'Server information',
    'dashboard.hostname': 'Hostname',
    'dashboard.os': 'Operating system',
    'dashboard.uptime': 'Uptime',
    'dashboard.cpuUsage': 'CPU usage',
    'dashboard.cores': '{n} cores',
    'dashboard.memoryUsage': 'Memory',
    'dashboard.diskUsage': 'Disk (/)',
    'dashboard.loadAverage': 'Load average',
    'dashboard.running': 'Running',
    'dashboard.stopped': 'Stopped',
    'dashboard.usedOfTotal': '{used} of {total}',
    'dashboard.viewAll': 'View all',
    'common.days': 'd',
    'common.hours': 'h',
    'common.minutes': 'm',

    'common.loading': 'Loading…',
    'common.error': 'Something went wrong.',
    'common.retry': 'Retry',
    'common.back': 'Back',
} as const;

export type TranslationKey = keyof typeof en;
