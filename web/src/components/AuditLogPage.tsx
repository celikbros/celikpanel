import { useEffect, useState } from 'react';
import { ScrollText } from 'lucide-react';
import { useI18n } from '../i18n';
import { PageHeader, EmptyState } from './ui';

interface AuditEntry {
    id: number;
    username: string;
    action: string;
    resource_type?: string;
    resource_id?: number;
    ip_address?: string;
    created_at: string;
}

// The audit trail: a server-wide, newest-first history of who did what, from
// where. Admin-only. Read straight from the audit_logs table the panel now
// writes to on every sensitive action.
// Denetim izi: sunucu-geneli, en yeniden eskiye, kim nereden ne yaptı. Yalnız
// admin. Panelin artık her hassas eylemde yazdığı audit_logs tablosundan
// doğrudan okunur.
export function AuditLogPage() {
    const { t } = useI18n();
    const [entries, setEntries] = useState<AuditEntry[]>([]);
    const [loading, setLoading] = useState(true);

    useEffect(() => {
        fetch('/api/v1/audit-logs?limit=200')
            .then((r) => (r.ok ? r.json() : { entries: [] }))
            .then((d) => setEntries(d.entries || []))
            .catch(() => {})
            .finally(() => setLoading(false));
    }, []);

    // action is stored as "verb:detail" — split for a readable two-part view.
    // action "fiil:detay" olarak saklanır — okunur iki parçalı görünüm için ayır.
    const splitAction = (a: string): [string, string] => {
        const i = a.indexOf(':');
        return i < 0 ? [a, ''] : [a.slice(0, i), a.slice(i + 1)];
    };

    return (
        <div className="p-6 md:p-8">
            <PageHeader
                title={t('audit.title')}
                subtitle={t('audit.subtitle')}
                breadcrumb={[t('common.home'), t('audit.title')]}
            />

            {loading ? (
                <div className="flex items-center justify-center py-16">
                    <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary" />
                </div>
            ) : entries.length === 0 ? (
                <EmptyState icon={ScrollText} title={t('audit.empty')} />
            ) : (
                <div className="overflow-x-auto rounded-xl border border-border bg-surface shadow-card">
                    <table className="w-full text-sm">
                        <thead>
                            <tr className="border-b border-border text-left text-xs font-semibold text-fg-muted">
                                <th className="px-4 py-2.5">{t('audit.col.time')}</th>
                                <th className="px-4 py-2.5">{t('audit.col.user')}</th>
                                <th className="px-4 py-2.5">{t('audit.col.action')}</th>
                                <th className="px-4 py-2.5">{t('audit.col.detail')}</th>
                                <th className="px-4 py-2.5">{t('audit.col.ip')}</th>
                            </tr>
                        </thead>
                        <tbody>
                            {entries.map((e) => {
                                const [verb, detail] = splitAction(e.action);
                                return (
                                    <tr key={e.id} className="border-b border-border last:border-0 hover:bg-surface-2/60">
                                        <td className="whitespace-nowrap px-4 py-3 text-fg-muted">
                                            {new Date(e.created_at.replace(' ', 'T') + 'Z').toLocaleString()}
                                        </td>
                                        <td className="px-4 py-3 text-base font-medium text-fg">{e.username}</td>
                                        <td className="px-4 py-3">
                                            <span className="rounded-md bg-surface-2 px-2 py-0.5 font-mono text-xs text-fg-muted">
                                                {verb}
                                            </span>
                                        </td>
                                        <td className="px-4 py-3 text-fg-muted">
                                            {detail || (e.resource_type ? `${e.resource_type} #${e.resource_id ?? ''}` : '—')}
                                        </td>
                                        <td className="whitespace-nowrap px-4 py-3 font-mono text-xs text-fg-subtle">
                                            {e.ip_address || '—'}
                                        </td>
                                    </tr>
                                );
                            })}
                        </tbody>
                    </table>
                </div>
            )}
        </div>
    );
}
