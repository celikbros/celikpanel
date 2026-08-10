import { LayoutDashboard, Globe, Database, Server, Users, Settings, DownloadCloud, ScrollText, Package, Shield, Activity, type LucideIcon } from 'lucide-react';
import type { TranslationKey } from './i18n/en';
import type { Role } from './auth/AuthContext';

// The single source of navigation. Each entry declares the roles that may
// see it and an optional group heading; the sidebar is this list filtered
// by the current user's role, and route guards check the same list. Adding
// a section is one entry here — no per-role layout, exactly as
// docs/UI_ARCHITECTURE.md prescribes.
//
// Navigasyonun tek kaynağı. Her girdi, onu görebilecek rolleri ve isteğe
// bağlı bir grup başlığını bildirir; kenar çubuğu bu listenin mevcut
// kullanıcının rolüne göre süzülmüş halidir ve rota koruyucuları aynı
// listeyi kontrol eder.
export type NavGroup = 'main' | 'hosting' | 'server';

export interface NavItem {
    id: string;
    path: string;
    labelKey: TranslationKey;
    icon: LucideIcon;
    roles: Role[];
    group: NavGroup;
    countKey?: 'domains' | 'databases' | 'services';
}

export interface NavAccessContext {
    accountType?: string;
    teamMembers?: boolean;
}

const ACCOUNT_ROLES: Role[] = ['admin', 'reseller', 'customer'];
const DOMAIN_ROLES: Role[] = [...ACCOUNT_ROLES, 'additional_user'];

export const navItems: NavItem[] = [
    { id: 'dashboard', path: '/', labelKey: 'nav.dashboard', icon: LayoutDashboard, roles: DOMAIN_ROLES, group: 'main' },
    { id: 'domains', path: '/domains', labelKey: 'nav.domains', icon: Globe, roles: DOMAIN_ROLES, group: 'hosting', countKey: 'domains' },
    // Admin-only UNTIL the v2 DB API is tenant-scoped: the backend hardcodes
    // subscription 1 (database_v2_handlers.go "TODO: Get from auth") and the
    // whole /api/v2/ prefix is admin-gated — non-admins only ever saw a
    // broken page here. Tenants manage their DBs on the domain detail tab.
    // B1 role split (Jul 17): the v2 DB API is tenant-scoped and the blanket
    // admin gate is gone — Databases is self-service again. Server
    // REGISTRATION stays admin-only in the backend; this page has no
    // register button anyway (servers are auto-discovered).
    // B1 rol ayrımı (17 Tem): v2 DB API'si kiracı-kapsamlı, battaniye admin
    // kilidi kalktı — Databases yeniden self-servis. Sunucu KAYDI arka uçta
    // admin'de kalır; bu sayfada zaten kayıt düğmesi yok (oto-keşif).
    { id: 'databases', path: '/databases', labelKey: 'nav.databases', icon: Database, roles: ['admin', 'reseller', 'customer'], group: 'hosting', countKey: 'databases' },
    { id: 'users', path: '/users', labelKey: 'nav.users', icon: Users, roles: ['admin', 'reseller', 'customer'], group: 'hosting' },
    { id: 'addons', path: '/addons', labelKey: 'nav.addons', icon: Package, roles: ['admin', 'reseller'], group: 'hosting' },
    { id: 'vpn', path: '/vpn', labelKey: 'nav.vpn', icon: Shield, roles: ACCOUNT_ROLES, group: 'hosting' },
    { id: 'services', path: '/services', labelKey: 'nav.services', icon: Server, roles: ['admin'], group: 'server', countKey: 'services' },
    { id: 'monitoring', path: '/monitoring', labelKey: 'nav.monitoring', icon: Activity, roles: ['admin'], group: 'server' },
    { id: 'import', path: '/import', labelKey: 'nav.import', icon: DownloadCloud, roles: ['admin'], group: 'server' },
    { id: 'audit', path: '/audit', labelKey: 'nav.audit', icon: ScrollText, roles: ['admin'], group: 'server' },
    { id: 'settings', path: '/settings', labelKey: 'nav.settings', icon: Settings, roles: ACCOUNT_ROLES, group: 'server' },
];

// Group order and their heading translation keys. A group with no visible
// items for the current role is skipped by the sidebar.
// Grup sırası ve başlık çeviri anahtarları. Mevcut rol için görünür öğesi
// olmayan bir grup kenar çubuğunda atlanır.
export const navGroups: { id: NavGroup; labelKey?: TranslationKey }[] = [
    { id: 'main' },
    { id: 'hosting', labelKey: 'nav.group.hosting' },
    { id: 'server', labelKey: 'nav.group.server' },
];

export function isNavItemAllowed(item: NavItem, role: Role, access?: NavAccessContext): boolean {
    if (!item.roles.includes(role)) return false;
    if (item.id === 'users' && role === 'customer') {
        return access?.accountType === 'account' && access.teamMembers === true;
    }
    return true;
}

export function navItemsForRole(role: Role, access?: NavAccessContext): NavItem[] {
    return navItems.filter((item) => isNavItemAllowed(item, role, access));
}

// canAccessPath guards routes from the same fail-closed navigation contract.
// Child pages inherit the nearest segment-aware parent; paths with no known
// parent are denied instead of accidentally inheriting customer access.
// canAccessPath rotaları korur: bir rol yalnızca görebileceği bir nav
// öğesine eşlenen bir yolu açabilir. Bilinmeyen yollar varsayılan izinli.
export function canAccessPath(role: Role, path: string, access?: NavAccessContext): boolean {
    const normalizedPath = path.split('?')[0].replace(/\/$/, '') || '/';
    const match = [...navItems]
        .sort((a, b) => b.path.length - a.path.length)
        .find((item) => item.path === '/'
            ? normalizedPath === '/'
            : normalizedPath === item.path || normalizedPath.startsWith(`${item.path}/`));
    return match ? isNavItemAllowed(match, role, access) : false;
}
