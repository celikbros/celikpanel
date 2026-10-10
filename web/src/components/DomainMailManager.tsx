import { lazy, useState, useEffect, useRef } from 'react';
import { Mail, Plus, Trash2, ArrowRight, AtSign, Pencil, Info, KeyRound } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { apiErrorText, readApiError } from '../lib/apiError';
import { useRemote, type Remote } from '../lib/remote';
import { Button, Checking, CouldNotCheck, Dialog, KnownEmpty, RemoteGate, UsageBar, inputClass } from './ui';
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
    readOnly?: boolean;
}

const mailPasswordByteLength = (value: string) => new TextEncoder().encode(value).byteLength;

// The decoders of the three reads of this screen. An answer that does not hold
// its list is not the contract and is unknown; it is never an empty list.
// Bu ekranın üç okumasının çözücüleri. Listesini taşımayan yanıt bilinmeyendir;
// asla boş liste değildir.
function decodeAccounts(raw: unknown): EmailAccount[] {
    const accounts = (raw as { accounts?: unknown } | null)?.accounts;
    if (!Array.isArray(accounts)) throw new Error('accounts');
    return accounts as EmailAccount[];
}
function decodeForwardings(raw: unknown): Forwarding[] {
    const forwardings = (raw as { forwardings?: unknown } | null)?.forwardings;
    if (!Array.isArray(forwardings)) throw new Error('forwardings');
    return forwardings as Forwarding[];
}
function decodeQuota(raw: unknown): QuotaStatus {
    const status = raw as Partial<QuotaStatus> | null;
    if (!status || typeof status.plugin_enabled !== 'boolean') throw new Error('quota');
    return { plugin_enabled: status.plugin_enabled, usages: Array.isArray(status.usages) ? status.usages : [] };
}

// The number beside a tab: the count once the list is known, "…" while it is
// read and "–" when it could not be read. Never a zero nobody counted.
// Sekmenin yanındaki sayı: liste bilinince sayı, okunurken "…", okunamayınca "–".
const countOf = (remote: Remote<unknown[]>) => (remote.state === 'known' ? remote.value.length : remote.state === 'loading' ? '…' : '–');
const WebmailAccess = lazy(() => import('./WebmailAccess'));

export function DomainMailManager({ domainId, domainName, readOnly = false }: DomainMailManagerProps) {
    const { t } = useI18n();
    const [activeTab, setActiveTab] = useState<'accounts' | 'forwarding' | 'auth' | 'settings'>('accounts');
    // The two lists are read once for the screen, so the number beside each
    // tab is the server's and not a zero for a tab nobody opened yet. Each is
    // being read, could not be read, or known (9 Oct 2026): a failed read used
    // to leave an empty list with no message.
    // İki liste ekran için bir kez okunur. Her biri okunuyor, okunamadı ya da
    // biliniyor durumundadır.
    const accountsRead = useRemote(`/api/v1/domains/${domainId}/mail/accounts`, decodeAccounts);
    const forwardingsRead = useRemote(`/api/v1/domains/${domainId}/mail/forwardings`, decodeForwardings);
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
    const quotaRead = useRemote(activeTab === 'accounts' ? `/api/v1/domains/${domainId}/mail/quota` : null, decodeQuota);
    const quotaStatus = quotaRead.remote.state === 'known' ? quotaRead.remote.value : null;
    const [editingQuota, setEditingQuota] = useState<number | null>(null);
    const [quotaDraft, setQuotaDraft] = useState(1024);
    const [passwordAccount, setPasswordAccount] = useState<EmailAccount | null>(null);
    const [newPassword, setNewPassword] = useState('');
    const [passwordConfirmation, setPasswordConfirmation] = useState('');
    const [passwordSaving, setPasswordSaving] = useState(false);
    const passwordRequestRef = useRef<AbortController | null>(null);
    useEffect(() => {
        setShowForm(false);
        setEditingQuota(null);
    }, [domainId, activeTab, readOnly]);

    useEffect(() => {
        // A mailbox secret must not survive an authorization or domain context
        // change, even briefly in an otherwise hidden dialog.
        passwordRequestRef.current?.abort();
        passwordRequestRef.current = null;
        setPasswordAccount(null);
        setNewPassword('');
        setPasswordConfirmation('');
        setPasswordSaving(false);

        return () => {
            passwordRequestRef.current?.abort();
            passwordRequestRef.current = null;
        };
    }, [domainId, readOnly, activeTab]);

    // After a change of this screen the list it changed is read again, and
    // with the mailboxes their usage.
    // Bu ekranın bir değişikliğinden sonra değiştirdiği liste yeniden okunur.
    const reloadAccounts = () => {
        void accountsRead.retry();
        void quotaRead.retry();
    };
    const reloadForwardings = () => void forwardingsRead.retry();

    const createAccount = async () => {
        if (readOnly || !user || !pass) return;
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
            reloadAccounts();
        } catch {
            showToast('error', t('mail.createFailed'));
        }
    };

    const saveQuota = async (id: number) => {
        if (readOnly || quotaDraft <= 0) return;
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/mail/accounts`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ id, quota_mb: quotaDraft }),
            });
            if (!res.ok) throw new Error();
            showToast('success', t('mail.quotaUpdated'));
            setEditingQuota(null);
            reloadAccounts();
        } catch {
            showToast('error', t('common.error'));
        }
    };

    const deleteAccount = async (id: number, address: string) => {
        if (readOnly) return;
        if (!confirm(t('mail.confirmDeleteAccount', { name: address }))) return;
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/mail/accounts?id=${id}`, { method: 'DELETE' });
            if (!res.ok) throw new Error();
            showToast('success', t('mail.accountDeleted'));
            reloadAccounts();
        } catch {
            showToast('error', t('common.error'));
        }
    };

    const openPasswordDialog = (account: EmailAccount) => {
        if (readOnly) return;
        setPasswordAccount(account);
        setNewPassword('');
        setPasswordConfirmation('');
    };

    const closePasswordDialog = () => {
        if (passwordSaving) return;
        setPasswordAccount(null);
        setNewPassword('');
        setPasswordConfirmation('');
    };

    const rotateAccountPassword = async () => {
        if (readOnly || passwordSaving || !passwordAccount || passwordRequestRef.current) return;
        const passwordBytes = mailPasswordByteLength(newPassword);
        if (passwordBytes < 8 || passwordBytes > 1024 || newPassword !== passwordConfirmation) return;

        const request = new AbortController();
        passwordRequestRef.current = request;
        let succeeded = false;
        setPasswordSaving(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/mail/accounts/password`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ id: passwordAccount.id, new_password: newPassword }),
                signal: request.signal,
            });
            if (!res.ok) {
                const apiError = await readApiError(res);
                // Stable codes stay useful, but an untrusted server message is
                // not echoed on this secret-bearing path.
                showToast('error', apiErrorText({ ...apiError, message: '' }, t, 'mail.passwordUpdateFailed'));
                return;
            }
            succeeded = true;
            showToast('success', t('mail.passwordUpdated'));
        } catch (error) {
            if (error instanceof DOMException && error.name === 'AbortError') return;
            showToast('error', t('mail.passwordUpdateFailed'));
        } finally {
            // Clear submitted secrets after both success and failure. On a
            // failure the target stays visible so the user can safely retry,
            // but the password must be entered again.
            if (passwordRequestRef.current === request) {
                passwordRequestRef.current = null;
                setNewPassword('');
                setPasswordConfirmation('');
                setPasswordSaving(false);
                if (succeeded) setPasswordAccount(null);
            }
        }
    };

    const createForwarding = async () => {
        if (readOnly || !fwdSource || !fwdDest) return;
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
            reloadForwardings();
        } catch {
            showToast('error', t('mail.forwarderFailed'));
        }
    };

    const deleteForwarding = async (id: number, source: string) => {
        if (readOnly) return;
        if (!confirm(t('mail.confirmDeleteForwarder', { name: source }))) return;
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/mail/forwardings?id=${id}`, { method: 'DELETE' });
            if (!res.ok) throw new Error();
            showToast('success', t('mail.forwarderDeleted'));
            reloadForwardings();
        } catch {
            showToast('error', t('common.error'));
        }
    };

    const listed = activeTab === 'accounts' ? accountsRead.remote : forwardingsRead.remote;
    const passwordBytes = mailPasswordByteLength(newPassword);
    const passwordInRange = passwordBytes >= 8 && passwordBytes <= 1024;
    const passwordMatches = passwordConfirmation.length > 0 && newPassword === passwordConfirmation;

    return (
        <div>
            <WebmailAccess domainId={domainId} />
            {/* The four tabs wrap on a narrow screen: a tab that runs off the
                edge cannot be reached on a phone.
                Dört sekme dar ekranda alt satıra geçer. */}
            <div className="mb-4 flex flex-wrap items-center gap-x-1 border-b border-border">
                <Tab active={activeTab === 'accounts'} onClick={() => setActiveTab('accounts')} label={t('mail.tab.accounts')} count={countOf(accountsRead.remote)} />
                <Tab active={activeTab === 'forwarding'} onClick={() => setActiveTab('forwarding')} label={t('mail.tab.forwarding')} count={countOf(forwardingsRead.remote)} />
                <Tab active={activeTab === 'auth'} onClick={() => setActiveTab('auth')} label={t('mailauth.tab')} />
                <Tab active={activeTab === 'settings'} onClick={() => setActiveTab('settings')} label={t('mail.tab.settings')} />
            </div>

            {activeTab === 'auth' ? (
                <>
                    <DeliverabilityCard domainId={domainId} />
                    <MailAuthPanel domainId={domainId} readOnly={readOnly} />
                </>
            ) : activeTab === 'settings' ? (
                <MailSettingsPanel domainId={domainId} domainName={domainName} readOnly={readOnly} />
            ) : (
                <>
            <div className="mb-3 flex min-h-[2.25rem] items-center justify-between gap-3">
                {/* The total is the server's; while the list is not known
                    there is no total to state.
                    Toplam sunucunundur; liste bilinmezken toplam yoktur. */}
                <span className="text-xs text-fg-muted">{listed.state === 'known' ? t('common.itemsTotal', { n: listed.value.length }) : ''}</span>
                {!readOnly && (
                    <Button variant="primary" icon={Plus} disabled={listed.state !== 'known'} onClick={() => setShowForm((s) => !s)}>
                        {activeTab === 'accounts' ? t('mail.addAccount') : t('mail.addForwarder')}
                    </Button>
                )}
            </div>

            {!readOnly && showForm && listed.state === 'known' && (
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

            {activeTab === 'accounts' ? (
                <RemoteGate
                    remote={accountsRead.remote}
                    checking={t('mail.accounts.checking')}
                    failed={t('mail.accounts.unknown')}
                    onRetry={() => void accountsRead.retry()}
                    busy={accountsRead.reading}
                    className="min-h-[2.75rem]"
                >
                    {({ value: accounts, observedAt, stale }) => (accounts.length === 0 ? (
                        <KnownEmpty of={{ value: accounts, observedAt }} icon={Mail} title={t('mail.emptyAccounts')} />
                    ) : (
                        <>
                            {quotaStatus && !quotaStatus.plugin_enabled && (
                                <p className="mb-3 flex items-start gap-2 rounded-lg border border-warning-mark/50 bg-warning-mark/20 px-3 py-2 text-xs text-fg-muted">
                                    <Info className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning" />
                                    {t('mail.quotaNotEnforced')}
                                </p>
                            )}
                            {quotaRead.remote.state === 'unknown' && (
                                <CouldNotCheck className="mb-3" text={t('mail.quota.unknown')} onRetry={() => void quotaRead.retry()} busy={quotaRead.reading} />
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
                                                    {!readOnly && !stale && editingQuota === a.id ? (
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
                                                            {!readOnly && (
                                                                <button
                                                                    disabled={stale}
                                                                    onClick={() => {
                                                                        setEditingQuota(a.id);
                                                                        setQuotaDraft(a.quota_mb);
                                                                    }}
                                                                    title={t('mail.editQuota')}
                                                                    aria-label={t('mail.editQuota')}
                                                                    className="rounded p-1.5 text-fg-subtle hover:bg-surface-2 hover:text-fg disabled:pointer-events-none disabled:opacity-50"
                                                                >
                                                                    <Pencil className="h-3.5 w-3.5" />
                                                                </button>
                                                            )}
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
                                                        // Usage that is still being read, could not be
                                                        // read, or that the server has none for.
                                                        // Hâlâ okunan, okunamayan ya da sunucunun
                                                        // tutmadığı kullanım.
                                                        <span className="text-xs text-fg-muted">
                                                            {quotaRead.remote.state === 'loading' ? '…' : quotaRead.remote.state === 'unknown' ? t('mail.usageUnknown') : '—'}
                                                        </span>
                                                    )}
                                                </td>
                                                <td className="px-4 py-2.5 text-right">
                                                    {!readOnly && (
                                                        <span className="inline-flex items-center justify-end gap-1">
                                                            <button
                                                                type="button"
                                                                onClick={() => openPasswordDialog(a)}
                                                                disabled={stale}
                                                                aria-label={t('mail.changePasswordFor', { address: a.address })}
                                                                title={t('mail.changePasswordFor', { address: a.address })}
                                                                className="rounded-md p-1.5 text-fg-subtle hover:bg-surface-2 hover:text-primary"
                                                            >
                                                                <KeyRound className="h-4 w-4" aria-hidden="true" />
                                                            </button>
                                                            <DeleteBtn disabled={stale} label={t('mail.deleteAccountNamed', { name: a.address })} onClick={() => deleteAccount(a.id, a.address)} />
                                                        </span>
                                                    )}
                                                </td>
                                            </tr>
                                        );
                                    })}
                                </tbody>
                            </table>
                        </div>
                        </>
                    ))}
                </RemoteGate>
            ) : (
                <RemoteGate
                    remote={forwardingsRead.remote}
                    checking={t('mail.forwarders.checking')}
                    failed={t('mail.forwarders.unknown')}
                    onRetry={() => void forwardingsRead.retry()}
                    busy={forwardingsRead.reading}
                    className="min-h-[2.75rem]"
                >
                    {({ value: forwardings, observedAt, stale }) => (forwardings.length === 0 ? (
                        <KnownEmpty of={{ value: forwardings, observedAt }} icon={ArrowRight} title={t('mail.emptyForwarders')} />
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
                                        {!readOnly && <DeleteBtn disabled={stale} label={t('mail.deleteForwarderNamed', { name: f.source })} onClick={() => deleteForwarding(f.id, f.source)} />}
                                    </td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
                    ))}
                </RemoteGate>
            )}
                </>
            )}

            {passwordAccount && !readOnly && (
                <Dialog
                    id="mail-password-dialog"
                    icon={KeyRound}
                    width="sm"
                    busy={passwordSaving}
                    onDismiss={closePasswordDialog}
                    noValidate
                    onSubmit={(event) => {
                        event.preventDefault();
                        void rotateAccountPassword();
                    }}
                    title={t('mail.passwordDialog.title')}
                    description={
                        <>
                            {t('mail.passwordDialog.account')}: <strong className="break-all font-medium text-fg">{passwordAccount.address}</strong>
                        </>
                    }
                    // The session warning describes the dialogue as much as the
                    // account does, so it is announced with it.
                    // Oturum uyarisi, hesap kadar bu diyalogu tanimlar.
                    extraDescribedBy="mail-password-session-warning"
                    actions={
                        <>

                            <Button type="button" onClick={closePasswordDialog} disabled={passwordSaving}>
                                {t('common.cancel')}
                            </Button>
                            <Button
                                type="submit"
                                variant="primary"
                                icon={KeyRound}
                                disabled={readOnly || passwordSaving || !passwordInRange || !passwordMatches}
                            >
                                {passwordSaving ? t('mail.passwordDialog.saving') : t('mail.passwordDialog.submit')}
                            </Button>
                        </>
                    }
                >
                    <div className="space-y-3">
                        <label className="block">
                            <span className="mb-1 block text-xs text-fg-muted">{t('mail.passwordDialog.new')}</span>
                            <input
                                type="password"
                                value={newPassword}
                                onChange={(event) => setNewPassword(event.target.value)}
                                minLength={8}
                                maxLength={1024}
                                autoComplete="new-password"
                                required
                                autoFocus
                                disabled={passwordSaving}
                                aria-invalid={newPassword.length > 0 && !passwordInRange}
                                aria-describedby="mail-password-requirements"
                                className={inputClass}
                            />
                        </label>
                        <label className="block">
                            <span className="mb-1 block text-xs text-fg-muted">{t('mail.passwordDialog.confirm')}</span>
                            <input
                                type="password"
                                value={passwordConfirmation}
                                onChange={(event) => setPasswordConfirmation(event.target.value)}
                                minLength={8}
                                maxLength={1024}
                                autoComplete="new-password"
                                required
                                disabled={passwordSaving}
                                aria-invalid={passwordConfirmation.length > 0 && !passwordMatches}
                                aria-describedby={passwordConfirmation.length > 0 && !passwordMatches ? 'mail-password-mismatch' : undefined}
                                className={inputClass}
                            />
                        </label>
                        <p id="mail-password-requirements" className={`text-xs ${newPassword.length > 0 && !passwordInRange ? 'text-danger' : 'text-fg-subtle'}`}>
                            {t('mail.passwordDialog.requirements')}
                        </p>
                        {passwordConfirmation.length > 0 && !passwordMatches && (
                            <p id="mail-password-mismatch" role="alert" className="text-xs text-danger">
                                {t('mail.passwordDialog.mismatch')}
                            </p>
                        )}
                        <p id="mail-password-session-warning" className="flex items-start gap-2 rounded-lg border border-warning-mark/50 bg-warning-mark/20 px-3 py-2 text-xs text-fg-muted">
                            <Info className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning" aria-hidden="true" />
                            {t('mail.passwordDialog.sessionWarning')}
                        </p>
                    </div>
                </Dialog>
            )}
        </div>
    );
}

function Tab({ active, onClick, label, count }: { active: boolean; onClick: () => void; label: string; count?: number | string }) {
    return (
        <button
            onClick={onClick}
            className={`-mb-px flex items-center gap-2 border-b-2 px-3 py-2.5 text-sm font-medium ${
                active ? 'border-primary text-primary' : 'border-transparent text-fg-muted hover:text-fg'
            }`}
        >
            {label}
            {count !== undefined && (
                <span className="rounded-full bg-surface-2 px-1.5 py-0.5 text-xs text-fg-muted">{count}</span>
            )}
        </button>
    );
}

function DeleteBtn({ onClick, disabled, label }: { onClick: () => void; disabled?: boolean; label: string }) {
    return (
        <button
            onClick={onClick}
            disabled={disabled}
            aria-label={label}
            title={label}
            className="rounded-md p-1.5 text-fg-subtle hover:bg-surface-2 hover:text-danger disabled:pointer-events-none disabled:opacity-50"
        >
            <Trash2 className="h-4 w-4" aria-hidden="true" />
        </button>
    );
}


// One traffic-light card for "will it land in the inbox": PTR, HELO, TLS,
// port 25, SPF/DKIM/DMARC, blacklists, mail certificate, DNSSEC. PTR cannot
// be fixed here — the card spells out what to enter at the hosting provider.
// "Gelen kutusuna düşer mi" için tek trafik-ışığı kartı. PTR buradan
// düzeltilemez — kart, barındırma sağlayıcısında ne girileceğini yazar.
interface MailHealth {
    overall: string;
    server_ip: string;
    expected_ptr: string;
    checks: { id: string; status: string; detail?: string }[];
}

function decodeMailHealth(raw: unknown): MailHealth {
    const health = raw as Partial<MailHealth> | null;
    if (!health || typeof health.overall !== 'string' || !Array.isArray(health.checks)) throw new Error('mail health');
    return health as MailHealth;
}

function DeliverabilityCard({ domainId }: { domainId: number }) {
    const { t } = useI18n();
    // The checks are read from the server. While they are read the card says
    // so; when they could not be read it says that, with Retry, instead of
    // vanishing as it did before 9 Oct 2026.
    // Denetimler sunucudan okunur. Okunamadıklarında kart bunu Tekrar dene ile
    // söyler; önceden kaybolurdu.
    const { remote, reading, retry } = useRemote(`/api/v1/domains/${domainId}/mail/health`, decodeMailHealth);
    if (remote.state !== 'known') {
        return (
            <section className="mb-5 rounded-xl border border-border bg-surface p-5">
                <h3 className="mb-3 text-sm font-semibold text-fg">{t('mail.health.title')}</h3>
                {remote.state === 'loading'
                    ? <Checking label={t('mail.healthChecking')} />
                    : <CouldNotCheck text={t('mail.healthUnknown')} onRetry={() => void retry()} busy={reading} />}
            </section>
        );
    }
    const data = remote.value;

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
                <p className="mt-3 rounded-lg bg-warning-mark/20 px-3 py-2 text-xs text-fg-muted">
                    {t('mail.health.ptrFix', { ip: data.server_ip, host: data.expected_ptr })}
                </p>
            )}
        </section>
    );
}
