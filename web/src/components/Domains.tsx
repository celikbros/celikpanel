import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Globe, Plus, Trash2, ExternalLink, Settings, Lock } from 'lucide-react';
import { AddDomainModal } from './AddDomainModal';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { PageHeader, Button, SearchInput, EmptyState, StatusDot } from './ui';

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
            <PageHeader
                title={t('nav.domains')}
                subtitle={t('domains.subtitle')}
                breadcrumb={[t('common.home'), t('nav.domains')]}
                actions={
                    <Button variant="primary" icon={Plus} onClick={() => setShowAddModal(true)}>
                        {t('domains.add')}
                    </Button>
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
                    hint={t('domains.emptyHint')}
                    action={
                        <Button variant="primary" icon={Plus} onClick={() => setShowAddModal(true)}>
                            {t('domains.add')}
                        </Button>
                    }
                />
            ) : (
                <div className="rounded-xl border border-border bg-surface shadow-card">
                    <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border p-3">
                        <div className="flex items-center gap-2">
                            <Button variant="primary" icon={Plus} onClick={() => setShowAddModal(true)}>
                                {t('domains.add')}
                            </Button>
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
                                            </div>
                                        </td>
                                        <td className="px-4 py-2.5 text-fg-muted">{d.php_version || '—'}</td>
                                        <td className="px-4 py-2.5 text-right text-fg-muted">{fmtMB(d.disk_usage)}</td>
                                        <td className="px-4 py-2.5 text-right text-fg-muted">
                                            {fmtMB(d.bandwidth)}/mo
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

function fmtMB(bytes: number = 0): string {
    if (!bytes) return '0 MB';
    const mb = bytes / (1024 * 1024);
    return mb < 1024 ? `${mb.toFixed(1)} MB` : `${(mb / 1024).toFixed(2)} GB`;
}
