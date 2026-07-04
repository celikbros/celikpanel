import { useState, useEffect } from 'react';
import { Globe, Plus, Trash2 } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { Button, EmptyState, inputClass } from './ui';

interface DNSRecord {
    id: number;
    name: string;
    type: string;
    content: string;
    ttl: number;
    prio?: number;
    disabled: boolean;
}

interface DomainDNSManagerProps {
    domainId: number;
    domainName: string;
}

const recordTypes = ['A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS', 'SRV'];

// Record-type pill colours. Categorical, readable in both themes; kept small
// and intentional rather than one hue per type.
// Kayıt-türü rozet renkleri. Kategorik, iki temada okunur; tür başına bir
// renk yerine küçük ve bilinçli tutuldu.
const typeColor: Record<string, string> = {
    A: 'bg-primary/10 text-primary',
    AAAA: 'bg-primary/10 text-primary',
    CNAME: 'bg-success/10 text-success',
    MX: 'bg-warning/15 text-warning',
    SRV: 'bg-warning/15 text-warning',
};

export function DomainDNSManager({ domainId, domainName }: DomainDNSManagerProps) {
    const { t } = useI18n();
    const [records, setRecords] = useState<DNSRecord[]>([]);
    const [loading, setLoading] = useState(true);
    const [zoneExists, setZoneExists] = useState<boolean | null>(null);
    const [showAddForm, setShowAddForm] = useState(false);
    const [newType, setNewType] = useState('A');
    const [newName, setNewName] = useState('@');
    const [newContent, setNewContent] = useState('');
    const [newTTL, setNewTTL] = useState(3600);
    const [newPrio, setNewPrio] = useState(0);

    useEffect(() => {
        checkZone();
    }, [domainId]);

    const checkZone = async () => {
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/dns/zone`);
            if (res.ok) {
                setZoneExists(true);
                loadRecords();
            } else {
                setZoneExists(false);
                setLoading(false);
            }
        } catch {
            setZoneExists(false);
            setLoading(false);
        }
    };

    const loadRecords = async () => {
        setLoading(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/dns/records`);
            if (res.ok) {
                const data = await res.json();
                setRecords(data.records || []);
            }
        } catch {
            showToast('error', t('common.error'));
        } finally {
            setLoading(false);
        }
    };

    const createZone = async () => {
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/dns/zone`, { method: 'POST' });
            if (!res.ok) throw new Error();
            showToast('success', t('dns.zoneCreated'));
            setZoneExists(true);
            loadRecords();
        } catch {
            showToast('error', t('dns.zoneCreateFailed'));
        }
    };

    const addRecord = async () => {
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/dns/records`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name: newName, type: newType, content: newContent, ttl: newTTL, prio: newPrio }),
            });
            if (!res.ok) throw new Error();
            showToast('success', t('dns.recordAdded'));
            setShowAddForm(false);
            setNewContent('');
            loadRecords();
        } catch {
            showToast('error', t('dns.recordAddFailed'));
        }
    };

    const deleteRecord = async (id: number) => {
        if (!confirm(t('dns.confirmDelete'))) return;
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/dns/records?id=${id}`, { method: 'DELETE' });
            if (!res.ok) throw new Error();
            showToast('success', t('dns.recordDeleted'));
            loadRecords();
        } catch {
            showToast('error', t('dns.recordDeleteFailed'));
        }
    };

    if (loading && zoneExists === null) {
        return <p className="text-sm text-fg-muted">{t('dns.checking')}</p>;
    }

    if (zoneExists === false) {
        return (
            <EmptyState
                icon={Globe}
                title={t('dns.zoneMissing')}
                hint={t('dns.zoneMissingHint', { name: domainName })}
                action={
                    <Button variant="primary" icon={Plus} onClick={createZone}>
                        {t('dns.enableZone')}
                    </Button>
                }
            />
        );
    }

    const needsPrio = newType === 'MX' || newType === 'SRV';

    return (
        <div>
            <div className="mb-3 flex items-center justify-between">
                <span className="text-xs text-fg-subtle">{t('common.itemsTotal', { n: records.length })}</span>
                <Button variant="primary" icon={Plus} onClick={() => setShowAddForm((s) => !s)}>
                    {t('dns.addRecord')}
                </Button>
            </div>

            {showAddForm && (
                <div className="mb-4 rounded-lg border border-border bg-surface-2/50 p-4">
                    <div className="grid grid-cols-1 gap-3 sm:grid-cols-12">
                        <label className="sm:col-span-2">
                            <span className="mb-1 block text-xs text-fg-muted">{t('dns.type')}</span>
                            <select value={newType} onChange={(e) => setNewType(e.target.value)} className={inputClass}>
                                {recordTypes.map((rt) => (
                                    <option key={rt} value={rt}>
                                        {rt}
                                    </option>
                                ))}
                            </select>
                        </label>
                        <label className="sm:col-span-3">
                            <span className="mb-1 block text-xs text-fg-muted">{t('dns.name')}</span>
                            <input value={newName} onChange={(e) => setNewName(e.target.value)} placeholder={t('dns.nameHint')} className={inputClass} />
                        </label>
                        <label className={needsPrio ? 'sm:col-span-4' : 'sm:col-span-5'}>
                            <span className="mb-1 block text-xs text-fg-muted">{t('dns.content')}</span>
                            <input value={newContent} onChange={(e) => setNewContent(e.target.value)} placeholder="192.168.1.1" className={inputClass} />
                        </label>
                        <label className="sm:col-span-2">
                            <span className="mb-1 block text-xs text-fg-muted">{t('dns.ttl')}</span>
                            <input type="number" value={newTTL} onChange={(e) => setNewTTL(parseInt(e.target.value))} className={inputClass} />
                        </label>
                        {needsPrio && (
                            <label className="sm:col-span-1">
                                <span className="mb-1 block text-xs text-fg-muted">{t('dns.priority')}</span>
                                <input type="number" value={newPrio} onChange={(e) => setNewPrio(parseInt(e.target.value))} className={inputClass} />
                            </label>
                        )}
                    </div>
                    <div className="mt-3 flex justify-end gap-2">
                        <Button variant="secondary" onClick={() => setShowAddForm(false)}>
                            {t('dns.cancel')}
                        </Button>
                        <Button variant="primary" icon={Plus} onClick={addRecord}>
                            {t('dns.save')}
                        </Button>
                    </div>
                </div>
            )}

            {records.length === 0 ? (
                <EmptyState icon={Globe} title={t('dns.noRecords')} />
            ) : (
                <div className="overflow-x-auto rounded-lg border border-border">
                    <table className="w-full text-sm">
                        <thead>
                            <tr className="border-b border-border text-left text-xs font-semibold text-fg-muted">
                                <th className="px-4 py-2.5">{t('dns.type')}</th>
                                <th className="px-4 py-2.5">{t('dns.name')}</th>
                                <th className="px-4 py-2.5">{t('dns.content')}</th>
                                <th className="px-4 py-2.5">{t('dns.ttl')}</th>
                                <th className="px-4 py-2.5" />
                            </tr>
                        </thead>
                        <tbody>
                            {records.map((rec) => (
                                <tr key={rec.id} className="border-b border-border last:border-0 hover:bg-surface-2/60">
                                    <td className="px-4 py-2.5">
                                        <span className={`rounded px-1.5 py-0.5 text-xs font-semibold ${typeColor[rec.type] ?? 'bg-surface-2 text-fg-muted'}`}>
                                            {rec.type}
                                        </span>
                                    </td>
                                    <td className="px-4 py-2.5 font-medium text-fg">{rec.name}</td>
                                    <td className="break-all px-4 py-2.5 font-mono text-fg-muted">
                                        {rec.prio ? <span className="mr-1.5 text-warning">[{rec.prio}]</span> : null}
                                        {rec.content}
                                    </td>
                                    <td className="px-4 py-2.5 text-fg-muted">{rec.ttl}</td>
                                    <td className="px-4 py-2.5 text-right">
                                        <button
                                            onClick={() => deleteRecord(rec.id)}
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
    );
}
