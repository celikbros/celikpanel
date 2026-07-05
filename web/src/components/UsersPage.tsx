import { useState, useEffect } from 'react';
import { Users, Plus, Trash2, LogIn, Pause, Play, Pencil, Save, X, Layers } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import { useAuth } from '../auth/AuthContext';
import { type PanelUser, type ServicePlan } from '../lib/api';
import { PageHeader, Button, EmptyState, StatusDot, inputClass } from './ui';

// Account management: the admin/reseller view over the role hierarchy.
// Everything here mirrors what the API enforces — role options, visibility
// and quota errors all come from the server.
//
// Hesap yönetimi: rol hiyerarşisi üzerinde admin/bayi görünümü. Buradaki her
// şey API'nin uyguladığını yansıtır — rol seçenekleri, görünürlük ve kota
// hataları sunucudan gelir.
export function UsersPage() {
    const { t } = useI18n();
    const { role } = useAuth();
    const isAdmin = role === 'admin';
    const [tab, setTab] = useState<'accounts' | 'plans'>('accounts');

    return (
        <div className="p-6 md:p-8">
            <PageHeader
                title={t('nav.users')}
                subtitle={t('users.subtitle')}
                breadcrumb={[t('common.home'), t('nav.users')]}
            />

            <div className="mb-4 flex items-center gap-1 border-b border-border">
                <Tab active={tab === 'accounts'} onClick={() => setTab('accounts')} label={t('users.tab.accounts')} />
                {isAdmin && <Tab active={tab === 'plans'} onClick={() => setTab('plans')} label={t('users.tab.plans')} />}
            </div>

            {tab === 'accounts' ? <AccountsTab isAdmin={isAdmin} /> : <PlansTab />}
        </div>
    );
}

function AccountsTab({ isAdmin }: { isAdmin: boolean }) {
    const { t } = useI18n();
    const [users, setUsers] = useState<PanelUser[]>([]);
    const [plans, setPlans] = useState<ServicePlan[]>([]);
    const [loading, setLoading] = useState(true);
    const [showForm, setShowForm] = useState(false);

    const [username, setUsername] = useState('');
    const [email, setEmail] = useState('');
    const [password, setPassword] = useState('');
    const [newRole, setNewRole] = useState('customer');
    const [planID, setPlanID] = useState(0);
    const [saving, setSaving] = useState(false);

    const load = async () => {
        try {
            const [ur, pr] = await Promise.all([
                fetch('/api/v1/users').then((r) => (r.ok ? r.json() : { users: [] })),
                fetch('/api/v1/plans').then((r) => (r.ok ? r.json() : { plans: [] })),
            ]);
            setUsers(ur.users || []);
            setPlans(pr.plans || []);
        } catch {
            showToast('error', t('common.error'));
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        load();
    }, []);

    // Conflict answers (quota, children, duplicates) carry real reasons from
    // the API; show them as-is instead of a vague "something went wrong".
    // Çakışma yanıtları (kota, alt hesap, mükerrer) API'den gerçek nedenlerle
    // gelir; belirsiz bir hata yerine olduğu gibi göster.
    const apiError = async (res: Response) => {
        const text = (await res.text()).trim();
        showToast('error', res.status === 409 || res.status === 400 ? text : t('common.error'));
    };

    const createUser = async () => {
        setSaving(true);
        try {
            const body: Record<string, unknown> = { username, email, password, role: newRole };
            if (planID > 0) body.plan_id = planID;
            const res = await fetch('/api/v1/users', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(body),
            });
            if (!res.ok) {
                await apiError(res);
                return;
            }
            showToast('success', t('users.created'));
            setShowForm(false);
            setUsername('');
            setEmail('');
            setPassword('');
            load();
        } finally {
            setSaving(false);
        }
    };

    const setStatus = async (u: PanelUser, status: 'active' | 'suspended') => {
        if (status === 'suspended' && !confirm(t('users.suspendConfirm', { name: u.username }))) return;
        const res = await fetch(`/api/v1/users/${u.id}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ status }),
        });
        if (!res.ok) {
            await apiError(res);
            return;
        }
        showToast('success', status === 'suspended' ? t('users.suspended') : t('users.activated'));
        load();
    };

    const deleteUser = async (u: PanelUser) => {
        if (!confirm(t('users.deleteConfirm', { name: u.username }))) return;
        const res = await fetch(`/api/v1/users/${u.id}`, { method: 'DELETE' });
        if (!res.ok) {
            await apiError(res);
            return;
        }
        showToast('success', t('users.deleted'));
        load();
    };

    const impersonate = async (u: PanelUser) => {
        const res = await fetch(`/api/v1/users/${u.id}/impersonate`, { method: 'POST' });
        if (!res.ok) {
            await apiError(res);
            return;
        }
        // Full reload: the whole shell re-derives from the new session.
        // Tam yenileme: kabuk yeni oturumdan baştan türesin.
        window.location.assign('/');
    };

    const roleKey = (r: string): TranslationKey =>
        r === 'admin' ? 'users.role.admin' : r === 'reseller' ? 'users.role.reseller' : 'users.role.customer';

    return (
        <div>
            <div className="mb-3 flex items-center justify-between">
                <span className="text-xs text-fg-subtle">{t('common.itemsTotal', { n: users.length })}</span>
                <Button variant="primary" icon={Plus} onClick={() => setShowForm((s) => !s)}>
                    {t('users.add')}
                </Button>
            </div>

            {showForm && (
                <div className="mb-4 rounded-xl border border-border bg-surface-2/50 p-4">
                    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-5">
                        <label>
                            <span className="mb-1 block text-xs text-fg-muted">{t('users.form.username')}</span>
                            <input value={username} onChange={(e) => setUsername(e.target.value)} className={inputClass} autoFocus />
                        </label>
                        <label>
                            <span className="mb-1 block text-xs text-fg-muted">{t('users.form.email')}</span>
                            <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} className={inputClass} />
                        </label>
                        <label>
                            <span className="mb-1 block text-xs text-fg-muted">{t('users.form.password')}</span>
                            <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} className={inputClass} />
                        </label>
                        <label>
                            <span className="mb-1 block text-xs text-fg-muted">{t('users.form.role')}</span>
                            <select
                                value={newRole}
                                onChange={(e) => setNewRole(e.target.value)}
                                className={inputClass}
                                disabled={!isAdmin}
                            >
                                <option value="customer">{t('users.role.customer')}</option>
                                {isAdmin && <option value="reseller">{t('users.role.reseller')}</option>}
                            </select>
                        </label>
                        <label>
                            <span className="mb-1 block text-xs text-fg-muted">{t('users.form.plan')}</span>
                            <select value={planID} onChange={(e) => setPlanID(Number(e.target.value))} className={inputClass}>
                                <option value={0}>{t('users.form.noPlan')}</option>
                                {plans.map((p) => (
                                    <option key={p.id} value={p.id}>
                                        {p.name}
                                    </option>
                                ))}
                            </select>
                        </label>
                    </div>
                    <div className="mt-3 flex justify-end gap-2">
                        <Button onClick={() => setShowForm(false)}>{t('users.cancel')}</Button>
                        <Button
                            variant="primary"
                            icon={Plus}
                            onClick={createUser}
                            disabled={saving || !username || !email || password.length < 8}
                        >
                            {t('users.create')}
                        </Button>
                    </div>
                </div>
            )}

            {loading ? (
                <div className="flex items-center justify-center py-16">
                    <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary" />
                </div>
            ) : users.length === 0 ? (
                <EmptyState icon={Users} title={t('users.empty')} hint={t('users.emptyHint')} />
            ) : (
                <div className="overflow-x-auto rounded-xl border border-border bg-surface shadow-card">
                    <table className="w-full text-sm">
                        <thead>
                            <tr className="border-b border-border text-left text-xs font-semibold text-fg-muted">
                                <th className="px-4 py-2.5">{t('users.col.user')}</th>
                                <th className="px-4 py-2.5">{t('users.col.role')}</th>
                                <th className="px-4 py-2.5">{t('users.col.status')}</th>
                                {isAdmin && <th className="px-4 py-2.5">{t('users.col.parent')}</th>}
                                <th className="px-4 py-2.5">{t('users.col.usage')}</th>
                                <th className="px-4 py-2.5" />
                            </tr>
                        </thead>
                        <tbody>
                            {users.map((u) => (
                                <tr key={u.id} className="border-b border-border last:border-0 hover:bg-surface-2/60">
                                    <td className="px-4 py-2.5">
                                        <div className="font-medium text-fg">{u.username}</div>
                                        <div className="text-xs text-fg-subtle">{u.email}</div>
                                    </td>
                                    <td className="px-4 py-2.5">
                                        <span
                                            className={`rounded-md px-2 py-0.5 text-xs font-medium ${
                                                u.role === 'admin'
                                                    ? 'bg-danger/10 text-danger'
                                                    : u.role === 'reseller'
                                                      ? 'bg-warning/15 text-warning'
                                                      : 'bg-primary/10 text-primary'
                                            }`}
                                        >
                                            {t(roleKey(u.role))}
                                        </span>
                                    </td>
                                    <td className="px-4 py-2.5">
                                        <span className="inline-flex items-center gap-1.5 text-fg-muted">
                                            <StatusDot ok={u.status === 'active'} />
                                            {u.status === 'active' ? t('users.status.active') : t('users.status.suspended')}
                                        </span>
                                    </td>
                                    {isAdmin && <td className="px-4 py-2.5 text-fg-muted">{u.parent_name || '—'}</td>}
                                    <td className="px-4 py-2.5 text-fg-muted">
                                        {u.subscriptions} / {u.domains}
                                    </td>
                                    <td className="px-4 py-2.5">
                                        {u.role !== 'admin' && (
                                            <div className="flex items-center justify-end gap-0.5">
                                                <RowBtn title={t('users.loginAs')} onClick={() => impersonate(u)}>
                                                    <LogIn className="h-4 w-4" />
                                                </RowBtn>
                                                {u.status === 'active' ? (
                                                    <RowBtn title={t('users.suspend')} onClick={() => setStatus(u, 'suspended')}>
                                                        <Pause className="h-4 w-4" />
                                                    </RowBtn>
                                                ) : (
                                                    <RowBtn title={t('users.activate')} onClick={() => setStatus(u, 'active')}>
                                                        <Play className="h-4 w-4" />
                                                    </RowBtn>
                                                )}
                                                <RowBtn danger title={t('users.delete')} onClick={() => deleteUser(u)}>
                                                    <Trash2 className="h-4 w-4" />
                                                </RowBtn>
                                            </div>
                                        )}
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

const emptyPlan: ServicePlan = {
    id: 0,
    name: '',
    max_domains: 5,
    max_databases: 10,
    max_email_accounts: 50,
    disk_quota_mb: 10240,
    bandwidth_quota_mb: 102400,
};

function PlansTab() {
    const { t } = useI18n();
    const [plans, setPlans] = useState<ServicePlan[]>([]);
    const [loading, setLoading] = useState(true);
    const [editing, setEditing] = useState<ServicePlan | null>(null);

    const load = () =>
        fetch('/api/v1/plans')
            .then((r) => (r.ok ? r.json() : { plans: [] }))
            .then((d) => setPlans(d.plans || []))
            .catch(() => showToast('error', t('common.error')))
            .finally(() => setLoading(false));

    useEffect(() => {
        load();
    }, []);

    const save = async () => {
        if (!editing) return;
        const isNew = editing.id === 0;
        const res = await fetch(isNew ? '/api/v1/plans' : `/api/v1/plans/${editing.id}`, {
            method: isNew ? 'POST' : 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(editing),
        });
        if (!res.ok) {
            showToast('error', (await res.text()).trim() || t('common.error'));
            return;
        }
        showToast('success', isNew ? t('plans.created') : t('plans.updated'));
        setEditing(null);
        load();
    };

    const remove = async (p: ServicePlan) => {
        if (!confirm(`${p.name}?`)) return;
        const res = await fetch(`/api/v1/plans/${p.id}`, { method: 'DELETE' });
        if (!res.ok) {
            showToast('error', (await res.text()).trim() || t('common.error'));
            return;
        }
        showToast('success', t('plans.deleted'));
        load();
    };

    const fmtGB = (mb: number) => (mb >= 1024 ? `${(mb / 1024).toFixed(0)} GB` : `${mb} MB`);

    return (
        <div>
            <div className="mb-3 flex items-center justify-between">
                <span className="text-xs text-fg-subtle">{t('common.itemsTotal', { n: plans.length })}</span>
                <Button variant="primary" icon={Plus} onClick={() => setEditing({ ...emptyPlan })}>
                    {t('plans.add')}
                </Button>
            </div>

            {editing && (
                <div className="mb-4 rounded-xl border border-border bg-surface-2/50 p-4">
                    <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
                        <label className="col-span-2 sm:col-span-3 lg:col-span-1">
                            <span className="mb-1 block text-xs text-fg-muted">{t('plans.form.name')}</span>
                            <input value={editing.name} onChange={(e) => setEditing({ ...editing, name: e.target.value })} className={inputClass} autoFocus />
                        </label>
                        <NumField label={t('plans.col.domains')} value={editing.max_domains} onChange={(v) => setEditing({ ...editing, max_domains: v })} />
                        <NumField label={t('plans.col.databases')} value={editing.max_databases} onChange={(v) => setEditing({ ...editing, max_databases: v })} />
                        <NumField label={t('plans.col.mail')} value={editing.max_email_accounts} onChange={(v) => setEditing({ ...editing, max_email_accounts: v })} />
                        <NumField label={`${t('plans.col.disk')} (MB)`} value={editing.disk_quota_mb} onChange={(v) => setEditing({ ...editing, disk_quota_mb: v })} />
                        <NumField label={`${t('plans.col.traffic')} (MB)`} value={editing.bandwidth_quota_mb} onChange={(v) => setEditing({ ...editing, bandwidth_quota_mb: v })} />
                    </div>
                    <div className="mt-3 flex justify-end gap-2">
                        <Button icon={X} onClick={() => setEditing(null)}>{t('users.cancel')}</Button>
                        <Button variant="primary" icon={Save} onClick={save} disabled={!editing.name.trim()}>
                            {t('plans.save')}
                        </Button>
                    </div>
                </div>
            )}

            {loading ? (
                <div className="flex items-center justify-center py-16">
                    <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary" />
                </div>
            ) : plans.length === 0 ? (
                <EmptyState icon={Layers} title={t('plans.empty')} hint={t('plans.emptyHint')} />
            ) : (
                <div className="overflow-x-auto rounded-xl border border-border bg-surface shadow-card">
                    <table className="w-full text-sm">
                        <thead>
                            <tr className="border-b border-border text-left text-xs font-semibold text-fg-muted">
                                <th className="px-4 py-2.5">{t('plans.col.name')}</th>
                                <th className="px-4 py-2.5">{t('plans.col.domains')}</th>
                                <th className="px-4 py-2.5">{t('plans.col.databases')}</th>
                                <th className="px-4 py-2.5">{t('plans.col.mail')}</th>
                                <th className="px-4 py-2.5">{t('plans.col.disk')}</th>
                                <th className="px-4 py-2.5">{t('plans.col.traffic')}</th>
                                <th className="px-4 py-2.5">{t('plans.col.subscribers')}</th>
                                <th className="px-4 py-2.5" />
                            </tr>
                        </thead>
                        <tbody>
                            {plans.map((p) => (
                                <tr key={p.id} className="border-b border-border last:border-0 hover:bg-surface-2/60">
                                    <td className="px-4 py-2.5 font-medium text-fg">{p.name}</td>
                                    <td className="px-4 py-2.5 text-fg-muted">{p.max_domains}</td>
                                    <td className="px-4 py-2.5 text-fg-muted">{p.max_databases}</td>
                                    <td className="px-4 py-2.5 text-fg-muted">{p.max_email_accounts}</td>
                                    <td className="px-4 py-2.5 text-fg-muted">{fmtGB(p.disk_quota_mb)}</td>
                                    <td className="px-4 py-2.5 text-fg-muted">{fmtGB(p.bandwidth_quota_mb)}</td>
                                    <td className="px-4 py-2.5 text-fg-muted">{p.subscribers ?? 0}</td>
                                    <td className="px-4 py-2.5">
                                        <div className="flex items-center justify-end gap-0.5">
                                            <RowBtn title={t('plans.edit')} onClick={() => setEditing({ ...p })}>
                                                <Pencil className="h-4 w-4" />
                                            </RowBtn>
                                            <RowBtn danger title={t('users.delete')} onClick={() => remove(p)}>
                                                <Trash2 className="h-4 w-4" />
                                            </RowBtn>
                                        </div>
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

function Tab({ active, onClick, label }: { active: boolean; onClick: () => void; label: string }) {
    return (
        <button
            onClick={onClick}
            className={`-mb-px border-b-2 px-3 py-2.5 text-sm font-medium transition-colors ${
                active ? 'border-primary text-primary' : 'border-transparent text-fg-muted hover:text-fg'
            }`}
        >
            {label}
        </button>
    );
}

function RowBtn({ children, title, onClick, danger }: { children: React.ReactNode; title: string; onClick: () => void; danger?: boolean }) {
    return (
        <button
            title={title}
            onClick={onClick}
            className={`rounded-md p-1.5 text-fg-subtle transition-colors hover:bg-surface-2 ${danger ? 'hover:text-danger' : 'hover:text-fg'}`}
        >
            {children}
        </button>
    );
}

function NumField({ label, value, onChange }: { label: string; value: number; onChange: (v: number) => void }) {
    return (
        <label>
            <span className="mb-1 block text-xs text-fg-muted">{label}</span>
            <input type="number" value={value} onChange={(e) => onChange(parseInt(e.target.value) || 0)} className={inputClass} />
        </label>
    );
}
