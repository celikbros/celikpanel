import { Fragment, Suspense, lazy, useCallback, useState, useEffect, type ReactNode } from 'react';
import { useSearchParams } from '../router';
import {
    ArrowLeft, Globe, Lock, ExternalLink,
    LayoutGrid, Server, Network, Mail, Database, Folder, Wrench, AppWindow,
} from 'lucide-react';
import { DomainPHPSettings } from './DomainPHPSettings';
import { DomainGeneralSettings } from './DomainGeneralSettings';
import { DomainSSLSettings, type SSLRuntimeSummary } from './DomainSSLSettings';
import { DomainSSLOverviewCard } from './DomainSSLOverviewCard';
import { DomainConnection } from './DomainConnection';
import { DomainDatabaseManager } from './DomainDatabaseManager';
import { DomainFileManager } from './DomainFileManager';
import { DomainMailManager } from './DomainMailManager';
import { DomainDNSManager } from './DomainDNSManager';
import { HostingTypePanel } from './HostingTypePanel';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import { Button, Checking, CouldNotCheck, KnownEmpty, Spinner, StatusDot } from './ui';
import { useAuth } from '../auth/AuthContext';
import { useHostingCapabilities } from '../lib/hostingCapabilities';
import { decodeList, lastKnown, useRemote } from '../lib/remote';
import {
    hasAnyDomainAccess,
    hasDomainAccess,
    normalizeDomainAccess,
    type DomainAccess,
    type DomainAccessMode,
    type DomainCapability,
} from '../auth/domainAccess';

const DomainAppsPanel = lazy(() => import('./DomainAppsPanel').then((module) => ({
    default: module.DomainAppsPanel,
})));

// The three panels of the Advanced tab are fetched when one of them is opened,
// like the Applications panel above. They are the least visited part of a
// domain's page and were a quarter of its bundle; the page had 0.72 KiB left
// under its size limit, and the limit is not raised (9 Oct 2026). The tab
// content already waits inside one Suspense boundary.
// Gelişmiş sekmesinin üç bölümü, yukarıdaki Uygulamalar bölümü gibi, biri
// açıldığında getirilir. Alan adı sayfasının en az ziyaret edilen kısmıdır ve
// paketinin dörtte biriydi; sayfanın boyut sınırına 0,72 KiB payı kalmıştı ve
// sınır yükseltilmez. Sekme içeriği zaten tek bir Suspense sınırında bekler.
const DomainBackupManager = lazy(() => import('./DomainBackupManager').then((module) => ({
    default: module.DomainBackupManager,
})));
const DomainCronManager = lazy(() => import('./DomainCronManager').then((module) => ({
    default: module.DomainCronManager,
})));
const DomainLogsViewer = lazy(() => import('./DomainLogsViewer').then((module) => ({
    default: module.DomainLogsViewer,
})));

interface Domain {
    id: number;
    domain_name: string;
    php_version?: string;
    project_type?: string;
    ssl_enabled?: boolean;
    status: string;
    created_at: string;
    disk_usage?: number;
    bandwidth?: number;
    access?: DomainAccess;
}

// The measured usage of one domain. An answer without the numbers is not
// "0 B used".
// Bir alan adının ölçülen kullanımı. Sayıları taşımayan yanıt "0 B" değildir.
interface DomainUsage {
    disk_usage: number;
    bandwidth: number;
}
function decodeUsage(raw: unknown): DomainUsage {
    const body = raw as { disk_usage?: unknown; bandwidth?: unknown } | null;
    if (!body || typeof body.disk_usage !== 'number' || typeof body.bandwidth !== 'number') throw new Error('shape');
    return { disk_usage: body.disk_usage, bandwidth: body.bandwidth };
}

// What became of looking this domain up in the list the server gave.
// `absent` and `noAccess` are answers; a list that could not be read is
// neither, and none of the three sends the person back to the list unasked.
// Bu alan adının, sunucunun verdiği listede aranmasının sonucu. `absent` ve
// `noAccess` yanıttır; okunamayan liste ikisi de değildir ve hiçbiri kişiyi
// sormadan listeye geri göndermez.
const LIST_FRESH_FOR_MS = 30_000;

type Lookup =
    | { state: 'found'; domain: Domain }
    | { state: 'absent' }
    | { state: 'noAccess'; name: string };

function lookUp(rows: unknown[], domainId: number, isTeamMember: boolean): Lookup {
    const found = rows.find((item) => (
        item !== null
        && typeof item === 'object'
        && !Array.isArray(item)
        && Number((item as { id?: unknown }).id) === domainId
    ));
    if (!found || typeof found !== 'object' || Array.isArray(found)) return { state: 'absent' };
    if (!isTeamMember) return { state: 'found', domain: found as Domain };

    const row = found as Domain & { access?: unknown };
    const access = normalizeDomainAccess(row.access);
    if (!access || !hasAnyDomainAccess(access)) return { state: 'noAccess', name: String(row.domain_name ?? '') };
    return { state: 'found', domain: { ...row, access } };
}

// The page of one domain as the address names it. The name is looked up in the
// list the server gives; that is the address the page below, the Domains page
// and the navigation read too, so one request serves all of them. A list that
// could not be read is said so with Retry, a name the server does not list is
// said so with the way back, and neither moves the person off this address.
//
// Adresin adlandırdığı alan adının sayfası. Ad, sunucunun verdiği listede
// aranır; aşağıdaki sayfa, Alan Adları sayfası ve gezinme de aynı adresi okur,
// tek istek hepsine yeter. Okunamayan liste Tekrar dene ile, sunucunun
// listelemediği ad geri dönüş yoluyla söylenir; ikisi de kişiyi bu adresten
// almaz.
export function DomainDetailByName({ domainName, onBack }: { domainName: string; onBack: () => void }) {
    const { t } = useI18n();
    const list = useRemote('/api/v1/domains', decodeList<unknown>);
    const listed = lastKnown(list.remote);
    const back = <Button type="button" icon={ArrowLeft} onClick={onBack}>{t('nav.domains')}</Button>;

    if (!listed) {
        return (
            <div className="p-6 md:p-8">
                {list.remote.state === 'loading' ? (
                    <Checking label={t('domain.checking')} />
                ) : (
                    <>
                        <CouldNotCheck text={t('domain.unknown')} onRetry={() => void list.retry()} busy={list.reading} />
                        <div className="mt-3">{back}</div>
                    </>
                )}
            </div>
        );
    }

    const row = listed.value.find((item) => (
        !!item && typeof item === 'object' && (item as { domain_name?: unknown }).domain_name === domainName
    )) as { id?: unknown } | undefined;
    const domainId = Number(row?.id);
    if (!row || !Number.isFinite(domainId)) {
        return (
            <div className="p-6 md:p-8">
                <KnownEmpty of={listed} icon={Globe} title={t('domain.absent')} hint={t('domain.absentHint')} action={back} />
            </div>
        );
    }

    return <DomainDetail key={domainId} domainId={domainId} onBack={onBack} />;
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
    const { t, locale } = useI18n();
    const { role } = useAuth();
    const isTeamMember = role === 'additional_user';
    const [searchParams] = useSearchParams();
    const requestedTab = searchParams.get('tab');
    // The list is the same address the Domains page and the navigation read,
    // so this page shares their request. What this page changed itself (the
    // PHP version, whether a certificate is active) lies over the row until
    // the list is read again.
    // Liste, Alan Adları sayfasının ve gezinmenin okuduğu adrestir; bu sayfa
    // onların isteğini paylaşır. Sayfanın kendi değiştirdiği (PHP sürümü,
    // sertifikanın etkinliği), liste yeniden okunana dek satırın üzerinde durur.
    // The lookup by name just above has read it a moment ago; that answer is
    // used as it is and nothing is requested a second time.
    // Hemen üstteki ada göre arama onu az önce okudu; o yanıt olduğu gibi
    // kullanılır ve ikinci kez istek gönderilmez.
    const list = useRemote('/api/v1/domains', decodeList<unknown>, { freshFor: LIST_FRESH_FOR_MS });
    const [changed, setChanged] = useState<Partial<Domain>>({});
    const [activeTab, setActiveTab] = useState(requestedTab === 'dns' ? 'dns' : 'overview');
    const [activeSub, setActiveSub] = useState<Record<string, string>>({});
    const [sslRuntime, setSSLRuntime] = useState<SSLRuntimeSummary | null>(null);

    const handleCertificateChange = useCallback((status: SSLRuntimeSummary) => {
        setSSLRuntime(status);
        setChanged((current) => ({ ...current, ssl_enabled: status.activated }));
    }, []);

    useEffect(() => {
        setSSLRuntime(null);
        if (requestedTab === 'dns') setActiveTab('dns');
    }, [domainId, requestedTab]);

    const listed = lastKnown(list.remote);
    const lookup = listed ? lookUp(listed.value, domainId, isTeamMember) : null;
    const domain: Domain | null = lookup?.state === 'found' ? { ...lookup.domain, ...changed } : null;

    // The measured usage is read after the page is drawn: one domain, one
    // measurement. Until it answers, and if it cannot be read, the figures the
    // list carried stay; they are the server's own, only older.
    // Ölçülen kullanım sayfa çizildikten sonra okunur. Yanıt gelene dek ve
    // okunamazsa listenin taşıdığı rakamlar kalır; onlar da sunucunundur.
    const domainLoaded = domain !== null;
    const canViewStatistics = !isTeamMember
        || Boolean(domain?.access && hasDomainAccess(domain.access, 'statistics'));
    const usage = useRemote(domainLoaded && canViewStatistics ? `/api/v1/domains/${domainId}/usage` : null, decodeUsage);
    const measured = domainLoaded && canViewStatistics && usage.remote.state === 'known' ? usage.remote.value : null;

    // What the server can actually do — tabs for services that are not
    // installed would be settings pages for ghosts. One shared read
    // (lib/hostingCapabilities.ts); the panels under the tabs use the same
    // answer. `caps` is the KNOWN answer or null, and the three states mean:
    //   - being checked:      every tab stays; nothing is hidden on a guess;
    //   - could not be checked: every tab stays; the panel that needs the
    //                         answer says so itself and offers the read again;
    //   - known:              only now is a tab for a missing service removed.
    // A team member is never asked: the server-wide inventory is not theirs.
    // Sunucunun gerçekten yapabildiği — kurulu olmayan servislerin sekmeleri,
    // hayaletlerin ayar sayfaları olurdu. Tek ortak okuma; sekmelerin altındaki
    // bölümler aynı yanıtı kullanır. `caps` BİLİNEN yanıttır ya da null:
    // kontrol edilirken ve kontrol edilemediğinde her sekme kalır (yanıta
    // ihtiyacı olan bölüm bunu kendisi söyler); ancak bilindiğinde eksik
    // servisin sekmesi kaldırılır. Ekip üyesi için hiç sorulmaz.
    const capabilities = useHostingCapabilities({ enabled: !isTeamMember });
    const caps = !isTeamMember && capabilities.remote.state === 'known' ? capabilities.remote.value : null;
    // Which tabs exist follows the last answer the server gave. A refresh that
    // failed is not a reason to bring back a tab the server ruled out, nor to
    // take one away.
    // Hangi sekmelerin var olduğu, sunucunun verdiği son yanıtı izler. Başarısız
    // bir yenileme, sunucunun elediği sekmeyi geri getirmez; var olanı da almaz.
    const tabCaps = isTeamMember ? null : lastKnown(capabilities.remote)?.value ?? null;

    if (list.remote.state === 'loading') {
        return (
            <div className="p-6 md:p-8">
                <Checking label={t('domain.checking')} />
            </div>
        );
    }
    if (!domain) {
        // Not shown, and said why, with the way back as a choice.
        // Gösterilmez, nedeni söylenir ve geri dönüş bir seçenek olarak sunulur.
        const back = <Button type="button" icon={ArrowLeft} onClick={onBack}>{t('nav.domains')}</Button>;
        return (
            <div className="p-6 md:p-8">
                {!listed || !lookup ? (
                    <>
                        <CouldNotCheck text={t('domain.unknown')} onRetry={() => void list.retry()} busy={list.reading} />
                        <div className="mt-3">{back}</div>
                    </>
                ) : (
                    <KnownEmpty
                        of={listed}
                        icon={Globe}
                        title={t(lookup.state === 'noAccess' ? 'domain.noAccess' : 'domain.absent')}
                        hint={t(lookup.state === 'noAccess' ? 'domain.noAccessHint' : 'domain.absentHint')}
                        action={back}
                    />
                )}
            </div>
        );
    }

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
    const sslUsable = sslRuntime?.usable === true;
    const sslConfigured = sslRuntime?.activated === true;
    const canView = (capability: DomainCapability) => (
        !isTeamMember || Boolean(domain.access && hasDomainAccess(domain.access, capability))
    );
    const combinedAccessMode = (capabilities: DomainCapability[]): DomainAccessMode => {
        if (!isTeamMember) return 'manage';
        const modes = capabilities.map((capability) => domain.access?.[capability] ?? 'none');
        if (modes.some((mode) => mode === 'none')) return 'none';
        if (modes.some((mode) => mode === 'view')) return 'view';
        return 'manage';
    };
    const hostingSubs: SubDef[] = [
        ...(!isTeamMember && canView('files') ? [
            { id: 'general', labelKey: 'domain.sub.general', capabilities: ['files'], render: () => <DomainGeneralSettings domainId={domain.id} domainName={domain.domain_name} /> } satisfies SubDef,
            { id: 'type', labelKey: 'domain.sub.hostingType', capabilities: ['files'], render: () => <HostingTypePanel domainId={domain.id} domainName={domain.domain_name} /> } satisfies SubDef,
        ] : []),
        ...(projectType === 'php' && canView('php') ? [
            { id: 'php', labelKey: 'domain.sub.php', capabilities: ['php'], render: (readOnly) => <DomainPHPSettings domainId={domain.id} domainName={domain.domain_name} currentVersion={domain.php_version ?? ''} onVersionChange={(v) => setChanged((current) => ({ ...current, php_version: v }))} readOnly={readOnly} isAdditionalUser={isTeamMember} /> } satisfies SubDef,
        ] : []),
        ...(canView('ssl') ? [{
            id: 'ssl',
            labelKey: 'domain.sub.ssl',
            capabilities: ['ssl'],
            render: (readOnly) => (
                <DomainSSLSettings
                    domainId={domain.id}
                    domainName={domain.domain_name}
                    mailAvailable={isTeamMember ? canView('mail') : (caps?.mail_server ?? null)}
                    onCertificateChange={handleCertificateChange}
                    readOnly={readOnly}
                />
            ),
        } satisfies SubDef] : []),
    ];
    const advancedSubs: SubDef[] = [
        ...(canView('backups') ? [{ id: 'backups', labelKey: 'domain.sub.backups', capabilities: ['backups'], render: (readOnly) => <DomainBackupManager domainId={domain.id} domainName={domain.domain_name} readOnly={readOnly} /> } satisfies SubDef] : []),
        ...(canView('cron') ? [{ id: 'cron', labelKey: 'domain.sub.cron', capabilities: ['cron'], render: (readOnly) => <DomainCronManager domainId={domain.id} domainName={domain.domain_name} readOnly={readOnly} /> } satisfies SubDef] : []),
        ...(canView('statistics') ? [{ id: 'logs', labelKey: 'domain.sub.logs', capabilities: ['statistics'], render: (readOnly) => <DomainLogsViewer domainId={domain.id} domainName={domain.domain_name} readOnly={readOnly} /> } satisfies SubDef] : []),
    ];
    const tabs: TabDef[] = [
        ...(!isTeamMember ? [{
            // The connection card leads the Overview: it is the precondition for
            // the site, the certificate and the mail records, and it was the one
            // thing the panel never said out loud.
            // Bağlantı kartı Genel Bakış'ı açar: site, sertifika ve posta
            // kayıtlarının ön koşuludur ve panelin hiç yüksek sesle söylemediği
            // tek şeydi.
            id: 'overview', labelKey: 'domain.tab.overview', icon: LayoutGrid,
        } satisfies TabDef] : []),
        ...(!isDnsOnly && hostingSubs.length > 0 ? [{
            id: 'hosting', labelKey: 'domain.tab.hosting', icon: Server,
            subs: hostingSubs,
        } satisfies TabDef] : []),
        ...(canView('dns') ? [{ id: 'dns', labelKey: 'domain.tab.dns', icon: Network, capabilities: ['dns'], render: (readOnly) => <DomainDNSManager domainId={domain.id} domainName={domain.domain_name} readOnly={readOnly} isAdditionalUser={isTeamMember} /> } satisfies TabDef] : []),
        ...(canView('mail') && (isTeamMember || !tabCaps || tabCaps.mail_server) ? [{ id: 'mail', labelKey: 'domain.tab.mail', icon: Mail, capabilities: ['mail'], render: (readOnly) => <DomainMailManager domainId={domain.id} domainName={domain.domain_name} readOnly={readOnly} /> } satisfies TabDef] : []),
        ...(canView('databases') && (isTeamMember || !tabCaps || tabCaps.database_servers.length > 0) ? [{ id: 'databases', labelKey: 'domain.tab.databases', icon: Database, capabilities: ['databases'], render: (readOnly) => <DomainDatabaseManager domainId={domain.id} domainName={domain.domain_name} readOnly={readOnly} isAdditionalUser={isTeamMember} /> } satisfies TabDef] : []),
        ...(!isTeamMember && projectType === 'php' && canView('files') && canView('php') ? [{ id: 'apps', labelKey: 'domain.tab.apps', icon: AppWindow, capabilities: ['files', 'php'], render: () => <DomainAppsPanel domainId={domain.id} domainName={domain.domain_name} /> } satisfies TabDef] : []),
        ...(!isDnsOnly && canView('files') ? [{ id: 'files', labelKey: 'domain.tab.files', icon: Folder, capabilities: ['files'], render: (readOnly) => <DomainFileManager domainId={domain.id} domainName={domain.domain_name} readOnly={readOnly} /> } satisfies TabDef] : []),
        ...(!isDnsOnly && advancedSubs.length > 0 ? [{
            id: 'advanced', labelKey: 'domain.tab.advanced', icon: Wrench,
            subs: advancedSubs,
        } satisfies TabDef] : []),
    ];

    // A tab can disappear when capabilities load (or the type changes) —
    // never crash on a stale selection, fall back to the overview.
    // Yetenekler yüklenince (ya da tip değişince) bir sekme kaybolabilir —
    // bayat seçimde asla çökme, genel bakışa düş.
    const current = tabs.find((tb) => tb.id === activeTab) ?? tabs[0];
    if (!current) return null;
    // The person is moved only off a tab that is not there any more, and the
    // stored choice follows at once, so a tab that comes back later (a service
    // installed since) does not pull them to it. A tab that exists is never
    // left on their behalf: the list above is the one place that decides.
    // Kişi yalnız artık var olmayan sekmeden alınır ve saklanan seçim hemen
    // onu izler; sonradan geri gelen sekme kişiyi kendine çekmez. Var olan
    // sekme onun adına terk edilmez: buna yalnız yukarıdaki liste karar verir.
    if (current.id !== activeTab && (isTeamMember || tabCaps)) setActiveTab(current.id);
    const requestedSubId = current.subs ? activeSub[current.id] : undefined;
    const currentSub = current.subs
        ? current.subs.find((sub) => sub.id === requestedSubId) ?? current.subs[0]
        : undefined;
    const subId = currentSub?.id;
    const currentMode = combinedAccessMode(currentSub?.capabilities ?? current.capabilities ?? []);
    const readOnly = isTeamMember && currentMode === 'view';
    const facts: Array<{ key: string; content: ReactNode }> = [];
    if (!isTeamMember || canView('files') || canView('php')) {
        facts.push({
            key: 'type',
            content: <Fact label={t('domain.info.type')}>{projectType}</Fact>,
        });
    }
    if (projectType === 'php' && canView('php')) {
        facts.push({
            key: 'php',
            content: <Fact label={t('domain.info.php')}>{domain.php_version || '—'}</Fact>,
        });
    }
    if (!isDnsOnly && canView('ssl')) {
        facts.push({
            key: 'ssl',
            content: (
                <Fact label={t('domain.info.ssl')}>
                    <span className={sslUsable ? 'text-success' : sslConfigured ? 'text-warning' : 'text-fg-subtle'}>
                        {sslRuntime === null
                            ? t('domain.overview.ssl.checking')
                            : sslUsable
                              ? t('domain.info.on')
                              : sslConfigured
                                ? t('domain.info.sslIssue')
                                : t('domain.info.off')}
                    </span>
                </Fact>
            ),
        });
    }
    if (!isDnsOnly && canView('statistics')) {
        facts.push({
            key: 'disk',
            content: <Fact label={t('domain.info.disk')}>{fmtBytes(measured ? measured.disk_usage : domain.disk_usage)}</Fact>,
        });
        facts.push({
            key: 'traffic',
            content: <Fact label={t('domain.info.traffic')}>{fmtBytes(measured ? measured.bandwidth : domain.bandwidth)}/mo</Fact>,
        });
    }

    return (
        <div className="p-6 md:p-8">
            {/* The list could not be read again: the page stays, and says that
                what it shows about this domain is the earlier answer.
                Liste yeniden okunamadı: sayfa kalır ve bu alan adı hakkında
                gösterdiğinin önceki yanıt olduğunu söyler. */}
            {list.remote.state === 'unknown' && listed && (
                <CouldNotCheck
                    className="mb-4"
                    text={t('common.staleNotice', {
                        time: new Date(listed.observedAt).toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit' }),
                    })}
                    onRetry={() => void list.retry()}
                    busy={list.reading}
                />
            )}
            {/* Header */}
            <div className="mb-5">
                <button onClick={onBack} className="mb-3 inline-flex items-center gap-1.5 text-sm text-fg-muted hover:text-fg">
                    <ArrowLeft className="h-4 w-4" />
                    {t('nav.domains')}
                </button>
                <div className="flex flex-wrap items-center gap-3">
                    <span className="flex h-10 w-10 items-center justify-center rounded-xl bg-primary/10 text-primary">
                        {canView('ssl') && sslUsable ? <Lock className="h-5 w-5" /> : <Globe className="h-5 w-5" />}
                    </span>
                    <h1 className="text-2xl font-bold tracking-tight">{domain.domain_name}</h1>
                    <span className="inline-flex items-center gap-1.5 text-sm text-fg-muted">
                        <StatusDot ok={domain.status === 'active'} />
                        {domain.status === 'active' ? t('domains.status.active') : domain.status}
                    </span>
                    {!isDnsOnly && canView('files') && (
                        <a
                            href={`${sslUsable ? 'https' : 'http'}://${domain.domain_name}`}
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
                {facts.length > 0 && (
                    <div className="mt-3 flex flex-wrap items-center gap-x-3 gap-y-1.5 text-sm">
                        {facts.map((fact, index) => (
                            <Fragment key={fact.key}>
                                {index > 0 && <FactDivider />}
                                {fact.content}
                            </Fragment>
                        ))}
                    </div>
                )}
            </div>

            <div className="min-w-0">
                    <div className="mb-4 flex flex-wrap gap-1 border-b border-border">
                        {tabs.map((tb) => {
                            const Icon = tb.icon;
                            const active = tb.id === current.id;
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
                    {current.id === 'overview' && (
                        <div className="mb-4">
                            <DomainConnection domainId={domain.id} domainName={domain.domain_name} />
                        </div>
                    )}

                    {readOnly && (
                        <div className="mb-3 rounded-lg border border-info/30 bg-info/10 px-3 py-2 text-sm text-fg">
                            View-only access / Salt görüntüleme yetkisi
                        </div>
                    )}

                    <div className="rounded-xl border border-border bg-surface p-5">
                        <Suspense fallback={<Spinner />}>
                            {current.id === 'overview' ? (
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
                            ) : currentSub ? (
                                currentSub.render(readOnly)
                            ) : (
                                current.render!(readOnly)
                            )}
                        </Suspense>
                    </div>
            </div>
        </div>
    );
}

interface SubDef {
    id: string;
    labelKey: TranslationKey;
    render: (readOnly: boolean) => ReactNode;
    capabilities?: DomainCapability[];
}
interface TabDef {
    id: string;
    labelKey: TranslationKey;
    icon: typeof Server;
    render?: (readOnly: boolean) => ReactNode;
    subs?: SubDef[];
    capabilities?: DomainCapability[];
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
    onCertificateChange: (status: SSLRuntimeSummary) => void;
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
