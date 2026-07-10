import { useEffect, useState } from 'react';
import { Settings, Play, Square, RotateCw, RefreshCw, ScanSearch, DownloadCloud, ChevronDown, ChevronRight, Trash2, ShieldCheck, ShieldOff, Layers, Globe, Database, Mail, Network, Shield, Zap, FolderUp } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { PageHeader, StatusDot, EmptyState, Button, SearchInput } from './ui';

interface ManagedService {
    id: string;
    name: string;
    description: string;
    icon: string;
    category: string;
    versions: string[];
    status: string;
    is_installed: boolean;
    conflict_with?: string;
    requires_missing?: string[];
    daemonless?: boolean;
    packages?: string[];
}

// Category display order + label key + section icon + icon tint. Each
// category renders as its own card; the colored icon chip is what makes the
// sections scannable at a glance in both themes.
// Kategori gösterim sırası + etiket anahtarı + bölüm ikonu + ikon rengi.
// Her kategori kendi kartı; renkli ikon rozeti bölümleri iki temada da tek
// bakışta taranabilir kılan şeydir.
// Ordered by necessity, not alphabet: the panel is authoritative DNS for its
// domains (D-009 — no DNS server, no domains), so DNS leads; then the hosting
// core (web, database, email), then hardening and extras.
// Alfabetik değil gereklilik sırası: panel domain'lerinin DNS otoritesidir
// (D-009 — DNS yoksa domain yok), bu yüzden DNS başta; sonra barındırma
// çekirdeği (web, veritabanı, e-posta), sonra sertleştirme ve ekstralar.
const categoryOrder: { id: string; labelKey: string; icon: LucideIcon; tint: string }[] = [
    { id: 'dns', labelKey: 'services.cat.dns', icon: Network, tint: 'bg-teal-500/10 text-teal-600 dark:text-teal-400' },
    { id: 'web', labelKey: 'services.cat.web', icon: Globe, tint: 'bg-blue-500/10 text-blue-600 dark:text-blue-400' },
    { id: 'database', labelKey: 'services.cat.database', icon: Database, tint: 'bg-violet-500/10 text-violet-600 dark:text-violet-400' },
    { id: 'email', labelKey: 'services.cat.email', icon: Mail, tint: 'bg-amber-500/10 text-amber-600 dark:text-amber-400' },
    { id: 'security', labelKey: 'services.cat.security', icon: Shield, tint: 'bg-red-500/10 text-red-600 dark:text-red-400' },
    { id: 'cache', labelKey: 'services.cat.cache', icon: Zap, tint: 'bg-orange-500/10 text-orange-600 dark:text-orange-400' },
    { id: 'ftp', labelKey: 'services.cat.ftp', icon: FolderUp, tint: 'bg-slate-500/10 text-slate-600 dark:text-slate-400' },
];

interface ServiceListProps {
    onSelectConfig: (path: string) => void;
    onManageService?: (serviceId: string, versions: string[]) => void;
}

// A service's optional managed vendor repository (e.g. PGDG for PostgreSQL).
// When available+enabled, the install dialog offers a specific major version.
// Bir servisin isteğe bağlı yönetilen vendor deposu (örn. PostgreSQL için PGDG).
// Mevcut+etkinse, kurulum modalı belirli bir major sürüm sunar.
interface RepoInfo {
    available: boolean;
    enabled: boolean;
    id?: string;
    name?: string;
    detail?: string;
    packages?: string[];
    error?: string;
}

// Services grouped into per-category cards (Claude Design'dan uyarlandı).
// Status comes straight from managed-services (the reliable source:
// "active (running)" vs "inactive (dead)"), not the wrapper-unit endpoint
// that misreports.
//
// Servisler kategori başına kartlar halinde. Durum doğrudan
// managed-services'ten gelir (güvenilir kaynak), yanlış raporlayan
// wrapper-unit uç noktasından değil.
export function ServiceList({ onManageService }: ServiceListProps) {
    const { t } = useI18n();
    const [services, setServices] = useState<ManagedService[]>([]);
    const [scannedAt, setScannedAt] = useState<string | null>(null);
    const [loading, setLoading] = useState(true);
    const [scanning, setScanning] = useState(false);
    const [busy, setBusy] = useState<string | null>(null);
    const [installTarget, setInstallTarget] = useState<ManagedService | null>(null);
    const [uninstallTarget, setUninstallTarget] = useState<ManagedService | null>(null);
    const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
    const [query, setQuery] = useState('');
    const [hideNotInstalled, setHideNotInstalled] = useState(false);

    useEffect(() => {
        loadServices();
    }, []);

    // Client-side filter: text search over name/description/id plus the
    // "hide not installed" switch. A live search overrides collapse so
    // matches are never hidden inside a folded group.
    // İstemci tarafı filtre: ad/açıklama/id üzerinde arama + "kurulu
    // olmayanları gizle" anahtarı. Aktif arama katlamayı geçersiz kılar;
    // eşleşme katlanmış grup içinde saklı kalmaz.
    const q = query.trim().toLowerCase();
    const filtered = services.filter(
        (s) =>
            (!hideNotInstalled || s.is_installed) &&
            (q === '' || `${s.name} ${s.description} ${s.id}`.toLowerCase().includes(q)),
    );

    const toggleGroup = (cat: string) =>
        setCollapsed((prev) => {
            const next = new Set(prev);
            next.has(cat) ? next.delete(cat) : next.add(cat);
            return next;
        });
    const visibleCats = () => categoryOrder.filter(({ id }) => filtered.some((s) => s.category === id));
    const collapseAll = () => setCollapsed(new Set(visibleCats().map((c) => c.id)));
    const expandAll = () => setCollapsed(new Set());

    // Reads the CACHED scan — instant, never probes the system. A fresh
    // probe is the explicit user action below (scan).
    // ÖNBELLEKTEKİ taramayı okur — anlık, sistemi asla yoklamaz. Taze
    // yoklama aşağıdaki açık kullanıcı eylemidir (scan).
    const loadServices = () => {
        setLoading(true);
        fetch('/api/v1/managed-services')
            .then((res) => res.json())
            .then((data) => {
                setServices(data?.services || []);
                setScannedAt(data?.scanned_at ?? null);
            })
            .catch(() => showToast('error', t('common.error')))
            .finally(() => setLoading(false));
    };

    const scan = async () => {
        setScanning(true);
        try {
            const res = await fetch('/api/v1/managed-services/scan', { method: 'POST' });
            if (!res.ok) throw new Error();
            const data = await res.json();
            setServices(data?.services || []);
            setScannedAt(data?.scanned_at ?? null);
        } catch {
            showToast('error', t('services.scanFailed'));
        } finally {
            setScanning(false);
        }
    };

    const resolveUnit = (serviceId: string, version?: string) => {
        if (serviceId === 'php-fpm') return version && version !== 'default' ? `php${version}-fpm` : 'php-fpm';
        return serviceId;
    };

    // Install a not-yet-installed catalogue service on demand. The agent
    // installs exactly the whitelisted packages for this id, then enables the
    // unit. This is the one-click install the VPN page (and others) point to.
    // Kurulu-olmayan bir katalog servisini talep üzerine kur. Agent bu id
    // için yalnız beyaz-listedeki paketleri kurar, sonra unit'i etkinleştirir.
    // Actual install, run after the confirmation modal. The agent installs
    // exactly the whitelisted packages for this id, then enables the unit.
    // Gerçek kurulum, onay modalından sonra koşar. Agent bu id için yalnız
    // beyaz-listedeki paketleri kurar, sonra unit'i etkinleştirir.
    const doInstall = async (service: ManagedService, pkg?: string) => {
        setInstallTarget(null);
        setBusy(service.id);
        try {
            const res = await fetch('/api/v1/service/install', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ service_id: service.id, ...(pkg ? { package: pkg } : {}) }),
            });
            const data = await res.json();
            if (!res.ok || data.error) throw new Error(data.error || 'install failed');
            showToast('success', t('services.installed', { name: service.name }));
            scan();
        } catch (e) {
            showToast('error', e instanceof Error && e.message ? e.message : t('services.actionFailed'));
        } finally {
            setBusy(null);
        }
    };

    // Remove an installed service: stop + disable + purge via the agent, then
    // rescan. Shrinks the attack surface back down — the mirror of install.
    // Kurulu bir servisi kaldır: agent ile durdur + devre dışı + purge, sonra
    // yeniden tara. Saldırı yüzeyini geri küçültür — kurulumun aynası.
    const doUninstall = async (service: ManagedService) => {
        setUninstallTarget(null);
        setBusy(service.id);
        try {
            const res = await fetch('/api/v1/service/uninstall', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ service_id: service.id }),
            });
            const data = await res.json();
            if (!res.ok || data.error) throw new Error(data.error || 'uninstall failed');
            showToast('success', t('services.uninstalled', { name: service.name }));
            scan();
        } catch (e) {
            showToast('error', e instanceof Error && e.message ? e.message : t('services.actionFailed'));
        } finally {
            setBusy(null);
        }
    };

    const handleAction = async (service: ManagedService, action: 'start' | 'stop' | 'restart') => {
        setBusy(service.id);
        try {
            const res = await fetch('/api/v1/service/action', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name: resolveUnit(service.id, service.versions[0]), action }),
            });
            if (!res.ok) throw new Error();
            await new Promise((r) => setTimeout(r, 1000));
            loadServices();
        } catch {
            showToast('error', t('services.actionFailed'));
        } finally {
            setBusy(null);
        }
    };

    const isRunning = (s: ManagedService) => s.status?.toLowerCase().includes('running');

    return (
        <div className="p-6 md:p-8">
            <PageHeader
                title={t('nav.services')}
                subtitle={t('services.subtitle')}
                breadcrumb={[t('common.home'), t('nav.services')]}
            />

            <FirewallBar />

            <div className="mb-4 flex flex-wrap items-center gap-3">
                <button
                    onClick={scan}
                    disabled={scanning}
                    className="inline-flex items-center gap-2 rounded-lg border border-border-strong bg-surface px-3 py-1.5 text-sm font-medium text-fg transition-colors hover:bg-surface-2 disabled:opacity-50"
                >
                    <RefreshCw className={`h-4 w-4 ${scanning ? 'animate-spin' : ''}`} />
                    {scanning ? t('services.scanning') : scannedAt ? t('services.rescan') : t('services.scanNow')}
                </button>
                <span className="text-xs text-fg-subtle">
                    {scannedAt
                        ? t('services.lastScan', { time: new Date(scannedAt).toLocaleString() })
                        : t('services.neverScannedShort')}
                </span>
                {scannedAt && services.length > 0 && (
                    <div className="ml-auto flex flex-wrap items-center gap-3">
                        <div className="w-52">
                            <SearchInput value={query} onChange={setQuery} placeholder={t('services.search')} />
                        </div>
                        <label className="flex cursor-pointer select-none items-center gap-1.5 text-xs text-fg-muted">
                            <input
                                type="checkbox"
                                checked={hideNotInstalled}
                                onChange={(e) => setHideNotInstalled(e.target.checked)}
                                className="h-3.5 w-3.5 rounded border-border-strong accent-primary"
                            />
                            {t('services.hideNotInstalled')}
                        </label>
                        <div className="flex gap-1 text-xs">
                            <button onClick={expandAll} className="rounded-md px-2 py-1 text-fg-muted hover:bg-surface-2 hover:text-fg">
                                {t('services.expandAll')}
                            </button>
                            <button onClick={collapseAll} className="rounded-md px-2 py-1 text-fg-muted hover:bg-surface-2 hover:text-fg">
                                {t('services.collapseAll')}
                            </button>
                        </div>
                    </div>
                )}
            </div>

            {loading ? (
                <div className="flex items-center justify-center py-16">
                    <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary" />
                </div>
            ) : !scannedAt && services.length === 0 ? (
                <EmptyState
                    icon={ScanSearch}
                    title={t('services.neverScanned')}
                    hint={t('services.neverScannedHint')}
                />
            ) : (
                <>
                <p className="mb-2 text-xs text-fg-subtle">
                    {filtered.length === services.length
                        ? t('common.itemsTotal', { n: services.length })
                        : t('services.matchCount', { shown: filtered.length, total: services.length })}
                </p>
                {filtered.length === 0 ? (
                    <div className="rounded-xl border border-border bg-surface p-8 text-center text-sm text-fg-muted shadow-card">
                        {t('services.noMatches')}
                    </div>
                ) : (
                <div className="space-y-4">
                    {categoryOrder.map(({ id: cat, labelKey, icon: CatIcon, tint }) => {
                        const group = filtered.filter((s) => s.category === cat);
                        if (group.length === 0) return null;
                        const installedCount = group.filter((s) => s.is_installed).length;
                        const isOpen = q !== '' || !collapsed.has(cat);
                        return (
                            <section key={cat} className="overflow-hidden rounded-xl border border-border bg-surface shadow-card">
                                <button
                                    type="button"
                                    onClick={() => toggleGroup(cat)}
                                    className="flex w-full items-center gap-3 px-4 py-3.5 text-left transition-colors hover:bg-surface-2/60"
                                >
                                    <span className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-lg ${tint}`}>
                                        <CatIcon className="h-5 w-5" />
                                    </span>
                                    <span className="text-lg font-semibold text-fg">{t(labelKey as Parameters<typeof t>[0])}</span>
                                    <span className="text-sm text-fg-subtle">{installedCount}/{group.length}</span>
                                    <span className="ml-auto text-fg-subtle">
                                        {isOpen ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
                                    </span>
                                </button>
                                {isOpen && (
                                    <ul className="border-t border-border">
                                        {group.map((s) => {
                                            const running = isRunning(s);
                                            return (
                                                <li key={s.id} className="flex flex-wrap items-center gap-x-4 gap-y-2 border-b border-border px-4 py-3.5 last:border-0 hover:bg-surface-2/40">
                                                    {/* Selective dimming, not block opacity: the name stays
                                                        readable in the light theme; state is carried by the
                                                        muted tone + status text + Install button.
                                                        / Blok saydamlık değil seçici soldurma: ad açık temada
                                                        da okunur kalır; durumu soluk ton + durum yazısı +
                                                        Kur düğmesi anlatır. */}
                                                    <div className="flex min-w-0 flex-1 basis-52 items-center gap-3">
                                                        <span className={`text-2xl leading-none ${s.is_installed ? '' : 'opacity-60 grayscale'}`}>{s.icon}</span>
                                                        <div className="min-w-0">
                                                            <div className={`text-base font-medium ${s.is_installed ? 'text-fg' : 'text-fg-muted'}`}>{s.name}</div>
                                                            <div className="truncate text-xs text-fg-subtle">{s.description}</div>
                                                        </div>
                                                    </div>
                                                    <div className="hidden w-28 shrink-0 sm:block">
                                                        {!s.is_installed ? (
                                                            <span className="font-mono text-sm text-fg-muted">—</span>
                                                        ) : s.versions.length > 1 ? (
                                                            <div className="flex flex-wrap gap-1">
                                                                {s.versions.map((v) => (
                                                                    <span key={v} className="rounded bg-surface-2 px-1.5 py-0.5 font-mono text-xs text-fg-muted">{v}</span>
                                                                ))}
                                                            </div>
                                                        ) : (
                                                            <span className="font-mono text-sm text-fg-muted">
                                                                {s.versions[0] === 'default' ? '—' : s.versions[0]}
                                                            </span>
                                                        )}
                                                    </div>
                                                    <div className="w-32 shrink-0">
                                                        <span className={`inline-flex items-center gap-1.5 text-sm ${s.is_installed ? 'text-fg-muted' : 'text-fg-subtle'}`}>
                                                            <StatusDot ok={s.is_installed && (running || !!s.daemonless)} />
                                                            {!s.is_installed
                                                                ? t('services.notInstalled')
                                                                : s.daemonless
                                                                  ? t('services.installedLabel')
                                                                  : running
                                                                    ? t('services.running')
                                                                    : t('services.stopped')}
                                                        </span>
                                                    </div>
                                                    <div className="ml-auto flex items-center justify-end gap-1">
                                                    {!s.is_installed ? (
                                                        s.conflict_with ? (
                                                            <span
                                                                title={t('services.conflictHint', { name: s.conflict_with })}
                                                                className="inline-flex items-center gap-1.5 rounded-lg border border-border bg-surface-2 px-3 py-1.5 text-xs font-medium text-fg-subtle"
                                                            >
                                                                {t('services.conflictWith', { name: s.conflict_with })}
                                                            </span>
                                                        ) : s.requires_missing && s.requires_missing.length > 0 ? (
                                                            <span
                                                                title={t('services.requiresHint', { names: s.requires_missing.join(', ') })}
                                                                className="inline-flex items-center gap-1.5 rounded-lg border border-border bg-surface-2 px-3 py-1.5 text-xs font-medium text-fg-subtle"
                                                            >
                                                                {t('services.requiresLabel', { names: s.requires_missing.join(', ') })}
                                                            </span>
                                                        ) : (
                                                        <button
                                                            onClick={() => setInstallTarget(s)}
                                                            disabled={busy === s.id}
                                                            className="inline-flex items-center gap-1.5 rounded-lg bg-primary px-3 py-1.5 text-xs font-semibold text-primary-fg transition-colors hover:bg-primary/90 disabled:opacity-50"
                                                        >
                                                            <DownloadCloud className="h-3.5 w-3.5" />
                                                            {busy === s.id ? t('services.installing') : t('services.install')}
                                                        </button>
                                                        )
                                                    ) : (
                                                    <>
                                                    {/* Daemonless tools (phpMyAdmin) have no unit to start/stop —
                                                        only manage/uninstall. / Daemon'suz araçların (phpMyAdmin)
                                                        başlatılacak unit'i yok — yalnız yönet/kaldır. */}
                                                    {/* Contextual: only actions that make sense for the current
                                                        state — no Stop on a stopped service, no Start on a running
                                                        one. / Bağlama duyarlı: yalnız mevcut duruma anlamlı gelen
                                                        eylemler — durmuşta Durdur, çalışanda Başlat gösterilmez. */}
                                                    {!s.daemonless && (
                                                    <>
                                                    {running ? (
                                                    <>
                                                    <ActionIcon
                                                        title={t('services.restart')}
                                                        onClick={() => handleAction(s, 'restart')}
                                                        disabled={busy === s.id}
                                                        tone="warning"
                                                    >
                                                        <RotateCw className="h-4 w-4" />
                                                    </ActionIcon>
                                                    <ActionIcon
                                                        title={t('services.stop')}
                                                        onClick={() => handleAction(s, 'stop')}
                                                        disabled={busy === s.id}
                                                        tone="danger"
                                                    >
                                                        <Square className="h-4 w-4" fill="currentColor" />
                                                    </ActionIcon>
                                                    </>
                                                    ) : (
                                                    <ActionIcon
                                                        title={t('services.start')}
                                                        onClick={() => handleAction(s, 'start')}
                                                        disabled={busy === s.id}
                                                        tone="success"
                                                    >
                                                        <Play className="h-4 w-4" fill="currentColor" />
                                                    </ActionIcon>
                                                    )}
                                                    <button
                                                        onClick={() => onManageService?.(s.id, s.versions)}
                                                        className="ml-1 inline-flex items-center gap-1.5 rounded-lg border border-border-strong bg-surface px-2.5 py-1.5 text-xs font-medium text-fg transition-colors hover:bg-surface-2"
                                                    >
                                                        <Settings className="h-3.5 w-3.5" />
                                                        {t('services.manage')}
                                                    </button>
                                                    </>
                                                    )}
                                                    <button
                                                        onClick={() => setUninstallTarget(s)}
                                                        title={t('services.uninstall')}
                                                        className="inline-flex items-center rounded-lg border border-border-strong bg-surface p-1.5 text-fg-muted transition-colors hover:bg-danger/10 hover:text-danger"
                                                    >
                                                        <Trash2 className="h-3.5 w-3.5" />
                                                    </button>
                                                    </>
                                                    )}
                                                    </div>
                                                </li>
                                            );
                                        })}
                                    </ul>
                                )}
                            </section>
                        );
                    })}
                </div>
                )}
                </>
            )}

            {installTarget && (
                <InstallServiceDialog
                    service={installTarget}
                    busy={busy === installTarget.id}
                    onCancel={() => setInstallTarget(null)}
                    onConfirm={(pkg) => doInstall(installTarget, pkg)}
                />
            )}
            {uninstallTarget && (
                <UninstallServiceDialog
                    service={uninstallTarget}
                    busy={busy === uninstallTarget.id}
                    onCancel={() => setUninstallTarget(null)}
                    onConfirm={() => doUninstall(uninstallTarget)}
                />
            )}
        </div>
    );
}

// Themed install confirmation — replaces the browser's native confirm().
// Shows exactly what will land on the server (the distro packages) so an
// install is never a blind "yallah". PHP notes that the distro default
// version is installed; extra versions are managed per-site elsewhere.
// Temalı kurulum onayı — tarayıcının native confirm()'ünün yerine. Sunucuya
// tam olarak ne ineceğini (dağıtım paketleri) gösterir; kurulum asla kör bir
// "yallah" değildir. PHP için dağıtım varsayılan sürümünün kurulacağı,
// ek sürümlerin site başına başka yerde yönetildiği belirtilir.
function InstallServiceDialog({
    service,
    busy,
    onCancel,
    onConfirm,
}: {
    service: ManagedService;
    busy: boolean;
    onCancel: () => void;
    onConfirm: (pkg?: string) => void;
}) {
    const { t } = useI18n();
    const [version, setVersion] = useState<string | null>(null);
    const [verLoading, setVerLoading] = useState(true);

    // Managed vendor repo (e.g. PGDG): most services have none, so this whole
    // section simply does not render. When present, the admin can enable it and
    // pick a specific major version instead of the single one the distro froze.
    // selectedPkg === '' means "distro default" (install from the OS repo).
    // Yönetilen vendor deposu (örn. PGDG): çoğu servisin yok, o durumda bu bölüm
    // hiç render edilmez. Varsa, yönetici açıp dağıtımın dondurduğu tek sürüm
    // yerine belirli bir major seçebilir. selectedPkg === '' → "dağıtım
    // varsayılanı" (OS deposundan kur).
    const [repo, setRepo] = useState<RepoInfo | null>(null);
    const [repoBusy, setRepoBusy] = useState(false);
    const [selectedPkg, setSelectedPkg] = useState<string>('');

    // Ask the server what apt would actually install right now — honest
    // "what will land", not a made-up version picker (the distro offers one).
    // Sunucuya apt'ın şu an gerçekten ne kuracağını sor — uydurma bir sürüm
    // seçici değil, dürüst "ne inecek" (dağıtım tek sürüm sunar).
    useEffect(() => {
        setVerLoading(true);
        fetch(`/api/v1/service/candidate?id=${encodeURIComponent(service.id)}`)
            .then((r) => (r.ok ? r.json() : null))
            .then((d) => setVersion(d?.version || ''))
            .catch(() => setVersion(''))
            .finally(() => setVerLoading(false));
        fetch(`/api/v1/repo?service_id=${encodeURIComponent(service.id)}`)
            .then((r) => (r.ok ? r.json() : null))
            .then((d: RepoInfo | null) => setRepo(d && d.available ? d : null))
            .catch(() => setRepo(null));
    }, [service.id]);

    // Enable/disable the vendor repo, then reflect the new state (and the
    // versions it now exposes) straight from the server's reply.
    // Vendor deposunu aç/kapat, sonra yeni durumu (ve açtığı sürümleri) doğrudan
    // sunucunun yanıtından yansıt.
    const toggleRepo = async (action: 'enable' | 'disable') => {
        setRepoBusy(true);
        try {
            const res = await fetch('/api/v1/repo', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ service_id: service.id, action }),
            });
            const data: RepoInfo = await res.json();
            if (!res.ok || data.error) throw new Error(data.error || 'repo failed');
            setRepo(data.available ? data : null);
            if (action === 'disable') setSelectedPkg('');
        } catch (e) {
            showToast('error', e instanceof Error && e.message ? e.message : t('services.actionFailed'));
        } finally {
            setRepoBusy(false);
        }
    };

    const willInstall = selectedPkg
        ? [selectedPkg]
        : service.packages && service.packages.length > 0
          ? service.packages
          : [service.id];

    return (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onCancel}>
            <div
                className="w-full max-w-md rounded-2xl border border-border bg-surface p-6 shadow-xl"
                onClick={(e) => e.stopPropagation()}
            >
                <div className="mb-4 flex items-start gap-3">
                    <span className="text-3xl leading-none">{service.icon}</span>
                    <div className="min-w-0">
                        <h3 className="text-lg font-semibold text-fg">{t('services.installTitle', { name: service.name })}</h3>
                        <p className="text-sm text-fg-muted">{service.description}</p>
                    </div>
                </div>

                <div className="mb-4 rounded-lg border border-border bg-surface-2/50 p-3">
                    <div className="mb-2 flex items-center justify-between">
                        <span className="text-xs font-medium text-fg-subtle">{t('services.versionToInstall')}</span>
                        <span className="font-mono text-sm font-semibold text-fg">
                            {selectedPkg ? selectedPkg : verLoading ? '…' : version ? version : t('services.versionDefault')}
                        </span>
                    </div>
                    <p className="mb-1 text-xs font-medium text-fg-subtle">{t('services.willInstall')}</p>
                    <div className="flex flex-wrap gap-1.5">
                        {willInstall.map((pkg) => (
                            <span key={pkg} className="rounded bg-surface-3 px-2 py-0.5 font-mono text-xs text-fg">{pkg}</span>
                        ))}
                    </div>
                    {service.id === 'php-fpm' && (
                        <p className="mt-2 text-xs text-fg-subtle">{t('services.phpVersionNote')}</p>
                    )}
                </div>

                {repo && (
                    <div className="mb-4 rounded-lg border border-border bg-surface-2/50 p-3">
                        <div className="mb-2 flex items-start gap-2">
                            <Layers className="mt-0.5 h-4 w-4 shrink-0 text-fg-subtle" />
                            <div className="min-w-0">
                                <p className="text-sm font-medium text-fg">{repo.name}</p>
                                <p className="text-xs text-fg-subtle">{repo.detail}</p>
                            </div>
                        </div>

                        {!repo.enabled ? (
                            <>
                                <p className="mb-2 text-xs text-fg-subtle">{t('services.repo.note')}</p>
                                <Button
                                    variant="secondary"
                                    onClick={() => toggleRepo('enable')}
                                    disabled={repoBusy || busy}
                                    icon={Layers}
                                >
                                    {repoBusy ? t('services.repo.enabling') : t('services.repo.enable')}
                                </Button>
                            </>
                        ) : (
                            <>
                                <p className="mb-1.5 text-xs font-medium text-fg-subtle">{t('services.repo.chooseVersion')}</p>
                                <div className="flex flex-wrap gap-1.5">
                                    <button
                                        onClick={() => setSelectedPkg('')}
                                        className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors ${
                                            selectedPkg === ''
                                                ? 'bg-primary text-primary-fg'
                                                : 'bg-surface-3 text-fg-muted hover:text-fg'
                                        }`}
                                    >
                                        {t('services.repo.distroDefault')}
                                        {version ? ` (${version})` : ''}
                                    </button>
                                    {(repo.packages || []).map((pkg) => (
                                        <button
                                            key={pkg}
                                            onClick={() => setSelectedPkg(pkg)}
                                            className={`rounded-md px-2.5 py-1 font-mono text-xs font-medium transition-colors ${
                                                selectedPkg === pkg
                                                    ? 'bg-primary text-primary-fg'
                                                    : 'bg-surface-3 text-fg-muted hover:text-fg'
                                            }`}
                                        >
                                            {pkg}
                                        </button>
                                    ))}
                                </div>
                                <button
                                    onClick={() => toggleRepo('disable')}
                                    disabled={repoBusy || busy}
                                    className="mt-2 text-xs text-fg-subtle underline decoration-dotted underline-offset-2 hover:text-fg disabled:opacity-40"
                                >
                                    {repoBusy ? '…' : t('services.repo.disable')}
                                </button>
                            </>
                        )}
                    </div>
                )}

                <div className="flex justify-end gap-2">
                    <Button variant="secondary" onClick={onCancel} disabled={busy}>{t('common.cancel')}</Button>
                    <Button variant="primary" onClick={() => onConfirm(selectedPkg || undefined)} disabled={busy} icon={DownloadCloud}>
                        {busy ? t('services.installing') : t('services.install')}
                    </Button>
                </div>
            </div>
        </div>
    );
}

function ActionIcon({
    children,
    title,
    onClick,
    disabled,
    tone,
}: {
    children: React.ReactNode;
    title: string;
    onClick: () => void;
    disabled?: boolean;
    tone: 'success' | 'danger' | 'warning';
}) {
    // Filled tone chips (user feedback: outline glyphs read as 'empty') —
    // matches the filled controls on the service detail page.
    // Dolu ton yongaları (kullanıcı geri bildirimi: kontur ikonlar 'boş'
    // okunuyor) — servis detay sayfasındaki dolu kontrollerle eşleşir.
    const toneCls = {
        success: 'bg-success text-success-fg hover:bg-success/90',
        danger: 'bg-danger text-danger-fg hover:bg-danger/90',
        warning: 'bg-warning text-warning-fg hover:bg-warning/90',
    }[tone];
    return (
        <button
            title={title}
            onClick={onClick}
            disabled={disabled}
            className={`rounded-md p-1.5 transition-colors disabled:opacity-40 ${toneCls}`}
        >
            {children}
        </button>
    );
}


// Themed uninstall confirmation — a destructive action, so it states plainly
// what will be purged and that dependent sites/mail may break. Removing a
// service is how the operator shrinks the attack surface back down.
// Temalı kaldırma onayı — yıkıcı bir eylem; ne purge edileceğini ve bağımlı
// site/postanın çalışmayabileceğini açıkça söyler. Bir servisi kaldırmak,
// operatörün saldırı yüzeyini geri küçültme yoludur.
function UninstallServiceDialog({
    service,
    busy,
    onCancel,
    onConfirm,
}: {
    service: ManagedService;
    busy: boolean;
    onCancel: () => void;
    onConfirm: () => void;
}) {
    const { t } = useI18n();
    return (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onCancel}>
            <div className="w-full max-w-md rounded-2xl border border-danger/40 bg-surface p-6 shadow-xl" onClick={(e) => e.stopPropagation()}>
                <div className="mb-4 flex items-start gap-3">
                    <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-danger/10 text-danger">
                        <Trash2 className="h-5 w-5" />
                    </span>
                    <div className="min-w-0">
                        <h3 className="text-lg font-semibold text-fg">{t('services.uninstallTitle', { name: service.name })}</h3>
                        <p className="text-sm text-fg-muted">{service.description}</p>
                    </div>
                </div>
                <div className="mb-4 rounded-lg border border-danger/30 bg-danger/5 p-3 text-sm text-fg-muted">
                    <p className="mb-2">{t('services.uninstallWarn')}</p>
                    <div className="flex flex-wrap gap-1.5">
                        {(service.packages && service.packages.length > 0 ? service.packages : [service.id]).map((pkg) => (
                            <span key={pkg} className="rounded bg-surface-2 px-2 py-0.5 font-mono text-xs text-fg">{pkg}</span>
                        ))}
                    </div>
                </div>
                <div className="flex justify-end gap-2">
                    <Button variant="secondary" onClick={onCancel} disabled={busy}>{t('common.cancel')}</Button>
                    <Button variant="danger" onClick={onConfirm} disabled={busy} icon={Trash2}>
                        {busy ? t('services.uninstalling') : t('services.uninstall')}
                    </Button>
                </div>
            </div>
        </div>
    );
}


// Firewall status + toggle. Default-deny inbound: only the panel port, SSH
// (auto-detected, never closable) and installed services' ports are open. The
// open set tracks the running services, so the box exposes only what it runs.
// Güvenlik duvarı durumu + anahtar. Varsayılan-reddet gelen: yalnız panel
// portu, SSH (otomatik tespit, asla kapatılamaz) ve kurulu servis portları
// açık. Açık küme koşan servisleri izler; kutu yalnız koşturduğunu açar.
function FirewallBar() {
    const { t } = useI18n();
    const [st, setSt] = useState<{ enabled: boolean; tcp_ports?: number[]; udp_ports?: number[]; ssh_ports?: number[] } | null>(null);
    const [busy, setBusy] = useState(false);

    const load = () => {
        fetch('/api/v1/firewall').then((r) => (r.ok ? r.json() : null)).then(setSt).catch(() => {});
    };
    useEffect(load, []);

    const toggle = async () => {
        if (!st) return;
        const turningOff = st.enabled;
        if (turningOff && !confirm(t('firewall.offConfirm'))) return;
        setBusy(true);
        try {
            const r = await fetch('/api/v1/firewall', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ enabled: !st.enabled }),
            });
            const d = await r.json();
            if (!r.ok || d.error) throw new Error(d.error);
            setSt(d);
            showToast('success', d.enabled ? t('firewall.onDone') : t('firewall.offDone'));
        } catch (e) {
            showToast('error', e instanceof Error && e.message ? e.message : t('common.error'));
        } finally {
            setBusy(false);
        }
    };

    if (!st) return null;
    const openTcp = st.tcp_ports || [];
    const openUdp = st.udp_ports || [];

    // Off is the state that must not look calm: every port is exposed, so the
    // whole banner turns amber. On earns a quiet green tint.
    // Kapalı hal sakin GÖRÜNMEMESİ gereken haldir: tüm portlar açıktır, bu
    // yüzden bandın tamamı ambere döner. Açık hal sessiz yeşil tondadır.
    return (
        <section
            className={`mb-4 rounded-xl border p-4 ${
                st.enabled ? 'border-success/30 bg-success/5' : 'border-warning/50 bg-warning/10'
            }`}
        >
            <div className="flex flex-wrap items-center gap-3">
                {st.enabled ? <ShieldCheck className="h-5 w-5 text-success" /> : <ShieldOff className="h-5 w-5 text-warning" />}
                <div className="min-w-0 flex-1">
                    <div className="text-sm font-semibold text-fg">
                        {t('firewall.title')}{' '}
                        <span className={st.enabled ? 'text-success' : 'text-warning'}>
                            {st.enabled ? t('firewall.on') : t('firewall.off')}
                        </span>
                    </div>
                    <p className="text-xs text-fg-muted">
                        {st.enabled ? t('firewall.onHint') : t('firewall.offHint')}
                    </p>
                </div>
                <button
                    onClick={toggle}
                    disabled={busy}
                    className={`rounded-lg px-3 py-1.5 text-xs font-semibold transition-colors disabled:opacity-50 ${
                        st.enabled
                            ? 'border border-border-strong bg-surface text-fg hover:bg-surface-2'
                            : 'bg-primary text-primary-fg hover:bg-primary/90'
                    }`}
                >
                    {st.enabled ? t('firewall.turnOff') : t('firewall.turnOn')}
                </button>
            </div>
            {st.enabled && (openTcp.length > 0 || openUdp.length > 0) && (
                <div className="mt-3 flex flex-wrap gap-1.5">
                    {openTcp.map((p) => (
                        <span key={`t${p}`} className="rounded bg-surface-2 px-2 py-0.5 font-mono text-xs text-fg-muted">
                            {p}/tcp{st.ssh_ports?.includes(p) ? ' (SSH)' : ''}
                        </span>
                    ))}
                    {openUdp.map((p) => (
                        <span key={`u${p}`} className="rounded bg-surface-2 px-2 py-0.5 font-mono text-xs text-fg-muted">{p}/udp</span>
                    ))}
                </div>
            )}
        </section>
    );
}
