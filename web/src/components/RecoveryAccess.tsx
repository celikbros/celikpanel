import { useCallback, useEffect, useRef, useState } from 'react';
import type { CurrentUser } from '../lib/api';
import { useI18n } from '../i18n';
import { BrandMark } from './BrandMark';
import { LanguageSwitcher } from './LanguageSwitcher';
import { Button, Spinner } from './ui';
import { parseRecoveryObservation, reconcileRecoveryObservation, recoveryFailureGuidanceKey, retryingCauseKey, savedRecoveryRequestId, UPDATE_MARKER_KEY, type RecoveryObservation } from '../lib/recoveryObservation';
import { handoverAddress, recoveryHandover, savedSetupHandoverHost, setupStartMarkerKey } from '../lib/panelHandover';

// Planned certificate handover during setup (2026-10-08). The page names it
// only when the server reports a managed certificate for the host that the
// setup started in this browser was securing. One public, read-only request
// per access check; a failed read leaves the ordinary unknown wording.
// Sunucu, bu tarayicida baslatilan kurulumun guvenceye aldigi ad icin yonetilen
// sertifika bildirdiginde planli devir anlatilir; okuma basarisizsa metin degismez.
function usePanelHandover(username: string | undefined, enabled: boolean, checking: boolean) {
    const [handover, setHandover] = useState<{ host: string; elsewhere: boolean } | null>(null);
    useEffect(() => {
        if (!enabled || !username) { setHandover(null); return; }
        let saved: string | null = null;
        try { saved = savedSetupHandoverHost(localStorage.getItem(setupStartMarkerKey(username))); } catch { saved = null; }
        if (!saved) { setHandover(null); return; }
        const controller = new AbortController();
        const timeout = window.setTimeout(() => controller.abort(), 5000);
        void fetch('/api/v1/panel/access-address', { cache: 'no-store', signal: controller.signal })
            .then(async response => (response.ok ? response.json() : null))
            .then(body => { if (body && !controller.signal.aborted) setHandover(recoveryHandover(saved, body.hostname, window.location.hostname)); })
            .catch(() => {}).finally(() => window.clearTimeout(timeout));
        return () => { controller.abort(); window.clearTimeout(timeout); };
    }, [username, enabled, checking]);
    return enabled ? handover : null;
}

export function RecoveryStatus({ username, onUnauthorized, embedded = false }: { username: string; onUnauthorized?: () => void; embedded?: boolean }) {
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
    return <section className={embedded ? 'mt-2' : 'mt-8 border-t border-border pt-6'} aria-labelledby="recovery-operation-heading">
        <h2 id="recovery-operation-heading" className={embedded ? 'sr-only' : 'text-lg font-semibold'}>{t('recovery.operationTitle')}</h2>
        {!requestId ? <p className="mt-3 max-w-prose text-sm text-fg-muted">{t('recovery.noOperation')}</p> : <>
            <p className="mt-3 break-all text-sm text-fg-muted">{t('recovery.operationId')}: <span className="font-mono">{requestId}</span></p>
            <div className="mt-4 space-y-3 text-sm" role="status" aria-live="polite">
                {(unavailable || !last) && <p>{t(busy && !unavailable && !last ? 'recovery.checking' : 'recovery.observationUnavailable')}</p>}
                {last?.phase && (last.automatic_recovery === 'retry_scheduled' || last.automatic_recovery === 'pause_pending' ? <>
                    {/* Attempts remain, or the last one is finishing: the owner is not asked to act before the pause. */}
                    <p className="font-semibold">{t(last.automatic_recovery === 'pause_pending' ? 'recovery.automatic.pausingTitle' : 'recovery.automatic.retryTitle')}</p>
                    {retryingCauseKey(last.first_failure_code) && <p className="max-w-prose">{t(retryingCauseKey(last.first_failure_code)!)}</p>}
                    <p className="max-w-prose text-fg-muted">{t(last.automatic_recovery === 'pause_pending' ? 'recovery.automatic.pausingHelp' : 'recovery.automatic.retryHelp')}</p>
                </> : <><p className="font-semibold">{t(last.automatic_recovery ? 'recovery.automatic.pausedTitle' : last.waiting_for ? `recovery.wait.${last.waiting_for}` : `recovery.phase.${last.phase}`)}</p>{last.automatic_recovery && last.first_failure_code && <p className="max-w-prose">{t(`recovery.automatic.cause.${last.first_failure_code}`)}</p>}<p className="max-w-prose text-fg-muted">{t(last.automatic_recovery ? 'recovery.automatic.pausedHelp' : last.waiting_for ? 'recovery.wait.next' : recoveryFailureGuidanceKey(last) ?? `recovery.next.${last.phase}`)}</p>{last.automatic_recovery && <p className="max-w-prose text-fg-muted">{t(last.renewal_before_update === 'off' ? 'recovery.automatic.renewalOff' : 'recovery.automatic.renewal')}</p>}</>)}
                {last?.automatic_recovery === 'paused_retry_limit' && <div className="space-y-3">
                    <p className="max-w-prose">{t('recovery.automatic.inspect')}</p>
                    <pre className="whitespace-pre-wrap break-words rounded border border-border bg-surface px-3 py-3 text-sm"><code>sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50</code></pre>
                    <p className="max-w-prose text-fg-muted">{t('recovery.automatic.resume')}</p>
                </div>}
                {/* upd4 O9: a verified update shows no failure label for its earlier attempts. */}
                {last?.previous_failure && last.terminal_proof !== 'update_verified' && <p>{t('recovery.previousFailure')}: {t(`recovery.reason.${last.failure_code ?? last.previous_failure}`)}</p>}
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
    const handover = usePanelHandover(user?.username, cause === 'availability' || cause === 'starting', checking);
    const address = handover?.elsewhere ? handoverAddress(handover.host, window.location.port) : '';
    return <div className="min-h-screen bg-bg text-fg">
        <header className="border-b border-border bg-surface px-4 py-5 sm:px-8"><div className="mx-auto flex max-w-3xl items-center justify-between gap-4"><div className="flex items-center gap-3 font-semibold"><BrandMark className="h-7 w-7 text-primary" />CelikPanel</div><LanguageSwitcher /></div></header>
        <main className="mx-auto max-w-3xl px-4 py-10 sm:px-8 sm:py-16">
            {user && <p className="mb-5 break-words text-sm text-fg-muted">{user.username}</p>}
            <h1 className="text-2xl font-semibold">{t(checking && !user ? 'recovery.checkingTitle' : handover ? 'recovery.handoverTitle' : `recovery.${cause}Title`)}</h1>
            <p className="mt-4 max-w-prose break-words text-sm leading-relaxed text-fg-muted" role="status">{handover ? t('recovery.handoverHelp', { host: handover.host }) : t(checking && !user ? 'recovery.checkingHelp' : `recovery.${cause}Help`)}</p>
            {address && <p className="mt-4 max-w-prose text-sm leading-relaxed text-fg-muted">{t('recovery.handoverAddress')} <a href={`${address}/setup`} className="break-all font-semibold text-primary underline underline-offset-4">{address}</a></p>}
            <div className="mt-6 flex flex-wrap items-center gap-3"><Button disabled={checking} onClick={onRetry}>{checking && <Spinner />}{t(checking ? 'recovery.checking' : 'recovery.retry')}</Button><Button variant="secondary" onClick={() => window.location.reload()}>{t('app.reload')}</Button></div>
            {/* During the planned handover the saved update result is not the reason for this page: it stays one step away. */}
            {user?.effective_role === 'admin' && (handover
                ? <details className="mt-8 border-t border-border pt-6 text-sm"><summary className="cursor-pointer font-semibold text-primary">{t('recovery.operationTitle')}</summary><RecoveryStatus key={user.username} username={user.username} onUnauthorized={onUnauthorized} embedded /></details>
                : <RecoveryStatus key={user.username} username={user.username} onUnauthorized={onUnauthorized} />)}
        </main>
    </div>;
}
