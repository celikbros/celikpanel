import { useCallback, useState, useEffect, type ReactNode } from 'react';
import { useSearchParams } from 'react-router-dom';
import {
    ArrowLeft, Globe, Lock, ExternalLink,
    LayoutGrid, Server, Network, Mail, Database, Folder, Wrench, AppWindow,
} from 'lucide-react';
import { DomainPHPSettings } from './DomainPHPSettings';
import { DomainGeneralSettings } from './DomainGeneralSettings';
import { DomainSSLSettings } from './DomainSSLSettings';
import { DomainSSLOverviewCard } from './DomainSSLOverviewCard';
import { DomainConnection } from './DomainConnection';
import { DomainLogsViewer } from './DomainLogsViewer';
import { DomainDatabaseManager } from './DomainDatabaseManager';
import { DomainFileManager } from './DomainFileManager';
import { DomainBackupManager } from './DomainBackupManager';
import { DomainCronManager } from './DomainCronManager';
import { DomainMailManager } from './DomainMailManager';
import { DomainDNSManager } from './DomainDNSManager';
import { HostingTypePanel } from './HostingTypePanel';
import { DomainAppsPanel } from './DomainAppsPanel';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import { StatusDot } from './ui';

interface Domain {
    id: number;
    domain_name: string;
    php_version: string;
    project_type?: string;
    ssl_enabled: boolean;
    status: string;
    created_at: string;
    disk_usage?: number;
    bandwidth?: number;
}

// What the server can actually do — tabs for services that are not installed
// would be settings pages for ghosts. Fetched once per visit.
// Sunucunun gerçekten yapabildiği — kurulu olmayan servislerin sekmeleri,
// hayaletlerin ayar sayfaları olurdu. Ziyaret başına bir kez çekilir.
interface Caps {
    web_server: string;
    php_versions: string[];
    dns_server: string;
    mail_server: boolean;
    database_servers: string[] | null;
}

interface DomainDetailProps {
    domainId: number;
    onBack: () => void;
}

// Domain detail hub. A quiet one-line fact strip under the title (PHP, SSL,
// disk, traffic) and full-width task-grouped tabs. The facts used to live in
// a 260px side card that wasted a column and misaligned the tabs — the page
// belongs to the content, not to a summary. Related tools nest under one tab
// (Hosting → General/PHP/SSL; Advanced → Backups/Cron/Logs) so the top bar
// stays short.
//
// Alan adı detay hub'ı. Başlığın altında tek satırlık sessiz bir bilgi
// şeridi (PHP, SSL, disk, trafik) ve tam genişlikte görev-gruplu sekmeler.
// Bu bilgiler eskiden bir sütunu israf eden ve sekmeleri hizadan kaydıran
// 260px'lik yan karttaydı — sayfa özete değil içeriğe aittir. İlgili araçlar
// tek sekme altında toplanır (Barındırma → Genel/PHP/SSL; Gelişmiş →
// Yedekler/Cron/Loglar); böylece üst çubuk kısa kalır.
export function DomainDetail({ domainId, onBack }: DomainDetailProps) {
    const { t } = useI18n();
    const [searchParams] = useSearchParams();
    const requestedTab = searchParams.get('tab');
    const [domain, setDomain] = useState<Domain | null>(null);
    const [loading, setLoading] = useState(true);
    const [activeTab, setActiveTab] = useState(requestedTab === 'dns' ? 'dns' : 'overview');
    const [activeSub, setActiveSub] = useState<Record<string, string>>({});

    const handleCertificateChange = useCallback((hasCertificate: boolean) => {
        setDomain((currentDomain) => (
            currentDomain ? { ...currentDomain, ssl_enabled: hasCertificate } : currentDomain
        ));
    }, []);

    useEffect(() => {
        if (requestedTab === 'dns') setActiveTab('dns');
    }, [domainId, requestedTab]);

    useEffect(() => {
        fetch('/api/v1/domains')
            .then((r) => (r.ok ? r.json() : []))
            .then((list: Domain[]) => {
                const found = list.find((d) => d.id === domainId);
                if (found) setDomain(found);
                else onBack();
            })
            .catch(onBack)
            .finally(() => setLoading(false));
    }, [domainId]);

    // Refresh the real usage numbers in the background after render: one
    // domain, one measurement. The page shows cached values instantly and
    // updates when the fresh ones land — it never blocks on a probe.
    // Render'dan sonra gerçek kullanım sayılarını arka planda tazele: bir
    // domain, bir ölçüm. Sayfa önbellekli değerleri anında gösterir, tazeler
    // gelince güncellenir — asla bir yoklamayı beklemez.
    const domainLoaded = domain !== null;
    useEffect(() => {
        if (!domainLoaded) return;
        fetch(`/api/v1/domains/${domainId}/usage`)
            .then((r) => (r.ok ? r.json() : null))
            .then((u) => {
                if (u) setDomain((d) => (d ? { ...d, disk_usage: u.disk_usage, bandwidth: u.bandwidth } : d));
            })
            .catch(() => {});
    }, [domainId, domainLoaded]);

    const [caps, setCaps] = useState<Caps | null>(null);
    useEffect(() => {
        fetch('/api/v1/hosting/capabilities')
            .then((r) => (r.ok ? r.json() : null))
            .then(setCaps)
            .catch(() => setCaps(null));
    }, []);

    if (loading) {
        return (
            <div className="flex h-full items-center justify-center">
                <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary" />
            </div>
        );
    }
    if (!domain) return null;

    // Tab tree — honest to the domain's role and the server's capabilities.
    // A DNS-only domain has no files, no PHP, no vhost: showing those tabs
    // would be settings pages for ghosts. Likewise Mail/Databases only exist
    // when the matching server is actually installed (caps=null while loading
    // keeps them visible rather than flashing tabs in and out).
    // Sekme ağacı — domain'in rolüne ve sunucunun yeteneklerine dürüst.
    // Yalnız-DNS domain'in dosyası, PHP'si, vhost'u yok: o sekmeleri
    // göstermek hayaletlere ayar sayfası olurdu. Mail/Veritabanı da ancak
    // ilgili sunucu gerçekten kuruluyken vardır (caps yüklenirken null →
    // sekmeler girip çıkarak titremesin diye görünür kalırlar).
    const projectType = domain.project_type || 'php';
    const isDnsOnly = projectType === 'dnsonly';
    const tabs: TabDef[] = [
        {
            // The connection card leads the Overview: it is the precondition for
            // the site, the certificate and the mail records, and it was the one
            // thing the panel never said out loud.
            // Bağlantı kartı Genel Bakış'ı açar: site, sertifika ve posta
            // kayıtlarının ön koşuludur ve panelin hiç yüksek sesle söylemediği
            // tek şeydi.
            id: 'overview', labelKey: 'domain.tab.overview', icon: LayoutGrid,
        },
        ...(!isDnsOnly ? [{
            id: 'hosting', labelKey: 'domain.tab.hosting', icon: Server,
            subs: [
                { id: 'general', labelKey: 'domain.sub.general', render: () => <DomainGeneralSettings domainId={domain.id} domainName={domain.domain_name} /> },
                { id: 'type', labelKey: 'domain.sub.hostingType', render: () => <HostingTypePanel domainId={domain.id} domainName={domain.domain_name} /> },
                ...(projectType === 'php' ? [{ id: 'php', labelKey: 'domain.sub.php', render: () => <DomainPHPSettings domainId={domain.id} domainName={domain.domain_name} currentVersion={domain.php_version} onVersionChange={(v) => setDomain({ ...domain, php_version: v })} /> } satisfies SubDef] : []),
                {
                    id: 'ssl',
                    labelKey: 'domain.sub.ssl',
                    render: () => (
                        <DomainSSLSettings
                            domainId={domain.id}
                            domainName={domain.domain_name}
                            onCertificateChange={handleCertificateChange}
                        />
                    ),
                },
            ],
        } satisfies TabDef] : []),
        { id: 'dns', labelKey: 'domain.tab.dns', icon: Network, render: () => <DomainDNSManager domainId={domain.id} domainName={domain.domain_name} /> },
        ...(!caps || caps.mail_server ? [{ id: 'mail', labelKey: 'domain.tab.mail', icon: Mail, render: () => <DomainMailManager domainId={domain.id} domainName={domain.domain_name} /> } satisfies TabDef] : []),
        ...(!caps || (caps.database_servers?.length ?? 0) > 0 ? [{ id: 'databases', labelKey: 'domain.tab.databases', icon: Database, render: () => <DomainDatabaseManager domainId={domain.id} domainName={domain.domain_name} /> } satisfies TabDef] : []),
        ...(projectType === 'php' ? [{ id: 'apps', labelKey: 'domain.tab.apps', icon: AppWindow, render: () => <DomainAppsPanel domainId={domain.id} domainName={domain.domain_name} /> } satisfies TabDef] : []),
        ...(!isDnsOnly ? [{ id: 'files', labelKey: 'domain.tab.files', icon: Folder, render: () => <DomainFileManager domainId={domain.id} domainName={domain.domain_name} /> } satisfies TabDef] : []),
        ...(!isDnsOnly ? [{
            id: 'advanced', labelKey: 'domain.tab.advanced', icon: Wrench,
            subs: [
                { id: 'backups', labelKey: 'domain.sub.backups', render: () => <DomainBackupManager domainId={domain.id} domainName={domain.domain_name} /> },
                { id: 'cron', labelKey: 'domain.sub.cron', render: () => <DomainCronManager domainId={domain.id} domainName={domain.domain_name} /> },
                { id: 'logs', labelKey: 'domain.sub.logs', render: () => <DomainLogsViewer domainId={domain.id} domainName={domain.domain_name} /> },
            ],
        } satisfies TabDef] : []),
    ];

    // A tab can disappear when capabilities load (or the type changes) —
    // never crash on a stale selection, fall back to the overview.
    // Yetenekler yüklenince (ya da tip değişince) bir sekme kaybolabilir —
    // bayat seçimde asla çökme, genel bakışa düş.
    const current = tabs.find((tb) => tb.id === activeTab) ?? tabs[0];
    const subId = current.subs ? activeSub[current.id] ?? current.subs[0].id : undefined;

    return (
        <div className="p-6 md:p-8">
            {/* Header */}
            <div className="mb-5">
                <button onClick={onBack} className="mb-3 inline-flex items-center gap-1.5 text-sm text-fg-muted hover:text-fg">
                    <ArrowLeft className="h-4 w-4" />
                    {t('nav.domains')}
                </button>
                <div className="flex flex-wrap items-center gap-3">
                    <span className="flex h-10 w-10 items-center justify-center rounded-xl bg-primary/10 text-primary">
                        {domain.ssl_enabled ? <Lock className="h-5 w-5" /> : <Globe className="h-5 w-5" />}
                    </span>
                    <h1 className="text-2xl font-bold tracking-tight">{domain.domain_name}</h1>
                    <span className="inline-flex items-center gap-1.5 text-sm text-fg-muted">
                        <StatusDot ok={domain.status === 'active'} />
                        {domain.status === 'active' ? t('domains.status.active') : domain.status}
                    </span>
                    {!isDnsOnly && (
                        <a
                            href={`https://${domain.domain_name}`}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="ml-auto inline-flex items-center gap-1.5 rounded-lg border border-border-strong bg-surface px-3 py-1.5 text-sm font-medium text-fg hover:bg-surface-2"
                        >
                            <ExternalLink className="h-4 w-4" />
                            {t('domain.openSite')}
                        </a>
                    )}
                </div>

                {/* Fact strip — status already lives next to the title, so
                    only the facts that add something. / Bilgi şeridi — durum
                    zaten başlığın yanında; yalnız bir şey katan bilgiler. */}
                <div className="mt-3 flex flex-wrap items-center gap-x-3 gap-y-1.5 text-sm">
                    <Fact label={t('domain.info.type')}>{projectType}</Fact>
                    {projectType === 'php' && (
                        <>
                            <FactDivider />
                            <Fact label={t('domain.info.php')}>{domain.php_version || '—'}</Fact>
                        </>
                    )}
                    {!isDnsOnly && (
                        <>
                            <FactDivider />
                            <Fact label={t('domain.info.ssl')}>
                                <span className={domain.ssl_enabled ? 'text-success' : 'text-fg-subtle'}>
                                    {domain.ssl_enabled ? t('domain.info.on') : t('domain.info.off')}
                                </span>
                            </Fact>
                            <FactDivider />
                            <Fact label={t('domain.info.disk')}>{fmtBytes(domain.disk_usage)}</Fact>
                            <FactDivider />
                            <Fact label={t('domain.info.traffic')}>{fmtBytes(domain.bandwidth)}/mo</Fact>
                        </>
                    )}
                </div>
            </div>

            <div className="min-w-0">
                    <div className="mb-4 flex flex-wrap gap-1 border-b border-border">
                        {tabs.map((tb) => {
                            const Icon = tb.icon;
                            const active = tb.id === activeTab;
                            return (
                                <button
                                    key={tb.id}
                                    onClick={() => setActiveTab(tb.id)}
                                    className={`-mb-px flex items-center gap-2 border-b-2 px-3 py-2.5 text-sm font-medium transition-colors ${
                                        active ? 'border-primary text-primary' : 'border-transparent text-fg-muted hover:text-fg'
                                    }`}
                                >
                                    <Icon className="h-4 w-4" />
                                    {t(tb.labelKey)}
                                </button>
                            );
                        })}
                    </div>

                    {/* Sub-tabs (for grouped areas) */}
                    {current.subs && (
                        <div className="mb-4 flex flex-wrap gap-1">
                            {current.subs.map((s) => {
                                const active = s.id === subId;
                                return (
                                    <button
                                        key={s.id}
                                        onClick={() => setActiveSub((m) => ({ ...m, [current.id]: s.id }))}
                                        className={`rounded-lg px-3 py-1.5 text-sm font-medium transition-colors ${
                                            active ? 'bg-primary/10 text-primary' : 'text-fg-muted hover:bg-surface-2'
                                        }`}
                                    >
                                        {t(s.labelKey)}
                                    </button>
                                );
                            })}
                        </div>
                    )}

                    {/* The connection check sits ABOVE the card, on the Overview,
                        because it is the precondition for everything inside it:
                        no site, certificate or mail record can work until the
                        domain points here. The panel knew this and never said it
                        (operator, 25 Jul). / Bağlantı kontrolü Genel Bakış'ta
                        kartın ÜSTÜNDE durur, çünkü içindeki her şeyin ön
                        koşuludur: alan adı buraya bakmadan ne site, ne sertifika,
                        ne posta kaydı çalışır. Panel bunu biliyor ve hiç
                        söylemiyordu (operatör, 25 Tem). */}
                    {activeTab === 'overview' && (
                        <div className="mb-4">
                            <DomainConnection domainId={domain.id} domainName={domain.domain_name} />
                        </div>
                    )}

                    <div className="rounded-xl border border-border bg-surface p-5 shadow-card">
                        {activeTab === 'overview' ? (
                            <Overview
                                domainId={domain.id}
                                tabs={tabs}
                                onCertificateChange={handleCertificateChange}
                                onGo={(tabId, subId) => {
                                    if (subId) {
                                        setActiveSub((currentSubs) => ({
                                            ...currentSubs,
                                            [tabId]: subId,
                                        }));
                                    }
                                    setActiveTab(tabId);
                                }}
                            />
                        ) : current.subs ? (
                            current.subs.find((s) => s.id === subId)!.render()
                        ) : (
                            current.render!()
                        )}
                    </div>
            </div>
        </div>
    );
}

interface SubDef {
    id: string;
    labelKey: TranslationKey;
    render: () => ReactNode;
}
interface TabDef {
    id: string;
    labelKey: TranslationKey;
    icon: typeof Server;
    render?: () => ReactNode;
    subs?: SubDef[];
}

// Overview is a compact launcher — tiles for each area, task-oriented and
// few, rather than Plesk's wall of icons.
// Genel Bakış kompakt bir başlatıcıdır — her bölüm için kutucuklar; Plesk'in
// ikon duvarı yerine görev-odaklı ve az.
function Overview({
    domainId,
    tabs,
    onGo,
    onCertificateChange,
}: {
    domainId: number;
    tabs: TabDef[];
    onGo: (id: string, subId?: string) => void;
    onCertificateChange: (hasCertificate: boolean) => void;
}) {
    const { t } = useI18n();
    const areas = tabs.filter((tb) => tb.id !== 'overview');
    const hasHosting = areas.some((tb) => tb.id === 'hosting');
    return (
        <div>
            {hasHosting && (
                <DomainSSLOverviewCard
                    domainId={domainId}
                    onOpen={() => onGo('hosting', 'ssl')}
                    onCertificateChange={onCertificateChange}
                />
            )}
            <p className="mb-4 text-sm text-fg-muted">{t('domain.overview.hint')}</p>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
                {areas.map((tb) => {
                    const Icon = tb.icon;
                    return (
                        <button
                            key={tb.id}
                            onClick={() => onGo(tb.id)}
                            className="group flex items-center gap-3 rounded-xl border border-border bg-surface p-4 text-left transition-colors hover:border-primary/40 hover:bg-surface-2"
                        >
                            <span className="flex h-10 w-10 items-center justify-center rounded-lg bg-surface-2 text-fg-muted transition-colors group-hover:bg-primary group-hover:text-primary-fg">
                                <Icon className="h-5 w-5" />
                            </span>
                            <span className="text-sm font-semibold text-fg">{t(tb.labelKey)}</span>
                        </button>
                    );
                })}
            </div>
        </div>
    );
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
    return (
        <span className="inline-flex items-baseline gap-1.5">
            <span className="text-fg-subtle">{label}</span>
            <span className="font-medium text-fg">{children}</span>
        </span>
    );
}

function FactDivider() {
    return <span aria-hidden className="h-3.5 w-px self-center bg-border-strong" />;
}

function fmtBytes(bytes: number = 0): string {
    // Honest sizes: a 627-byte site reads "627 B", never a fake-looking
    // "0.0 MB". / Dürüst boyutlar: 627 baytlık site "627 B" okunur, sahte
    // görünen "0.0 MB" asla.
    if (!bytes) return '0 B';
    if (bytes < 1024) return `${bytes} B`;
    const kb = bytes / 1024;
    if (kb < 1024) return `${kb.toFixed(kb < 10 ? 1 : 0)} KB`;
    const mb = kb / 1024;
    if (mb < 1024) return `${mb.toFixed(mb < 10 ? 1 : 0)} MB`;
    return `${(mb / 1024).toFixed(2)} GB`;
}
