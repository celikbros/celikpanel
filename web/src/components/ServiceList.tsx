import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { Settings, Play, Square, RotateCw, RefreshCw, ScanSearch, DownloadCloud, ChevronDown, ChevronRight, Trash2, ShieldCheck, ShieldOff, Layers, Globe, Database, Mail, Network, Shield, Zap, FolderUp, Activity, Boxes } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { PageHeader, StatusDot, EmptyState, Button, SearchInput, ErrorBanner } from './ui';
import { readApiError, apiErrorText, type ApiError } from '../lib/apiError';
import {
    decodeManagedMailProfiles,
    useComponentOperation,
    type ManagedMailProfile,
} from './ComponentOperation';
import { useLocation, useNavigate } from '../router';

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
    /** Taramanın bulduğu gerçek systemd unit'i (BIND id'si "bind", unit'i "named"). */
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
    not_offered_kind?: 'integration' | 'distribution';
    not_offered_reason?: string;
    requires_missing?: string[];
    kind?: 'service' | 'runtime' | 'tool';
    packages?: string[];
    ports?: string[];
    /** Exact installed version package to reuse for a safe idempotent repair. */
    /** Güvenli ve idempotent onarımda yeniden kullanılacak tam kurulu sürüm paketi. */
    repair_package?: string;
    /** False when a versioned apt install cannot be tied to one exact package. */
    /** Sürümlü apt kurulumu tek bir kesin paketle eşleştirilemiyorsa false olur. */
    repair_available: boolean;
}

interface ManagedServicesSnapshot {
    services: ManagedService[];
    profiles: ManagedMailProfile[];
    scannedAt: string | null;
}

interface ServiceVerificationState {
    unverified: boolean;
    baselineScannedAt: string | null;
}

const serviceVerificationStorageKey = 'celikpanel.components.unverified-state';

function isServiceInstance(value: unknown): value is ServiceInstance {
    if (!value || typeof value !== 'object') return false;
    const instance = value as Record<string, unknown>;
    return (
        typeof instance.version === 'string' &&
        typeof instance.managed === 'boolean' &&
        (instance.unit === undefined || typeof instance.unit === 'string') &&
        (instance.path === undefined || typeof instance.path === 'string') &&
        (instance.status === undefined || typeof instance.status === 'string') &&
        (instance.size_bytes === undefined || typeof instance.size_bytes === 'number')
    );
}

function isManagedService(value: unknown): value is ManagedService {
    if (!value || typeof value !== 'object') return false;
    const service = value as Record<string, unknown>;
    return (
        typeof service.id === 'string' &&
        typeof service.name === 'string' &&
        typeof service.description === 'string' &&
        typeof service.icon === 'string' &&
        typeof service.category === 'string' &&
        typeof service.status === 'string' &&
        typeof service.is_installed === 'boolean' &&
        Array.isArray(service.versions) &&
        service.versions.every((version) => typeof version === 'string') &&
        (service.instances === undefined ||
            (Array.isArray(service.instances) && service.instances.every(isServiceInstance))) &&
        (service.ports === undefined ||
            (Array.isArray(service.ports) && service.ports.every((port) => typeof port === 'string'))) &&
        (service.repair_available === undefined || typeof service.repair_available === 'boolean')
    );
}

// A snapshot is trusted only after validating the fields this screen uses.
// An array-shaped error or half-written payload must never unlock mutations.
// Bir snapshot yalnız bu ekranın kullandığı alanlar doğrulandıktan sonra
// güvenilir sayılır. Dizi görünümlü hata ya da yarım yük mutasyon kilidini
// asla açmamalıdır.
function parseManagedServicesSnapshot(value: unknown): ManagedServicesSnapshot | null {
    if (!value || typeof value !== 'object') return null;
    const payload = value as Record<string, unknown>;
    if (!Array.isArray(payload.services) || !payload.services.every(isManagedService)) return null;
    const serviceIDs = new Set<string>();
    for (const service of payload.services) {
        if (
            service.id.trim() !== service.id
            || service.id === ''
            || serviceIDs.has(service.id)
        ) {
            return null;
        }
        serviceIDs.add(service.id);
    }
    const profiles = decodeManagedMailProfiles(payload.profiles, serviceIDs);
    if (profiles === null) return null;
    if (
        payload.scanned_at !== undefined
        && payload.scanned_at !== null
        && (
            typeof payload.scanned_at !== 'string'
            || !Number.isFinite(Date.parse(payload.scanned_at))
        )
    ) {
        return null;
    }
    return {
        services: payload.services,
        profiles,
        scannedAt: typeof payload.scanned_at === 'string' ? payload.scanned_at : null,
    };
}

function readServiceVerificationState(): ServiceVerificationState {
    if (typeof sessionStorage === 'undefined') return { unverified: false, baselineScannedAt: null };
    try {
        const value = JSON.parse(sessionStorage.getItem(serviceVerificationStorageKey) ?? 'null') as Record<string, unknown> | null;
        if (!value || value.unverified !== true) return { unverified: false, baselineScannedAt: null };
        return {
            unverified: true,
            baselineScannedAt: typeof value.baselineScannedAt === 'string' ? value.baselineScannedAt : null,
        };
    } catch {
        return { unverified: false, baselineScannedAt: null };
    }
}

function persistServiceVerificationState(value: ServiceVerificationState) {
    if (typeof sessionStorage === 'undefined') return;
    try {
        if (value.unverified) sessionStorage.setItem(serviceVerificationStorageKey, JSON.stringify(value));
        else sessionStorage.removeItem(serviceVerificationStorageKey);
    } catch {
        // Restricted storage must not bypass the in-memory safety lock.
        // Kısıtlı depolama, bellek içindeki güvenlik kilidini aşmamalıdır.
    }
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
    'kv-store': 'services.role.kvStore',
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

// A cached scan keeps the page instant. Revalidate it in the background when
// it is older than this, and always scan on the first visit after installation.
// Önbellekteki tarama sayfayı anında açar. Bu süreden eskiyse arka planda
// doğrula; kurulumdan sonraki ilk ziyarette ise mutlaka tara.
const AUTO_SCAN_MAX_AGE_SECONDS = 300;

// A service's optional managed vendor repository (e.g. PGDG for PostgreSQL).
// When available+enabled, the install dialog offers a specific major version.
// Bir servisin isteğe bağlı yönetilen vendor deposu (örn. PostgreSQL için PGDG).
// Mevcut+etkinse, kurulum modalı belirli bir major sürüm sunar.
interface RepoInfo {
    available: boolean;
    enabled: boolean;
    repairable?: boolean;
    id?: string;
    name?: string;
    detail?: string;
    required?: boolean;
    packages?: string[];
    error_code?: string;
}

// unknownCategories returns a card definition for every category present in
// the data but missing from categoryOrder above, so nothing can be silently
// dropped. The label falls back to the raw category id — honest and obviously
// unfinished, which is exactly the signal that the id belongs in categoryOrder
// with a proper label and icon.
// unknownCategories, veride bulunup yukarıdaki categoryOrder'da olmayan her
// kategori için bir kart tanımı döndürür; böylece hiçbir şey sessizce
// düşmez. Etiket ham kategori id'sine düşer — dürüst ve bariz biçimde
// yarım; ki bu da o id'nin düzgün bir etiket ve ikonla categoryOrder'a
// girmesi gerektiğinin işaretidir.
function unknownCategories(list: ManagedService[]) {
    const known = new Set(categoryOrder.map((c) => c.id));
    const extra: string[] = [];
    for (const s of list) {
        if (s.category && !known.has(s.category) && !extra.includes(s.category)) extra.push(s.category);
    }
    return extra.map((id) => ({ id, labelKey: id, icon: Boxes, tint: 'bg-slate-500/10 text-slate-600 dark:text-slate-400' }));
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
    const navigate = useNavigate();
    const location = useLocation();
    const { startInstall, catalogSnapshot } = useComponentOperation();
    const [services, setServices] = useState<ManagedService[]>([]);
    const [profiles, setProfiles] = useState<ManagedMailProfile[]>([]);
    const [scannedAt, setScannedAt] = useState<string | null>(null);
    const [loading, setLoading] = useState(true);
    const [scanning, setScanning] = useState(false);
    const [busy, setBusy] = useState<string | null>(null);
    const [installTarget, setInstallTarget] = useState<ManagedService | null>(null);
    const [profileTarget, setProfileTarget] = useState<ManagedMailProfile | null>(null);
    const [uninstallTarget, setUninstallTarget] = useState<ManagedService | null>(null);
    const [verification, setVerification] = useState<ServiceVerificationState>(readServiceVerificationState);
    const latestScannedAtRef = useRef<string | null>(null);
    const stateUnverified = verification.unverified;

    const markStateUnverified = () => {
        setVerification((previous) => {
            const next = {
                unverified: true,
                baselineScannedAt: previous.unverified ? previous.baselineScannedAt : scannedAt,
            };
            persistServiceVerificationState(next);
            return next;
        });
    };

    const clearStateUnverified = () => {
        const next = { unverified: false, baselineScannedAt: null };
        persistServiceVerificationState(next);
        setVerification(next);
    };

    const applySnapshot = (value: unknown, source: 'load' | 'scan'): boolean => {
        const snapshot = parseManagedServicesSnapshot(value);
        if (!snapshot) return false;
        if (source === 'scan' && snapshot.scannedAt === null) return false;

        const currentScannedAt = latestScannedAtRef.current;
        const currentTimestamp = Date.parse(currentScannedAt || '');
        const nextTimestamp = Date.parse(snapshot.scannedAt || '');
        if (
            Number.isFinite(currentTimestamp)
            && (
                !Number.isFinite(nextTimestamp)
                || nextTimestamp < currentTimestamp
                || (source === 'load' && nextTimestamp === currentTimestamp)
            )
        ) {
            return true;
        }
        latestScannedAtRef.current = snapshot.scannedAt;
        setServices(snapshot.services);
        setProfiles(snapshot.profiles);
        setScannedAt(snapshot.scannedAt);

        // A cached load may still be the exact pre-mutation snapshot retained
        // after an atomic scan failure. Only a fresh scan, or a load carrying
        // a newer scan token, may release the mutation lock.
        // Önbellek yüklemesi, atomik tarama hatasından sonra korunan mutasyon
        // öncesi snapshot'ın aynısı olabilir. Kilidi yalnız taze tarama veya
        // daha yeni tarama belirteci taşıyan yükleme açabilir.
        const loadProvesNewState =
            source === 'load' &&
            snapshot.scannedAt !== null &&
            snapshot.scannedAt !== verification.baselineScannedAt;
        if (source === 'scan' || !stateUnverified || loadProvesNewState) clearStateUnverified();
        return true;
    };

    const handleStateRefreshFailure = (error: ApiError) => {
        markStateUnverified();
        showToast('error', apiErrorText(error, t, 'services.actionFailed'));
    };
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
        void loadServices();
    }, []);

    // The operation controller keeps the panel locked through its mandatory
    // post-install scan. Consume that exact fresh snapshot before the overlay
    // is removed instead of racing it with another independent load.
    // İşlem denetleyicisi, zorunlu kurulum sonrası tarama boyunca paneli kilitli
    // tutar. Overlay'i kaldırmadan önce, başka bağımsız yüklemeyle yarıştırmak
    // yerine bu kesin taze snapshot'ı kullan.
    useLayoutEffect(() => {
        if (!catalogSnapshot) return;
        if (!applySnapshot(catalogSnapshot, 'scan')) {
            markStateUnverified();
            showToast('error', t('services.scanFailed'));
        }
        setLoading(false);
    }, [catalogSnapshot]);

    useLayoutEffect(() => {
        if (location.hash !== '#mail-stacks' || loading || profiles.length === 0) return;
        const target = document.getElementById('mail-stacks');
        if (!target) return;
        target.scrollIntoView({ block: 'start' });
        target.focus({ preventScroll: true });
    }, [loading, location.hash, profiles.length]);

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

    // Load the cached snapshot first so returning visits stay instant. A
    // missing or stale snapshot is then revalidated through a conditional
    // scan: the backend coalesces StrictMode, multiple tabs and simultaneous
    // page opens into at most one host-wide probe.
    // Önce önbellekteki snapshot'ı yükle; geri dönen ziyaretler anında açılsın.
    // Eksik veya bayat snapshot daha sonra koşullu taramayla doğrulanır:
    // backend StrictMode'u, çoklu sekmeleri ve eşzamanlı açılışları en fazla
    // tek sistem geneli yoklamada birleştirir.
    const loadServices = async (
        { markUnverifiedOnFailure = true }: { markUnverifiedOnFailure?: boolean } = {},
    ): Promise<boolean> => {
        const failVerification = (): false => {
            if (markUnverifiedOnFailure) markStateUnverified();
            return false;
        };
        setLoading(true);
        try {
            const res = await fetch('/api/v1/managed-services');
            if (!res.ok) {
                showToast('error', apiErrorText(await readApiError(res), t));
                return failVerification();
            }
            const data: unknown = await res.json();
            const snapshot = parseManagedServicesSnapshot(data);
            if (!snapshot || !applySnapshot(data, 'load')) {
                showToast('error', t('services.scanFailed'));
                return failVerification();
            }

            const scannedTimestamp = Date.parse(snapshot.scannedAt ?? '');
            const cacheIsStale =
                !Number.isFinite(scannedTimestamp)
                || Date.now() - scannedTimestamp > AUTO_SCAN_MAX_AGE_SECONDS * 1000;
            if (!cacheIsStale) return true;

            // Keep a valid cached page visible while it is refreshed. A fresh
            // install has no trusted observation yet, so retain the loader
            // until the first scan completes instead of briefly claiming that
            // every catalogue item is not installed.
            // Geçerli önbelleği yenilerken sayfada tut. Taze kurulumda henüz
            // güvenilir gözlem yoktur; bütün katalog yanlışlıkla "kurulu değil"
            // görünmesin diye ilk tarama bitene dek yükleyiciyi koru.
            if (snapshot.scannedAt !== null) setLoading(false);
            setScanning(true);
            try {
                const scanResponse = await fetch(
                    `/api/v1/managed-services/scan?max_age_seconds=${AUTO_SCAN_MAX_AGE_SECONDS}`,
                    { method: 'POST', cache: 'no-store' },
                );
                if (!scanResponse.ok) {
                    showToast('error', apiErrorText(await readApiError(scanResponse), t, 'services.scanFailed'));
                    return failVerification();
                }
                const refreshed: unknown = await scanResponse.json();
                if (!applySnapshot(refreshed, 'scan')) {
                    showToast('error', t('services.scanFailed'));
                    return failVerification();
                }
            } catch {
                showToast('error', t('services.scanFailed'));
                return failVerification();
            } finally {
                setScanning(false);
            }
            return true;
        } catch {
            showToast('error', t('common.error'));
            return failVerification();
        } finally {
            setLoading(false);
        }
    };

    const scan = async (): Promise<boolean> => {
        setScanning(true);
        try {
            const res = await fetch('/api/v1/managed-services/scan', {
                method: 'POST',
                cache: 'no-store',
            });
            if (!res.ok) {
                showToast('error', apiErrorText(await readApiError(res), t, 'services.scanFailed'));
                markStateUnverified();
                return false;
            }
            const data: unknown = await res.json();
            if (!applySnapshot(data, 'scan')) {
                showToast('error', t('services.scanFailed'));
                markStateUnverified();
                return false;
            }
            return true;
        } catch {
            showToast('error', t('services.scanFailed'));
            markStateUnverified();
            return false;
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
        if (stateUnverified) {
            showToast('error', t('services.stateUnverifiedHint'));
            return;
        }
        setInstallTarget(null);
        await startInstall({
            serviceId: service.id,
            name: service.name,
            ...(pkg ? { package: pkg } : {}),
        });
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
    const [retryCleanup, setRetryCleanup] = useState(false);
    const [cleanupAttempt, setCleanupAttempt] = useState<'initial' | 'retry' | null>(null);
    const openUninstallDialog = (service: ManagedService) => {
        setUninstallTarget(service);
        setUninstallError(null);
        setRetryCleanup(false);
        setCleanupAttempt(null);
    };
    const openWebmailCleanupDialog = (service: ManagedService) => {
        setUninstallTarget(service);
        setUninstallError(null);
        setRetryCleanup(true);
        setCleanupAttempt(null);
    };
    const closeUninstallDialog = () => {
        setUninstallTarget(null);
        setUninstallError(null);
        setRetryCleanup(false);
        setCleanupAttempt(null);
    };
    const doUninstall = async (service: ManagedService) => {
        if (stateUnverified) {
            showToast('error', t('services.stateUnverifiedHint'));
            return;
        }
        // Capture whether the operator clicked the post-error Retry action before
        // clearing that error. The busy label must describe this request for its
        // entire lifetime, including the verified-cache reload below.
        const nextCleanupAttempt = retryCleanup
            ? (uninstallError ? 'retry' : 'initial')
            : null;
        setCleanupAttempt(nextCleanupAttempt);
        setBusy(service.id);
        setUninstallError(null);
        try {
            const res = await fetch('/api/v1/service/uninstall', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ service_id: service.id }),
            });
            if (!res.ok) {
                const error = await readApiError(res);
                if (error.code === 'SERVICE_STATE_REFRESH_FAILED') {
                    closeUninstallDialog();
                    handleStateRefreshFailure(error);
                    return;
                }
                // Package removal has already succeeded, but the idempotent webmail
                // cleanup still needs another attempt. Keep this stale target only as
                // the retry handle while the verified cache updates the service row.
                if (error.code === 'WEBMAIL_UNINSTALL_PARTIAL') {
                    // An ordinary uninstall can enter cleanup mode here. It is still
                    // the initial cleanup attempt, never a user-requested retry.
                    if (nextCleanupAttempt === null) setCleanupAttempt('initial');
                    const verified = await loadServices({ markUnverifiedOnFailure: false });
                    if (!verified) {
                        // Closing also resets the stale retry error/label. Do this
                        // before the fail-closed Rescan lock disables the fieldset,
                        // otherwise the operator can be trapped in this modal.
                        closeUninstallDialog();
                        markStateUnverified();
                        return;
                    }
                    // The partial-success copy directs the operator back to this
                    // dialog, so expose it only after a verified reload proves the
                    // dialog can safely stay open and its Retry action is reachable.
                    setUninstallError(error);
                    setRetryCleanup(true);
                    showToast('error', apiErrorText(error, t, 'services.actionFailed'));
                    return;
                }
                // The removal and fresh scan succeeded; reload that verified cache while
                // keeping the firewall warning visible instead of retaining the old row.
                // Kaldırma ve taze tarama başarılıdır; eski satırı tutmak yerine güvenlik
                // duvarı uyarısını gösterirken doğrulanmış cache'i yeniden yükle.
                if (
                    error.code === 'FIREWALL_SYNC_FAILED' ||
                    error.code === 'MAIL_FILTER_SYNC_FAILED' ||
                    error.code === 'SERVICE_UNINSTALL_PARTIAL'
                ) {
                    closeUninstallDialog();
                    showToast('error', apiErrorText(error, t, 'services.actionFailed'));
                    if (!(await loadServices())) markStateUnverified();
                    return;
                }
                setUninstallError(error);
                return;
            }
            closeUninstallDialog();
            showToast(
                'success',
                nextCleanupAttempt !== null
                    ? t('services.webmailCleanupCompleted')
                    : t('services.uninstalled', { name: service.name }),
            );
            if (!(await loadServices())) markStateUnverified();
        } catch {
            // A transport/read exception can happen after the server has already
            // removed Roundcube. Reset the stale retry handle before the
            // fail-closed lock disables this fieldset, otherwise the dialog's
            // Cancel and Retry controls become unreachable.
            closeUninstallDialog();
            markStateUnverified();
            showToast('error', t('services.actionFailed'));
        } finally {
            setBusy(null);
            setCleanupAttempt(null);
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
        if (stateUnverified) {
            showToast('error', t('services.stateUnverifiedHint'));
            return;
        }
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
                const error = await readApiError(res);
                if (error.code === 'SERVICE_STATE_REFRESH_FAILED') {
                    handleStateRefreshFailure(error);
                    return;
                }
                showToast('error', apiErrorText(error, t, 'services.actionFailed'));
                return;
            }
            if (!(await loadServices())) markStateUnverified();
        } catch {
            markStateUnverified();
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
        if (stateUnverified) {
            showToast('error', t('services.stateUnverifiedHint'));
            return;
        }
        setBusy(`${serviceId}:${unit}`);
        try {
            const res = await fetch('/api/v1/service/action', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name: unit, action }),
            });
            if (!res.ok) {
                const error = await readApiError(res);
                if (error.code === 'SERVICE_STATE_REFRESH_FAILED') {
                    handleStateRefreshFailure(error);
                    return;
                }
                showToast('error', apiErrorText(error, t, 'services.actionFailed'));
                return;
            }
            if (!(await loadServices())) markStateUnverified();
        } catch {
            markStateUnverified();
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
    const pageControlsBusy = scanning || busy !== null;
    const mutationControlsDisabled = pageControlsBusy || stateUnverified;
    const openProfilePlan = (profile: ManagedMailProfile) => {
        if (stateUnverified || !profile.available) return;
        setProfileTarget(profile);
    };
    const installProfile = async () => {
        const profile = profileTarget;
        if (!profile || stateUnverified || !profile.available) return;
        setProfileTarget(null);
        await startInstall({
            serviceId: profile.id,
            name: profile.name,
            operationKind: 'mail_profile_install',
        });
    };

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
                    disabled={pageControlsBusy}
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

            {stateUnverified && (
                <div role="alert" className="mb-4 flex items-start gap-3 rounded-xl border border-warning/40 bg-warning/5 p-4">
                    <ShieldOff className="mt-0.5 h-5 w-5 shrink-0 text-warning" />
                    <div>
                        <div className="text-sm font-semibold text-fg">{t('services.stateUnverifiedTitle')}</div>
                        <p className="text-sm text-fg-muted">{t('services.stateUnverifiedHint')}</p>
                    </div>
                </div>
            )}

            {!loading && profiles.length > 0 && (
                <MailProfileCards
                    profiles={profiles}
                    services={services}
                    disabled={mutationControlsDisabled}
                    onInstall={openProfilePlan}
                />
            )}

            {loading ? (
                <div className="flex flex-col items-center justify-center gap-3 py-16 text-sm text-fg-muted">
                    <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary" />
                    {scanning && <span>{t('services.autoScanning')}</span>}
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
                    {/* A component whose category is not in categoryOrder used to
                        render NOWHERE: the map only walks known categories and
                        there was no leftover bucket — so a newly added component
                        installed fine and was invisible on this page, while still
                        counting in the "Catalog (n)" button. Adding the unknown
                        categories to the walk means a new component always shows
                        up somewhere, which is the whole promise of this page.
                        Kategorisi categoryOrder'da olmayan bir bileşen HİÇBİR
                        YERDE çizilmiyordu: map yalnız bilinen kategorileri
                        dolaşıyor ve artık kova yoktu — yani yeni eklenen bileşen
                        sorunsuz kuruluyor ama bu sayfada görünmüyordu, üstelik
                        "Katalog (n)" sayacında görünmeye devam ediyordu.
                        Bilinmeyen kategorileri de dolaşıma katmak, yeni bir
                        bileşenin her zaman bir yerde görünmesi demektir; bu
                        sayfanın bütün vaadi de budur. */}
                    {[...categoryOrder, ...unknownCategories(filtered)].map(({ id: cat, labelKey, icon: CatIcon, tint }) => {
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
                                                    <fieldset
                                                        disabled={mutationControlsDisabled}
                                                        aria-busy={pageControlsBusy}
                                                        className="ml-auto flex min-w-0 items-center justify-end gap-1 border-0 p-0"
                                                    >
                                                    {!s.is_installed ? (
                                                    <>
                                                    {/* An interrupted uninstall can leave Roundcube application
                                                        data after the package scan says it is not installed. Keep
                                                        cleanup reachable across reloads, independently of install
                                                        and repair availability. */}
                                                    {s.conflict_with ? (
                                                            <span
                                                                title={t('services.conflictHint', { name: s.conflict_with })}
                                                                className="inline-flex items-center gap-1.5 rounded-lg border border-border bg-surface-2 px-3 py-1.5 text-xs font-medium text-fg-subtle"
                                                            >
                                                                {t('services.conflictWith', { name: s.conflict_with })}
                                                            </span>
                                                        ) : s.not_offered_kind === 'integration' && s.id === 'vsftpd' ? (
                                                            <button
                                                                type="button"
                                                                onClick={() => navigate('/domains')}
                                                                title={t('services.useBuiltInSFTPHint')}
                                                                className="inline-flex items-center gap-1.5 rounded-lg border border-primary/30 bg-primary/10 px-3 py-1.5 text-xs font-semibold text-primary transition-colors hover:bg-primary/15"
                                                            >
                                                                {t('services.useBuiltInSFTP')}
                                                                <ChevronRight className="h-3.5 w-3.5" />
                                                            </button>
                                                        ) : s.not_offered ? (
                                                            /* Honest instead of a dead Install button that
                                                               fails late in the agent (spamassassin on Arch,
                                                               netdata on Debian). The full per-distro list
                                                               lives in docs/DISTRO-SUPPORT.
                                                               Agent'ta geç patlayan ölü bir Kur düğmesi yerine
                                                               dürüstlük (Arch'ta spamassassin, Debian'da
                                                               netdata). Dağıtım başına tam liste
                                                               docs/DISTRO-SUPPORT içinde. */
                                                            <span
                                                                title={s.not_offered_kind === 'integration'
                                                                    ? t('services.integrationPendingHint')
                                                                    : (s.not_offered_reason || t('services.notOfferedHint'))}
                                                                className="inline-flex items-center gap-1.5 rounded-lg border border-border bg-surface-2 px-3 py-1.5 text-xs font-medium text-fg-subtle"
                                                            >
                                                                {s.not_offered_kind === 'integration'
                                                                    ? t('services.integrationPending')
                                                                    : t('services.notOffered')}
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
                                                    }
                                                    {s.id === 'roundcube' && (
                                                    <button
                                                        type={'button'}
                                                        onClick={() => openWebmailCleanupDialog(s)}
                                                        disabled={mutationControlsDisabled || busy === s.id}
                                                        title={t('services.cleanupWebmailHint')}
                                                        className={'inline-flex items-center gap-1.5 rounded-lg border border-danger/30 bg-danger/5 px-2.5 py-1.5 text-xs font-semibold text-danger transition-colors hover:bg-danger/10 disabled:opacity-50'}
                                                    >
                                                        <Trash2 className={'h-3.5 w-3.5'} />
                                                        {t('services.cleanupWebmail')}
                                                    </button>
                                                    )}
                                                    </>
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
                                                    ) : s.not_offered ? (
                                                    /* A legacy installation whose automatic integration is
                                                       deliberately closed must remain manageable. Starting it
                                                       is the only honest action because Repair is not supported. */
                                                    <ActionIcon
                                                        title={t('services.start')}
                                                        onClick={() => handleAction(s, 'start')}
                                                        disabled={busy === s.id}
                                                        tone="success"
                                                    >
                                                        <Play className="h-4 w-4" fill="currentColor" />
                                                    </ActionIcon>
                                                    ) : (
                                                    /* A stopped supported service may be a partial install.
                                                       Re-running the durable idempotent install path repairs
                                                       packages, configuration, helpers and readiness under the
                                                       same full-page operation lock. */
                                                    <ActionIcon
                                                        title={s.repair_available ? t('services.repair') : t('services.repairUnavailable')}
                                                        onClick={() => startInstall({
                                                            serviceId: s.id,
                                                            name: s.name,
                                                            ...(s.repair_package ? { package: s.repair_package } : {}),
                                                        })}
                                                        disabled={busy === s.id || !s.repair_available}
                                                        tone="warning"
                                                    >
                                                        <RotateCw className="h-4 w-4" />
                                                    </ActionIcon>
                                                    ))}
                                                    {/* Roundcube repair reuses the durable install operation. */}
                                                    {s.kind === 'tool' && s.id === 'roundcube' && (
                                                    <button
                                                        type={'button'}
                                                        onClick={() => startInstall({
                                                            serviceId: s.id,
                                                            name: s.name,
                                                            ...(s.repair_package ? { package: s.repair_package } : {}),
                                                        })}
                                                        disabled={mutationControlsDisabled || busy === s.id || !s.repair_available}
                                                        title={s.repair_available ? t('services.repairWebmail') : t('services.repairUnavailable')}
                                                        className={'inline-flex items-center gap-1.5 rounded-lg border border-warning/30 bg-warning/10 px-2.5 py-1.5 text-xs font-semibold text-warning disabled:opacity-50'}
                                                    >
                                                        <RotateCw className={'h-3.5 w-3.5'} />
                                                        {t('services.repairWebmail')}
                                                    </button>
                                                    )}
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
                                                        onClick={() => openUninstallDialog(s)}
                                                        title={t('services.uninstall')}
                                                        className="inline-flex items-center rounded-lg border border-border-strong bg-surface p-1.5 text-fg-muted transition-colors hover:bg-danger/10 hover:text-danger"
                                                    >
                                                        <Trash2 className="h-3.5 w-3.5" />
                                                    </button>
                                                    )}
                                                    </>
                                                    )}
                                                    </fieldset>
                                                    {s.kind === 'runtime' && openDrawers.has(s.id) && (
                                                        <VersionDrawer
                                                            service={s}
                                                            busy={busy}
                                                            mutationDisabled={mutationControlsDisabled}
                                                            onInstanceAction={handleInstanceAction}
                                                            onVersionInstalled={scan}
                                                            onStateUnverified={handleStateRefreshFailure}
                                                            onInstallPackage={(pkg, version) => {
                                                                if (stateUnverified) return Promise.resolve(false);
                                                                return startInstall({
                                                                    serviceId: s.id,
                                                                    package: pkg,
                                                                    name: `${s.name} ${version}`,
                                                                });
                                                            }}
                                                            onInstallNode={(version) => {
                                                                if (stateUnverified) return Promise.resolve(false);
                                                                return startInstall({
                                                                    serviceId: s.id,
                                                                    version,
                                                                    name: `Node ${version}`,
                                                                });
                                                            }}
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

            <fieldset
                disabled={mutationControlsDisabled}
                aria-busy={pageControlsBusy}
                className="min-w-0 border-0 p-0"
            >
                {installTarget && (
                    <InstallServiceDialog
                        service={installTarget}
                        busy={busy === installTarget.id}
                        onCancel={() => setInstallTarget(null)}
                        onConfirm={(pkg) => doInstall(installTarget, pkg)}
                    />
                )}
                {profileTarget && (
                    <MailProfileInstallDialog
                        profile={profileTarget}
                        services={services}
                        onCancel={() => setProfileTarget(null)}
                        onConfirm={() => void installProfile()}
                    />
                )}
                {uninstallTarget && (
                    <UninstallServiceDialog
                        service={uninstallTarget}
                        busy={busy === uninstallTarget.id}
                        error={uninstallError}
                        retryCleanup={retryCleanup}
                        cleanupAttempt={cleanupAttempt}
                        onCancel={closeUninstallDialog}
                        onConfirm={() => doUninstall(uninstallTarget)}
                    />
                )}
            </fieldset>
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
function MailProfileCards({ profiles, services, disabled, onInstall }: {
    profiles: ManagedMailProfile[];
    services: ManagedService[];
    disabled: boolean;
    onInstall: (profile: ManagedMailProfile) => void;
}) {
    const { t } = useI18n();
    const actionLabel = (profile: ManagedMailProfile) => {
        if (profile.status === 'available') return t('services.mailProfiles.install');
        if (profile.status === 'partial') return t('services.mailProfiles.continue');
        if (profile.status === 'complete') return t('services.mailProfiles.repair');
        return t('services.mailProfiles.unavailable');
    };
    const serviceName = (id: string) => services.find((service) => service.id === id)?.name ?? id;
    return (
        <section id='mail-stacks' tabIndex={-1} aria-labelledby='mail-profile-heading' className='mb-6 scroll-mt-24 focus:outline-none'>
            <div className='mb-3 flex items-start gap-3'>
                <span className='flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-amber-500/10 text-amber-600 dark:text-amber-400'>
                    <Layers className='h-5 w-5' />
                </span>
                <div>
                    <h2 id='mail-profile-heading' className='text-lg font-semibold text-fg'>
                        {t('services.mailProfiles.title')}
                    </h2>
                    <p className='text-sm text-fg-muted'>{t('services.mailProfiles.subtitle')}</p>
                </div>
            </div>
            <div className='grid gap-3 lg:grid-cols-3'>
                {profiles.map((profile) => {
                    const actionable = profile.available && (
                        profile.status === 'available'
                        || profile.status === 'partial'
                        || profile.status === 'complete'
                    );
                    const detail = profile.status === 'complete' && profile.warning
                        ? t('services.mailProfiles.profileComponentsNeedRepair')
                        : profile.status === 'blocked'
                            ? profile.blocked_reason
                            : profile.warning;
                    const ActionIcon = profile.status === 'available' ? DownloadCloud : RotateCw;
                    return (
                        <article key={profile.id} className='flex min-w-0 flex-col rounded-xl border border-border bg-surface p-4 shadow-card'>
                            <div className='flex items-start gap-3'>
                                <span className='flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary'>
                                    {profile.id === 'protected-mail'
                                        ? <ShieldCheck className='h-5 w-5' />
                                        : <Mail className='h-5 w-5' />}
                                </span>
                                <div className='min-w-0'>
                                    <h3 className='font-semibold text-fg'>{profile.name}</h3>
                                    <p className='mt-1 text-xs leading-5 text-fg-muted'>{profile.description}</p>
                                </div>
                            </div>
                            <div className='mt-4 flex flex-wrap gap-1.5' aria-label={t('services.mailProfiles.includes')}>
                                {profile.services.map((id) => (
                                    <span key={id} className='rounded-md bg-surface-2 px-2 py-1 text-[11px] font-medium text-fg-muted'>
                                        {serviceName(id)}
                                    </span>
                                ))}
                            </div>
                            {detail && (
                                <p className={`mt-3 text-xs leading-5 ${profile.status === 'blocked' ? 'text-danger' : 'text-warning'}`}>
                                    {detail}
                                </p>
                            )}
                            <div className='mt-auto flex items-center justify-between gap-3 pt-4'>
                                <span className='inline-flex items-center gap-1.5 text-xs font-medium text-fg-muted'>
                                    <StatusDot ok={profile.status === 'complete'} />
                                    {t(`services.mailProfiles.status.${profile.status}` as Parameters<typeof t>[0])}
                                </span>
                                <button
                                    type='button'
                                    onClick={() => onInstall(profile)}
                                    disabled={disabled || !actionable}
                                    title={detail || actionLabel(profile)}
                                    className='inline-flex items-center gap-1.5 rounded-lg bg-primary px-3 py-1.5 text-xs font-semibold text-primary-fg hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-50'
                                >
                                    <ActionIcon className='h-3.5 w-3.5' />
                                    {actionLabel(profile)}
                                </button>
                            </div>
                        </article>
                    );
                })}
            </div>
        </section>
    );
}

function MailProfileInstallDialog({
    profile,
    services,
    onCancel,
    onConfirm,
}: {
    profile: ManagedMailProfile;
    services: ManagedService[];
    onCancel: () => void;
    onConfirm: () => void;
}) {
    const { t } = useI18n();
    const [acknowledged, setAcknowledged] = useState(false);
    const [candidateVersions, setCandidateVersions] = useState<Record<string, string>>({});
    const [candidatesLoading, setCandidatesLoading] = useState(true);
    const acknowledgementRef = useRef<HTMLInputElement>(null);
    const components = profile.services
        .map((id) => services.find((service) => service.id === id))
        .filter((service): service is ManagedService => Boolean(service));
    const mode = profile.status === 'available'
        ? 'install'
        : profile.status === 'partial'
            ? 'continue'
            : 'repair';
    const ports = Array.from(new Set(components.flatMap((service) => service.ports ?? []))).sort();
    const restarted = components
        .filter((service) => service.kind === 'service')
        .map((service) => service.name);

    useEffect(() => {
        acknowledgementRef.current?.focus();
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.key === 'Escape') onCancel();
        };
        window.addEventListener('keydown', onKeyDown);
        return () => window.removeEventListener('keydown', onKeyDown);
    }, [onCancel]);

    useEffect(() => {
        let cancelled = false;
        const packageComponents = components.filter((service) =>
            !service.is_installed && (service.packages?.length ?? 0) > 0);
        if (packageComponents.length === 0) {
            setCandidateVersions({});
            setCandidatesLoading(false);
            return () => {
                cancelled = true;
            };
        }
        setCandidatesLoading(true);
        void Promise.all(packageComponents.map(async (service) => {
            try {
                const response = await fetch(`/api/v1/service/candidate?id=${encodeURIComponent(service.id)}`);
                if (!response.ok) return [service.id, ''] as const;
                const value: unknown = await response.json();
                if (!value || typeof value !== 'object') return [service.id, ''] as const;
                const version = (value as Record<string, unknown>).version;
                return [service.id, typeof version === 'string' ? version : ''] as const;
            } catch {
                return [service.id, ''] as const;
            }
        })).then((entries) => {
            if (!cancelled) setCandidateVersions(Object.fromEntries(entries));
        }).finally(() => {
            if (!cancelled) setCandidatesLoading(false);
        });
        return () => {
            cancelled = true;
        };
        // The dialog is bound to one immutable verified catalogue snapshot.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [profile.id]);

    const componentVersion = (service: ManagedService) => {
        if (service.is_installed) {
            return service.versions.length > 0
                ? service.versions.join(', ')
                : t('services.mailProfiles.plan.installed');
        }
        if (service.id === 'roundcube') return '1.6.15';
        if (candidatesLoading) return '…';
        return candidateVersions[service.id] || t('services.mailProfiles.plan.repositoryCandidate');
    };
    const componentPackages = (service: ManagedService) => {
        if ((service.packages?.length ?? 0) > 0) return service.packages!.join(', ');
        if (service.id === 'roundcube') return 'Roundcube 1.6.15';
        return service.id;
    };

    return (
        <div
            className='fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4'
            onClick={onCancel}
        >
            <div
                role='dialog'
                aria-modal='true'
                aria-labelledby='mail-profile-confirm-title'
                aria-describedby='mail-profile-confirm-description'
                className='max-h-[90vh] w-full max-w-2xl overflow-y-auto rounded-2xl border border-border bg-surface p-6 shadow-xl'
                onClick={(event) => event.stopPropagation()}
            >
                <div className='mb-5'>
                    <h3 id='mail-profile-confirm-title' className='text-lg font-semibold text-fg'>
                        {t(`services.mailProfiles.plan.title.${mode}` as Parameters<typeof t>[0], { name: profile.name })}
                    </h3>
                    <p id='mail-profile-confirm-description' className='mt-1 text-sm text-fg-muted'>
                        {t('services.mailProfiles.plan.description')}
                    </p>
                </div>

                <div className='mb-4 overflow-hidden rounded-xl border border-border'>
                    <div className='grid grid-cols-[minmax(0,1fr)_auto] gap-3 bg-surface-2 px-4 py-2 text-xs font-semibold text-fg-subtle'>
                        <span>{t('services.mailProfiles.plan.component')}</span>
                        <span>{t('services.mailProfiles.plan.version')}</span>
                    </div>
                    {components.map((service) => (
                        <div key={service.id} className='grid grid-cols-[minmax(0,1fr)_auto] gap-3 border-t border-border px-4 py-3'>
                            <div className='min-w-0'>
                                <p className='text-sm font-medium text-fg'>{service.name}</p>
                                <p className='break-words font-mono text-xs text-fg-subtle'>{componentPackages(service)}</p>
                            </div>
                            <span className='max-w-52 text-right font-mono text-xs font-medium text-fg'>
                                {componentVersion(service)}
                            </span>
                        </div>
                    ))}
                </div>

                <div className='mb-4 grid gap-3 sm:grid-cols-2'>
                    <div className='rounded-lg border border-border bg-surface-2/50 p-3'>
                        <p className='text-xs font-semibold text-fg'>{t('services.mailProfiles.plan.serviceImpact')}</p>
                        <p className='mt-1 text-xs leading-5 text-fg-muted'>
                            {restarted.length > 0
                                ? t('services.mailProfiles.plan.restarts', { services: restarted.join(', ') })
                                : t('services.mailProfiles.plan.noRestarts')}
                        </p>
                    </div>
                    <div className='rounded-lg border border-border bg-surface-2/50 p-3'>
                        <p className='text-xs font-semibold text-fg'>{t('services.mailProfiles.plan.firewallImpact')}</p>
                        <p className='mt-1 text-xs leading-5 text-fg-muted'>
                            {ports.length > 0
                                ? t('services.mailProfiles.plan.ports', { ports: ports.join(', ') })
                                : t('services.mailProfiles.plan.noPorts')}
                        </p>
                    </div>
                </div>

                <div className='mb-4 rounded-lg border border-warning/40 bg-warning/5 p-3 text-xs leading-5 text-fg-muted'>
                    <p>{t('services.mailProfiles.plan.tls')}</p>
                    <p className='mt-1'>{t('services.mailProfiles.plan.partialProgress')}</p>
                </div>

                <label className='mb-5 flex cursor-pointer items-start gap-3 rounded-lg border border-border p-3'>
                    <input
                        ref={acknowledgementRef}
                        type='checkbox'
                        checked={acknowledged}
                        onChange={(event) => setAcknowledged(event.target.checked)}
                        className='mt-0.5 h-4 w-4 rounded border-border-strong text-primary'
                    />
                    <span className='text-sm leading-5 text-fg'>
                        {t('services.mailProfiles.plan.acknowledgement')}
                    </span>
                </label>

                <div className='flex justify-end gap-2'>
                    <Button variant='secondary' onClick={onCancel}>
                        {t('common.cancel')}
                    </Button>
                    <Button
                        variant='primary'
                        icon={mode === 'install' ? DownloadCloud : RotateCw}
                        disabled={!acknowledged}
                        onClick={onConfirm}
                    >
                        {t(`services.mailProfiles.plan.confirm.${mode}` as Parameters<typeof t>[0], { name: profile.name })}
                    </Button>
                </div>
            </div>
        </div>
    );
}

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
    const repoError = repo?.error_code ? apiErrorText({ message: '', code: repo.error_code }, t, 'services.actionFailed') : '';

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
            if (!res.ok) {
                showToast('error', apiErrorText(await readApiError(res), t, 'services.actionFailed'));
                return;
            }
            const data: RepoInfo = await res.json();
            setRepo(data.available ? data : null);
            if (action === 'disable') setSelectedPkg('');
        } catch {
            showToast('error', t('services.actionFailed'));
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
        <div
            className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
            onClick={() => {
                if (!busy && !repoBusy) onCancel();
            }}
        >
            <div
                className="w-full max-w-md rounded-2xl border border-border bg-surface p-6 shadow-xl"
                onClick={(e) => e.stopPropagation()}
                aria-busy={busy || repoBusy}
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
                                {repoError && <p className="mb-2 text-xs text-danger">{repoError}</p>}
                                <Button
                                    variant="secondary"
                                    onClick={() => toggleRepo('enable')}
                                    disabled={repoBusy || busy || Boolean(repo.error_code && !repo.repairable)}
                                    icon={Layers}
                                >
                                    {repoBusy ? t('services.repo.enabling') : t('services.repo.enable')}
                                </Button>
                            </>
                        ) : (
                            <>
                                {repoError && <p className="mb-2 text-xs text-danger">{repoError}</p>}
                                <p className="mb-1.5 text-xs font-medium text-fg-subtle">{t('services.repo.chooseVersion')}</p>
                                <div className="flex flex-wrap gap-1.5">
                                    <button
                                        onClick={() => setSelectedPkg('')}
                                        disabled={repoBusy || busy}
                                        className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors ${
                                            selectedPkg === ''
                                                ? 'bg-primary text-primary-fg'
                                                : 'bg-surface-3 text-fg-muted hover:text-fg'
                                        } disabled:cursor-not-allowed disabled:opacity-50`}
                                    >
                                        {t('services.repo.distroDefault')}
                                        {version ? ` (${version})` : ''}
                                    </button>
                                    {(repo.packages || []).map((pkg) => (
                                        <button
                                            key={pkg}
                                            onClick={() => setSelectedPkg(pkg)}
                                            disabled={repoBusy || busy}
                                            className={`rounded-md px-2.5 py-1 font-mono text-xs font-medium transition-colors ${
                                                selectedPkg === pkg
                                                    ? 'bg-primary text-primary-fg'
                                                    : 'bg-surface-3 text-fg-muted hover:text-fg'
                                            } disabled:cursor-not-allowed disabled:opacity-50`}
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
                    <Button variant="secondary" onClick={onCancel} disabled={busy || repoBusy}>{t('common.cancel')}</Button>
                    <Button
                        variant="primary"
                        onClick={() => onConfirm(selectedPkg || undefined)}
                        disabled={busy || repoBusy || Boolean(repo?.required && (repo.error_code || !repo.enabled))}
                        icon={DownloadCloud}
                    >
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
    mutationDisabled,
    onInstanceAction,
    onVersionInstalled,
    onStateUnverified,
    onInstallPackage,
    onInstallNode,
}: {
    service: ManagedService;
    busy: string | null;
    mutationDisabled: boolean;
    onInstanceAction: (serviceId: string, unit: string, action: 'start' | 'stop' | 'restart') => void;
    onVersionInstalled: () => Promise<boolean>;
    onStateUnverified: (error: ApiError) => void;
    onInstallPackage: (pkg: string, version: string) => Promise<boolean>;
    onInstallNode: (version: string) => Promise<boolean>;
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
    const repoError = repo?.error_code ? apiErrorText({ message: '', code: repo.error_code }, t, 'services.actionFailed') : '';
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
            if (!res.ok) {
                showToast('error', apiErrorText(await readApiError(res), t, 'services.actionFailed'));
                return;
            }
            const d: RepoInfo = await res.json();
            setRepo(d);
        } catch {
            showToast('error', t('services.actionFailed'));
        } finally {
            setRepoBusy(false);
        }
    };

    const installPackage = async (pkg: string, version: string) => {
        if (installing) return;
        setInstalling(pkg);
        try {
            await onInstallPackage(pkg, version);
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
            await onInstallNode(version);
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
                const error = await readApiError(res);
                if (error.code === 'SERVICE_STATE_REFRESH_FAILED') {
                    setRemoveTarget(null);
                    onStateUnverified(error);
                    return;
                }
                // These responses carry a verified post-mutation snapshot.
                // Warn, close the stale modal and reload instead of pretending
                // the original row is still authoritative.
                // Bu yanıtlar değişiklik sonrası doğrulanmış snapshot taşır.
                // Eski satırı yetkili sanmak yerine uyar, modalı kapat ve yeniden yükle.
                if (
                    error.code === 'FIREWALL_SYNC_FAILED' ||
                    error.code === 'MAIL_FILTER_SYNC_FAILED' ||
                    error.code === 'SERVICE_UNINSTALL_PARTIAL'
                ) {
                    setRemoveTarget(null);
                    showToast('error', apiErrorText(error, t, 'services.actionFailed'));
                    await onVersionInstalled();
                    return;
                }
                setRemoveError(error);
                return;
            }
            setRemoveTarget(null);
            showToast('success', t('services.versionRemoved', { version: inst.version }));
            // The endpoint mutation is not enough to trust the old row. Wait
            // for the parent scan and keep its safety lock when verification
            // fails.
            // Uç nokta mutasyonu eski satıra güvenmek için yeterli değildir.
            // Üst bileşenin taramasını bekle; doğrulama başarısızsa güvenlik
            // kilidini koru.
            if (!(await onVersionInstalled())) return;
        } catch {
            onStateUnverified({ message: t('services.actionFailed') });
        } finally {
            setRemoving(false);
        }
    };
    const canRemove = (inst: ServiceInstance) =>
        inst.managed && (inst.unit ? service.kind === 'runtime' : service.id === 'node');

    return (
        <fieldset
            disabled={mutationDisabled}
            aria-busy={mutationDisabled}
            className="mt-1 w-full rounded-lg border border-border bg-surface-2/40 px-4 py-3"
        >
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
                            <span className={`text-xs ${repoError ? 'text-danger' : 'text-fg-subtle'}`}>
                                {repoError || t('services.repo.note')}
                            </span>
                            <Button variant="secondary" onClick={enableRepo} disabled={repoBusy || Boolean(repo.error_code && !repo.repairable)}>
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
        </fieldset>
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
    retryCleanup,
    cleanupAttempt,
    onCancel,
    onConfirm,
}: {
    service: ManagedService;
    busy: boolean;
    error: ApiError | null;
    retryCleanup: boolean;
    cleanupAttempt: 'initial' | 'retry' | null;
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
                        <h3 className="text-lg font-semibold text-fg">
                            {retryCleanup
                                ? t('services.cleanupWebmailTitle')
                                : t('services.uninstallTitle', { name: service.name })}
                        </h3>
                        <p className="text-sm text-fg-muted">{service.description}</p>
                    </div>
                </div>
                <div className="mb-4 rounded-lg border border-danger/30 bg-danger/5 p-3 text-sm text-fg-muted">
                    <p className={retryCleanup ? '' : 'mb-2'}>
                        {t(retryCleanup ? 'services.cleanupWebmailWarn' : 'services.uninstallWarn')}
                    </p>
                    {!retryCleanup && (
                        <div className="flex flex-wrap gap-1.5">
                            {(service.packages && service.packages.length > 0 ? service.packages : [service.id]).map((pkg) => (
                                <span key={pkg} className="rounded bg-surface-2 px-2 py-0.5 font-mono text-xs text-fg">{pkg}</span>
                            ))}
                        </div>
                    )}
                </div>
                {/* A refusal shows its evidence HERE, where the decision is
                    made — who blocks, line by line (B3d).
                    Ret, kanıtını kararın verildiği yerde gösterir — kimin
                    engellediği, satır satır (B3d). */}
                <ErrorBanner error={error} className="mb-4" />
                <div className="flex justify-end gap-2">
                    <Button variant="secondary" onClick={onCancel} disabled={busy}>{t('common.cancel')}</Button>
                    <Button variant="danger" onClick={onConfirm} disabled={busy} icon={Trash2}>
                        {busy
                            ? t(retryCleanup
                                ? (cleanupAttempt === 'retry' ? 'services.retryingWebmailCleanup' : 'services.cleaningWebmail')
                                : 'services.uninstalling')
                            : t(retryCleanup
                                ? (error ? 'services.retryWebmailCleanup' : 'services.cleanupWebmail')
                                : 'services.uninstall')}
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
    type FirewallViewStatus = {
        enabled: boolean;
        engine_available?: boolean;
        tcp_ports?: number[];
        udp_ports?: number[];
        ssh_ports?: number[];
        persistence_state?: string;
        persistence_error?: string;
        snapshot_version?: number;
    };
    const [st, setSt] = useState<FirewallViewStatus | null>(null);
    const [statusError, setStatusError] = useState<string | null>(null);
    const [busy, setBusy] = useState(false);

    // A failed status read is neither "on" nor "off". Preserve that distinction
    // so an agent/permission/network failure can never be mistaken for an open firewall.
    // Başarısız durum okuması ne "açık" ne de "kapalı"dır. Agent/yetki/ağ
    // hatasının açık bir güvenlik duvarıyla karıştırılmaması için bu ayrımı koru.
    const load = async () => {
        try {
            const r = await fetch('/api/v1/firewall');
            let body: unknown;
            try {
                body = await r.json();
            } catch {
                throw new Error(
                    r.ok
                        ? t('firewall.invalidResponse')
                        : `${t('firewall.loadFailed')} (HTTP ${r.status})`,
                );
            }

            const apiError =
                typeof body === 'object' &&
                body !== null &&
                'error' in body &&
                typeof body.error === 'string' &&
                body.error.trim()
                    ? body.error
                    : '';
            if (!r.ok) {
                throw new Error(apiError || `${t('firewall.loadFailed')} (HTTP ${r.status})`);
            }
            if (apiError) {
                throw new Error(apiError);
            }
            if (
                typeof body !== 'object' ||
                body === null ||
                !('enabled' in body) ||
                typeof body.enabled !== 'boolean'
            ) {
                throw new Error(t('firewall.invalidResponse'));
            }

            setSt(body as FirewallViewStatus);
            setStatusError(null);
        } catch (e) {
            setSt(null);
            setStatusError(e instanceof Error && e.message ? e.message : t('firewall.loadFailed'));
        }
    };
    useEffect(() => {
        void load();
    }, []);

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

    const saveForReboot = async () => {
        setBusy(true);
        try {
            const r = await fetch('/api/v1/firewall', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ action: 'save_for_reboot' }),
            });
            const d = await r.json();
            if (!r.ok || d.error) throw new Error(d.error || t('firewall.persistenceSaveFailed'));
            if (d.persistence_state !== 'ready') throw new Error(d.persistence_error || t('firewall.persistenceSaveFailed'));
            setSt(d);
            showToast('success', t('firewall.persistenceSaved'));
        } catch (e) {
            showToast('error', e instanceof Error && e.message ? e.message : t('firewall.persistenceSaveFailed'));
        } finally {
            setBusy(false);
        }
    };

    if (statusError) {
        return (
            <section role="alert" className="mb-4 rounded-xl border border-danger/40 bg-danger/5 p-4">
                <div className="flex flex-wrap items-center gap-3">
                    <ShieldOff className="h-5 w-5 shrink-0 text-danger" />
                    <div className="min-w-0 flex-1">
                        <div className="text-sm font-semibold text-fg">
                            {t('firewall.title')}{' '}
                            <span className="text-danger">{t('firewall.unknown')}</span>
                        </div>
                        <p className="text-xs text-fg-muted">{t('firewall.unknownHint')}</p>
                        <p className="mt-1 break-words text-xs text-danger">{statusError}</p>
                    </div>
                    <button
                        type="button"
                        disabled
                        className="cursor-not-allowed rounded-lg border border-border-strong bg-surface px-3 py-1.5 text-xs font-semibold text-fg opacity-50"
                    >
                        {t('firewall.controlUnavailable')}
                    </button>
                </div>
            </section>
        );
    }

    if (!st) return null;
    const openTcp = st.tcp_ports || [];
    const openUdp = st.udp_ports || [];
    const persistenceWarning = st.persistence_state === 'stale'
        ? t('firewall.persistence.stale')
        : st.persistence_state === 'invalid'
            ? t('firewall.persistence.invalid')
            : st.persistence_state === 'unverified'
                ? t('firewall.persistence.unverified')
                : '';

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
                    {st.enabled && st.persistence_state === 'missing' && (
                        <p className="mt-1 text-xs text-warning">{t('firewall.persistenceMissing')}</p>
                    )}
                    {st.persistence_state && ['stale', 'invalid', 'unverified'].includes(st.persistence_state) && (
                        <p className="mt-1 text-xs text-danger">
                            {persistenceWarning}
                            {st.persistence_error ? `: ${st.persistence_error}` : ''}
                        </p>
                    )}
                </div>
                {st.enabled && st.persistence_state === 'missing' && (
                    <button
                        onClick={saveForReboot}
                        disabled={busy}
                        className="rounded-lg border border-warning/50 bg-warning/10 px-3 py-1.5 text-xs font-semibold text-warning transition-colors hover:bg-warning/20 disabled:opacity-50"
                    >
                        {t('firewall.saveForReboot')}
                    </button>
                )}
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
