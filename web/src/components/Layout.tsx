import { useState, useRef, useEffect } from 'react';
import { Server, LogOut, ChevronDown } from 'lucide-react';
import { api } from '../lib/api';
import { useAuth } from '../auth/AuthContext';
import { useI18n } from '../i18n';
import { navItemsForRole } from '../nav';
import { ThemeSwitcher } from './ThemeSwitcher';
import { LanguageSwitcher } from './LanguageSwitcher';
import type { TranslationKey } from '../i18n/en';

// The single inherited shell. Sidebar items come from the nav registry
// filtered by the user's role; the top bar carries theme, language and the
// user menu. Feature pages render in the content outlet unchanged.
//
// Tek kalıtımsal kabuk. Kenar çubuğu öğeleri, kullanıcının rolüne göre
// süzülmüş nav kaydından gelir; üst çubuk tema, dil ve kullanıcı menüsünü
// taşır. Özellik sayfaları içerik alanında değişmeden render edilir.
interface LayoutProps {
    children: React.ReactNode;
    currentPage: string;
    onPageChange: (id: string) => void;
}

export function Layout({ children, currentPage, onPageChange }: LayoutProps) {
    const { user, role } = useAuth();
    const { t } = useI18n();
    const items = navItemsForRole(role);

    return (
        <div className="flex h-screen bg-bg text-fg">
            {/* Sidebar */}
            <aside className="hidden w-64 shrink-0 flex-col border-r border-border bg-surface md:flex">
                <div className="flex items-center gap-3 border-b border-border px-6 py-5">
                    <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-primary text-primary-fg">
                        <Server className="h-5 w-5" />
                    </div>
                    <div>
                        <h1 className="text-base font-bold leading-tight">{t('app.name')}</h1>
                        <p className="text-xs text-fg-subtle">{t('app.tagline')}</p>
                    </div>
                </div>

                <nav className="flex-1 space-y-1 p-3">
                    {items.map((item) => {
                        const Icon = item.icon;
                        const active = currentPage === item.id;
                        return (
                            <button
                                key={item.id}
                                onClick={() => onPageChange(item.id)}
                                className={`flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition-colors ${
                                    active
                                        ? 'bg-primary text-primary-fg'
                                        : 'text-fg-muted hover:bg-surface-2 hover:text-fg'
                                }`}
                            >
                                <Icon className="h-5 w-5" />
                                <span>{t(item.labelKey)}</span>
                            </button>
                        );
                    })}
                </nav>

                <div className="border-t border-border p-3 text-xs text-fg-subtle">
                    <p>{t('app.name')} · v0.1.0</p>
                </div>
            </aside>

            {/* Main column */}
            <div className="flex min-w-0 flex-1 flex-col">
                {/* Top bar */}
                <header className="flex items-center justify-between gap-3 border-b border-border bg-surface px-4 py-3 md:px-6">
                    <MobileNav items={items} currentPage={currentPage} onPageChange={onPageChange} />
                    <div className="ml-auto flex items-center gap-2 sm:gap-3">
                        <div className="hidden sm:block">
                            <LanguageSwitcher />
                        </div>
                        <ThemeSwitcher />
                        <UserMenu username={user.username} roleKey={`role.${role}` as TranslationKey} />
                    </div>
                </header>

                {/* Content */}
                <main className="flex-1 overflow-auto">{children}</main>
            </div>
        </div>
    );
}

// UserMenu shows the current user and role with a dropdown to sign out.
// UserMenu, mevcut kullanıcıyı ve rolü, çıkış için bir açılır menüyle gösterir.
function UserMenu({ username, roleKey }: { username: string; roleKey: TranslationKey }) {
    const { t } = useI18n();
    const [open, setOpen] = useState(false);
    const ref = useRef<HTMLDivElement>(null);

    useEffect(() => {
        if (!open) return;
        const onClick = (e: MouseEvent) => {
            if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
        };
        document.addEventListener('mousedown', onClick);
        return () => document.removeEventListener('mousedown', onClick);
    }, [open]);

    const initials = username.slice(0, 2).toUpperCase();

    return (
        <div className="relative" ref={ref}>
            <button
                onClick={() => setOpen((o) => !o)}
                className="flex items-center gap-2 rounded-lg border border-border bg-surface-2 py-1 pl-1 pr-2 text-sm transition-colors hover:border-border-strong"
            >
                <span className="flex h-7 w-7 items-center justify-center rounded-md bg-primary text-xs font-semibold text-primary-fg">
                    {initials}
                </span>
                <span className="hidden max-w-[8rem] truncate font-medium sm:block">{username}</span>
                <ChevronDown className="h-4 w-4 text-fg-subtle" />
            </button>

            {open && (
                <div className="absolute right-0 z-20 mt-2 w-52 overflow-hidden rounded-xl border border-border bg-surface shadow-lg">
                    <div className="border-b border-border px-4 py-3">
                        <p className="truncate text-sm font-semibold">{username}</p>
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

// MobileNav collapses the sidebar into a dropdown on small screens.
// MobileNav, küçük ekranlarda kenar çubuğunu bir açılır menüye toplar.
function MobileNav({
    items,
    currentPage,
    onPageChange,
}: {
    items: ReturnType<typeof navItemsForRole>;
    currentPage: string;
    onPageChange: (id: string) => void;
}) {
    const { t } = useI18n();
    const [open, setOpen] = useState(false);
    const current = items.find((i) => i.id === currentPage);

    return (
        <div className="relative md:hidden">
            <button
                onClick={() => setOpen((o) => !o)}
                className="flex items-center gap-2 rounded-lg border border-border bg-surface-2 px-3 py-2 text-sm font-medium"
            >
                {current ? t(current.labelKey) : t('app.name')}
                <ChevronDown className="h-4 w-4 text-fg-subtle" />
            </button>
            {open && (
                <div className="absolute left-0 z-20 mt-2 w-56 overflow-hidden rounded-xl border border-border bg-surface shadow-lg">
                    {items.map((item) => {
                        const Icon = item.icon;
                        return (
                            <button
                                key={item.id}
                                onClick={() => {
                                    onPageChange(item.id);
                                    setOpen(false);
                                }}
                                className={`flex w-full items-center gap-3 px-4 py-2.5 text-sm ${
                                    currentPage === item.id
                                        ? 'bg-primary/10 font-medium text-primary'
                                        : 'text-fg-muted hover:bg-surface-2'
                                }`}
                            >
                                <Icon className="h-4 w-4" />
                                {t(item.labelKey)}
                            </button>
                        );
                    })}
                </div>
            )}
        </div>
    );
}
