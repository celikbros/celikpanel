import { useState, useEffect } from 'react';
import { Mail, Activity, Trash2, RefreshCw, RotateCw } from 'lucide-react';
import { ServiceShell } from './ServiceShell';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { Button, StatusDot } from './ui';
import type { TranslationKey } from '../i18n/en';

interface PostfixManagementProps {
    onBack: () => void;
}

interface PostfixQueueItem {
    id: string;
    size: string;
    sender: string;
    arrival: string;
    status: string;
}

interface PostfixSummary {
    active: number;
    deferred: number;
    hold: number;
    corrupt: number;
}

export function PostfixManagement({ onBack }: PostfixManagementProps) {
    const { t } = useI18n();
    const [activeTab, setActiveTab] = useState<'queue' | 'logs'>('queue');
    const [queue, setQueue] = useState<PostfixQueueItem[]>([]);
    const [summary, setSummary] = useState<PostfixSummary | null>(null);

    const loadQueue = async () => {
        try {
            const [q, s] = await Promise.all([
                fetch('/api/v1/postfix/queue').then((r) => (r.ok ? r.json() : [])),
                fetch('/api/v1/postfix/summary').then((r) => (r.ok ? r.json() : null)),
            ]);
            setQueue(q || []);
            setSummary(s);
        } catch {
            /* silent */
        }
    };

    useEffect(() => {
        loadQueue();
    }, []);

    const queueAction = async (action: string, id?: string) => {
        const msg =
            action === 'flush' ? t('postfix.confirmFlush') : action === 'delete_all' ? t('postfix.confirmDeleteAll') : t('postfix.confirmDelete', { id: id ?? '' });
        if (!confirm(msg)) return;
        try {
            await fetch('/api/v1/postfix/queue', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ action, id }),
            });
            showToast('success', t('postfix.done'));
            loadQueue();
        } catch {
            showToast('error', t('common.error'));
        }
    };

    return (
        <ServiceShell serviceId="postfix" name="Postfix" icon={Mail} onBack={onBack}>
            <div className="mb-4 flex items-center gap-1 border-b border-border">
                <Tab active={activeTab === 'queue'} onClick={() => setActiveTab('queue')} icon={Mail} label={t('postfix.tab.queue')} />
                <Tab active={activeTab === 'logs'} onClick={() => setActiveTab('logs')} icon={Activity} label={t('postfix.tab.logs')} />
            </div>

            {activeTab === 'logs' ? (
                <div className="rounded-xl border border-border bg-surface p-10 text-center text-fg-subtle shadow-card">
                    <Activity className="mx-auto mb-3 h-10 w-10 opacity-40" />
                    <p>{t('postfix.logsSoon')}</p>
                </div>
            ) : (
                <div>
                    {summary && (
                        <div className="mb-4 grid grid-cols-2 gap-3 lg:grid-cols-4">
                            <Stat labelKey="postfix.active" value={summary.active} />
                            <Stat labelKey="postfix.deferred" value={summary.deferred} accent={summary.deferred > 0} />
                            <Stat labelKey="postfix.hold" value={summary.hold} />
                            <Stat labelKey="postfix.corrupt" value={summary.corrupt} danger={summary.corrupt > 0} />
                        </div>
                    )}

                    <div className="mb-3 flex items-center justify-end gap-2">
                        <Button variant="secondary" icon={RefreshCw} onClick={loadQueue}>
                            {t('postfix.refresh')}
                        </Button>
                        <Button variant="secondary" icon={RotateCw} onClick={() => queueAction('flush')}>
                            {t('postfix.flush')}
                        </Button>
                        <Button variant="danger" icon={Trash2} onClick={() => queueAction('delete_all')} disabled={queue.length === 0}>
                            {t('postfix.deleteAll')}
                        </Button>
                    </div>

                    {queue.length === 0 ? (
                        <div className="rounded-xl border border-border bg-surface p-12 text-center shadow-card">
                            <Mail className="mx-auto mb-3 h-10 w-10 text-fg-subtle" />
                            <p className="text-fg-muted">{t('postfix.empty')}</p>
                        </div>
                    ) : (
                        <div className="overflow-x-auto rounded-xl border border-border bg-surface shadow-card">
                            <table className="w-full text-sm">
                                <thead>
                                    <tr className="border-b border-border text-left text-xs font-semibold text-fg-muted">
                                        <th className="px-4 py-2.5">ID</th>
                                        <th className="px-4 py-2.5">{t('postfix.col.sender')}</th>
                                        <th className="px-4 py-2.5">{t('postfix.col.size')}</th>
                                        <th className="px-4 py-2.5">{t('postfix.col.arrival')}</th>
                                        <th className="px-4 py-2.5">{t('domains.col.status')}</th>
                                        <th className="px-4 py-2.5" />
                                    </tr>
                                </thead>
                                <tbody>
                                    {queue.map((item) => (
                                        <tr key={item.id} className="border-b border-border last:border-0 hover:bg-surface-2/60">
                                            <td className="px-4 py-2.5 font-mono text-fg">{item.id}</td>
                                            <td className="px-4 py-2.5 text-fg-muted">{item.sender}</td>
                                            <td className="px-4 py-2.5 text-fg-muted">{item.size}</td>
                                            <td className="px-4 py-2.5 text-fg-subtle">{item.arrival}</td>
                                            <td className="px-4 py-2.5">
                                                <span className="inline-flex items-center gap-1.5 text-fg-muted">
                                                    <StatusDot ok={item.status === 'active'} />
                                                    {item.status}
                                                </span>
                                            </td>
                                            <td className="px-4 py-2.5 text-right">
                                                <button
                                                    onClick={() => queueAction('delete_id', item.id)}
                                                    className="rounded-md p-1.5 text-fg-subtle transition-colors hover:bg-surface-2 hover:text-danger"
                                                >
                                                    <Trash2 className="h-4 w-4" />
                                                </button>
                                            </td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                        </div>
                    )}
                </div>
            )}
        </ServiceShell>
    );
}

function Tab({ active, onClick, icon: Icon, label }: { active: boolean; onClick: () => void; icon: typeof Mail; label: string }) {
    return (
        <button
            onClick={onClick}
            className={`-mb-px flex items-center gap-2 border-b-2 px-3 py-2.5 text-sm font-medium transition-colors ${
                active ? 'border-primary text-primary' : 'border-transparent text-fg-muted hover:text-fg'
            }`}
        >
            <Icon className="h-4 w-4" />
            {label}
        </button>
    );
}

function Stat({ labelKey, value, accent, danger }: { labelKey: TranslationKey; value: number; accent?: boolean; danger?: boolean }) {
    const { t } = useI18n();
    const color = danger ? 'text-danger' : accent ? 'text-warning' : 'text-fg';
    return (
        <div className="rounded-xl border border-border bg-surface p-4 shadow-card">
            <p className="text-xs font-medium uppercase tracking-wide text-fg-subtle">{t(labelKey)}</p>
            <p className={`mt-1 text-2xl font-bold ${color}`}>{value}</p>
        </div>
    );
}
