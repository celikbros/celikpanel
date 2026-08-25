import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Server, LogOut, ChevronDown, Menu, KeyRound, UserCheck } from 'lucide-react';
import { api } from '../lib/api';
import { useAuth } from '../auth/AuthContext';
import { useI18n } from '../i18n';
import { navItemsForRole, navGroups, type NavAccessContext, type NavItem } from '../nav';
import { ThemeSwitcher } from './ThemeSwitcher';
import { SkinSwitcher } from './SkinSwitcher';
import { LanguageSwitcher } from './LanguageSwitcher';
import { ChangePasswordModal } from './ChangePasswordModal';
import { DesktopPageHeaderTargetContext } from './pageHeaderSlot';
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
type PanelRuntime = {
    version: string;
    commit: string;
    agent_commit: string;
    agent_matches: boolean;
    hostname: string;
    ipv4: string;
};
type ServerIdentity = Pick<PanelRuntime, 'hostname' | 'ipv4'>;

function decodePanelRuntime(value: unknown): PanelRuntime | null {
    if (!value || typeof value !== 'object') return null;
    const raw = value as Record<string, unknown>;
    if (typeof raw.version !== 'string' || raw.version.length > 80
        || typeof raw.commit !== 'string' || raw.commit.length > 80
        || typeof raw.agent_commit !== 'string' || raw.agent_commit.length > 80
        || typeof raw.agent_matches !== 'boolean') {
        return null;
    }

    const hostname = typeof raw.hostname === 'string' && raw.hostname.length <= 253
        ? raw.hostname.trim()
        : '';
    const ipv4 = typeof raw.ipv4 === 'string' && raw.ipv4.length <= 64
        ? raw.ipv4.trim()
        : '';
    return {
        version: raw.version,
        commit: raw.commit,
        agent_commit: raw.agent_commit,
        agent_matches: raw.agent_matches,
        hostname,
        ipv4,
    };
}

export function Layout({ children, currentPage, onPageChange }: LayoutProps) {
    const { role, user } = useAuth();
    const navAccess: NavAccessContext = {
        accountType: typeof user?.account_type === 'string' ? user.account_type : undefined,
        teamMembers: user?.features?.team_members === true,
    };
    const [counts, setCounts] = useState<Counts>({});
    const [panelRuntime, setPanelRuntime] = useState<PanelRuntime | null>(null);
    const [mobileOpen, setMobileOpen] = useState(false);
    const [desktopPageHeaderTarget, setDesktopPageHeaderTarget] = useState<HTMLDivElement | null>(null);
    const [desktopPageHeaderCount, setDesktopPageHeaderCount] = useState(0);
    const registerDesktopPageHeader = useCallback(() => {
        setDesktopPageHeaderCount((count) => count + 1);
        return () => setDesktopPageHeaderCount((count) => Math.max(0, count - 1));
    }, []);
    const desktopPageHeaderSlot = useMemo(
        () => ({ target: desktopPageHeaderTarget, register: registerDesktopPageHeader }),
        [desktopPageHeaderTarget, registerDesktopPageHeader],
    );
    const hasDesktopPageHeader = desktopPageHeaderCount > 0;

    // Live counts feed the sidebar badges. Failures are silent — a missing
    // badge is better than a broken shell.
    // Canlı sayılar kenar çubuğu rozetlerini besler. Hatalar sessizdir —
    // eksik bir rozet, bozuk bir kabuktan iyidir.
    useEffect(() => {
        fetch('/api/v1/domains')
            .then((r) => (r.ok ? r.json() : []))
            .then((d) => setCounts((c) => ({ ...c, domains: Array.isArray(d) ? d.length : 0 })))
            .catch(() => {});
        // Services are an admin-only view; only the admin sidebar shows the badge.
        // Servisler yalnızca yönetici görünümüdür; rozeti yalnızca yönetici çubuğu gösterir.
        if (role === 'admin') {
            api.getServices()
                // The badge answers "how many components are installed?".
                // Catalogue size belongs on the Components page and made a
                // fresh server look as though 26 services were already present.
                .then((s) => setCounts((c) => ({
                    ...c,
                    services: s.filter((service) => service.is_installed).length,
                })))
                .catch(() => {});
        }
    }, [role]);

    // The existing admin-only version request now carries the bounded machine
    // identity too. One no-store response feeds both the build stamp and every
    // persistent shell label without sampling dashboard metrics again.
    useEffect(() => {
        if (role !== 'admin') {
            setPanelRuntime(null);
            return;
        }

        let cancelled = false;
        fetch('/api/v1/panel/version', { cache: 'no-store', credentials: 'same-origin' })
            .then(async (response) => (response.ok ? decodePanelRuntime(await response.json()) : null))
            .then((runtime) => {
                if (!cancelled) setPanelRuntime(runtime);
            })
            .catch(() => {});
        return () => {
            cancelled = true;
        };
    }, [role]);

    const serverIdentity: ServerIdentity | null = panelRuntime
        && (panelRuntime.hostname || panelRuntime.ipv4)
        ? { hostname: panelRuntime.hostname, ipv4: panelRuntime.ipv4 }
        : null;

    return (
        <DesktopPageHeaderTargetContext.Provider value={desktopPageHeaderSlot}>
            <div className="flex h-screen bg-bg text-fg">
                <Sidebar
                    role={role}
                    access={navAccess}
                    counts={counts}
                    currentPage={currentPage}
                    onPageChange={(id) => {
                        onPageChange(id);
                        setMobileOpen(false);
                    }}
                    mobileOpen={mobileOpen}
                    onCloseMobile={() => setMobileOpen(false)}
                    expandedHeader={hasDesktopPageHeader}
                    serverIdentity={serverIdentity}
                    panelRuntime={panelRuntime}
                />

                <div className="flex min-w-0 flex-1 flex-col">
                    <ImpersonationBanner />
                    <header
                        className={'flex h-14 items-center gap-3 border-b border-border bg-surface px-4 md:px-6 ' +
                            (hasDesktopPageHeader ? 'xl:h-auto xl:min-h-[90px] xl:py-2' : '')}
                    >
                        <button
                            className="rounded-lg p-1.5 text-fg-muted hover:bg-surface-2 md:hidden"
                            onClick={() => setMobileOpen(true)}
                            aria-label="Menu"
                        >
                            <Menu className="h-5 w-5" />
                        </button>
                        {serverIdentity && (
                            <ServerIdentityLabel identity={serverIdentity} placement="mobile" />
                        )}
                        <div
                            ref={setDesktopPageHeaderTarget}
                            className="hidden min-w-0 flex-1 self-stretch xl:flex xl:items-center"
                            data-shell-page-header-target
                        />
                        <div className="ml-auto flex shrink-0 items-center gap-2 sm:gap-3">
                            <div className="hidden sm:block">
                                <LanguageSwitcher />
                            </div>
                            <SkinSwitcher />
                            <ThemeSwitcher />
                            <UserMenu />
                        </div>
                    </header>

                    <main className="flex-1 overflow-auto">{children}</main>
                </div>
            </div>
        </DesktopPageHeaderTargetContext.Provider>
    );
}

function Sidebar({
    role,
    access,
    counts,
    currentPage,
    onPageChange,
    mobileOpen,
    onCloseMobile,
    expandedHeader,
    serverIdentity,
    panelRuntime,
}: {
    role: ReturnType<typeof useAuth>['role'];
    access: NavAccessContext;
    counts: Counts;
    currentPage: string;
    onPageChange: (id: string) => void;
    mobileOpen: boolean;
    onCloseMobile: () => void;
    expandedHeader: boolean;
    serverIdentity: ServerIdentity | null;
    panelRuntime: PanelRuntime | null;
}) {
    const { t } = useI18n();
    const items = navItemsForRole(role, access);

    const content = (
        <div className="flex h-full w-64 flex-col bg-sidebar text-sidebar-fg">
            <div
                className={'flex h-14 items-center gap-2.5 border-b border-sidebar-border px-5 ' +
                    (expandedHeader ? 'xl:h-[90px]' : '')}
            >
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
                {serverIdentity && (
                    <ServerIdentityLabel identity={serverIdentity} placement="sidebar" />
                )}
                <div className={serverIdentity ? 'border-t border-sidebar-border pt-3' : ''}>
                    <BuildStamp runtime={panelRuntime} />
                </div>
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

function ServerIdentityLabel({ identity, placement }: {
    identity: ServerIdentity;
    placement: 'mobile' | 'sidebar';
}) {
    const { t } = useI18n();
    const title = [identity.hostname, identity.ipv4].filter(Boolean).join(' · ');
    const accessibleLabel = [
        t('dashboard.serverInfo'),
        identity.hostname ? t('dashboard.hostname') + ': ' + identity.hostname : '',
        identity.ipv4 ? t('dashboard.ipv4') + ': ' + identity.ipv4 : '',
    ].filter(Boolean).join(', ');

    if (placement === 'mobile') {
        return (
            <section
                data-server-identity="mobile"
                className="min-w-0 max-w-[10rem] md:hidden"
                aria-label={accessibleLabel}
                title={title}
            >
                {identity.hostname && (
                    <p className="truncate text-xs font-semibold text-fg">{identity.hostname}</p>
                )}
                {identity.ipv4 && (
                    <p className="truncate font-mono text-[10px] text-fg-muted" dir="ltr">
                        {identity.ipv4}
                    </p>
                )}
            </section>
        );
    }

    return (
        <section
            data-server-identity="sidebar"
            className="mb-3 min-w-0"
            aria-label={accessibleLabel}
            title={title}
        >
            <p className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-sidebar-heading">
                {t('dashboard.serverInfo')}
            </p>
            {identity.hostname && (
                <p className="truncate font-medium text-sidebar-fg" title={identity.hostname}>
                    {identity.hostname}
                </p>
            )}
            {identity.ipv4 && (
                <p
                    className="truncate font-mono text-[11px] text-sidebar-muted"
                    title={identity.ipv4}
                    dir="ltr"
                >
                    {identity.ipv4}
                </p>
            )}
        </section>
    );
}

// ImpersonationBanner: a loud, always-visible band while an operator browses
// as another account, with the way back one click away.
// ImpersonationBanner: bir operatör başka bir hesap olarak gezinirken hep
// görünen belirgin bir şerit; dönüş yolu tek tık uzakta.
function ImpersonationBanner() {
    const { user } = useAuth();
    const { t } = useI18n();
    if (!user.impersonating) return null;

    const exit = async () => {
        await fetch('/api/v1/auth/unimpersonate', { method: 'POST' });
        window.location.assign('/');
    };

    return (
        <div className="flex flex-wrap items-center justify-center gap-2 bg-warning px-4 py-1.5 text-sm font-medium text-warning-fg">
            <UserCheck className="h-4 w-4" />
            {t('imp.banner', { name: user.username })}
            <button onClick={exit} className="rounded-md bg-black/15 px-2.5 py-0.5 font-semibold hover:bg-black/25">
                {t('imp.return')}
            </button>
        </div>
    );
}

// UserMenu shows the current user and role with a dropdown to sign out.
// UserMenu, mevcut kullanıcıyı ve rolü, çıkış için bir açılır menüyle gösterir.
function UserMenu() {
    const { user, role } = useAuth();
    const { t } = useI18n();
    const [open, setOpen] = useState(false);
    const [showPassword, setShowPassword] = useState(false);
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
                        onClick={() => {
                            setOpen(false);
                            setShowPassword(true);
                        }}
                        className="flex w-full items-center gap-2 px-4 py-2.5 text-sm text-fg-muted transition-colors hover:bg-surface-2 hover:text-fg"
                    >
                        <KeyRound className="h-4 w-4" />
                        {t('profile.changePassword')}
                    </button>
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

            {showPassword && <ChangePasswordModal onClose={() => setShowPassword(false)} />}
        </div>
    );
}

// The footer stamp, read from the SERVER. The version used to be a literal
// typed into this file, and the commit came from the frontend bundle only — so
// a deploy that replaced the backend changed neither, while common.buildHint
// promised the operator this is how you see a new build land. Now the version
// and the panel's commit come from the running binary, and a panel/agent
// mismatch is shown as a warning rather than hidden behind a reassuring stamp.
//
// Footer damgası, SUNUCUDAN okunur. Sürüm eskiden bu dosyaya yazılmış bir
// metindi ve commit yalnız ön yüz paketinden geliyordu — yani arka ucu
// değiştiren bir dağıtım ikisini de değiştirmiyordu, oysa common.buildHint
// operatöre yeni yapının indiğini böyle göreceğini söylüyordu. Artık sürüm ve
// panelin commit'i çalışan binary'den gelir; panel/agent uyuşmazlığı da güven
// veren bir damganın arkasına gizlenmek yerine uyarı olarak gösterilir.
function BuildStamp({ runtime }: { runtime: PanelRuntime | null }) {
    const { t } = useI18n();
    if (!runtime) return <>{t('app.name')}</>;
    const v = runtime;

    return (
        <>
            {t('app.name')} · {v.version} ·{' '}
            <span className="font-mono" title={t('common.buildHint')}>
                build {v.commit}
            </span>
            {!v.agent_matches && v.agent_commit !== '' && (
                <span className="ml-1.5 rounded bg-warning/15 px-1.5 py-0.5 text-warning" title={t('common.agentMismatchHint', { commit: v.agent_commit })}>
                    {t('common.agentMismatch')}
                </span>
            )}
        </>
    );
}
