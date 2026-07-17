import { LayoutDashboard, Globe, Database, Server, Users, Settings, DownloadCloud, ScrollText, Package, Shield, type LucideIcon } from 'lucide-react';
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

const ALL: Role[] = ['admin', 'reseller', 'customer', 'additional_user'];

export const navItems: NavItem[] = [
    { id: 'dashboard', path: '/', labelKey: 'nav.dashboard', icon: LayoutDashboard, roles: ALL, group: 'main' },
    { id: 'domains', path: '/domains', labelKey: 'nav.domains', icon: Globe, roles: ALL, group: 'hosting', countKey: 'domains' },
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
    { id: 'users', path: '/users', labelKey: 'nav.users', icon: Users, roles: ['admin', 'reseller'], group: 'hosting' },
    { id: 'addons', path: '/addons', labelKey: 'nav.addons', icon: Package, roles: ['admin', 'reseller'], group: 'hosting' },
    { id: 'vpn', path: '/vpn', labelKey: 'nav.vpn', icon: Shield, roles: ALL, group: 'hosting' },
    { id: 'services', path: '/services', labelKey: 'nav.services', icon: Server, roles: ['admin'], group: 'server', countKey: 'services' },
    { id: 'import', path: '/import', labelKey: 'nav.import', icon: DownloadCloud, roles: ['admin'], group: 'server' },
    { id: 'audit', path: '/audit', labelKey: 'nav.audit', icon: ScrollText, roles: ['admin'], group: 'server' },
    { id: 'settings', path: '/settings', labelKey: 'nav.settings', icon: Settings, roles: ALL, group: 'server' },
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

export function navItemsForRole(role: Role): NavItem[] {
    return navItems.filter((item) => item.roles.includes(role));
}

// canAccessPath guards routes: a role may open a path only if it maps to a
// nav item that role can see. Unknown paths default to allowed (child pages
// like /domains/:name are gated by their parent section).
// canAccessPath rotaları korur: bir rol yalnızca görebileceği bir nav
// öğesine eşlenen bir yolu açabilir. Bilinmeyen yollar varsayılan izinli.
export function canAccessPath(role: Role, path: string): boolean {
    const match = navItems.find((item) => item.path === path);
    return match ? match.roles.includes(role) : true;
}
