import { useState, useEffect } from 'react';
import { Mail, Plus, Trash2, ArrowRight, AtSign, Pencil, Info } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { Button, EmptyState, UsageBar, inputClass } from './ui';
import { MailAuthPanel } from './MailAuthPanel';
import { MailSettingsPanel } from './MailSettingsPanel';

interface EmailAccount {
    id: number;
    address: string;
    quota_mb: number;
}

interface QuotaUsage {
    email: string;
    used_kb: number;
    limit_kb: number;
    available: boolean;
}

interface QuotaStatus {
    plugin_enabled: boolean;
    usages: QuotaUsage[];
}

interface Forwarding {
    id: number;
    source: string;
    destination: string;
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
    const [activeTab, setActiveTab] = useState<'accounts' | 'forwarding' | 'auth' | 'settings'>('accounts');
    const [showForm, setShowForm] = useState(false);

    const [user, setUser] = useState('');
    const [pass, setPass] = useState('');
    const [quota, setQuota] = useState(1024);
    const [fwdSource, setFwdSource] = useState('');
    const [fwdDest, setFwdDest] = useState('');

    // Live quota usage arrives separately from the (fast) account list; the
    // doveadm calls behind it can be slow with many mailboxes.
    // Canlı kota kullanımı (hızlı) hesap listesinden ayrı gelir; arkasındaki
    // doveadm çağrıları çok kutuda yavaş olabilir.
    const [quotaStatus, setQuotaStatus] = useState<QuotaStatus | null>(null);
    const [editingQuota, setEditingQuota] = useState<number | null>(null);
    const [quotaDraft, setQuotaDraft] = useState(1024);

    useEffect(() => {
        loadData();
        setShowForm(false);
    }, [domainId, activeTab]);

    const loadData = async () => {
        // The auth tab owns its own data loading (MailAuthPanel).
        // Auth sekmesi kendi veri yüklemesine sahiptir (MailAuthPanel).
        if (activeTab === 'auth' || activeTab === 'settings') {
            setLoading(false);
            return;
        }
        setLoading(true);
        try {
            if (activeTab === 'accounts') {
                const res = await fetch(`/api/v1/domains/${domainId}/mail/accounts`);
                if (res.ok) setAccounts((await res.json()).accounts || []);
                fetch(`/api/v1/domains/${domainId}/mail/quota`)
                    .then((r) => (r.ok ? r.json() : null))
                    .then(setQuotaStatus)
                    .catch(() => {});
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

    const saveQuota = async (id: number) => {
        if (quotaDraft <= 0) return;
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/mail/accounts`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ id, quota_mb: quotaDraft }),
            });
            if (!res.ok) throw new Error();
            showToast('success', t('mail.quotaUpdated'));
            setEditingQuota(null);
            loadData();
        } catch {
            showToast('error', t('common.error'));
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
                <Tab active={activeTab === 'auth'} onClick={() => setActiveTab('auth')} label={t('mailauth.tab')} />
                <Tab active={activeTab === 'settings'} onClick={() => setActiveTab('settings')} label={t('mail.tab.settings')} />
            </div>

            {activeTab === 'auth' ? (
                <>
                    <DeliverabilityCard domainId={domainId} />
                    <MailAuthPanel domainId={domainId} />
                </>
            ) : activeTab === 'settings' ? (
                <MailSettingsPanel domainId={domainId} domainName={domainName} />
            ) : (
                <>
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
                    <>
                        {quotaStatus && !quotaStatus.plugin_enabled && (
                            <p className="mb-3 flex items-start gap-2 rounded-lg border border-warning/30 bg-warning/10 px-3 py-2 text-xs text-fg-muted">
                                <Info className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning" />
                                {t('mail.quotaNotEnforced')}
                            </p>
                        )}
                        <div className="overflow-x-auto rounded-lg border border-border">
                            <table className="w-full text-sm">
                                <thead>
                                    <tr className="border-b border-border text-left text-xs font-semibold text-fg-muted">
                                        <th className="px-4 py-2.5">{t('mail.col.address')}</th>
                                        <th className="px-4 py-2.5">{t('mail.col.quota')}</th>
                                        <th className="w-44 px-4 py-2.5">{t('mail.col.usage')}</th>
                                        <th className="px-4 py-2.5" />
                                    </tr>
                                </thead>
                                <tbody>
                                    {accounts.map((a) => {
                                        const usage = quotaStatus?.usages.find((u) => u.email === a.address);
                                        const usedMB = usage?.available ? usage.used_kb / 1024 : null;
                                        return (
                                            <tr key={a.id} className="border-b border-border last:border-0 hover:bg-surface-2/60">
                                                <td className="px-4 py-2.5">
                                                    <span className="flex items-center gap-2 font-medium text-fg">
                                                        <AtSign className="h-4 w-4 text-fg-subtle" />
                                                        {a.address}
                                                    </span>
                                                </td>
                                                <td className="px-4 py-2.5 text-fg-muted">
                                                    {editingQuota === a.id ? (
                                                        <span className="flex items-center gap-2">
                                                            <input
                                                                type="number"
                                                                value={quotaDraft}
                                                                onChange={(e) => setQuotaDraft(parseInt(e.target.value))}
                                                                onKeyDown={(e) => e.key === 'Enter' && saveQuota(a.id)}
                                                                className={`${inputClass} w-24 py-1`}
                                                                autoFocus
                                                            />
                                                            <span className="text-xs">MB</span>
                                                            <Button variant="primary" onClick={() => saveQuota(a.id)}>
                                                                {t('mail.saveQuota')}
                                                            </Button>
                                                            <Button onClick={() => setEditingQuota(null)}>{t('mail.cancel')}</Button>
                                                        </span>
                                                    ) : (
                                                        <span className="flex items-center gap-1.5">
                                                            {a.quota_mb} MB
                                                            <button
                                                                onClick={() => {
                                                                    setEditingQuota(a.id);
                                                                    setQuotaDraft(a.quota_mb);
                                                                }}
                                                                title={t('mail.editQuota')}
                                                                className="rounded p-1 text-fg-subtle hover:bg-surface-2 hover:text-fg"
                                                            >
                                                                <Pencil className="h-3.5 w-3.5" />
                                                            </button>
                                                        </span>
                                                    )}
                                                </td>
                                                <td className="px-4 py-2.5">
                                                    {usedMB !== null ? (
                                                        <div className="flex items-center gap-2">
                                                            <div className="w-20">
                                                                <UsageBar percent={a.quota_mb > 0 ? (usedMB / a.quota_mb) * 100 : 0} />
                                                            </div>
                                                            <span className="whitespace-nowrap text-xs text-fg-muted">
                                                                {usedMB < 1 ? '<1' : Math.round(usedMB)} / {a.quota_mb} MB
                                                            </span>
                                                        </div>
                                                    ) : (
                                                        <span className="text-fg-subtle">—</span>
                                                    )}
                                                </td>
                                                <td className="px-4 py-2.5 text-right">
                                                    <DeleteBtn onClick={() => deleteAccount(a.id, a.address)} />
                                                </td>
                                            </tr>
                                        );
                                    })}
                                </tbody>
                            </table>
                        </div>
                    </>
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
                </>
            )}
        </div>
    );
}

function Tab({ active, onClick, label, count }: { active: boolean; onClick: () => void; label: string; count?: number }) {
    return (
        <button
            onClick={onClick}
            className={`-mb-px flex items-center gap-2 border-b-2 px-3 py-2.5 text-sm font-medium transition-colors ${
                active ? 'border-primary text-primary' : 'border-transparent text-fg-muted hover:text-fg'
            }`}
        >
            {label}
            {count !== undefined && (
                <span className="rounded-full bg-surface-2 px-1.5 py-0.5 text-[11px] text-fg-muted">{count}</span>
            )}
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


// One traffic-light card for "will it land in the inbox": PTR, HELO, TLS,
// port 25, SPF/DKIM/DMARC, blacklists, mail certificate, DNSSEC. PTR cannot
// be fixed here — the card spells out what to enter at the hosting provider.
// "Gelen kutusuna düşer mi" için tek trafik-ışığı kartı. PTR buradan
// düzeltilemez — kart, barındırma sağlayıcısında ne girileceğini yazar.
function DeliverabilityCard({ domainId }: { domainId: number }) {
    const { t } = useI18n();
    const [data, setData] = useState<{
        overall: string;
        server_ip: string;
        expected_ptr: string;
        checks: { id: string; status: string; detail?: string }[];
    } | null>(null);

    useEffect(() => {
        fetch(`/api/v1/domains/${domainId}/mail/health`)
            .then((r) => (r.ok ? r.json() : null))
            .then(setData)
            .catch(() => {});
    }, [domainId]);

    if (!data) return null;

    const tone: Record<string, string> = {
        ok: 'bg-success', warn: 'bg-warning', fail: 'bg-danger', unknown: 'bg-fg-subtle',
    };
    const ptr = data.checks.find((c) => c.id === 'ptr');

    return (
        <section className="mb-5 rounded-xl border border-border bg-surface p-5">
            <div className="mb-3 flex items-center gap-2">
                <span className={`h-2.5 w-2.5 rounded-full ${tone[data.overall]}`} />
                <h3 className="text-sm font-semibold text-fg">{t('mail.health.title')}</h3>
            </div>
            <div className="grid grid-cols-1 gap-x-6 gap-y-1.5 sm:grid-cols-2">
                {data.checks.map((c) => (
                    <div key={c.id} className="flex items-center gap-2 text-sm">
                        <span className={`h-2 w-2 shrink-0 rounded-full ${tone[c.status] ?? 'bg-fg-subtle'}`} />
                        <span className="text-fg">{t(`mail.health.${c.id}` as Parameters<typeof t>[0])}</span>
                        {c.detail && <span className="truncate text-xs text-fg-subtle">{c.detail}</span>}
                    </div>
                ))}
            </div>
            {ptr && ptr.status !== 'ok' && (
                <p className="mt-3 rounded-lg bg-warning/10 px-3 py-2 text-xs text-fg-muted">
                    {t('mail.health.ptrFix', { ip: data.server_ip, host: data.expected_ptr })}
                </p>
            )}
        </section>
    );
}
