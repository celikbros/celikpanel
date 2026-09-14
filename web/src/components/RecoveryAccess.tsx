import { useCallback, useEffect, useRef, useState } from 'react';
import type { CurrentUser } from '../lib/api';
import { useI18n } from '../i18n';
import { BrandMark } from './BrandMark';
import { LanguageSwitcher } from './LanguageSwitcher';
import { Button, Spinner } from './ui';
import { parseRecoveryObservation, reconcileRecoveryObservation, savedRecoveryRequestId, UPDATE_MARKER_KEY, type RecoveryObservation } from '../lib/recoveryObservation';

export function RecoveryStatus({ username, onUnauthorized }: { username: string; onUnauthorized?: () => void }) {
    const { t, locale } = useI18n();
    const [requestId, setRequestId] = useState<string | null>(() => { try { return savedRecoveryRequestId(localStorage.getItem(UPDATE_MARKER_KEY)); } catch { return null; } });
    const [last, setLast] = useState<RecoveryObservation | null>(null);
    const lastRef = useRef<RecoveryObservation | null>(null);
    const [unavailable, setUnavailable] = useState(false);
    const [busy, setBusy] = useState(false);
    const pending = useRef<AbortController | null>(null);
    const check = useCallback(async () => {
        if (!requestId || pending.current) return;
        const request = new AbortController(); pending.current = request; setBusy(true);
        const timeout = window.setTimeout(() => request.abort(), 15000);
        try {
            const response = await fetch(`/api/v1/recovery/status?request_id=${requestId}`, { signal: request.signal, cache: 'no-store' });
            if (response.status === 401) { if (pending.current === request) { lastRef.current = null; setLast(null); onUnauthorized?.(); } throw new Error('session unavailable'); }
            if (!response.ok) throw new Error('recovery unavailable');
            const observed = parseRecoveryObservation(await response.json(), requestId);
            if (!request.signal.aborted && pending.current === request) {
                const merged = reconcileRecoveryObservation(lastRef.current, observed);
                lastRef.current = merged.record; setLast(merged.record); setUnavailable(merged.unavailable);
            }
        } catch { if (pending.current === request) setUnavailable(true); }
        finally { window.clearTimeout(timeout); if (pending.current === request) { pending.current = null; setBusy(false); } }
    }, [requestId, username, onUnauthorized]);
    useEffect(() => {
        lastRef.current = null; setLast(null); setUnavailable(false); void check();
        const refresh = () => { if (document.visibilityState === 'visible') void check(); };
        const timer = window.setInterval(refresh, 5000); window.addEventListener('focus', refresh);
        return () => { pending.current?.abort(); pending.current = null; window.clearInterval(timer); window.removeEventListener('focus', refresh); };
    }, [check]);
    useEffect(() => {
        const changed = (event: StorageEvent) => { if (event.key === UPDATE_MARKER_KEY) setRequestId(current => current ?? savedRecoveryRequestId(event.newValue)); };
        window.addEventListener('storage', changed); return () => window.removeEventListener('storage', changed);
    }, []);
    return <section className="mt-8 border-t border-border pt-6" aria-labelledby="recovery-operation-heading">
        <h2 id="recovery-operation-heading" className="text-lg font-semibold">{t('recovery.operationTitle')}</h2>
        {!requestId ? <p className="mt-3 max-w-prose text-sm text-fg-muted">{t('recovery.noOperation')}</p> : <>
            <p className="mt-3 break-all text-sm text-fg-muted">{t('recovery.operationId')}: <span className="font-mono">{requestId}</span></p>
            <div className="mt-4 space-y-3 text-sm" role="status" aria-live="polite">
                {(unavailable || !last) && <p>{t(busy && !unavailable && !last ? 'recovery.checking' : 'recovery.observationUnavailable')}</p>}
                {last?.phase && <><p className="font-semibold">{t(`recovery.phase.${last.phase}`)}</p><p className="max-w-prose text-fg-muted">{t(`recovery.next.${last.phase}`)}</p></>}
                {last?.previous_failure && <p>{t('recovery.previousFailure')}: {t(`recovery.reason.${last.previous_failure}`)}</p>}
                {last?.observed_at && <p className="text-fg-muted">{t('recovery.observedAt')}: <time dateTime={last.observed_at}>{new Date(last.observed_at).toLocaleString(locale === 'tr' ? 'tr-TR' : 'en-US')}</time></p>}
            </div>
            <Button className="mt-4" variant="secondary" disabled={busy} onClick={() => void check()}>{t(busy ? 'recovery.checking' : 'recovery.checkStatus')}</Button>
        </>}
    </section>;
}

/** Eager shell: no lazy screen catalogue, router, update provider, or mutation API. */
export function RecoveryAccess({ user, cause, checking = false, onRetry, onUnauthorized }: {
    user?: CurrentUser | null; cause: 'auth' | 'starting' | 'availability' | 'license' | 'bundle';
    checking?: boolean; onRetry: () => void; onUnauthorized?: () => void;
}) {
    const { t } = useI18n();
    return <div className="min-h-screen bg-bg text-fg">
        <header className="border-b border-border bg-surface px-4 py-5 sm:px-8"><div className="mx-auto flex max-w-3xl items-center justify-between gap-4"><div className="flex items-center gap-3 font-semibold"><BrandMark className="h-7 w-7 text-primary" />CelikPanel</div><LanguageSwitcher /></div></header>
        <main className="mx-auto max-w-3xl px-4 py-10 sm:px-8 sm:py-16">
            {user && <p className="mb-5 break-words text-sm text-fg-muted">{user.username}</p>}
            <h1 className="text-2xl font-semibold">{t(checking && !user ? 'recovery.checkingTitle' : `recovery.${cause}Title`)}</h1>
            <p className="mt-4 max-w-prose text-sm leading-relaxed text-fg-muted" role="status">{t(checking && !user ? 'recovery.checkingHelp' : `recovery.${cause}Help`)}</p>
            <div className="mt-6 flex flex-wrap items-center gap-3"><Button disabled={checking} onClick={onRetry}>{checking && <Spinner />}{t(checking ? 'recovery.checking' : 'recovery.retry')}</Button><Button variant="secondary" onClick={() => window.location.reload()}>{t('app.reload')}</Button></div>
            {user?.effective_role === 'admin' && <RecoveryStatus key={user.username} username={user.username} onUnauthorized={onUnauthorized} />}
        </main>
    </div>;
}
