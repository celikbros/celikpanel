import { useState, useEffect, type ReactNode } from 'react';
import {
    ArrowLeft, Globe, Lock, ExternalLink,
    LayoutGrid, Server, Network, Mail, Database, Folder, Wrench,
} from 'lucide-react';
import { DomainPHPSettings } from './DomainPHPSettings';
import { DomainGeneralSettings } from './DomainGeneralSettings';
import { DomainSSLSettings } from './DomainSSLSettings';
import { DomainLogsViewer } from './DomainLogsViewer';
import { DomainDatabaseManager } from './DomainDatabaseManager';
import { DomainFileManager } from './DomainFileManager';
import { DomainBackupManager } from './DomainBackupManager';
import { DomainCronManager } from './DomainCronManager';
import { DomainMailManager } from './DomainMailManager';
import { DomainDNSManager } from './DomainDNSManager';
import { HostingTypePanel } from './HostingTypePanel';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import { StatusDot } from './ui';

interface Domain {
    id: number;
    domain_name: string;
    php_version: string;
    ssl_enabled: boolean;
    status: string;
    created_at: string;
    disk_usage?: number;
    bandwidth?: number;
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
    const [domain, setDomain] = useState<Domain | null>(null);
    const [loading, setLoading] = useState(true);
    const [activeTab, setActiveTab] = useState('overview');
    const [activeSub, setActiveSub] = useState<Record<string, string>>({});

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

    if (loading) {
        return (
            <div className="flex h-full items-center justify-center">
                <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary" />
            </div>
        );
    }
    if (!domain) return null;

    // Tab tree. Panels reuse the existing managers unchanged.
    // Sekme ağacı. Paneller mevcut yöneticileri değiştirmeden kullanır.
    const tabs: TabDef[] = [
        { id: 'overview', labelKey: 'domain.tab.overview', icon: LayoutGrid },
        {
            id: 'hosting', labelKey: 'domain.tab.hosting', icon: Server,
            subs: [
                { id: 'general', labelKey: 'domain.sub.general', render: () => <DomainGeneralSettings domainId={domain.id} domainName={domain.domain_name} /> },
                { id: 'type', labelKey: 'domain.sub.hostingType', render: () => <HostingTypePanel domainId={domain.id} domainName={domain.domain_name} /> },
                { id: 'php', labelKey: 'domain.sub.php', render: () => <DomainPHPSettings domainId={domain.id} domainName={domain.domain_name} currentVersion={domain.php_version} onVersionChange={(v) => setDomain({ ...domain, php_version: v })} /> },
                { id: 'ssl', labelKey: 'domain.sub.ssl', render: () => <DomainSSLSettings domainId={domain.id} domainName={domain.domain_name} /> },
            ],
        },
        { id: 'dns', labelKey: 'domain.tab.dns', icon: Network, render: () => <DomainDNSManager domainId={domain.id} domainName={domain.domain_name} /> },
        { id: 'mail', labelKey: 'domain.tab.mail', icon: Mail, render: () => <DomainMailManager domainId={domain.id} domainName={domain.domain_name} /> },
        { id: 'databases', labelKey: 'domain.tab.databases', icon: Database, render: () => <DomainDatabaseManager domainId={domain.id} domainName={domain.domain_name} /> },
        { id: 'files', labelKey: 'domain.tab.files', icon: Folder, render: () => <DomainFileManager domainId={domain.id} domainName={domain.domain_name} /> },
        {
            id: 'advanced', labelKey: 'domain.tab.advanced', icon: Wrench,
            subs: [
                { id: 'backups', labelKey: 'domain.sub.backups', render: () => <DomainBackupManager domainId={domain.id} domainName={domain.domain_name} /> },
                { id: 'cron', labelKey: 'domain.sub.cron', render: () => <DomainCronManager domainId={domain.id} domainName={domain.domain_name} /> },
                { id: 'logs', labelKey: 'domain.sub.logs', render: () => <DomainLogsViewer domainId={domain.id} domainName={domain.domain_name} /> },
            ],
        },
    ];

    const current = tabs.find((tb) => tb.id === activeTab)!;
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
                    <a
                        href={`https://${domain.domain_name}`}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="ml-auto inline-flex items-center gap-1.5 rounded-lg border border-border-strong bg-surface px-3 py-1.5 text-sm font-medium text-fg hover:bg-surface-2"
                    >
                        <ExternalLink className="h-4 w-4" />
                        {t('domain.openSite')}
                    </a>
                </div>

                {/* Fact strip — status already lives next to the title, so
                    only the facts that add something. / Bilgi şeridi — durum
                    zaten başlığın yanında; yalnız bir şey katan bilgiler. */}
                <div className="mt-3 flex flex-wrap items-center gap-x-3 gap-y-1.5 text-sm">
                    <Fact label={t('domain.info.php')}>{domain.php_version || '—'}</Fact>
                    <FactDivider />
                    <Fact label={t('domain.info.ssl')}>
                        <span className={domain.ssl_enabled ? 'text-success' : 'text-fg-subtle'}>
                            {domain.ssl_enabled ? t('domain.info.on') : t('domain.info.off')}
                        </span>
                    </Fact>
                    <FactDivider />
                    <Fact label={t('domain.info.disk')}>{fmtMB(domain.disk_usage)}</Fact>
                    <FactDivider />
                    <Fact label={t('domain.info.traffic')}>{fmtMB(domain.bandwidth)}/mo</Fact>
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

                    <div className="rounded-xl border border-border bg-surface p-5 shadow-card">
                        {activeTab === 'overview' ? (
                            <Overview tabs={tabs} onGo={setActiveTab} />
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
function Overview({ tabs, onGo }: { tabs: TabDef[]; onGo: (id: string) => void }) {
    const { t } = useI18n();
    const areas = tabs.filter((tb) => tb.id !== 'overview');
    return (
        <div>
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

function fmtMB(bytes: number = 0): string {
    if (!bytes) return '0 MB';
    const mb = bytes / (1024 * 1024);
    return mb < 1024 ? `${mb.toFixed(1)} MB` : `${(mb / 1024).toFixed(2)} GB`;
}
