import { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from '../router';
import { Globe, Plus, Trash2, ExternalLink, Settings, Lock, HardDrive } from 'lucide-react';
import { AddDomainModal } from './AddDomainModal';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { Button, CouldNotCheck, KnownEmpty, RemoteGate, SearchInput, StatusDot, UsageBar } from './ui';
import { PageHeader } from './PageHeader';
import { apiErrorText, readApiError } from '../lib/apiError';
import { domainDeletionDetailKey, domainDeletionReasonKey, readDomainDeletionOutcome, readSavedDomainDeletion } from '../lib/domainDeletionPending';
import { decodeList, gateOn, lastKnown, useRemote } from '../lib/remote';
import { dnsBlocker, useHostingCapabilities } from '../lib/hostingCapabilities';
import type { TranslationKey } from '../i18n/en';
import { useAuth } from '../auth/AuthContext';
import {
    hasAnyDomainAccess,
    hasDomainAccess,
    normalizeDomainAccess,
    type DomainAccess,
    type DomainCapability,
} from '../auth/domainAccess';

// Type badge colours: categorical, readable in both themes.
// Tip rozeti renkleri: kategorik, iki temada da okunur.
const typeBadge: Record<string, string> = {
    php: 'bg-primary/10 text-primary',
    static: 'bg-surface-2 text-fg-muted',
    node: 'bg-success/10 text-success',
    proxy: 'bg-warning/15 text-warning',
    forwarding: 'bg-warning/15 text-warning',
    dnsonly: 'bg-surface-2 text-fg-muted',
};

interface Domain {
    id: number;
    domain_name: string;
    php_version?: string;
    ssl_enabled?: boolean;
    status: string;
    project_type?: string;
    created_at: string;
    disk_usage?: number;
    bandwidth?: number;
    parent_id?: number | null;
    access?: DomainAccess;
}

const API_BASE = '/api/v1';

// A waiting deletion shown above the list. `message` is null when the saved
// marker of a pending row could not be read: the page then says exactly that.
// Listenin üstünde gösterilen bekleyen silme. Beklemedeki satırın kayıtlı
// işareti okunamadıysa `message` null olur; sayfa tam olarak bunu söyler.
interface PendingDeletion {
    id: number;
    name: string;
    message: string | null;
}

const HIDDEN_INVALID_ACCESS = 'One or more domains were hidden because their access information was invalid. / Bir veya daha fazla alan adı, erişim bilgisi geçersiz olduğu için gizlendi.';

// The rows this person may see. An administrator, reseller or customer sees
// what the server listed. A team member sees only rows whose access record is
// valid and grants something; anything else is hidden and the page says that
// rows were hidden (fail closed).
// Bu kişinin görebileceği satırlar. Ekip üyesi yalnız erişim kaydı geçerli ve
// bir şey veren satırları görür; gerisi gizlenir ve sayfa bunu söyler.
function visibleDomains(items: unknown[], teamMember: boolean): { rows: Domain[]; hiddenInvalid: boolean } {
    if (!teamMember) return { rows: items as Domain[], hiddenInvalid: false };
    let hiddenInvalid = false;
    const rows: Domain[] = [];
    for (const item of items) {
        if (!item || typeof item !== 'object' || Array.isArray(item)) {
            hiddenInvalid = true;
            continue;
        }
        const row = item as Domain & { access?: unknown };
        const access = normalizeDomainAccess(row.access);
        if (!access || !hasAnyDomainAccess(access)) {
            hiddenInvalid = true;
            continue;
        }
        rows.push({ ...row, access });
    }
    return { rows, hiddenInvalid };
}

// Plesk-style list page: breadcrumb + title, a toolbar (primary add +
// contextual remove) with search, an item count, a clean data table with
// per-row actions, and a paging footer.
//
// Plesk tarzı liste sayfası: breadcrumb + başlık, araç çubuğu (birincil
// ekle + bağlamsal kaldır) ve arama, öğe sayısı, satır-başı aksiyonlu temiz
// bir veri tablosu ve sayfalama alt bilgisi.
export function Domains() {
    const navigate = useNavigate();
    const { t } = useI18n();
    const { role } = useAuth();
    const isTeamMember = role === 'additional_user';
    const [showAddModal, setShowAddModal] = useState(false);
    const [query, setQuery] = useState('');
    const [selected, setSelected] = useState<number[]>([]);
    const [pendingDeletions, setPendingDeletions] = useState<PendingDeletion[]>([]);
    const pendingReadEpoch = useRef(0);

    // The list is in one of three states (lib/remote.ts): being read, read, or
    // not readable. "No domains yet" is a claim about the server and is drawn
    // only for an answer the server gave; a failed read says it failed and
    // offers the read again. After a failed refresh the earlier list stays,
    // marked as the earlier list, with nothing that removes a domain enabled.
    // Liste üç durumdan birindedir: okunuyor, okundu ya da okunamadı. "Henüz
    // alan adı yok" sunucu hakkında bir iddiadır ve yalnız sunucunun verdiği
    // yanıt için çizilir; başarısız okuma başarısız olduğunu söyler ve okumayı
    // yeniden sunar. Başarısız yenilemede önceki liste, önceki liste olduğu
    // belirtilerek kalır; alan adı silen hiçbir denetim etkin olmaz.
    const list = useRemote('/api/v1/domains', decodeList<unknown>);
    const listed = lastKnown(list.remote);
    const { rows: domains, hiddenInvalid } = useMemo(
        () => visibleDomains(listed?.value ?? [], isTeamMember),
        [listed?.value, isTeamMember],
    );
    const listCurrent = list.remote.state === 'known';

    // D-009 on the page itself, not only inside the dialog: when this server is
    // KNOWN to be unable to publish a website's DNS, the Add buttons are
    // disabled with the reason and the empty state leads to where it is fixed.
    // While that is being checked, or could not be checked, the page says
    // nothing negative about DNS and Add stays available: the dialog shares
    // this same read, shows the checking line or the could-not-check notice
    // with Retry, and keeps its own submit disabled until the answer is known.
    // The backend refuses an unpublishable domain in every case.
    // D-009 yalnız pencerede değil sayfanın kendisinde: bu sunucunun bir web
    // sitesinin DNS'ini yayımlayamadığı BİLİNİYORSA Ekle düğmeleri gerekçesiyle
    // pasiftir ve boş durum düzeltileceği yere götürür. Bu kontrol edilirken ya
    // da edilemediğinde sayfa DNS hakkında olumsuz bir şey söylemez ve Ekle
    // kullanılabilir kalır: pencere aynı okumayı paylaşır, kontrol satırını ya
    // da Tekrar dene'li bildirimi gösterir ve yanıt bilinene kadar kendi
    // gönder düğmesini kapalı tutar.
    const capabilities = useHostingCapabilities({ enabled: !isTeamMember });
    const dns = gateOn(capabilities.remote, (value) => dnsBlocker(value, 'website'));
    const dnsBlocked = !isTeamMember && dns.state === 'blocked' ? dns.reason : null;
    // Whether an engine is missing or only its identity is, the DNS
    // infrastructure section is where it gets fixed; the Services page can no
    // longer install a DNS engine (DNS_ENGINE_WORKFLOW_REQUIRED), so a fresh
    // host sent there had nowhere to go (R-029, screen side).
    // Motor mu eksik yoksa yalnız kimliği mi, düzelten yer DNS altyapısı
    // bölümüdür; Servisler sayfası artık DNS motoru kuramaz
    // (DNS_ENGINE_WORKFLOW_REQUIRED), oraya gönderilen taze sunucunun gidecek
    // yeri yoktu (R-029, ekran tarafı).
    const openDNSRequirement = () => navigate('/settings?section=dns');
    // Name the half that is missing: no engine at all, or an engine whose
    // identity is not staged yet. The same key feeds the button label.
    // Eksik yarıyı adlandır: hiç motor yok ya da kimliği henüz hazırlanmamış
    // bir motor var. Aynı anahtar düğme etiketini de besler.
    const dnsRequirementText = dnsBlocked === null
        ? undefined
        : dnsBlocked === 'engine' ? t('domains.add.needsDns') : t('err.DNS_SETTINGS_REQUIRED');

    const pendingMessage = (reason: string, detail = '') => {
        const key = domainDeletionReasonKey(reason) as TranslationKey | null;
        const translated = key ? t(key) : '';
        if (!key || translated === key) return t('domains.deletionPending');
        // What the secondary's inspector reported, or which check of this
        // server's proof differed, as its own sentence after the reason's
        // guidance. Only reviewed tokens reach this point.
        const detailKey = domainDeletionDetailKey(reason, detail) as TranslationKey | null;
        const detailText = detailKey ? t(detailKey) : '';
        return detailKey && detailText !== detailKey ? `${translated} ${detailText}` : translated;
    };

    // Every row the server lists as pending is asked for its saved deletion
    // marker, after each answer of the list. These are reads. A marker that
    // could not be read is shown as exactly that; it is not "no deletion is
    // waiting".
    // Sunucunun beklemede listelediği her satır için kayıtlı silme işareti,
    // listenin her yanıtından sonra sorulur. Bunlar okumadır. Okunamayan
    // işaret tam olarak öyle gösterilir; "bekleyen silme yok" sayılmaz.
    const listedAt = listed?.observedAt;
    useEffect(() => {
        const epoch = ++pendingReadEpoch.current;
        // Without a current answer of the list there is nothing to ask about;
        // what an earlier answer established stays on screen with that list.
        // Listenin güncel yanıtı yokken sorulacak bir şey yoktur; önceki
        // yanıtın saptadığı, o listeyle birlikte ekranda kalır.
        if (isTeamMember || !listCurrent) return;
        const pending = domains.filter((domain) => domain.status === 'pending');
        void Promise.all(pending.map(async (domain): Promise<PendingDeletion | null> => {
            const saved = await readSavedDomainDeletion(domain.id);
            if (saved.state === 'unknown') return { id: domain.id, name: domain.domain_name, message: null };
            return saved.saved === null ? null : {
                id: domain.id, name: domain.domain_name, message: pendingMessage(saved.saved.reason, saved.saved.detail),
            };
        })).then((observed) => {
            if (epoch === pendingReadEpoch.current) {
                setPendingDeletions(observed.filter((item): item is PendingDeletion => item !== null));
            }
        });
        // One run per answer of the list: `listedAt` changes with every read.
    }, [listedAt, listCurrent, isTeamMember]);

    const reload = () => void list.retry();

    const handleDelete = async (id: number, name: string) => {
        if (!listCurrent) return;
        if (!confirm(t('domains.confirmDelete', { name }))) return;
        try {
            const res = await fetch(`${API_BASE}/domains/${id}`, { method: 'DELETE' });
            const outcome = await readDomainDeletionOutcome(res);
            if (outcome.state === 'pending') {
                const message = pendingMessage(outcome.reason, outcome.detail);
                pendingReadEpoch.current++;
                setPendingDeletions((current) => [
                    ...current.filter((item) => item.id !== id),
                    { id, name, message },
                ]);
                showToast('warning', message);
                reload();
                return;
            }
            if (outcome.state === 'error') {
                const apiError = await readApiError(res);
                showToast('error', apiErrorText(apiError, t));
                return;
            }
            pendingReadEpoch.current++;
            setPendingDeletions((current) => current.filter((item) => item.id !== id));
            showToast('success', t('domains.deleted', { name }));
            setSelected((s) => s.filter((x) => x !== id));
            reload();
        } catch {
            showToast('error', t('common.error'));
        }
    };

    const filtered = domains.filter((d) => d.domain_name.toLowerCase().includes(query.toLowerCase()));
    const allSelected = !isTeamMember && filtered.length > 0 && selected.length === filtered.length;
    const canView = (domain: Domain, capability: DomainCapability) =>
        !isTeamMember || Boolean(domain.access && hasDomainAccess(domain.access, capability, 'view'));
    const addButton = (
        <span title={dnsRequirementText}>
            <Button variant="primary" icon={Plus} disabled={dnsBlocked !== null} onClick={() => setShowAddModal(true)}>
                {t('domains.add')}
            </Button>
        </span>
    );

    return (
        <div className="p-6 md:p-8">
            {!isTeamMember && <SubscriptionUsage />}
            <PageHeader
                title={t('nav.domains')}
                subtitle={hiddenInvalid ? HIDDEN_INVALID_ACCESS : t('domains.subtitle')}
                breadcrumb={[t('common.home'), t('nav.domains')]}
                actions={!isTeamMember && addButton}
            />

            {pendingDeletions.map((pending) => (pending.message === null ? (
                <CouldNotCheck
                    key={pending.id}
                    className="mb-4"
                    text={t('domains.pendingUnknown', { name: pending.name })}
                    onRetry={reload}
                    busy={list.reading}
                />
            ) : (
                <div key={pending.id} role="status" className="mb-4 rounded-lg border border-warning bg-warning/10 p-4 text-sm text-fg">
                    <p className="font-semibold">{t('domains.deletionWaiting', { name: pending.name })}</p>
                    <p className="mt-1">{pending.message}</p>
                    <Button variant="secondary" className="mt-3" onClick={reload}>
                        {t('domains.checkDeletionStatus')}
                    </Button>
                </div>
            )))}

            <RemoteGate
                remote={list.remote}
                checking={t('domains.checking')}
                failed={t('domains.unknown')}
                onRetry={reload}
                busy={list.reading}
                className="py-1"
            >
                {(shown) => (domains.length === 0 ? (
                <KnownEmpty
                    of={shown}
                    icon={Globe}
                    title={t('domains.empty')}
                    hint={dnsRequirementText ?? t('domains.emptyHint')}
                    action={!isTeamMember && (
                        dnsBlocked !== null ? (
                            // The honest next step is not a dead Add button but
                            // the page where the requirement is met.
                            // Dürüst sonraki adım ölü bir Ekle düğmesi değil,
                            // gereksinimin karşılandığı sayfadır.
                            <Button variant="primary" icon={Settings} onClick={openDNSRequirement}>
                                {dnsBlocked === 'engine'
                                    ? t('err.DNS_SERVER_REQUIRED.action')
                                    : t('err.DNS_SETTINGS_REQUIRED.action')}
                            </Button>
                        ) : (
                            <Button variant="primary" icon={Plus} onClick={() => setShowAddModal(true)}>
                                {t('domains.add')}
                            </Button>
                        )
                    )}
                />
            ) : (
                <div className="rounded-xl border border-border-strong bg-surface">
                    <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border p-3">
                        {!isTeamMember && <div className="flex items-center gap-2">
                            {addButton}
                            {selected.length > 0 && (
                                <Button
                                    variant="danger"
                                    icon={Trash2}
                                    disabled={shown.stale}
                                    onClick={() => {
                                        const names = filtered.filter((d) => selected.includes(d.id));
                                        if (confirm(t('domains.confirmDelete', { name: `${selected.length}` }))) {
                                            names.forEach((d) => handleDelete(d.id, d.domain_name));
                                        }
                                    }}
                                >
                                    {t('common.remove')} ({selected.length})
                                </Button>
                            )}
                        </div>}
                        <SearchInput value={query} onChange={setQuery} placeholder={t('domains.search')} />
                    </div>

                    <p className="px-4 pt-3 text-xs text-fg-subtle">
                        {t('common.itemsTotal', { n: filtered.length })}
                    </p>

                    <div className="overflow-x-auto">
                        <table className="w-full text-sm">
                            <thead>
                                <tr className="border-b border-border text-left text-xs font-semibold text-fg-muted">
                                    {!isTeamMember && <th className="w-10 px-4 py-2.5">
                                        <input
                                            type="checkbox"
                                            checked={allSelected}
                                            disabled={shown.stale}
                                            onChange={() =>
                                                setSelected(allSelected ? [] : filtered.map((d) => d.id))
                                            }
                                            className="h-4 w-4 accent-primary"
                                        />
                                    </th>}
                                    <th className="px-4 py-2.5">{t('domains.col.name')}</th>
                                    <th className="px-4 py-2.5">{t('domains.col.php')}</th>
                                    <th className="px-4 py-2.5 text-right">{t('domains.col.disk')}</th>
                                    <th className="px-4 py-2.5 text-right">{t('domains.col.traffic')}</th>
                                    <th className="px-4 py-2.5">{t('domains.col.status')}</th>
                                    <th className="px-4 py-2.5" />
                                </tr>
                            </thead>
                            <tbody>
                                {filtered.map((d) => (
                                    <tr key={d.id} className="border-b border-border last:border-0 hover:bg-surface-2/60">
                                        {!isTeamMember && <td className="px-4 py-3">
                                            <input
                                                type="checkbox"
                                                checked={selected.includes(d.id)}
                                                disabled={shown.stale}
                                                onChange={() =>
                                                    setSelected((s) =>
                                                        s.includes(d.id) ? s.filter((x) => x !== d.id) : [...s, d.id],
                                                    )
                                                }
                                                className="h-4 w-4 accent-primary"
                                            />
                                        </td>}
                                        <td className="px-4 py-3">
                                            <div className="flex items-center gap-2">
                                                {canView(d, 'ssl') && d.ssl_enabled ? (
                                                    <Lock className="h-4 w-4 shrink-0 text-success" />
                                                ) : (
                                                    <Globe className="h-4 w-4 shrink-0 text-fg-subtle" />
                                                )}
                                                <button
                                                    onClick={() =>
                                                        navigate(`/domains/${encodeURIComponent(d.domain_name)}`)
                                                    }
                                                    className="text-base font-medium text-primary hover:underline"
                                                >
                                                    {d.domain_name}
                                                </button>
                                                {d.parent_id ? (
                                                    <span className="rounded-md bg-surface-2 px-1.5 py-0.5 text-xs font-medium text-fg-subtle">
                                                        {t('domains.subdomain')}
                                                    </span>
                                                ) : null}
                                            </div>
                                        </td>
                                        <td className="px-4 py-3">
                                            {canView(d, 'php') ? (
                                                <>
                                                    <span className={`rounded-md px-2 py-0.5 text-xs font-medium ${typeBadge[d.project_type || 'php'] ?? 'bg-surface-2 text-fg-muted'}`}>
                                                        {d.project_type || 'php'}
                                                    </span>
                                                    {(d.project_type || 'php') === 'php' && d.php_version && (
                                                        <span className="ml-1.5 text-xs text-fg-subtle">{d.php_version}</span>
                                                    )}
                                                </>
                                            ) : '—'}
                                        </td>
                                        <td className="px-4 py-3 text-right text-fg-muted">
                                            {canView(d, 'statistics') ? fmtBytes(d.disk_usage) : '—'}
                                        </td>
                                        <td className="px-4 py-3 text-right text-fg-muted">
                                            {canView(d, 'statistics') ? `${fmtBytes(d.bandwidth)}/mo` : '—'}
                                        </td>
                                        <td className="px-4 py-3">
                                            <span className="inline-flex items-center gap-1.5 text-fg-muted">
                                                <StatusDot ok={d.status === 'active'} />
                                                {d.status === 'active' ? t('domains.status.active') : d.status}
                                            </span>
                                        </td>
                                        <td className="px-4 py-3">
                                            <div className="flex items-center justify-end gap-0.5">
                                                {canView(d, 'files') && <IconAction
                                                    href={`https://${d.domain_name}`}
                                                    title={t('domains.action.visit')}
                                                >
                                                    <ExternalLink className="h-4 w-4" />
                                                </IconAction>}
                                                <IconAction
                                                    onClick={() =>
                                                        navigate(`/domains/${encodeURIComponent(d.domain_name)}`)
                                                    }
                                                    title={t('domains.action.manage')}
                                                >
                                                    <Settings className="h-4 w-4" />
                                                </IconAction>
                                                {!isTeamMember && <IconAction
                                                    onClick={() => handleDelete(d.id, d.domain_name)}
                                                    title={t('domains.action.delete')}
                                                    danger
                                                    disabled={shown.stale}
                                                >
                                                    <Trash2 className="h-4 w-4" />
                                                </IconAction>}
                                            </div>
                                        </td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    </div>

                    <div className="border-t border-border px-4 py-2.5 text-xs text-fg-subtle">
                        {t('common.itemsTotal', { n: filtered.length })}
                    </div>
                </div>
                ))}
            </RemoteGate>

            {!isTeamMember && showAddModal && (
                <AddDomainModal
                    onClose={() => setShowAddModal(false)}
                    onSuccess={() => {
                        setShowAddModal(false);
                        reload();
                    }}
                />
            )}
        </div>
    );
}

function IconAction({
    children,
    title,
    href,
    onClick,
    danger,
    disabled,
}: {
    children: React.ReactNode;
    title: string;
    href?: string;
    onClick?: () => void;
    danger?: boolean;
    disabled?: boolean;
}) {
    const cls = `rounded-md p-1.5 text-fg-subtle transition-colors hover:bg-surface-2 disabled:pointer-events-none disabled:opacity-40 ${
        danger ? 'hover:text-danger' : 'hover:text-primary'
    }`;
    if (href) {
        return (
            <a href={href} target="_blank" rel="noopener noreferrer" title={title} className={cls}>
                {children}
            </a>
        );
    }
    return (
        <button onClick={onClick} title={title} disabled={disabled} className={cls}>
            {children}
        </button>
    );
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


// A compact usage strip: the caller's subscription(s) with measured disk
// against the plan limit and resource counts. Real numbers straight from
// the quota system that gates creation — what you see is what's enforced.
// Kompakt kullanım şeridi: çağıranın aboneliği/leri, plan limitine karşı
// ölçülen disk ve kaynak sayıları. Oluşturmayı kapılayan kota sisteminden
// gelen gerçek sayılar — gördüğün, uygulanandır.
interface SubUsage {
    disk_used_bytes: number;
    disk_limit_bytes: number;
    domains: number;
    domains_limit: number;
    databases: number;
    databases_limit: number;
    mail_accounts: number;
    mail_limit: number;
}
interface SubRow {
    id: number;
    name: string;
    owner: string;
    usage?: SubUsage;
}

function decodeSubscriptions(raw: unknown): SubRow[] {
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) throw new Error('shape');
    return decodeList<SubRow>((raw as { subscriptions?: unknown }).subscriptions ?? null);
}

function SubscriptionUsage() {
    const { t } = useI18n();
    const usage = useRemote('/api/v1/subscriptions', decodeSubscriptions);

    // The strip exists only for subscriptions that carry measured usage, so
    // while that is being read there is nothing to announce. A failed read is
    // said in one line with Retry rather than drawn as "no usage".
    // Şerit yalnız ölçülmüş kullanımı olan abonelikler için vardır; okunurken
    // duyurulacak bir şey yoktur. Başarısız okuma "kullanım yok" diye
    // çizilmez; Tekrar dene ile tek satırda söylenir.
    if (usage.remote.state === 'loading') return null;
    if (usage.remote.state === 'unknown') {
        return (
            <CouldNotCheck
                className="mb-5"
                text={t('quota.unknown')}
                onRetry={() => void usage.retry()}
                busy={usage.reading}
            />
        );
    }
    const subs = usage.remote.value;

    const withUsage = subs.filter((s) => s.usage);
    if (withUsage.length === 0) return null;

    return (
        <div className="mb-5 grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {withUsage.map((s) => {
                const u = s.usage!;
                const unlimited = u.disk_limit_bytes <= 0;
                const pct = unlimited ? 0 : Math.min(100, (u.disk_used_bytes / u.disk_limit_bytes) * 100);
                return (
                    <div key={s.id} className="rounded-xl border border-border bg-surface p-4">
                        <div className="mb-2 flex items-center gap-2">
                            <HardDrive className="h-4 w-4 text-primary" />
                            <span className="truncate text-base font-semibold text-fg">{s.name}</span>
                        </div>
                        {unlimited ? (
                            <p className="text-sm text-fg-muted">
                                {fmtBytes(u.disk_used_bytes)} · {t('quota.unlimited')}
                            </p>
                        ) : (
                            <>
                                <UsageBar percent={pct} />
                                <p className="mt-1.5 text-xs text-fg-muted">
                                    {t('quota.diskOf', { used: fmtBytes(u.disk_used_bytes), total: fmtBytes(u.disk_limit_bytes) })}
                                </p>
                            </>
                        )}
                        <div className="mt-2 flex flex-wrap gap-x-4 gap-y-0.5 text-xs text-fg-subtle">
                            <span>{t('quota.domains', { n: u.domains, max: u.domains_limit })}</span>
                            <span>{t('quota.databases', { n: u.databases, max: u.databases_limit })}</span>
                            <span>{t('quota.mail', { n: u.mail_accounts, max: u.mail_limit })}</span>
                        </div>
                    </div>
                );
            })}
        </div>
    );
}
