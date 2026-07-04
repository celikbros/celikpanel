import { useState, useEffect } from 'react';
import { Mail, Plus, Trash2, ArrowRight, AtSign } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { Button, EmptyState, inputClass } from './ui';

interface EmailAccount {
    id: number;
    address: string;
    quota_mb: number;
    created_at: string;
}

interface Forwarding {
    id: number;
    source: string;
    destination: string;
    created_at: string;
}

interface DomainMailManagerProps {
    domainId: number;
    domainName: string;
}

export function DomainMailManager({ domainId, domainName }: DomainMailManagerProps) {
    const { t } = useI18n();
    const [accounts, setAccounts] = useState<EmailAccount[]>([]);
    const [forwardings, setForwardings] = useState<Forwarding[]>([]);
    const [loading, setLoading] = useState(true);
    const [activeTab, setActiveTab] = useState<'accounts' | 'forwarding'>('accounts');
    const [showForm, setShowForm] = useState(false);

    const [user, setUser] = useState('');
    const [pass, setPass] = useState('');
    const [quota, setQuota] = useState(1024);
    const [fwdSource, setFwdSource] = useState('');
    const [fwdDest, setFwdDest] = useState('');

    useEffect(() => {
        loadData();
        setShowForm(false);
    }, [domainId, activeTab]);

    const loadData = async () => {
        setLoading(true);
        try {
            if (activeTab === 'accounts') {
                const res = await fetch(`/api/v1/domains/${domainId}/mail/accounts`);
                if (res.ok) setAccounts((await res.json()).accounts || []);
            } else {
                const res = await fetch(`/api/v1/domains/${domainId}/mail/forwardings`);
                if (res.ok) setForwardings((await res.json()).forwardings || []);
            }
        } catch {
            showToast('error', t('mail.loadFailed'));
        } finally {
            setLoading(false);
        }
    };

    const createAccount = async () => {
        if (!user || !pass) return;
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/mail/accounts`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ address: `${user}@${domainName}`, password: pass, quota_mb: quota }),
            });
            const data = await res.json();
            if (!data.success) throw new Error(data.error);
            showToast('success', t('mail.accountCreated'));
            setShowForm(false);
            setUser('');
            setPass('');
            loadData();
        } catch {
            showToast('error', t('mail.createFailed'));
        }
    };

    const deleteAccount = async (id: number, address: string) => {
        if (!confirm(t('mail.confirmDeleteAccount', { name: address }))) return;
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/mail/accounts?id=${id}`, { method: 'DELETE' });
            if (!res.ok) throw new Error();
            showToast('success', t('mail.accountDeleted'));
            loadData();
        } catch {
            showToast('error', t('common.error'));
        }
    };

    const createForwarding = async () => {
        if (!fwdSource || !fwdDest) return;
        try {
            const source = fwdSource.includes('@') ? fwdSource : `${fwdSource}@${domainName}`;
            const res = await fetch(`/api/v1/domains/${domainId}/mail/forwardings`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ source, destination: fwdDest }),
            });
            const data = await res.json();
            if (!data.success) throw new Error(data.error);
            showToast('success', t('mail.forwarderCreated'));
            setShowForm(false);
            setFwdSource('');
            setFwdDest('');
            loadData();
        } catch {
            showToast('error', t('mail.forwarderFailed'));
        }
    };

    const deleteForwarding = async (id: number, source: string) => {
        if (!confirm(t('mail.confirmDeleteForwarder', { name: source }))) return;
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/mail/forwardings?id=${id}`, { method: 'DELETE' });
            if (!res.ok) throw new Error();
            showToast('success', t('mail.forwarderDeleted'));
            loadData();
        } catch {
            showToast('error', t('common.error'));
        }
    };

    const count = activeTab === 'accounts' ? accounts.length : forwardings.length;

    return (
        <div>
            <div className="mb-4 flex items-center gap-1 border-b border-border">
                <Tab active={activeTab === 'accounts'} onClick={() => setActiveTab('accounts')} label={t('mail.tab.accounts')} count={accounts.length} />
                <Tab active={activeTab === 'forwarding'} onClick={() => setActiveTab('forwarding')} label={t('mail.tab.forwarding')} count={forwardings.length} />
            </div>

            <div className="mb-3 flex items-center justify-between">
                <span className="text-xs text-fg-subtle">{t('common.itemsTotal', { n: count })}</span>
                <Button variant="primary" icon={Plus} onClick={() => setShowForm((s) => !s)}>
                    {activeTab === 'accounts' ? t('mail.addAccount') : t('mail.addForwarder')}
                </Button>
            </div>

            {showForm && (
                <div className="mb-4 rounded-lg border border-border bg-surface-2/50 p-4">
                    {activeTab === 'accounts' ? (
                        <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
                            <label>
                                <span className="mb-1 block text-xs text-fg-muted">{t('mail.username')}</span>
                                <div className="flex items-center rounded-lg border border-border bg-surface focus-within:border-primary focus-within:ring-2 focus-within:ring-primary/30">
                                    <input value={user} onChange={(e) => setUser(e.target.value)} placeholder="info" className="min-w-0 flex-1 bg-transparent px-3 py-2 text-sm text-fg outline-none" />
                                    <span className="whitespace-nowrap px-2 text-sm text-fg-subtle">@{domainName}</span>
                                </div>
                            </label>
                            <label>
                                <span className="mb-1 block text-xs text-fg-muted">{t('mail.password')}</span>
                                <input type="password" value={pass} onChange={(e) => setPass(e.target.value)} placeholder="••••••••" className={inputClass} />
                            </label>
                            <label>
                                <span className="mb-1 block text-xs text-fg-muted">{t('mail.quota')}</span>
                                <input type="number" value={quota} onChange={(e) => setQuota(parseInt(e.target.value))} className={inputClass} />
                            </label>
                        </div>
                    ) : (
                        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                            <label>
                                <span className="mb-1 block text-xs text-fg-muted">{t('mail.source')}</span>
                                <input value={fwdSource} onChange={(e) => setFwdSource(e.target.value)} placeholder={`info@${domainName}`} className={inputClass} />
                            </label>
                            <label>
                                <span className="mb-1 block text-xs text-fg-muted">{t('mail.destination')}</span>
                                <input value={fwdDest} onChange={(e) => setFwdDest(e.target.value)} placeholder="personal@gmail.com" className={inputClass} />
                            </label>
                        </div>
                    )}
                    <div className="mt-3 flex justify-end gap-2">
                        <Button variant="secondary" onClick={() => setShowForm(false)}>
                            {t('mail.cancel')}
                        </Button>
                        <Button variant="primary" icon={Plus} onClick={activeTab === 'accounts' ? createAccount : createForwarding}>
                            {t('mail.create')}
                        </Button>
                    </div>
                </div>
            )}

            {loading ? (
                <div className="flex items-center justify-center py-12">
                    <div className="h-7 w-7 animate-spin rounded-full border-b-2 border-primary" />
                </div>
            ) : activeTab === 'accounts' ? (
                accounts.length === 0 ? (
                    <EmptyState icon={Mail} title={t('mail.emptyAccounts')} />
                ) : (
                    <div className="overflow-x-auto rounded-lg border border-border">
                        <table className="w-full text-sm">
                            <thead>
                                <tr className="border-b border-border text-left text-xs font-semibold text-fg-muted">
                                    <th className="px-4 py-2.5">{t('mail.col.address')}</th>
                                    <th className="px-4 py-2.5">{t('mail.col.quota')}</th>
                                    <th className="px-4 py-2.5" />
                                </tr>
                            </thead>
                            <tbody>
                                {accounts.map((a) => (
                                    <tr key={a.id} className="border-b border-border last:border-0 hover:bg-surface-2/60">
                                        <td className="px-4 py-2.5">
                                            <span className="flex items-center gap-2 font-medium text-fg">
                                                <AtSign className="h-4 w-4 text-fg-subtle" />
                                                {a.address}
                                            </span>
                                        </td>
                                        <td className="px-4 py-2.5 text-fg-muted">{a.quota_mb} MB</td>
                                        <td className="px-4 py-2.5 text-right">
                                            <DeleteBtn onClick={() => deleteAccount(a.id, a.address)} />
                                        </td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    </div>
                )
            ) : forwardings.length === 0 ? (
                <EmptyState icon={ArrowRight} title={t('mail.emptyForwarders')} />
            ) : (
                <div className="overflow-x-auto rounded-lg border border-border">
                    <table className="w-full text-sm">
                        <thead>
                            <tr className="border-b border-border text-left text-xs font-semibold text-fg-muted">
                                <th className="px-4 py-2.5">{t('mail.source')}</th>
                                <th className="px-4 py-2.5">{t('mail.col.forwardsTo')}</th>
                                <th className="px-4 py-2.5" />
                            </tr>
                        </thead>
                        <tbody>
                            {forwardings.map((f) => (
                                <tr key={f.id} className="border-b border-border last:border-0 hover:bg-surface-2/60">
                                    <td className="px-4 py-2.5 font-medium text-fg">{f.source}</td>
                                    <td className="px-4 py-2.5">
                                        <span className="flex items-center gap-2 text-fg-muted">
                                            <ArrowRight className="h-4 w-4 text-fg-subtle" />
                                            {f.destination}
                                        </span>
                                    </td>
                                    <td className="px-4 py-2.5 text-right">
                                        <DeleteBtn onClick={() => deleteForwarding(f.id, f.source)} />
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

function Tab({ active, onClick, label, count }: { active: boolean; onClick: () => void; label: string; count: number }) {
    return (
        <button
            onClick={onClick}
            className={`-mb-px flex items-center gap-2 border-b-2 px-3 py-2.5 text-sm font-medium transition-colors ${
                active ? 'border-primary text-primary' : 'border-transparent text-fg-muted hover:text-fg'
            }`}
        >
            {label}
            <span className="rounded-full bg-surface-2 px-1.5 py-0.5 text-[11px] text-fg-muted">{count}</span>
        </button>
    );
}

function DeleteBtn({ onClick }: { onClick: () => void }) {
    return (
        <button onClick={onClick} className="rounded-md p-1.5 text-fg-subtle transition-colors hover:bg-surface-2 hover:text-danger">
            <Trash2 className="h-4 w-4" />
        </button>
    );
}
