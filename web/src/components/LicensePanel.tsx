import { useEffect, useState } from 'react';
import { BadgeCheck, Eye, EyeOff } from 'lucide-react';
import { useI18n } from '../i18n';
import { useNavigate, useSearchParams } from '../router';
import { Button, inputClass } from './ui';
import { apiErrorText, readApiError } from '../lib/apiError';

type LicenseStatus = { state: 'active' | 'missing' | 'invalid' | 'expired' | 'verification_unavailable'; can_provision: boolean; expires_at?: number; license_id?: string };
const states = ['active', 'missing', 'invalid', 'expired', 'verification_unavailable'] as const;

export function LicensePanel() {
    const { t, locale } = useI18n();
    const [params] = useSearchParams();
    const navigate = useNavigate();
    const setup = params.get('setup') === '1';
    const [status, setStatus] = useState<LicenseStatus | null>(null);
    const [key, setKey] = useState('');
    const [showKey, setShowKey] = useState(false);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState('');
    const [keyError, setKeyError] = useState('');
    async function request(action?: 'activate' | 'refresh', signal?: AbortSignal) {
        const normalizedKey = key.trim();
        if (action === 'activate' && !/^CPK-[a-f0-9]{64}$/.test(normalizedKey)) {
            setKeyError(t('license.keyInvalid'));
            return;
        }
        setBusy(true); setError(''); setKeyError('');
        try {
            const response = await fetch('/api/v1/panel/license', action ? {
                method: 'POST', headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ action, ...(action === 'activate' ? { key: normalizedKey } : {}) }), signal,
            } : { signal, cache: 'no-store' });
            if (!response.ok) {
                const problem = await readApiError(response);
                if (action) {
                    const message = problem.message;
                    const reason = message === 'license: license_in_use' ? 'license.inUse'
                        : message === 'license: invalid_license' || message === 'invalid license key' ? 'license.unknownKey'
                        : message === 'license: license_expired' ? 'license.expiredHelp'
                        : message === 'license: rate_limited' ? 'license.tryLater'
                        : problem.code === 'license_unavailable' || message === 'license service unavailable; existing license retained' ? 'license.unavailable'
                        : 'license.actionFailed';
                    throw new Error(t(reason));
                }
                throw new Error(apiErrorText(problem, t));
            }
            const result = await response.json() as LicenseStatus;
            if (signal?.aborted) return;
            if (!states.includes(result.state) || typeof result.can_provision !== 'boolean'
                || (result.expires_at !== undefined && (!Number.isSafeInteger(result.expires_at) || result.expires_at <= 0))) {
                throw new Error(t('license.loadFailed'));
            }
            setStatus(result);
            if (action === 'activate') { setKey(''); setShowKey(false); }
        } catch (cause) {
            if (!signal?.aborted) setError(cause instanceof TypeError ? t('license.unavailable') : cause instanceof Error ? cause.message : t('license.loadFailed'));
        } finally { if (!signal?.aborted) setBusy(false); }
    }
    useEffect(() => { const controller = new AbortController(); void request(undefined, controller.signal); return () => controller.abort(); }, []);
    return <section className="rounded-xl border border-border bg-surface p-5 sm:p-6" aria-labelledby="license-heading">
        <h2 id="license-heading" className="mb-3 flex items-center gap-2 text-lg font-semibold text-fg"><BadgeCheck className="h-5 w-5 shrink-0" />{t(setup ? status?.can_provision ? 'license.readyTitle' : 'license.setupTitle' : 'license.title')}</h2>
        <p className="mb-5 max-w-prose text-sm text-fg-muted">{t(setup && !status?.can_provision ? 'license.setupIntro' : 'license.description')}</p>
        {error && <p role="alert" className="mb-4 rounded-lg border border-danger/30 bg-danger/5 p-3 text-sm text-danger">{error}</p>}
        {status ? <div className="mb-6 space-y-2 text-sm" role="status">
            {!setup && <p className="font-semibold">{t(`license.state.${status.state}`)}</p>}
            {status.expires_at && <p>{t('license.expires')}: {new Date(status.expires_at * 1000).toLocaleString(locale === 'tr' ? 'tr-TR' : 'en-US')}</p>}
            {!status.can_provision && <p className="max-w-prose text-fg-muted">{t('license.restricted')}</p>}
            {setup && status.can_provision && <p>{t('license.setupSuccess')}</p>}
        </div> : <p role="status" className="mb-4 text-sm">{busy ? t('common.loading') : t('license.loadFailed')}</p>}
        {!(setup && status?.can_provision) && <form noValidate onSubmit={event => { event.preventDefault(); void request('activate'); }} className="space-y-3">
            <label htmlFor="license-key" className="block text-sm font-medium">{t('license.key')}</label>
            <div className="flex items-stretch gap-2">
                <input id="license-key" name="license_key" type={showKey ? 'text' : 'password'} autoComplete="off" autoCapitalize="off" spellCheck={false} value={key} onChange={event => { setKey(event.target.value); setKeyError(''); }} className={`${inputClass} min-w-0 flex-1`} aria-invalid={!!keyError} aria-describedby={keyError ? 'license-key-help license-key-error' : 'license-key-help'} disabled={busy} />
                <Button type="button" variant="secondary" disabled={busy} aria-label={t(showKey ? 'license.hideKey' : 'license.showKey')} aria-pressed={showKey} onClick={() => setShowKey(value => !value)}>{showKey ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}</Button>
            </div>
            <p id="license-key-help" className="text-sm text-fg-muted">{t('license.keyHelp')}</p>
            {keyError && <p id="license-key-error" role="alert" className="text-sm text-danger">{keyError}</p>}
            <div className="flex flex-wrap items-center gap-4 pt-2">
                <Button type="submit" disabled={busy || !key.trim()}>{t(busy ? 'common.loading' : 'license.activate')}</Button>
                <a href="https://celikpanel.net/account/" target="_blank" rel="noopener noreferrer" className="text-sm text-primary underline underline-offset-4">{t(status?.state === 'missing' ? 'license.getKey' : 'license.manage')}</a>
            </div>
        </form>}
        {(setup || !status || status.state !== 'missing') && <div className="mt-6 flex flex-wrap items-center gap-4 border-t border-border pt-5">
            {setup && <Button variant={status?.can_provision ? 'primary' : 'secondary'} disabled={busy} onClick={() => navigate('/')}>{t(status?.can_provision ? 'license.continueSetup' : 'license.explore')}</Button>}
            {(!status || status.state !== 'missing') && <Button variant="secondary" disabled={busy} onClick={() => void request(status ? 'refresh' : undefined)}>{t('license.refresh')}</Button>}
        </div>}
    </section>;
}
