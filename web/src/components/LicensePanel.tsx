import { useEffect, useState } from 'react';
import { BadgeCheck } from 'lucide-react';
import { useI18n } from '../i18n';
import { Button, inputClass } from './ui';
import { apiErrorText, readApiError } from '../lib/apiError';

type LicenseStatus = { state: 'active' | 'missing' | 'invalid' | 'expired' | 'verification_unavailable'; can_provision: boolean; expires_at?: number; license_id?: string };
const states = ['active', 'missing', 'invalid', 'expired', 'verification_unavailable'] as const;

export function LicensePanel() {
    const { t, locale } = useI18n();
    const [status, setStatus] = useState<LicenseStatus | null>(null);
    const [key, setKey] = useState('');
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState('');
    async function request(action?: 'activate' | 'refresh', signal?: AbortSignal) {
        setBusy(true); setError('');
        try {
            const response = await fetch('/api/v1/panel/license', action ? {
                method: 'POST', headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ action, ...(action === 'activate' ? { key: key.trim() } : {}) }), signal,
            } : { signal });
            if (!response.ok) throw new Error(apiErrorText(await readApiError(response), t));
            const result = await response.json() as LicenseStatus;
            if (!states.includes(result.state) || typeof result.can_provision !== 'boolean'
                || (result.expires_at !== undefined && (!Number.isSafeInteger(result.expires_at) || result.expires_at <= 0))) {
                throw new Error(t('license.loadFailed'));
            }
            setStatus(result);
            if (action === 'activate') setKey('');
        } catch (cause) {
            if (!signal?.aborted) setError(cause instanceof Error ? cause.message : t('license.loadFailed'));
        } finally { if (!signal?.aborted) setBusy(false); }
    }
    useEffect(() => { const controller = new AbortController(); void request(undefined, controller.signal); return () => controller.abort(); }, []);
    return <section className="rounded-xl border border-border bg-surface p-6" aria-labelledby="license-heading">
        <h2 id="license-heading" className="mb-3 flex items-center gap-2 font-semibold text-fg"><BadgeCheck className="h-5 w-5" />{t('license.title')}</h2>
        <p className="mb-5 text-sm text-fg-muted">{t('license.description')}</p>
        {error && <p role="alert" className="mb-4 rounded-lg border border-danger/30 bg-danger/5 p-3 text-sm text-danger">{error}</p>}
        {status ? <div className="mb-6 space-y-2 text-sm">
            <p className="font-semibold">{t(`license.state.${status.state}`)}</p>
            {status.expires_at && <p>{t('license.expires')}: {new Date(status.expires_at * 1000).toLocaleString(locale === 'tr' ? 'tr-TR' : 'en-US')}</p>}
            {!status.can_provision && <p>{t('license.restricted')}</p>}
        </div> : <p role="status" className="mb-4 text-sm">{busy ? t('common.loading') : t('license.loadFailed')}</p>}
        <div className="mb-6 flex flex-wrap items-center gap-4">
            <Button variant="secondary" disabled={busy} onClick={() => void request(status?.state === 'missing' ? undefined : 'refresh')}>{t('license.refresh')}</Button>
            <a href="https://celikpanel.net/account/" target="_blank" rel="noopener noreferrer" className="text-sm text-primary underline underline-offset-4">{t('license.manage')}</a>
        </div>
        <form onSubmit={event => { event.preventDefault(); void request('activate'); }} className="space-y-3 border-t border-border pt-5">
            <label htmlFor="license-key" className="block text-sm font-medium">{t('license.key')}</label>
            <input id="license-key" type="password" autoComplete="off" spellCheck={false} required pattern="CPK-[a-f0-9]{64}" maxLength={68} value={key} onChange={event => setKey(event.target.value)} className={inputClass} aria-describedby="license-key-help" disabled={busy} />
            <p id="license-key-help" className="text-sm text-fg-muted">{t('license.keyHelp')}</p>
            <Button type="submit" disabled={busy || !/^CPK-[a-f0-9]{64}$/.test(key.trim())}>{t('license.activate')}</Button>
        </form>
    </section>;
}
