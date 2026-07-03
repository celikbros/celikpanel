import { useState, useRef, useEffect } from 'react';
import { Server, LogOut, ChevronDown, Menu } from 'lucide-react';
import { api } from '../lib/api';
import { useAuth } from '../auth/AuthContext';
import { useI18n } from '../i18n';
import { navItemsForRole, navGroups, type NavItem } from '../nav';
import { ThemeSwitcher } from './ThemeSwitcher';
import { LanguageSwitcher } from './LanguageSwitcher';
import type { TranslationKey } from '../i18n/en';

// The single inherited shell: a dark navigation rail (grouped, with live
// counts) beside a light content column with a top bar. Feature pages
// render in the outlet unchanged.
//
// Tek kalıtımsal kabuk: koyu bir navigasyon rayı (gruplu, canlı sayılarla)
// ve yanında üst çubuklu açık bir içerik sütunu. Özellik sayfaları içerik
// alanında değişmeden render edilir.
interface LayoutProps {
    children: React.ReactNode;
    currentPage: string;
    onPageChange: (id: string) => void;
}

type Counts = Partial<Record<'domains' | 'databases' | 'services', number>>;

export function Layout({ children, currentPage, onPageChange }: LayoutProps) {
    const { role } = useAuth();
    const [counts, setCounts] = useState<Counts>({});
    const [mobileOpen, setMobileOpen] = useState(false);

    // Live counts feed the sidebar badges. Failures are silent — a missing
    // badge is better than a broken shell.
    // Canlı sayılar kenar çubuğu rozetlerini besler. Hatalar sessizdir —
    // eksik bir rozet, bozuk bir kabuktan iyidir.
    useEffect(() => {
        fetch('/api/v1/domains')
            .then((r) => (r.ok ? r.json() : []))
            .then((d) => setCounts((c) => ({ ...c, domains: Array.isArray(d) ? d.length : 0 })))
            .catch(() => {});
        api.getServices()
            .then((s) => setCounts((c) => ({ ...c, services: s.length })))
            .catch(() => {});
    }, []);

    return (
        <div className="flex h-screen bg-bg text-fg">
            <Sidebar
                role={role}
                counts={counts}
                currentPage={currentPage}
                onPageChange={(id) => {
                    onPageChange(id);
                    setMobileOpen(false);
                }}
                mobileOpen={mobileOpen}
                onCloseMobile={() => setMobileOpen(false)}
            />

            <div className="flex min-w-0 flex-1 flex-col">
                <header className="flex items-center gap-3 border-b border-border bg-surface px-4 py-2.5 md:px-6">
                    <button
                        className="rounded-lg p-1.5 text-fg-muted hover:bg-surface-2 md:hidden"
                        onClick={() => setMobileOpen(true)}
                        aria-label="Menu"
                    >
                        <Menu className="h-5 w-5" />
                    </button>
                    <div className="ml-auto flex items-center gap-2 sm:gap-3">
                        <div className="hidden sm:block">
                            <LanguageSwitcher />
                        </div>
                        <ThemeSwitcher />
                        <UserMenu />
                    </div>
                </header>

                <main className="flex-1 overflow-auto">{children}</main>
            </div>
        </div>
    );
}

function Sidebar({
    role,
    counts,
    currentPage,
    onPageChange,
    mobileOpen,
    onCloseMobile,
}: {
    role: ReturnType<typeof useAuth>['role'];
    counts: Counts;
    currentPage: string;
    onPageChange: (id: string) => void;
    mobileOpen: boolean;
    onCloseMobile: () => void;
}) {
    const { t } = useI18n();
    const items = navItemsForRole(role);

    const content = (
        <div className="flex h-full w-64 flex-col bg-sidebar text-sidebar-fg">
            <div className="flex items-center gap-2.5 border-b border-sidebar-border px-5 py-4">
                <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary text-primary-fg">
                    <Server className="h-5 w-5" />
                </div>
                <span className="text-lg font-bold text-white">{t('app.name')}</span>
            </div>

            <nav className="flex-1 space-y-4 overflow-y-auto px-3 py-4">
                {navGroups.map((group) => {
                    const groupItems = items.filter((i) => i.group === group.id);
                    if (groupItems.length === 0) return null;
                    return (
                        <div key={group.id}>
                            {group.labelKey && (
                                <p className="px-3 pb-1.5 text-[11px] font-semibold uppercase tracking-wider text-sidebar-heading">
                                    {t(group.labelKey)}
                                </p>
                            )}
                            <div className="space-y-0.5">
                                {groupItems.map((item) => (
                                    <SidebarItem
                                        key={item.id}
                                        item={item}
                                        active={currentPage === item.id}
                                        count={item.countKey ? counts[item.countKey] : undefined}
                                        onClick={() => onPageChange(item.id)}
                                    />
                                ))}
                            </div>
                        </div>
                    );
                })}
            </nav>

            <div className="border-t border-sidebar-border px-4 py-3 text-xs text-sidebar-muted">
                {t('app.name')} · v0.1.0
            </div>
        </div>
    );

    return (
        <>
            <aside className="hidden shrink-0 md:block">{content}</aside>
            {mobileOpen && (
                <div className="fixed inset-0 z-40 md:hidden">
                    <div className="absolute inset-0 bg-black/50" onClick={onCloseMobile} />
                    <div className="absolute left-0 top-0 h-full">{content}</div>
                </div>
            )}
        </>
    );
}

function SidebarItem({
    item,
    active,
    count,
    onClick,
}: {
    item: NavItem;
    active: boolean;
    count?: number;
    onClick: () => void;
}) {
    const { t } = useI18n();
    const Icon = item.icon;
    return (
        <button
            onClick={onClick}
            className={`flex w-full items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors ${
                active
                    ? 'bg-sidebar-active text-sidebar-active-fg'
                    : 'text-sidebar-fg hover:bg-sidebar-hover'
            }`}
        >
            <Icon className="h-[18px] w-[18px] shrink-0" />
            <span className="flex-1 text-left">{t(item.labelKey)}</span>
            {count !== undefined && count > 0 && (
                <span
                    className={`rounded-full px-1.5 py-0.5 text-[11px] font-semibold ${
                        active ? 'bg-white/20 text-white' : 'bg-sidebar-hover text-sidebar-muted'
                    }`}
                >
                    {count}
                </span>
            )}
        </button>
    );
}

// UserMenu shows the current user and role with a dropdown to sign out.
// UserMenu, mevcut kullanıcıyı ve rolü, çıkış için bir açılır menüyle gösterir.
function UserMenu() {
    const { user, role } = useAuth();
    const { t } = useI18n();
    const [open, setOpen] = useState(false);
    const ref = useRef<HTMLDivElement>(null);
    const roleKey = `role.${role}` as TranslationKey;

    useEffect(() => {
        if (!open) return;
        const onClick = (e: MouseEvent) => {
            if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
        };
        document.addEventListener('mousedown', onClick);
        return () => document.removeEventListener('mousedown', onClick);
    }, [open]);

    return (
        <div className="relative" ref={ref}>
            <button
                onClick={() => setOpen((o) => !o)}
                className="flex items-center gap-2 rounded-lg border border-border bg-surface-2 py-1 pl-1 pr-2 text-sm transition-colors hover:border-border-strong"
            >
                <span className="flex h-7 w-7 items-center justify-center rounded-md bg-primary text-xs font-semibold text-primary-fg">
                    {user.username.slice(0, 2).toUpperCase()}
                </span>
                <span className="hidden max-w-[8rem] truncate font-medium sm:block">{user.username}</span>
                <ChevronDown className="h-4 w-4 text-fg-subtle" />
            </button>

            {open && (
                <div className="absolute right-0 z-20 mt-2 w-52 overflow-hidden rounded-xl border border-border bg-surface shadow-lg">
                    <div className="border-b border-border px-4 py-3">
                        <p className="truncate text-sm font-semibold">{user.username}</p>
                        <p className="text-xs text-fg-subtle">{t(roleKey)}</p>
                    </div>
                    <div className="p-2 sm:hidden">
                        <LanguageSwitcher />
                    </div>
                    <button
                        onClick={async () => {
                            await api.logout();
                            window.location.reload();
                        }}
                        className="flex w-full items-center gap-2 px-4 py-2.5 text-sm text-fg-muted transition-colors hover:bg-surface-2 hover:text-danger"
                    >
                        <LogOut className="h-4 w-4" />
                        {t('user.logout')}
                    </button>
                </div>
            )}
        </div>
    );
}
