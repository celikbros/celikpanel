import { LayoutDashboard, Globe, Database, Server, Users, Settings, type LucideIcon } from 'lucide-react';
import type { TranslationKey } from './i18n/en';
import type { Role } from './auth/AuthContext';

// The single source of navigation. Each entry declares the roles that may
// see it; the sidebar is this list filtered by the current user's role, and
// route guards check the same list. Adding a section is one entry here — no
// per-role layout, exactly as docs/UI_ARCHITECTURE.md prescribes.
//
// Navigasyonun tek kaynağı. Her girdi, onu görebilecek rolleri bildirir;
// kenar çubuğu bu listenin mevcut kullanıcının rolüne göre süzülmüş halidir
// ve rota koruyucuları aynı listeyi kontrol eder. Bir bölüm eklemek buraya
// tek bir girdidir — rol-başına yerleşim yok, tam olarak
// docs/UI_ARCHITECTURE.md'nin öngördüğü gibi.
export interface NavItem {
    id: string;
    path: string;
    labelKey: TranslationKey;
    icon: LucideIcon;
    roles: Role[];
}

const ALL: Role[] = ['admin', 'reseller', 'customer', 'additional_user'];

export const navItems: NavItem[] = [
    { id: 'dashboard', path: '/', labelKey: 'nav.dashboard', icon: LayoutDashboard, roles: ALL },
    { id: 'domains', path: '/domains', labelKey: 'nav.domains', icon: Globe, roles: ALL },
    { id: 'databases', path: '/databases', labelKey: 'nav.databases', icon: Database, roles: ALL },
    { id: 'services', path: '/services', labelKey: 'nav.services', icon: Server, roles: ['admin'] },
    { id: 'users', path: '/users', labelKey: 'nav.users', icon: Users, roles: ['admin', 'reseller'] },
    { id: 'settings', path: '/settings', labelKey: 'nav.settings', icon: Settings, roles: ALL },
];

// navItemsForRole returns the items a role may see, preserving order.
// navItemsForRole, bir rolün görebileceği öğeleri sırayı koruyarak döndürür.
export function navItemsForRole(role: Role): NavItem[] {
    return navItems.filter((item) => item.roles.includes(role));
}

// canAccessPath guards routes: a role may open a path only if it maps to a
// nav item that role can see. Unknown paths default to allowed (they are
// child pages like /domains/:name gated by their parent section).
// canAccessPath rotaları korur: bir rol, yalnızca o rolün görebileceği bir
// nav öğesine eşlenen bir yolu açabilir. Bilinmeyen yollar varsayılan olarak
// izinlidir (bunlar /domains/:name gibi, üst bölümleriyle korunan alt
// sayfalardır).
export function canAccessPath(role: Role, path: string): boolean {
    const match = navItems.find((item) => item.path === path);
    return match ? match.roles.includes(role) : true;
}
