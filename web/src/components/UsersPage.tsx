import { useState } from 'react';
import { Users, Plus, Trash2, LogIn, Pause, Play, Pencil, Save, X, Layers } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import { useAuth } from '../auth/AuthContext';
import { type PanelUser, type ServicePlan } from '../lib/api';
import { Button, CouldNotCheck, KnownEmpty, RemoteGate, StatusDot, inputClass } from './ui';
import { PageHeader } from './PageHeader';
import { readApiError, apiErrorText } from '../lib/apiError';
import { countText, mapRemote, useRemote } from '../lib/remote';
import { PLANS_URL, USERS_URL, decodePlans, decodeUsers } from '../lib/accounts';
import { TeamMembersPage } from './TeamMembersPage';

// Account management: the admin/reseller view over the role hierarchy.
// Everything here mirrors what the API enforces — role options, visibility
// and quota errors all come from the server.
//
// Hesap yönetimi: rol hiyerarşisi üzerinde admin/bayi görünümü. Buradaki her
// şey API'nin uyguladığını yansıtır — rol seçenekleri, görünürlük ve kota
// hataları sunucudan gelir.
export function UsersPage() {
    const { role, user } = useAuth();
    if (role === 'customer') {
        if (user?.account_type !== 'account' || user.features.team_members !== true) return null;
        return <TeamMembersPage />;
    }
    if (role !== 'admin' && role !== 'reseller') return null;
    return <AccountUsersPage role={role} />;
}

function AccountUsersPage({ role }: { role: 'admin' | 'reseller' }) {
    const { t } = useI18n();
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
    const users = useRemote(USERS_URL, decodeUsers);
    const plans = useRemote(PLANS_URL, decodePlans);
    const [showForm, setShowForm] = useState(false);

    const [username, setUsername] = useState('');
    const [email, setEmail] = useState('');
    const [password, setPassword] = useState('');
    const [newRole, setNewRole] = useState('customer');
    const [planID, setPlanID] = useState(0);
    const [saving, setSaving] = useState(false);

    const load = () => {
        void users.retry();
        void plans.retry();
    };
    // An account is created with one of the plans the server named, and a row
    // is changed as the server last listed it. Neither is offered on a list
    // that is being read again or could not be read.
    // Hesap, sunucunun adlandırdığı planlardan biriyle oluşturulur; satır da
    // sunucunun son listelediği hâliyle değiştirilir. Liste yeniden okunurken
    // ya da okunamadığında ikisi de sunulmaz.
    const usersKnown = users.remote.state === 'known' && !users.reading;
    const plansKnown = plans.remote.state === 'known';
    const planOptions = plans.remote.state === 'known' ? plans.remote.value : [];

    // Conflict answers (quota, children, duplicates) carry real reasons from
    // the API; the coded contract picks a localized text when the refusal
    // has a code, else the server message — never a vague generic.
    // Çakışma yanıtları (kota, alt hesap, mükerrer) API'den gerçek nedenlerle
    // gelir; kodlu sözleşme, ret kodluysa yerelleştirilmiş metni, değilse
    // sunucu mesajını seçer — asla belirsiz bir genel değil.
    const apiError = async (res: Response) => {
        showToast('error', apiErrorText(await readApiError(res), t));
    };
    // The answer to a change did not arrive: it is not known whether it was
    // made. Nothing is sent again; the list is read so the person can look.
    // Değişikliğin yanıtı gelmedi: yapılıp yapılmadığı bilinmiyor. Hiçbir şey
    // yeniden gönderilmez; kişi bakabilsin diye liste okunur.
    const send = async (url: string, init: RequestInit): Promise<Response | null> => {
        try {
            return await fetch(url, init);
        } catch {
            showToast('error', t('common.resultUnknown'));
            load();
            return null;
        }
    };

    const createUser = async () => {
        setSaving(true);
        try {
            const body: Record<string, unknown> = { username, email, password, role: newRole };
            if (planID > 0) body.plan_id = planID;
            const res = await send('/api/v1/users', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(body),
            });
            if (!res) return;
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
        const res = await send(`/api/v1/users/${u.id}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ status }),
        });
        if (!res) return;
        if (!res.ok) {
            await apiError(res);
            return;
        }
        showToast('success', status === 'suspended' ? t('users.suspended') : t('users.activated'));
        load();
    };

    const deleteUser = async (u: PanelUser) => {
        if (!confirm(t('users.deleteConfirm', { name: u.username }))) return;
        const res = await send(`/api/v1/users/${u.id}`, { method: 'DELETE' });
        if (!res) return;
        if (!res.ok) {
            await apiError(res);
            return;
        }
        showToast('success', t('users.deleted'));
        load();
    };

    const impersonate = async (u: PanelUser) => {
        const res = await send(`/api/v1/users/${u.id}/impersonate`, { method: 'POST' });
        if (!res) return;
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
                <span className="text-xs text-fg-subtle">{t('common.itemsTotal', { n: countText(mapRemote(users.remote, (rows) => rows.length)) })}</span>
                <Button variant="primary" icon={Plus} disabled={!usersKnown || !plansKnown} onClick={() => setShowForm((s) => !s)}>
                    {t('users.add')}
                </Button>
            </div>

            {plans.remote.state === 'unknown' && (
                <CouldNotCheck
                    className="mb-4"
                    text={t('users.plansUnknown')}
                    onRetry={() => void plans.retry()}
                    busy={plans.reading}
                />
            )}

            {showForm && plansKnown && (
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
                                {planOptions.map((p) => (
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
                            disabled={saving || !usersKnown || !username || !email || password.length < 8}
                        >
                            {t('users.create')}
                        </Button>
                    </div>
                </div>
            )}

            <RemoteGate
                remote={users.remote}
                checking={t('users.checking')}
                failed={t('users.unknown')}
                onRetry={() => void users.retry()}
                busy={users.reading}
                className="py-3"
            >
                {(shown) => (shown.value.length === 0 ? (
                    <KnownEmpty of={shown} icon={Users} title={t('users.empty')} hint={t('users.emptyHint')} />
                ) : (
                    <div className="overflow-x-auto rounded-xl border border-border-strong bg-surface">
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
                                {shown.value.map((u) => (
                                    <tr key={u.id} className="border-b border-border last:border-0 hover:bg-surface-2/60">
                                        <td className="px-4 py-3">
                                            <div className="text-base font-medium text-fg">{u.username}</div>
                                            <div className="text-xs text-fg-subtle">{u.email}</div>
                                        </td>
                                        <td className="px-4 py-3">
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
                                        <td className="px-4 py-3">
                                            <span className="inline-flex items-center gap-1.5 text-fg-muted">
                                                <StatusDot ok={u.status === 'active'} />
                                                {u.status === 'active' ? t('users.status.active') : t('users.status.suspended')}
                                            </span>
                                        </td>
                                        {isAdmin && <td className="px-4 py-3 text-fg-muted">{u.parent_name || '—'}</td>}
                                        <td className="px-4 py-3 text-fg-muted">
                                            {u.subscriptions} / {u.domains}
                                        </td>
                                        <td className="px-4 py-3">
                                            {u.role !== 'admin' && (
                                                <div className="flex items-center justify-end gap-0.5">
                                                    <RowBtn disabled={!usersKnown} title={t('users.loginAs')} onClick={() => impersonate(u)}>
                                                        <LogIn className="h-4 w-4" />
                                                    </RowBtn>
                                                    {u.status === 'active' ? (
                                                        <RowBtn disabled={!usersKnown} title={t('users.suspend')} onClick={() => setStatus(u, 'suspended')}>
                                                            <Pause className="h-4 w-4" />
                                                        </RowBtn>
                                                    ) : (
                                                        <RowBtn disabled={!usersKnown} title={t('users.activate')} onClick={() => setStatus(u, 'active')}>
                                                            <Play className="h-4 w-4" />
                                                        </RowBtn>
                                                    )}
                                                    <RowBtn danger disabled={!usersKnown} title={t('users.delete')} onClick={() => deleteUser(u)}>
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
            ))}
            </RemoteGate>
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
    const plans = useRemote(PLANS_URL, decodePlans);
    const [editing, setEditing] = useState<ServicePlan | null>(null);
    const [saving, setSaving] = useState(false);
    const load = () => void plans.retry();
    // A plan is added, edited or removed only on the list as the server last
    // gave it.
    // Plan yalnız sunucunun son verdiği liste üzerinde eklenir, düzenlenir ya
    // da kaldırılır.
    const plansKnown = plans.remote.state === 'known' && !plans.reading;
    const send = async (url: string, init: RequestInit): Promise<Response | null> => {
        setSaving(true);
        try {
            return await fetch(url, init);
        } catch {
            showToast('error', t('common.resultUnknown'));
            load();
            return null;
        } finally {
            setSaving(false);
        }
    };

    const save = async () => {
        if (!editing) return;
        const isNew = editing.id === 0;
        const res = await send(isNew ? '/api/v1/plans' : `/api/v1/plans/${editing.id}`, {
            method: isNew ? 'POST' : 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(editing),
        });
        if (!res) return;
        if (!res.ok) {
            showToast('error', apiErrorText(await readApiError(res), t));
            return;
        }
        showToast('success', isNew ? t('plans.created') : t('plans.updated'));
        setEditing(null);
        load();
    };

    const remove = async (p: ServicePlan) => {
        if (!confirm(`${p.name}?`)) return;
        const res = await send(`/api/v1/plans/${p.id}`, { method: 'DELETE' });
        if (!res) return;
        if (!res.ok) {
            showToast('error', apiErrorText(await readApiError(res), t));
            return;
        }
        showToast('success', t('plans.deleted'));
        load();
    };

    const fmtGB = (mb: number) => (mb >= 1024 ? `${(mb / 1024).toFixed(0)} GB` : `${mb} MB`);

    return (
        <div>
            <div className="mb-3 flex items-center justify-between">
                <span className="text-xs text-fg-subtle">{t('common.itemsTotal', { n: countText(mapRemote(plans.remote, (rows) => rows.length)) })}</span>
                <Button variant="primary" icon={Plus} disabled={!plansKnown} onClick={() => setEditing({ ...emptyPlan })}>
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
                        <Button variant="primary" icon={Save} onClick={save} disabled={saving || !plansKnown || !editing.name.trim()}>
                            {t('plans.save')}
                        </Button>
                    </div>
                </div>
            )}

            <RemoteGate
                remote={plans.remote}
                checking={t('plans.checking')}
                failed={t('plans.unknown')}
                onRetry={() => void plans.retry()}
                busy={plans.reading}
                className="py-3"
            >
                {(shown) => (shown.value.length === 0 ? (
                    <KnownEmpty of={shown} icon={Layers} title={t('plans.empty')} hint={t('plans.emptyHint')} />
                ) : (
                    <div className="overflow-x-auto rounded-xl border border-border-strong bg-surface">
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
                                {shown.value.map((p) => (
                                    <tr key={p.id} className="border-b border-border last:border-0 hover:bg-surface-2/60">
                                        <td className="px-4 py-3 text-base font-medium text-fg">{p.name}</td>
                                        <td className="px-4 py-3 text-fg-muted">{p.max_domains}</td>
                                        <td className="px-4 py-3 text-fg-muted">{p.max_databases}</td>
                                        <td className="px-4 py-3 text-fg-muted">{p.max_email_accounts}</td>
                                        <td className="px-4 py-3 text-fg-muted">{fmtGB(p.disk_quota_mb)}</td>
                                        <td className="px-4 py-3 text-fg-muted">{fmtGB(p.bandwidth_quota_mb)}</td>
                                        <td className="px-4 py-3 text-fg-muted">{p.subscribers ?? 0}</td>
                                        <td className="px-4 py-3">
                                            <div className="flex items-center justify-end gap-0.5">
                                                <RowBtn disabled={!plansKnown} title={t('plans.edit')} onClick={() => setEditing({ ...p })}>
                                                    <Pencil className="h-4 w-4" />
                                                </RowBtn>
                                                <RowBtn danger disabled={!plansKnown || saving} title={t('users.delete')} onClick={() => remove(p)}>
                                                    <Trash2 className="h-4 w-4" />
                                                </RowBtn>
                                            </div>
                                        </td>
                                    </tr>
                ))}
                        </tbody>
                    </table>
                </div>
            ))}
            </RemoteGate>
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

function RowBtn({ children, title, onClick, danger, disabled }: { children: React.ReactNode; title: string; onClick: () => void; danger?: boolean; disabled?: boolean }) {
    return (
        <button
            type="button"
            title={title}
            aria-label={title}
            onClick={onClick}
            disabled={disabled}
            className={`rounded-md p-1.5 text-fg-subtle transition-colors hover:bg-surface-2 disabled:pointer-events-none disabled:opacity-40 ${danger ? 'hover:text-danger' : 'hover:text-fg'}`}
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
