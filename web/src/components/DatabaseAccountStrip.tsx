import { useState } from 'react';
import { KeyRound, Eye, RefreshCw, Trash2, Copy, Check } from 'lucide-react';
import { useI18n } from '../i18n';
import { showToast } from './Toast';
import { Button, Dialog } from './ui';

// R-065. CelikPanel opens an account of its own on a database engine and
// connects as that, so the operator's own root is never touched. That is only
// true on screen if the screen says so: an administrator has to be able to see
// whether the account is there, read its password, give it a new one, and take
// it away - and the refusal sentence an engine returns says "on this server's
// page", so this is the page it means.
//
// Placed between the server selector and the tabs, because the account belongs
// to the server rather than to the databases or the users listed under it, and
// because when it is missing it is the reason everything below will fail.
//
// R-065. CelikPanel motorda kendi hesabini acar ve onunla baglanir; boylece
// operatorun kok hesabina hic dokunulmaz. Bu, ancak ekran soylerse dogrudur.
// Sunucu seciciyle sekmeler arasina konur: hesap, altinda listelenen
// veritabanlarina degil sunucuya aittir ve eksik oldugunda asagidaki her seyin
// basarisiz olmasinin sebebidir.

const API_BASE = '/api/v1';

export interface DatabaseAccountServer {
    id: number;
    admin_username?: string;
    is_local?: boolean;
}

export function DatabaseAccountStrip({
    server,
    onChanged,
}: {
    server: DatabaseAccountServer;
    onChanged: () => void;
}) {
    const { t } = useI18n();
    const [busy, setBusy] = useState(false);
    const [password, setPassword] = useState<string | null>(null);
    const [copied, setCopied] = useState(false);

    const account = (server.admin_username ?? '').trim();

    const provision = async (rotating: boolean) => {
        setBusy(true);
        try {
            const res = await fetch(`${API_BASE}/database-servers/${server.id}/admin-account`, {
                method: 'POST',
            });
            if (!res.ok) {
                // The engine's refusal is written by the panel and is the
                // whole point of showing it; a generic failure would send the
                // administrator back to guessing.
                // Motorun reddini panel yazar ve onu gostermek isin ozudur.
                const body = await res.json().catch(() => null);
                throw new Error(body?.error || t('common.error'));
            }
            showToast('success', t(rotating ? 'databases.account.rotated' : 'databases.account.opened'));
            onChanged();
        } catch (error) {
            showToast('error', error instanceof Error ? error.message : t('common.error'));
        } finally {
            setBusy(false);
        }
    };

    const reveal = async () => {
        setBusy(true);
        try {
            const res = await fetch(`${API_BASE}/database-servers/${server.id}/admin-account`);
            if (!res.ok) throw new Error();
            const body = await res.json();
            setPassword(body.password ?? '');
            setCopied(false);
        } catch {
            showToast('error', t('common.error'));
        } finally {
            setBusy(false);
        }
    };

    const remove = async () => {
        if (!confirm(t('databases.account.confirmRemove'))) return;
        setBusy(true);
        try {
            const res = await fetch(`${API_BASE}/database-servers/${server.id}/admin-account`, {
                method: 'DELETE',
            });
            if (!res.ok) throw new Error();
            showToast('success', t('databases.account.removed'));
            onChanged();
        } catch {
            showToast('error', t('common.error'));
        } finally {
            setBusy(false);
        }
    };

    const copy = async () => {
        if (password === null) return;
        try {
            await navigator.clipboard.writeText(password);
            setCopied(true);
        } catch {
            showToast('error', t('common.error'));
        }
    };

    if (!account) {
        // The blocking state. Everything under the tabs will fail while this
        // is true, so it is said plainly and once, with the one action that
        // resolves it - or, for an engine on another machine, with what an
        // administrator has to do instead of a button that cannot work.
        // Engelleyen durum. Sekmelerin altindaki her sey bu dogruyken
        // basarisiz olacak.
        return (
            <div className="mb-4 rounded-xl border border-warning-mark/60 bg-warning-mark/20 px-4 py-3">
                <div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-3">
                    <div className="min-w-0 flex-1">
                        <p className="flex items-center gap-2 text-sm font-semibold text-fg">
                            <KeyRound className="h-4 w-4 shrink-0" aria-hidden="true" />
                            {t('databases.account.missing')}
                        </p>
                        <p className="mt-1 max-w-prose text-sm text-fg-muted">
                            {t(
                                server.is_local
                                    ? 'databases.account.missingHint'
                                    : 'databases.account.missingRemote',
                            )}
                        </p>
                    </div>
                    {server.is_local && (
                        <Button
                            variant="primary"
                            icon={KeyRound}
                            disabled={busy}
                            onClick={() => provision(false)}
                        >
                            {busy ? t('databases.account.opening') : t('databases.account.open')}
                        </Button>
                    )}
                </div>
            </div>
        );
    }

    return (
        <>
            <div className="mb-4 flex flex-wrap items-center justify-between gap-x-6 gap-y-2 px-1">
                <p className="flex min-w-0 items-center gap-2 text-sm text-fg-muted">
                    <KeyRound className="h-4 w-4 shrink-0 text-fg-subtle" aria-hidden="true" />
                    <span className="truncate">
                        {t('databases.account.present', { name: account })}
                    </span>
                </p>
                <div className="flex flex-wrap items-center gap-2">
                    <Button icon={Eye} disabled={busy} onClick={reveal}>
                        {t('databases.account.show')}
                    </Button>
                    <Button icon={RefreshCw} disabled={busy} onClick={() => provision(true)}>
                        {t('databases.account.rotate')}
                    </Button>
                    <Button variant="danger" icon={Trash2} disabled={busy} onClick={remove}>
                        {t('databases.account.remove')}
                    </Button>
                </div>
            </div>

            {password !== null && (
                <Dialog
                    id="database-admin-password"
                    title={t('databases.account.passwordTitle')}
                    description={t('databases.account.passwordRecorded')}
                    icon={KeyRound}
                    width="md"
                    onDismiss={() => setPassword(null)}
                    actions={
                        <>
                            <Button icon={copied ? Check : Copy} onClick={copy}>
                                {t(copied ? 'databases.account.copied' : 'databases.account.copy')}
                            </Button>
                            <Button variant="primary" onClick={() => setPassword(null)}>
                                {t('common.close')}
                            </Button>
                        </>
                    }
                >
                    {/* Read character by character, so it is set in the face
                        that distinguishes them, and selectable as one unit.
                        Karakter karakter okunur; bu yuzden onlari ayirt eden
                        yazi karakteriyle dizilir. */}
                    <p className="text-xs font-medium uppercase tracking-wide text-fg-subtle">
                        {account}
                    </p>
                    <p className="mt-2 select-all break-all rounded-lg border border-border bg-surface-2 px-3 py-2.5 font-mono text-sm text-fg">
                        {password}
                    </p>
                </Dialog>
            )}
        </>
    );
}
