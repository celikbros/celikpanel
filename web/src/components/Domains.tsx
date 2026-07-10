import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Globe, Plus, Trash2, ExternalLink, Settings, Lock, HardDrive } from 'lucide-react';
import { AddDomainModal } from './AddDomainModal';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { PageHeader, Button, SearchInput, EmptyState, StatusDot, UsageBar } from './ui';

// Type badge colours: categorical, readable in both themes.
// Tip rozeti renkleri: kategorik, iki temada da okunur.
const typeBadge: Record<string, string> = {
    php: 'bg-primary/10 text-primary',
    static: 'bg-surface-2 text-fg-muted',
    node: 'bg-success/10 text-success',
    proxy: 'bg-warning/15 text-warning',
    forwarding: 'bg-warning/15 text-warning',
    dnsonly: 'bg-teal-100 text-teal-700 dark:bg-teal-500/15 dark:text-teal-300',
};

interface Domain {
    id: number;
    domain_name: string;
    php_version: string;
    ssl_enabled: boolean;
    status: string;
    project_type?: string;
    created_at: string;
    disk_usage?: number;
    bandwidth?: number;
    parent_id?: number | null;
}

const API_BASE = '/api/v1';

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
    const [domains, setDomains] = useState<Domain[]>([]);
    const [loading, setLoading] = useState(true);
    const [showAddModal, setShowAddModal] = useState(false);
    const [query, setQuery] = useState('');
    const [selected, setSelected] = useState<number[]>([]);

    // D-009 on the page itself, not only inside the dialog: with no DNS
    // server installed, the Add buttons are disabled and the empty state
    // guides to Services — a button that only leads to a wall is a small
    // ghost. null = still loading (buttons stay enabled; the dialog and the
    // backend both guard anyway, so nothing can slip through).
    // D-009 yalnız pencerede değil sayfanın kendisinde: DNS sunucusu kurulu
    // değilken Ekle düğmeleri pasiftir ve boş durum Servisler'e yönlendirir —
    // yalnızca duvara götüren düğme küçük bir hayalettir. null = hâlâ
    // yükleniyor (düğmeler açık kalır; pencere ve backend zaten koruyor,
    // hiçbir şey sızamaz).
    const [dnsServer, setDnsServer] = useState<string | null>(null);
    useEffect(() => {
        fetch(`${API_BASE}/hosting/capabilities`)
            .then((r) => (r.ok ? r.json() : null))
            .then((c) => setDnsServer(c ? (c.dns_server ?? '') : null))
            .catch(() => setDnsServer(null));
    }, []);
    const dnsMissing = dnsServer === '';

    useEffect(() => {
        loadDomains();
    }, []);

    const loadDomains = async () => {
        setLoading(true);
        try {
            const res = await fetch(`${API_BASE}/domains`);
            if (!res.ok) throw new Error();
            setDomains((await res.json()) || []);
        } catch {
            showToast('error', t('domains.loadFailed'));
        } finally {
            setLoading(false);
        }
    };

    const handleDelete = async (id: number, name: string) => {
        if (!confirm(t('domains.confirmDelete', { name }))) return;
        try {
            const res = await fetch(`${API_BASE}/domains/${id}`, { method: 'DELETE' });
            if (!res.ok) throw new Error();
            showToast('success', t('domains.deleted', { name }));
            setSelected((s) => s.filter((x) => x !== id));
            loadDomains();
        } catch {
            showToast('error', t('common.error'));
        }
    };

    const filtered = domains.filter((d) => d.domain_name.toLowerCase().includes(query.toLowerCase()));
    const allSelected = filtered.length > 0 && selected.length === filtered.length;

    return (
        <div className="p-6 md:p-8">
            <SubscriptionUsage />
            <PageHeader
                title={t('nav.domains')}
                subtitle={t('domains.subtitle')}
                breadcrumb={[t('common.home'), t('nav.domains')]}
                actions={
                    <span title={dnsMissing ? t('domains.add.needsDns') : undefined}>
                        <Button variant="primary" icon={Plus} disabled={dnsMissing} onClick={() => setShowAddModal(true)}>
                            {t('domains.add')}
                        </Button>
                    </span>
                }
            />

            {loading ? (
                <div className="flex items-center justify-center py-16">
                    <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary" />
                </div>
            ) : domains.length === 0 ? (
                <EmptyState
                    icon={Globe}
                    title={t('domains.empty')}
                    hint={dnsMissing ? t('domains.add.needsDns') : t('domains.emptyHint')}
                    action={
                        dnsMissing ? (
                            // The honest next step is not a dead Add button but
                            // the page where the requirement is met.
                            // Dürüst sonraki adım ölü bir Ekle düğmesi değil,
                            // gereksinimin karşılandığı sayfadır.
                            <Button variant="primary" icon={Settings} onClick={() => navigate('/services')}>
                                {t('domains.goServices')}
                            </Button>
                        ) : (
                            <Button variant="primary" icon={Plus} onClick={() => setShowAddModal(true)}>
                                {t('domains.add')}
                            </Button>
                        )
                    }
                />
            ) : (
                <div className="rounded-xl border border-border bg-surface shadow-card">
                    <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border p-3">
                        <div className="flex items-center gap-2">
                            <span title={dnsMissing ? t('domains.add.needsDns') : undefined}>
                                <Button variant="primary" icon={Plus} disabled={dnsMissing} onClick={() => setShowAddModal(true)}>
                                    {t('domains.add')}
                                </Button>
                            </span>
                            {selected.length > 0 && (
                                <Button
                                    variant="danger"
                                    icon={Trash2}
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
                        </div>
                        <SearchInput value={query} onChange={setQuery} placeholder={t('domains.search')} />
                    </div>

                    <p className="px-4 pt-3 text-xs text-fg-subtle">
                        {t('common.itemsTotal', { n: filtered.length })}
                    </p>

                    <div className="overflow-x-auto">
                        <table className="w-full text-sm">
                            <thead>
                                <tr className="border-b border-border text-left text-xs font-semibold text-fg-muted">
                                    <th className="w-10 px-4 py-2.5">
                                        <input
                                            type="checkbox"
                                            checked={allSelected}
                                            onChange={() =>
                                                setSelected(allSelected ? [] : filtered.map((d) => d.id))
                                            }
                                            className="h-4 w-4 accent-primary"
                                        />
                                    </th>
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
                                        <td className="px-4 py-2.5">
                                            <input
                                                type="checkbox"
                                                checked={selected.includes(d.id)}
                                                onChange={() =>
                                                    setSelected((s) =>
                                                        s.includes(d.id) ? s.filter((x) => x !== d.id) : [...s, d.id],
                                                    )
                                                }
                                                className="h-4 w-4 accent-primary"
                                            />
                                        </td>
                                        <td className="px-4 py-2.5">
                                            <div className="flex items-center gap-2">
                                                {d.ssl_enabled ? (
                                                    <Lock className="h-4 w-4 shrink-0 text-success" />
                                                ) : (
                                                    <Globe className="h-4 w-4 shrink-0 text-fg-subtle" />
                                                )}
                                                <button
                                                    onClick={() =>
                                                        navigate(`/domains/${encodeURIComponent(d.domain_name)}`)
                                                    }
                                                    className="font-medium text-primary hover:underline"
                                                >
                                                    {d.domain_name}
                                                </button>
                                                {d.parent_id ? (
                                                    <span className="rounded-md bg-surface-2 px-1.5 py-0.5 text-[11px] font-medium text-fg-subtle">
                                                        {t('domains.subdomain')}
                                                    </span>
                                                ) : null}
                                            </div>
                                        </td>
                                        <td className="px-4 py-2.5">
                                            <span className={`rounded-md px-2 py-0.5 text-xs font-medium ${typeBadge[d.project_type || 'php'] ?? 'bg-surface-2 text-fg-muted'}`}>
                                                {d.project_type || 'php'}
                                            </span>
                                            {(d.project_type || 'php') === 'php' && d.php_version && (
                                                <span className="ml-1.5 text-xs text-fg-subtle">{d.php_version}</span>
                                            )}
                                        </td>
                                        <td className="px-4 py-2.5 text-right text-fg-muted">{fmtBytes(d.disk_usage)}</td>
                                        <td className="px-4 py-2.5 text-right text-fg-muted">
                                            {fmtBytes(d.bandwidth)}/mo
                                        </td>
                                        <td className="px-4 py-2.5">
                                            <span className="inline-flex items-center gap-1.5 text-fg-muted">
                                                <StatusDot ok={d.status === 'active'} />
                                                {d.status === 'active' ? t('domains.status.active') : d.status}
                                            </span>
                                        </td>
                                        <td className="px-4 py-2.5">
                                            <div className="flex items-center justify-end gap-0.5">
                                                <IconAction
                                                    href={`https://${d.domain_name}`}
                                                    title={t('domains.action.visit')}
                                                >
                                                    <ExternalLink className="h-4 w-4" />
                                                </IconAction>
                                                <IconAction
                                                    onClick={() =>
                                                        navigate(`/domains/${encodeURIComponent(d.domain_name)}`)
                                                    }
                                                    title={t('domains.action.manage')}
                                                >
                                                    <Settings className="h-4 w-4" />
                                                </IconAction>
                                                <IconAction
                                                    onClick={() => handleDelete(d.id, d.domain_name)}
                                                    title={t('domains.action.delete')}
                                                    danger
                                                >
                                                    <Trash2 className="h-4 w-4" />
                                                </IconAction>
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
            )}

            {showAddModal && (
                <AddDomainModal
                    onClose={() => setShowAddModal(false)}
                    onSuccess={() => {
                        setShowAddModal(false);
                        loadDomains();
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
}: {
    children: React.ReactNode;
    title: string;
    href?: string;
    onClick?: () => void;
    danger?: boolean;
}) {
    const cls = `rounded-md p-1.5 text-fg-subtle transition-colors hover:bg-surface-2 ${
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
        <button onClick={onClick} title={title} className={cls}>
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

function SubscriptionUsage() {
    const { t } = useI18n();
    const [subs, setSubs] = useState<SubRow[]>([]);

    useEffect(() => {
        fetch('/api/v1/subscriptions')
            .then((r) => (r.ok ? r.json() : null))
            .then((d) => setSubs(d?.subscriptions || []))
            .catch(() => {});
    }, []);

    const withUsage = subs.filter((s) => s.usage);
    if (withUsage.length === 0) return null;

    return (
        <div className="mb-5 grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {withUsage.map((s) => {
                const u = s.usage!;
                const unlimited = u.disk_limit_bytes <= 0;
                const pct = unlimited ? 0 : Math.min(100, (u.disk_used_bytes / u.disk_limit_bytes) * 100);
                return (
                    <div key={s.id} className="rounded-xl border border-border bg-surface p-4 shadow-card">
                        <div className="mb-2 flex items-center gap-2">
                            <HardDrive className="h-4 w-4 text-primary" />
                            <span className="truncate text-sm font-semibold text-fg">{s.name}</span>
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
