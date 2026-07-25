import { useEffect, useState } from 'react';
import { Settings, Play, Square, RotateCw, RefreshCw, ScanSearch, DownloadCloud, ChevronDown, ChevronRight, Trash2, ShieldCheck, ShieldOff, Layers, Globe, Database, Mail, Network, Shield, Zap, FolderUp, Activity } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { PageHeader, StatusDot, EmptyState, Button, SearchInput, ErrorBanner } from './ui';
import { readApiError, apiErrorText, type ApiError } from '../lib/apiError';

// One installed copy of a runtime (B3b): php8.3-fpm is an instance, a Node
// tree under /opt/celikpanel/runtimes is an instance. `unit` empty means the
// copy has no daemon of its own — no status, no start/stop for it.
// Bir runtime'ın kurulu tek kopyası (B3b): php8.3-fpm bir kopyadır,
// /opt/celikpanel/runtimes altındaki bir Node ağacı bir kopyadır. `unit` boşsa
// kopyanın kendine ait daemon'ı yoktur — durumu da başlat/durduru da olmaz.
interface ServiceInstance {
    version: string;
    unit?: string;
    path?: string;
    managed: boolean;
    status?: string;
    size_bytes?: number;
}

interface ManagedService {
    id: string;
    /** The real systemd unit the scan found (BIND's id is "bind", unit "named"). */
    unit?: string;
    name: string;
    description: string;
    icon: string;
    category: string;
    versions: string[];
    instances?: ServiceInstance[];
    status: string;
    is_installed: boolean;
    conflict_with?: string;
    not_offered?: boolean;
    requires_missing?: string[];
    kind?: 'service' | 'runtime' | 'tool';
    packages?: string[];
}

// A tarball runtime installs from upstream (nodejs.org), not from distro
// packages — its install box lives in the version drawer, not the package
// dialog. Data-driven: "runtime with no packages" is exactly that set.
// Tarball runtime, dağıtım paketinden değil kaynağından (nodejs.org) kurulur —
// kurulum kutusu paket modalında değil sürüm çekmecesindedir. Veriden türer:
// "paketi olmayan runtime" tam olarak o kümedir.
const isTarballRuntime = (s: ManagedService) => s.kind === 'runtime' && !(s.packages && s.packages.length > 0);

// Requirement ROLE tokens (seat names — any member satisfies) and their
// localized labels. Shared by the row badges and the version drawer.
// Gereksinim ROL belirteçleri (koltuk adları — herhangi bir üye tatmin eder)
// ve yerel etiketleri. Satır rozetleri ile sürüm çekmecesinin ortak malı.
const REQ_ROLE_KEYS: Record<string, string> = {
    'web-server': 'services.role.webServer',
    'dns-server': 'services.role.dnsServer',
    'smtp-server': 'services.role.smtpServer',
    'imap-server': 'services.role.imapServer',
    'spam-filter': 'services.role.spamFilter',
};

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
    { id: 'monitoring', labelKey: 'services.cat.monitoring', icon: Activity, tint: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400' },
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
    required?: boolean;
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
    // Installed-first (D-010, Jul 18): the page shows what this server RUNS,
    // not the whole catalog. Page length then tracks the installed count, not
    // the catalog size — a clean server is 3-4 rows whether the catalog holds
    // 19 items or 40. This is the constitution's "a service that isn't
    // installed is invisible" as mechanics instead of a checkbox; the catalog
    // is the same list with the filter off, not a second screen.
    // Kurulu-önce (D-010, 18 Tem): sayfa, sunucunun ÇALIŞTIRDIĞINI gösterir,
    // tüm kataloğu değil. Böylece sayfa uzunluğu katalog boyutunu değil kurulu
    // kalem sayısını izler — temiz sunucu, katalog 19 da olsa 40 da olsa 3-4
    // satırdır. Bu, anayasanın "kurulu olmayan görünmezdir" maddesinin onay
    // kutusu değil mekanik hâli; katalog, aynı listenin süzgeci kapalı hâlidir,
    // ikinci ekran değil.
    // …with one correction from the field (operator, Jul 23: "hide not
    // installed keeps coming back checked"): installed-first is the right
    // FIRST impression, not a standing rule. The choice is now remembered,
    // and the catalog is reachable by a labelled button instead of a
    // checkbox — a filter that hides the only path to installing is not a
    // filter, it is a hidden step.
    // …sahadan gelen bir düzeltmeyle (operatör, 23 Tem: "hide not installed
    // sürekli seçili geliyor"): kurulu-önce doğru İLK izlenimdir, kalıcı bir
    // kural değil. Seçim artık hatırlanıyor ve kataloğa onay kutusu yerine
    // adı yazan bir düğmeyle ulaşılıyor — kurulumun tek yolunu gizleyen bir
    // süzgeç, süzgeç değil gizli bir adımdır.
    const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
    const [query, setQuery] = useState('');
    const viewKey = 'celikpanel.components.view';
    const [hideNotInstalled, setHideNotInstalled] = useState(
        () => (typeof localStorage === 'undefined' ? true : localStorage.getItem(viewKey) !== 'catalog'),
    );

    // Collapse follows the view, not a fixed default: the installed list is
    // short and wants to be readable at a glance; the full catalog is long
    // and wants folding (D-010 correction, learned from implementing it).
    // Katlama görünümü izler, sabit varsayılanı değil: kurulu liste kısadır ve
    // tek bakışta okunmak ister; tam katalog uzundur ve katlanmak ister
    // (D-010 düzeltmesi, uygulamadan öğrenildi).
    const setView = (installedOnly: boolean) => {
        setHideNotInstalled(installedOnly);
        try {
            localStorage.setItem(viewKey, installedOnly ? 'installed' : 'catalog');
        } catch {
            /* private mode / kısıtlı depolama — görünüm yine değişir */
        }
        setCollapsed(installedOnly ? new Set() : new Set(categoryOrder.map((c) => c.id)));
    };

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

    // Version drawers open per runtime row (B3b). Independent of category
    // collapse: a drawer is row detail, not a group.
    // Sürüm çekmeceleri runtime satırı başına açılır (B3b). Kategori
    // katlamasından bağımsız: çekmece grup değil, satır ayrıntısıdır.
    const [openDrawers, setOpenDrawers] = useState<Set<string>>(new Set());
    const toggleDrawer = (id: string) =>
        setOpenDrawers((prev) => {
            const next = new Set(prev);
            next.has(id) ? next.delete(id) : next.add(id);
            return next;
        });

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
    // The dialog stays OPEN through the request (B3d): a refusal renders its
    // evidence — who blocks — right where the decision is being made. It used
    // to close first, so a 409 could only flash as a toast.
    // Kurulu bir servisi kaldır: agent ile durdur + devre dışı + purge, sonra
    // yeniden tara. Saldırı yüzeyini geri küçültür — kurulumun aynası.
    // Modal istek boyunca AÇIK kalır (B3d): ret, kanıtını — kimin
    // engellediğini — kararın verildiği yerde çizer. Eskiden önce kapanıyordu;
    // 409 ancak toast olarak parlayıp sönebiliyordu.
    const [uninstallError, setUninstallError] = useState<ApiError | null>(null);
    const doUninstall = async (service: ManagedService) => {
        setBusy(service.id);
        setUninstallError(null);
        try {
            const res = await fetch('/api/v1/service/uninstall', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ service_id: service.id }),
            });
            if (!res.ok) {
                setUninstallError(await readApiError(res));
                return;
            }
            setUninstallTarget(null);
            showToast('success', t('services.uninstalled', { name: service.name }));
            loadServices();
        } catch {
            showToast('error', t('services.actionFailed'));
        } finally {
            setBusy(null);
        }
    };

    // Inline actions are for `service` rows, whose one unit shares the
    // catalogue id. Per-version units (php8.3-fpm) go through
    // handleInstanceAction below — the "default"-sentinel unit guessing that
    // used to live here is gone with B3b.
    // Satır içi eylemler `service` satırları içindir; onların tek unit'i
    // katalog id'siyle aynıdır. Sürüm başına unit'ler (php8.3-fpm) aşağıdaki
    // handleInstanceAction'dan geçer — eskiden burada yaşayan "default"
    // sentinel'li unit tahmini B3b ile gitti.
    const handleAction = async (service: ManagedService, action: 'start' | 'stop' | 'restart') => {
        setBusy(service.id);
        try {
            // Target the unit the SCAN found, not the catalogue id. They differ
            // exactly where this broke live: BIND's id is "bind" but its unit is
            // "named", so every stop/restart called a unit that does not exist
            // and the row just sat there (operator, 24 Jul).
            // Hedef, taramanın bulduğu unit'tir; katalog id'si değil. İkisi tam
            // da bunun canlıda kırıldığı yerde ayrışır: BIND'in id'si "bind",
            // unit'i "named"; bu yüzden her durdur/yeniden başlat var olmayan bir
            // unit'i çağırıyor ve satır olduğu yerde kalıyordu (operatör, 24 Tem).
            const res = await fetch('/api/v1/service/action', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name: service.unit || service.id, action }),
            });
            if (!res.ok) {
                // Say WHY. A generic "action failed" toast is why a real error
                // read as "nothing happened".
                // NEDENİNİ söyle. Genel "işlem başarısız" balonu, gerçek bir
                // hatanın "hiçbir şey olmadı" diye okunmasının sebebiydi.
                showToast('error', apiErrorText(await readApiError(res), t, 'services.actionFailed'));
                return;
            }
            await new Promise((r) => setTimeout(r, 1000));
            loadServices();
        } catch {
            showToast('error', t('services.actionFailed'));
        } finally {
            setBusy(null);
        }
    };

    // Start/stop ONE version of a runtime by its own unit. The endpoint
    // rescans server-side, so the follow-up load returns fresh per-instance
    // status.
    // Bir runtime'ın TEK sürümünü kendi unit'iyle başlat/durdur. Uç nokta
    // sunucu tarafında yeniden tarar; ardından gelen yükleme kopya-başına
    // taze durumu getirir.
    const handleInstanceAction = async (serviceId: string, unit: string, action: 'start' | 'stop' | 'restart') => {
        setBusy(`${serviceId}:${unit}`);
        try {
            const res = await fetch('/api/v1/service/action', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name: unit, action }),
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

    // A requirement token is either a ROLE (seat name: any member satisfies
    // it — "smtp-server" after the operator's "maybe I'll install a different
    // SMTP server") or a component id. Roles localize, ids resolve to their
    // catalog display name; the raw token never reaches the screen.
    // Gereksinim belirteci ya bir ROLdür (koltuk adı: herhangi bir üye tatmin
    // eder — operatörün "belki başka bir SMTP sunucusu kurarım"ı sonrası
    // "smtp-server") ya da bileşen id'sidir. Roller yerelleşir, id'ler katalog
    // görünen adına çözülür; ham belirteç ekrana hiç çıkmaz.
    const reqLabel = (token: string) => {
        if (REQ_ROLE_KEYS[token]) return t(REQ_ROLE_KEYS[token] as Parameters<typeof t>[0]);
        return services.find((x) => x.id === token)?.name ?? token;
    };
    const reqNames = (tokens?: string[]) => (tokens ?? []).map(reqLabel).join(', ');

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
                    <div className="ml-auto flex w-full flex-wrap items-center justify-end gap-x-5 gap-y-2 sm:w-auto">
                        <div className="w-full sm:w-56">
                            <SearchInput value={query} onChange={setQuery} placeholder={t('services.search')} />
                        </div>
                        {/* View controls kept as one tight cluster, clearly
                            separated from the search by the larger gap-x-5. */}
                        <div className="flex items-center gap-4">
                            {/* Two named views instead of one checkbox: each
                                says what it shows and how many, so "where do I
                                install from?" is answered by reading, not by
                                discovering a filter.
                                Tek onay kutusu yerine adı yazan iki görünüm:
                                her biri neyi kaç kalemle gösterdiğini söyler;
                                "nereden kurarım?" sorusu süzgeç keşfederek
                                değil okuyarak cevaplanır. */}
                            <div className="inline-flex overflow-hidden rounded-lg border border-border-strong">
                                <button
                                    onClick={() => setView(true)}
                                    className={`whitespace-nowrap px-3 py-1.5 text-xs font-medium transition-colors ${
                                        hideNotInstalled ? 'bg-primary text-primary-fg' : 'bg-surface text-fg-muted hover:bg-surface-2'
                                    }`}
                                >
                                    {t('services.viewInstalled', { n: services.filter((s) => s.is_installed).length })}
                                </button>
                                <button
                                    onClick={() => setView(false)}
                                    className={`whitespace-nowrap border-l border-border-strong px-3 py-1.5 text-xs font-medium transition-colors ${
                                        hideNotInstalled ? 'bg-surface text-fg-muted hover:bg-surface-2' : 'bg-primary text-primary-fg'
                                    }`}
                                >
                                    {t('services.viewCatalog', { n: services.length })}
                                </button>
                            </div>
                            <div className="flex gap-1 text-xs">
                                <button onClick={expandAll} className="whitespace-nowrap rounded-md px-2 py-1 text-fg-muted hover:bg-surface-2 hover:text-fg">
                                    {t('services.expandAll')}
                                </button>
                                <button onClick={collapseAll} className="whitespace-nowrap rounded-md px-2 py-1 text-fg-muted hover:bg-surface-2 hover:text-fg">
                                    {t('services.collapseAll')}
                                </button>
                            </div>
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
                        {/* A fresh server's installed view is legitimately
                            empty — that is a starting point, not a failed
                            search, so it offers the next step instead of
                            reporting nothing.
                            Taze sunucunun kurulu görünümü meşru olarak boştur —
                            bu başarısız arama değil başlangıç noktasıdır; hiçlik
                            bildirmek yerine sonraki adımı sunar. */}
                        {hideNotInstalled && q === '' ? (
                            <div className="flex flex-col items-center gap-3">
                                <span>{t('services.noneInstalledYet')}</span>
                                <Button variant="primary" icon={DownloadCloud} onClick={() => setView(false)}>
                                    {t('services.browseCatalog')}
                                </Button>
                            </div>
                        ) : (
                            t('services.noMatches')
                        )}
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
                                                            /* Empty means "version unknown" and renders as an
                                                               honest dash. The "default" sentinel that used to
                                                               stand here is dead (B3b).
                                                               Boş, "sürüm bilinmiyor" demektir ve dürüst bir
                                                               tireyle çizilir. Eskiden burada duran "default"
                                                               sentinel'i öldü (B3b). */
                                                            <span className="font-mono text-sm text-fg-muted">
                                                                {s.versions[0] ?? '—'}
                                                            </span>
                                                        )}
                                                    </div>
                                                    <div className="w-32 shrink-0">
                                                        <span className={`inline-flex items-center gap-1.5 text-sm ${s.is_installed ? 'text-fg-muted' : 'text-fg-subtle'}`}>
                                                            {/* A tool has no daemon of ours; a runtime whose
                                                                copies have no units either (node — executed
                                                                only by per-site apps) reports status
                                                                "installed". For both, "installed" is the whole
                                                                truth. php-fpm HAS units and keeps
                                                                running/stopped: a dead php-fpm must still
                                                                raise the alarm (D-010/B3b).
                                                                Tool'un bize ait daemon'ı yok; kopyalarının
                                                                unit'i olmayan runtime da (node — onu yalnız
                                                                site başına uygulamalar çalıştırır) "installed"
                                                                durumu bildirir. İkisi için de "kurulu" tam
                                                                gerçektir. php-fpm'in unit'leri VARDIR ve
                                                                çalışıyor/durdu kalır: ölü php-fpm yine alarm
                                                                vermelidir (D-010/B3b). */}
                                                            <StatusDot ok={s.is_installed && (running || s.kind === 'tool' || s.status === 'installed')} />
                                                            {!s.is_installed
                                                                ? t('services.notInstalled')
                                                                : s.kind === 'tool' || s.status === 'installed'
                                                                  ? t('services.installedLabel')
                                                                  : running
                                                                    ? t('services.running')
                                                                    : t('services.stopped')}
                                                        </span>
                                                    </div>
                                                    <div className="ml-auto flex items-center justify-end gap-1">
                                                    {!s.is_installed ? (
                                                        s.not_offered ? (
                                                            /* Honest instead of a dead Install button that
                                                               fails late in the agent (spamassassin on Arch,
                                                               netdata on Debian). The full per-distro list
                                                               lives in docs/DISTRO-SUPPORT.
                                                               Agent'ta geç patlayan ölü bir Kur düğmesi yerine
                                                               dürüstlük (Arch'ta spamassassin, Debian'da
                                                               netdata). Dağıtım başına tam liste
                                                               docs/DISTRO-SUPPORT içinde. */
                                                            <span
                                                                title={t('services.notOfferedHint')}
                                                                className="inline-flex items-center gap-1.5 rounded-lg border border-border bg-surface-2 px-3 py-1.5 text-xs font-medium text-fg-subtle"
                                                            >
                                                                {t('services.notOffered')}
                                                            </span>
                                                        ) : s.conflict_with ? (
                                                            <span
                                                                title={t('services.conflictHint', { name: s.conflict_with })}
                                                                className="inline-flex items-center gap-1.5 rounded-lg border border-border bg-surface-2 px-3 py-1.5 text-xs font-medium text-fg-subtle"
                                                            >
                                                                {t('services.conflictWith', { name: s.conflict_with })}
                                                            </span>
                                                        ) : s.requires_missing && s.requires_missing.length > 0 ? (
                                                            <span
                                                                title={t('services.requiresHint', { names: reqNames(s.requires_missing) })}
                                                                className="inline-flex items-center gap-1.5 rounded-lg border border-border bg-surface-2 px-3 py-1.5 text-xs font-medium text-fg-subtle"
                                                            >
                                                                {t('services.requiresLabel', { names: reqNames(s.requires_missing) })}
                                                            </span>
                                                        ) : (
                                                        <button
                                                            /* A tarball runtime (node) is not an apt/pacman
                                                               package — Install opens the version drawer,
                                                               where versions come from upstream.
                                                               Tarball runtime (node) apt/pacman paketi değil —
                                                               Kur, sürüm çekmecesini açar; sürümler kaynaktan
                                                               gelir. */
                                                            onClick={() => (isTarballRuntime(s) ? toggleDrawer(s.id) : setInstallTarget(s))}
                                                            disabled={busy === s.id}
                                                            className="inline-flex items-center gap-1.5 rounded-lg bg-primary px-3 py-1.5 text-xs font-semibold text-primary-fg transition-colors hover:bg-primary/90 disabled:opacity-50"
                                                        >
                                                            <DownloadCloud className="h-3.5 w-3.5" />
                                                            {busy === s.id ? t('services.installing') : t('services.install')}
                                                        </button>
                                                        )
                                                    ) : (
                                                    <>
                                                    {/* Inline start/stop belongs to `service` alone. A tool
                                                        (phpMyAdmin) has no unit at all; a runtime has one unit
                                                        PER VERSION, so a single Stop button would be a lie —
                                                        stop which PHP? Its controls live in the version drawer,
                                                        one row per version. Neither is a special case bolted on:
                                                        it falls out of Kind (D-010).
                                                        Satır içi başlat/durdur yalnız `service`indir. Tool'un
                                                        (phpMyAdmin) hiç unit'i yoktur; runtime'ın SÜRÜM BAŞINA
                                                        bir unit'i vardır, tek bir Durdur düğmesi yalan olurdu —
                                                        hangi PHP durdurulacak? Onun denetimi sürüm çekmecesinde,
                                                        sürüm başına bir satırdadır. İkisi de sonradan eklenmiş
                                                        özel durum değil: Kind'den düşer (D-010). */}
                                                    {/* Contextual: only actions that make sense for the current
                                                        state — no Stop on a stopped service, no Start on a running
                                                        one. / Bağlama duyarlı: yalnız mevcut duruma anlamlı gelen
                                                        eylemler — durmuşta Durdur, çalışanda Başlat gösterilmez. */}
                                                    {s.kind === 'service' && (running ? (
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
                                                    ))}
                                                    {/* The version drawer is where a runtime's per-version
                                                        state and controls live (B3b) — and, for a tarball
                                                        runtime, where installing a version lives too.
                                                        Sürüm çekmecesi, runtime'ın sürüm başına durum ve
                                                        denetiminin yaşadığı yerdir (B3b) — tarball runtime'da
                                                        sürüm kurmak da orada yaşar. */}
                                                    {s.kind === 'runtime' && (
                                                    <button
                                                        onClick={() => toggleDrawer(s.id)}
                                                        className="ml-1 inline-flex items-center gap-1.5 rounded-lg border border-border-strong bg-surface px-2.5 py-1.5 text-xs font-medium text-fg transition-colors hover:bg-surface-2"
                                                    >
                                                        {openDrawers.has(s.id) ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
                                                        {t('services.versionsToggle')}
                                                        <span className="rounded bg-surface-2 px-1 font-mono text-[10px] text-fg-subtle">{(s.instances ?? []).filter((i) => i.managed).length}</span>
                                                    </button>
                                                    )}
                                                    {/* Manage: extensions/config pages. A tool has nothing to
                                                        manage. / Yönet: uzantı/ayar sayfaları. Tool'un
                                                        yönetecek şeyi yoktur. */}
                                                    {s.kind !== 'tool' && (
                                                    <button
                                                        onClick={() => onManageService?.(s.id, s.versions)}
                                                        className="ml-1 inline-flex items-center gap-1.5 rounded-lg border border-border-strong bg-surface px-2.5 py-1.5 text-xs font-medium text-fg transition-colors hover:bg-surface-2"
                                                    >
                                                        <Settings className="h-3.5 w-3.5" />
                                                        {t('services.manage')}
                                                    </button>
                                                    )}
                                                    {/* A tarball runtime has no whole-service package to
                                                        purge; removal is per version and lands with the
                                                        usage ledger (B3d) — no delete offered until it can
                                                        refuse "in use by 3 sites".
                                                        Tarball runtime'ın purge edilecek bütün-servis paketi
                                                        yok; kaldırma sürüm başınadır ve kullanım defteriyle
                                                        gelir (B3d) — "3 site kullanıyor" diye reddedemeyen
                                                        silme sunulmaz. */}
                                                    {!isTarballRuntime(s) && (
                                                    <button
                                                        onClick={() => setUninstallTarget(s)}
                                                        title={t('services.uninstall')}
                                                        className="inline-flex items-center rounded-lg border border-border-strong bg-surface p-1.5 text-fg-muted transition-colors hover:bg-danger/10 hover:text-danger"
                                                    >
                                                        <Trash2 className="h-3.5 w-3.5" />
                                                    </button>
                                                    )}
                                                    </>
                                                    )}
                                                    </div>
                                                    {s.kind === 'runtime' && openDrawers.has(s.id) && (
                                                        <VersionDrawer
                                                            service={s}
                                                            busy={busy}
                                                            onInstanceAction={handleInstanceAction}
                                                            onVersionInstalled={scan}
                                                        />
                                                    )}
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
                    error={uninstallError}
                    onCancel={() => { setUninstallTarget(null); setUninstallError(null); }}
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
                                {/* Required repo (Netdata on Debian/Ubuntu): the
                                    package exists NOWHERE else, so pressing Install
                                    without it fails inside apt with "no installation
                                    candidate" — the operator's failed attempt on
                                    25 Jul. Say it up front instead.
                                    Zorunlu depo (Debian/Ubuntu'da Netdata): paket
                                    BAŞKA HİÇBİR yerde yok; onsuz Kur'a basmak apt
                                    içinde "kurulum adayı yok" ile düşer — operatörün
                                    25 Tem'deki başarısız denemesi. Bunu baştan söyle. */}
                                <p className={`mb-2 text-xs ${repo.required ? 'text-warning' : 'text-fg-subtle'}`}>
                                    {repo.required ? t('services.repo.requiredNote') : t('services.repo.note')}
                                </p>
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
    // Quiet ghost buttons in the row, but tone-colored at rest and with the
    // GLYPH solid — the icon reads 'full' without turning the row into a
    // color block (user feedback, twice refined).
    // Satırda sessiz hayalet düğmeler ama duruşta ton renkli ve GLİF dolu —
    // ikon 'dolu' okunur, satır renk bloğuna dönüşmez (iki kez rafine
    // edilen kullanıcı geri bildirimi).
    const toneCls = {
        success: 'text-success hover:bg-success/10',
        danger: 'text-danger hover:bg-danger/10',
        warning: 'text-warning hover:bg-warning/10',
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


// Local until B4 unifies the 5+ copies across the app. / B4 uygulamadaki 5+
// kopyayı tekleştirene dek yerel.
function fmtBytes(n: number): string {
    if (n >= 1024 * 1024 * 1024) return `${(n / (1024 * 1024 * 1024)).toFixed(1)} GB`;
    if (n >= 1024 * 1024) return `${Math.round(n / (1024 * 1024))} MB`;
    return `${Math.max(1, Math.round(n / 1024))} KB`;
}

// The version drawer (B3b): a runtime's versions live INSIDE the row, one
// sub-row per installed copy — status and start/stop per unit-bearing copy
// (php8.3-fpm), size for tarball trees (node), an honest "system" badge for
// what the panel found but did not install. For a tarball runtime this is
// also the ONLY place a version is installed from — versions never multiply
// the main list (D-010), and the package dialog stays a package dialog.
//
// Sürüm çekmecesi (B3b): runtime'ın sürümleri satırın İÇİNDE yaşar, kurulu
// kopya başına bir alt satır — unit taşıyan kopyada (php8.3-fpm) durum ve
// başlat/durdur, tarball ağacında (node) boyut, panelin bulduğu ama kurmadığı
// için dürüst "sistem" rozeti. Tarball runtime için sürüm kurmanın TEK adresi
// de burasıdır — sürümler ana listeyi asla çoğaltmaz (D-010) ve paket modalı
// paket modalı olarak kalır.
function VersionDrawer({
    service,
    busy,
    onInstanceAction,
    onVersionInstalled,
}: {
    service: ManagedService;
    busy: string | null;
    onInstanceAction: (serviceId: string, unit: string, action: 'start' | 'stop' | 'restart') => void;
    onVersionInstalled: () => void;
}) {
    const { t } = useI18n();
    const instances = service.instances ?? [];
    const tarball = isTarballRuntime(service);
    const blocked = (service.requires_missing?.length ?? 0) > 0;
    const nodeInstallHere = tarball && service.id === 'node' && !blocked;
    const [installing, setInstalling] = useState<string | null>(null);

    // Package-repo runtimes (php-fpm via Sury): installing an ADDITIONAL
    // version happens here too — before this, the version-pick dialog only
    // appeared while the service was NOT installed, so with 8.4 on the
    // machine there was no panel path to add 7.4 for a legacy site (caught
    // by the operator, 23 Jul). Same single-address rule as node: install
    // and remove live in the drawer.
    // Paket-depolu runtime'lar (Sury'li php-fpm): EK sürüm kurmak da burada —
    // bundan önce sürüm seçtiren pencere yalnız servis KURULU DEĞİLKEN
    // çıkıyordu; makinede 8.4 varken eski bir site için 7.4 eklemenin
    // panelden yolu yoktu (operatör yakaladı, 23 Tem). Node'la aynı
    // tek-adres kuralı: kur da kaldır da çekmecede.
    const [repo, setRepo] = useState<RepoInfo | null>(null);
    const [repoBusy, setRepoBusy] = useState(false);
    useEffect(() => {
        if (tarball || service.kind !== 'runtime') return;
        fetch(`/api/v1/repo?service_id=${encodeURIComponent(service.id)}`)
            .then((r) => (r.ok ? r.json() : null))
            .then((d: RepoInfo | null) => setRepo(d && d.available ? d : null))
            .catch(() => {});
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [service.id]);

    const enableRepo = async () => {
        setRepoBusy(true);
        try {
            const res = await fetch('/api/v1/repo', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ service_id: service.id, action: 'enable' }),
            });
            const d = await res.json();
            if (!res.ok || d.error) throw new Error(d.error);
            setRepo(d);
        } catch (e) {
            showToast('error', e instanceof Error && e.message ? e.message : t('services.actionFailed'));
        } finally {
            setRepoBusy(false);
        }
    };

    const installPackage = async (pkg: string, version: string) => {
        if (installing) return;
        setInstalling(pkg);
        try {
            const res = await fetch('/api/v1/service/install', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ service_id: service.id, package: pkg }),
            });
            const data = await res.json();
            if (!res.ok || data.error) throw new Error(data.error);
            showToast('success', t('services.versionInstalled', { version }));
            onVersionInstalled();
        } catch (e) {
            showToast('error', e instanceof Error && e.message ? e.message : t('services.actionFailed'));
        } finally {
            setInstalling(null);
        }
    };

    // "php8.2-fpm" → "8.2". / "php8.2-fpm" → "8.2".
    const pkgVersion = (pkg: string) => /([0-9]+\.[0-9]+)/.exec(pkg)?.[1] ?? '';
    // Every PHP line below 8.2 is past upstream security support in mid-2026.
    // A static threshold is honest enough until a maintained EOL table exists
    // (B4 note); the point is that a legacy line is never offered unlabeled.
    // 8.2 altındaki her PHP hattı 2026 ortasında güvenlik desteğinin dışında.
    // Bakımlı bir EOL tablosu gelene dek (B4 notu) sabit eşik yeterince
    // dürüst; mesele eski hattın asla etiketsiz sunulmaması.
    const isEOL = (version: string) => {
        const m = /^([0-9]+)\.([0-9]+)$/.exec(version);
        if (!m) return false;
        const [maj, min] = [Number(m[1]), Number(m[2])];
        return maj < 8 || (maj === 8 && min < 2);
    };

    // Named LTS options from the official index — the free-text semver box
    // died with B3d: an operator picks "Node 24 (LTS)" the way they pick
    // "PHP 8.3", they do not transcribe version numbers.
    // Resmi dizinden adlandırılmış LTS seçenekleri — serbest semver kutusu
    // B3d ile öldü: operatör "PHP 8.3" seçer gibi "Node 24 (LTS)" seçer,
    // sürüm numarası kopyalamaz.
    const [lts, setLts] = useState<{ version: string; name: string }[] | null>(null);
    const [ltsFailed, setLtsFailed] = useState(false);
    const loadLts = () => {
        setLtsFailed(false);
        setLts(null);
        fetch('/api/v1/runtimes/node/lts')
            .then((r) => (r.ok ? r.json() : Promise.reject()))
            .then((d) => setLts(d?.releases ?? []))
            .catch(() => setLtsFailed(true));
    };
    useEffect(() => {
        if (nodeInstallHere) loadLts();
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [nodeInstallHere]);

    const installVersion = async (version: string) => {
        if (installing) return;
        setInstalling(version);
        try {
            const res = await fetch('/api/v1/runtimes/node', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ version }),
            });
            const data = await res.json();
            if (!res.ok || data.error) throw new Error(data.error);
            showToast('success', t('services.versionInstalled', { version }));
            onVersionInstalled();
        } catch (e) {
            showToast('error', e instanceof Error && e.message ? e.message : t('services.actionFailed'));
        } finally {
            setInstalling(null);
        }
    };

    // Per-version removal (B3d) — the reason the drawer may offer delete at
    // all: the panel refuses with the blocking-site list while anything runs
    // on the version, and that refusal renders right here in the confirm
    // dialog. php versions go through the package pick; node versions
    // through their own endpoint. Unmanaged ("system") rows never get one.
    // Sürüm başına kaldırma (B3d) — çekmecenin silme sunabilmesinin sebebi:
    // sürümün üstünde bir şey koşarken panel, engelleyen-site listesiyle
    // reddeder ve o ret tam burada, onay penceresinde çizilir. php sürümleri
    // paket seçiminden, node sürümleri kendi ucundan gider. Yönetilmeyen
    // ("sistem") satıra silme verilmez.
    const [removeTarget, setRemoveTarget] = useState<ServiceInstance | null>(null);
    const [removeError, setRemoveError] = useState<ApiError | null>(null);
    const [removing, setRemoving] = useState(false);
    const removeVersion = async (inst: ServiceInstance) => {
        setRemoving(true);
        setRemoveError(null);
        try {
            const res = inst.unit
                ? await fetch('/api/v1/service/uninstall', {
                      method: 'POST',
                      headers: { 'Content-Type': 'application/json' },
                      body: JSON.stringify({ service_id: service.id, package: inst.unit }),
                  })
                : await fetch(`/api/v1/runtimes/node/${inst.version}`, { method: 'DELETE' });
            if (!res.ok) {
                setRemoveError(await readApiError(res));
                return;
            }
            setRemoveTarget(null);
            showToast('success', t('services.versionRemoved', { version: inst.version }));
            onVersionInstalled();
        } catch {
            showToast('error', t('services.actionFailed'));
        } finally {
            setRemoving(false);
        }
    };
    const canRemove = (inst: ServiceInstance) =>
        inst.managed && (inst.unit ? service.kind === 'runtime' : service.id === 'node');

    return (
        <div className="mt-1 w-full rounded-lg border border-border bg-surface-2/40 px-4 py-3">
            <div className="mb-1 text-xs font-semibold uppercase tracking-wide text-fg-subtle">
                {t('services.instancesTitle')}
            </div>
            {instances.length === 0 ? (
                <div className="py-1 text-sm text-fg-subtle">{t('services.instancesEmpty')}</div>
            ) : (
                <ul className="divide-y divide-border/60">
                    {instances.map((inst) => {
                        const instRunning = (inst.status ?? '').toLowerCase().startsWith('active');
                        const instBusy = busy === `${service.id}:${inst.unit}`;
                        return (
                            <li key={`${inst.version}:${inst.unit ?? inst.path ?? ''}`} className="flex flex-wrap items-center gap-x-4 gap-y-1 py-2">
                                <span className="w-16 shrink-0 font-mono text-sm font-medium text-fg">{inst.version || '—'}</span>
                                {!inst.managed && (
                                    <span
                                        title={t('services.systemRuntimeHint')}
                                        className="rounded bg-surface-2 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-fg-subtle"
                                    >
                                        {t('services.systemRuntime')}
                                    </span>
                                )}
                                {inst.unit ? (
                                    <span className="inline-flex w-28 items-center gap-1.5 text-sm text-fg-muted">
                                        <StatusDot ok={instRunning} />
                                        {instRunning ? t('services.running') : t('services.stopped')}
                                    </span>
                                ) : (
                                    /* No unit → nothing to run; "installed" is the whole
                                       truth. / Unit yok → koşacak şey yok; "kurulu" tam
                                       gerçektir. */
                                    <span className="w-28 text-sm text-fg-subtle">{t('services.installedLabel')}</span>
                                )}
                                {inst.unit && <span className="hidden font-mono text-xs text-fg-subtle md:inline">{inst.unit}</span>}
                                {(inst.size_bytes ?? 0) > 0 && (
                                    <span className="text-xs text-fg-subtle">{fmtBytes(inst.size_bytes as number)}</span>
                                )}
                                {(inst.unit && inst.managed) || canRemove(inst) ? (
                                    <span className="ml-auto flex items-center gap-1">
                                        {inst.unit && inst.managed && (instRunning ? (
                                            <>
                                                <ActionIcon
                                                    title={t('services.restart')}
                                                    onClick={() => onInstanceAction(service.id, inst.unit as string, 'restart')}
                                                    disabled={instBusy}
                                                    tone="warning"
                                                >
                                                    <RotateCw className="h-4 w-4" />
                                                </ActionIcon>
                                                <ActionIcon
                                                    title={t('services.stop')}
                                                    onClick={() => onInstanceAction(service.id, inst.unit as string, 'stop')}
                                                    disabled={instBusy}
                                                    tone="danger"
                                                >
                                                    <Square className="h-4 w-4" fill="currentColor" />
                                                </ActionIcon>
                                            </>
                                        ) : (
                                            <ActionIcon
                                                title={t('services.start')}
                                                onClick={() => onInstanceAction(service.id, inst.unit as string, 'start')}
                                                disabled={instBusy}
                                                tone="success"
                                            >
                                                <Play className="h-4 w-4" fill="currentColor" />
                                            </ActionIcon>
                                        ))}
                                        {canRemove(inst) && (
                                            <ActionIcon
                                                title={t('services.removeVersion')}
                                                onClick={() => { setRemoveError(null); setRemoveTarget(inst); }}
                                                disabled={removing}
                                                tone="danger"
                                            >
                                                <Trash2 className="h-4 w-4" />
                                            </ActionIcon>
                                        )}
                                    </span>
                                ) : null}
                            </li>
                        );
                    })}
                </ul>
            )}
            {/* Additional versions for a repo-backed runtime (php via Sury).
                Not-yet-installed lines render as install buttons; EOL lines
                are labeled, never hidden — a legacy site is a legitimate
                reason, an unlabeled trap is not.
                Depo-destekli runtime'da ek sürümler (Sury'li php). Kurulu
                olmayan hatlar kurulum düğmesi olur; EOL hatlar etiketlenir,
                asla gizlenmez — eski site meşru sebeptir, etiketsiz tuzak
                değildir. */}
            {!tarball && repo && (
                <div className="mt-2 border-t border-border/60 pt-3">
                    {!repo.enabled ? (
                        <div className="flex flex-wrap items-center gap-3">
                            <span className="text-xs text-fg-subtle">{t('services.repo.note')}</span>
                            <Button variant="secondary" onClick={enableRepo} disabled={repoBusy}>
                                {repoBusy ? t('services.repo.enabling') : t('services.repo.enable')}
                            </Button>
                        </div>
                    ) : (
                        <>
                            <div className="flex flex-wrap items-center gap-2">
                                {(repo.packages ?? []).map((pkg) => {
                                    const v = pkgVersion(pkg);
                                    const already = instances.some((i) => i.managed && i.version === v);
                                    if (!v || already) return null;
                                    return (
                                        <button
                                            key={pkg}
                                            onClick={() => installPackage(pkg, v)}
                                            disabled={installing !== null}
                                            title={isEOL(v) ? t('services.eolHint') : pkg}
                                            className="inline-flex items-center gap-1.5 rounded-lg border border-border-strong bg-surface px-3 py-1.5 text-xs font-semibold text-fg transition-colors hover:bg-surface-2 disabled:opacity-50"
                                        >
                                            <DownloadCloud className="h-3.5 w-3.5" />
                                            {installing === pkg ? t('services.installing') : `${service.name} ${v}`}
                                            {isEOL(v) && (
                                                <span className="rounded bg-warning/15 px-1 py-0.5 text-[10px] font-bold uppercase text-warning">
                                                    {t('services.eolBadge')}
                                                </span>
                                            )}
                                        </button>
                                    );
                                })}
                            </div>
                            <div className="mt-1.5 text-xs text-fg-subtle">{t('services.phpVersionsHint')}</div>
                        </>
                    )}
                </div>
            )}
            {/* The install endpoint is node-specific today; when a second
                tarball runtime arrives the endpoint generalizes and this id
                check dissolves into it. Posting python versions to the node
                endpoint is the bug this line prevents.
                Kurulum ucu bugün node'a özgü; ikinci bir tarball runtime
                gelince uç genelleşir ve bu id denetimi onun içinde erir.
                Node ucuna python sürümü göndermek, bu satırın önlediği hatadır. */}
            {tarball && service.id === 'node' && (
                <div className="mt-2 border-t border-border/60 pt-3">
                    {blocked ? (
                        /* The same declarative gate as the row's Install button:
                           requirements first. / Satırdaki Kur ile aynı bildirimsel
                           kapı: önce gereksinimler. */
                        <div className="text-sm text-fg-subtle">
                            {t('services.requiresLabel', {
                                names: (service.requires_missing ?? [])
                                    .map((tok) => (REQ_ROLE_KEYS[tok] ? t(REQ_ROLE_KEYS[tok] as Parameters<typeof t>[0]) : tok))
                                    .join(', '),
                            })}
                        </div>
                    ) : (
                        <>
                            {lts === null && !ltsFailed && (
                                <div className="text-sm text-fg-subtle">{t('services.ltsLoading')}</div>
                            )}
                            {ltsFailed && (
                                <div className="flex flex-wrap items-center gap-3 text-sm text-fg-subtle">
                                    {t('services.ltsFailed')}
                                    <Button variant="secondary" onClick={loadLts}>{t('services.ltsRetry')}</Button>
                                </div>
                            )}
                            {lts !== null && (
                                <div className="flex flex-wrap items-center gap-2">
                                    {lts.map((rel) => {
                                        const already = instances.some((i) => i.managed && i.version === rel.version);
                                        const major = rel.version.split('.')[0];
                                        return (
                                            <button
                                                key={rel.version}
                                                onClick={() => installVersion(rel.version)}
                                                disabled={already || installing !== null}
                                                className="inline-flex items-center gap-1.5 rounded-lg border border-border-strong bg-surface px-3 py-1.5 text-xs font-semibold text-fg transition-colors hover:bg-surface-2 disabled:opacity-50"
                                            >
                                                <DownloadCloud className="h-3.5 w-3.5" />
                                                {installing === rel.version
                                                    ? t('services.installing')
                                                    : already
                                                      ? t('services.ltsInstalled', { major })
                                                      : `Node ${major} LTS · ${rel.version}`}
                                            </button>
                                        );
                                    })}
                                </div>
                            )}
                            <div className="mt-1.5 text-xs text-fg-subtle">{t('services.nodeInstallHint')}</div>
                        </>
                    )}
                </div>
            )}
                {/* No backdrop dismissal on a DESTRUCTIVE dialog. The operator
                    pressed Uninstall on BIND, clicked next to the box, and the
                    dialog vanished without a trace — which reads exactly like
                    "I said remove and it did not remove" (24 Jul; the audit log
                    later showed the request had never been sent). A silent
                    cancel is indistinguishable from a broken button, so leaving
                    requires the explicit Cancel. The install dialog keeps its
                    backdrop dismissal: dropping an install prompt costs nothing.
                    Yıkıcı diyalogda arka plana tıklayarak kapatma YOK. Operatör
                    BIND'de Kaldır'a bastı, kutunun yanına tıkladı ve diyalog iz
                    bırakmadan kayboldu — bu, "kaldır dedim kaldırmadı" diye
                    okunur (24 Tem; denetim kaydı isteğin hiç gönderilmediğini
                    sonradan gösterdi). Sessiz vazgeçme, bozuk düğmeden ayırt
                    edilemez; çıkmak için açıkça Vazgeç gerekir. Kurulum
                    diyalogu arka plan kapatmasını korur: kurulum istemini
                    düşürmenin bedeli yok. */}
            {removeTarget && (
                <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
                    <div className="w-full max-w-md rounded-2xl border border-danger/40 bg-surface p-6 shadow-xl">
                        <div className="mb-4 flex items-start gap-3">
                            <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-danger/10 text-danger">
                                <Trash2 className="h-5 w-5" />
                            </span>
                            <div className="min-w-0">
                                <h3 className="text-lg font-semibold text-fg">
                                    {t('services.removeVersionTitle', { name: service.name, version: removeTarget.version })}
                                </h3>
                                <p className="text-sm text-fg-muted">{t('services.removeVersionWarn')}</p>
                            </div>
                        </div>
                        {/* The refusal's evidence — the blocking sites — lands
                            here, in the confirm dialog (B3d).
                            Retin kanıtı — engelleyen siteler — buraya, onay
                            penceresine düşer (B3d). */}
                        <ErrorBanner error={removeError} className="mb-4" />
                        <div className="flex justify-end gap-2">
                            <Button variant="secondary" onClick={() => setRemoveTarget(null)} disabled={removing}>
                                {t('common.cancel')}
                            </Button>
                            <Button variant="danger" onClick={() => removeVersion(removeTarget)} disabled={removing} icon={Trash2}>
                                {removing ? t('services.uninstalling') : t('services.removeVersion')}
                            </Button>
                        </div>
                    </div>
                </div>
            )}
        </div>
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
    error,
    onCancel,
    onConfirm,
}: {
    service: ManagedService;
    busy: boolean;
    error: ApiError | null;
    onCancel: () => void;
    onConfirm: () => void;
}) {
    const { t } = useI18n();
    // Destructive: no backdrop dismissal — see the note on the version-removal
    // dialog. / Yıkıcı: arka plana tıklayarak kapatma yok — sürüm kaldırma
    // diyalogundaki nota bakın.
    return (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
            <div className="w-full max-w-md rounded-2xl border border-danger/40 bg-surface p-6 shadow-xl">
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
                {/* A refusal shows its evidence HERE, where the decision is
                    made — who blocks, line by line (B3d).
                    Ret, kanıtını kararın verildiği yerde gösterir — kimin
                    engellediği, satır satır (B3d). */}
                <ErrorBanner error={error} className="mb-4" />
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
    const [st, setSt] = useState<{ enabled: boolean; engine_available?: boolean; tcp_ports?: number[]; udp_ports?: number[]; ssh_ports?: number[] } | null>(null);
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

    // No engine → the box is exposed (no firewall) AND "Turn on" cannot work.
    // Route the operator to install the engine (it's the nftables card in the
    // Security section on this very page) rather than fail an opaque toggle.
    // Motor yok → kutu açık (duvar yok) VE "Turn on" çalışamaz. Anlamsız bir
    // hata yerine operatörü motoru kurmaya yönlendir (bu sayfadaki Güvenlik
    // bölümündeki nftables kartı).
    if (st.engine_available === false) {
        return (
            <section className="mb-4 rounded-xl border border-warning/50 bg-warning/10 p-4">
                <div className="flex flex-wrap items-center gap-3">
                    <ShieldOff className="h-5 w-5 shrink-0 text-warning" />
                    <div className="min-w-0 flex-1">
                        <div className="text-sm font-semibold text-fg">
                            {t('firewall.title')} <span className="text-warning">{t('firewall.noEngine')}</span>
                        </div>
                        <p className="text-xs text-fg-muted">{t('firewall.noEngineHint')}</p>
                    </div>
                </div>
            </section>
        );
    }

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
