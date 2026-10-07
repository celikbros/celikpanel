import { lazy, Suspense, useCallback, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { useLocation, useNavigate } from '../router';
import { useAuth } from '../auth/AuthContext';
import { useI18n } from '../i18n';
import { Button, Spinner } from './ui';
import { parseAccessObservation, type AccessObservation } from '../lib/accessObservation';
import { RecoveryAccess } from './RecoveryAccess';
import { AccessHold } from './AccessHold';

const LicenseLockScreen = lazy(() => import('./LicenseLockScreen').then(module => ({ default: module.LicenseLockScreen })));

/** While access is unknown the server's answer is read again this often in a visible tab. */
const UNKNOWN_RECHECK_MS = 5000;

/**
 * Never mount management pages before a positive, server-verified decision.
 *
 * Once they are mounted, only a KNOWN negative decision (license missing,
 * expired or invalid) takes them away. A decision that ran out, or a read that
 * failed, leaves access unknown: the pages stay mounted and unreachable behind
 * AccessHold until the server confirms access again. The decision itself, its
 * validity and what the server refuses without it are unchanged.
 *
 * Yonetim sayfalari, sunucunun dogruladigi olumlu karardan once baglanmaz.
 * Baglandiktan sonra onlari yalnizca BILINEN olumsuz karar kaldirir. Suresi dolan
 * karar ya da basarisiz okuma erisimi bilinmez kilar: sayfalar bagli kalir ve
 * sunucu erisimi yeniden dogrulayana kadar AccessHold arkasinda erisilmezdir.
 */
export function LicenseOnboarding({ children, onAccessChange, onRecoveryChange, suspended = false }: {
    children?: ReactNode; onAccessChange?: (allowed: boolean) => void; onRecoveryChange?: (recovering: boolean) => void;
    /** The session or panel readiness is itself unknown and already explained by an outer hold. */
    suspended?: boolean;
}) {
    const { user } = useAuth();
    const navigate = useNavigate();
    const location = useLocation();
    const { t, screensReady, screensFailed } = useI18n();
    const [access, setAccess] = useState<(AccessObservation & { owner: string }) | null>(null);
    const [failed, setFailed] = useState(false);
    const [pending, setPending] = useState(false);
    const controller = useRef<AbortController | null>(null);
    const mountedFor = useRef<string | null>(null);
    const latest = useRef(access);
    latest.current = access;
    const check = useCallback(async () => {
        // Focus and periodic checks share the pending request.
        // Odak ve zamanlayıcı kontrolleri sürmekte olan isteği paylaşır.
        if (controller.current) return;
        const request = new AbortController();
        controller.current = request;
        setPending(true);
        const timeout = window.setTimeout(() => request.abort(), 15000);
        try {
            const response = await fetch('/api/v1/license/access', { cache: 'no-store', signal: request.signal });
            if (!response.ok) throw new Error('access unavailable');
            const result = parseAccessObservation(await response.json());
            if (!request.signal.aborted && controller.current === request) {
                setAccess({ owner: user.username, ...result });
                setFailed(result.allowed === null);
            }
        } catch {
            if (controller.current === request) {
                // A failed request is not a license rejection. Keep only an
                // unexpired decision for this identity; never extend its deadline.
                // Bağlantı hatası lisans reddi değildir; bu kimliğin geçerli kararı
                // yalnızca sunucunun belirlediği süre dolana kadar korunur.
                setAccess(previous => previous?.owner === user.username
                    && (previous.allowed === false || (previous.allowed && previous.until * 1000 > Date.now()))
                    ? previous : { owner: user.username, allowed: null, until: 0, state: 'status_unavailable' });
                setFailed(true);
            }
        } finally {
            window.clearTimeout(timeout);
            if (controller.current === request) { controller.current = null; setPending(false); }
        }
    }, [user.username]);

    useEffect(() => {
        void check();
        // Only a mounted authenticated browser checks; the server has no idle timer.
        const interval = window.setInterval(() => { if (document.visibilityState === 'visible') void check(); }, 60000);
        // A browser may delay the timers of a hidden tab. On return the deadline is
        // applied by the clock before anything is read, so a decision that has run
        // out is never used while its replacement is in flight.
        // Tarayici gizli sekmenin zamanlayicilarini geciktirebilir. Donuste sure
        // saate gore uygulanir; suresi dolan karar yenisi okunurken kullanilmaz.
        const returned = () => {
            void check();
            setAccess(previous => previous?.allowed && previous.until * 1000 <= Date.now() ? { ...previous, allowed: null } : previous);
        };
        const shown = () => { if (document.visibilityState === 'visible') returned(); };
        const locked = () => {
            // The server refused a management request for want of a current
            // decision. Pages that stay mounted report this with every poll; one
            // read answers all of them, so a read that began after access became
            // unknown is left to finish. Only typed status can require activation.
            if (latest.current?.allowed !== null) { controller.current?.abort(); controller.current = null; }
            // The read is in flight before access is marked unknown, so no render
            // shows an unknown state that nothing is reading.
            void check();
            setFailed(true);
            setAccess({ owner: user.username, allowed: null, until: 0, state: 'status_unavailable' });
        };
        window.addEventListener('focus', returned);
        document.addEventListener('visibilitychange', shown);
        window.addEventListener('celikpanel:license-locked', locked);
        return () => {
            controller.current?.abort(); controller.current = null;
            window.clearInterval(interval);
            window.removeEventListener('focus', returned);
            document.removeEventListener('visibilitychange', shown);
            window.removeEventListener('celikpanel:license-locked', locked);
        };
    }, [check, user.username]);

    useEffect(() => {
        if (!access?.allowed) return;
        // Refresh shortly before the server's hard deadline so normal renewals
        // preserve the current page and form state.
        const refreshDelay = access.until * 1000 - Date.now() - 15000;
        const refreshTimer = refreshDelay > 0 ? window.setTimeout(() => {
            if (document.visibilityState === 'visible') void check();
        }, Math.min(2147483647, refreshDelay)) : undefined;
        // At the deadline the decision is no longer used. That is not a failed
        // check: a hidden tab is deliberately not refreshed, and it reads again
        // when the owner returns to it.
        const timer = window.setTimeout(() => {
            if (document.visibilityState === 'visible') void check();
            setAccess(previous => previous ? { ...previous, allowed: null } : null);
        }, Math.min(2147483647, Math.max(0, access.until * 1000 - Date.now())));
        return () => { window.clearTimeout(timer); window.clearTimeout(refreshTimer); };
    }, [access, check]);

    const known = access?.owner === user.username ? access : null;
    const allowed = known?.allowed === true;
    const denied = known?.allowed === false;
    // Management was mounted for this identity and no known negative has arrived since.
    if (allowed) mountedFor.current = user.username;
    else if (denied || mountedFor.current !== user.username) mountedFor.current = null;
    const held = !allowed && mountedFor.current === user.username;
    const unknown = !allowed && !denied && access !== null;

    useEffect(() => {
        if (!unknown) return;
        const timer = window.setInterval(() => { if (document.visibilityState === 'visible') void check(); }, UNKNOWN_RECHECK_MS);
        return () => window.clearInterval(timer);
    }, [unknown, check]);

    // Pause the root update overlay while activation owns the screen. This
    // changes browser tracking only; the server's running operation continues.
    useLayoutEffect(() => { onAccessChange?.(!!allowed); }, [allowed, onAccessChange]);
    useLayoutEffect(() => {
        onRecoveryChange?.(!!screensFailed || (!!failed && !allowed));
        return () => onRecoveryChange?.(false);
    }, [screensFailed, failed, allowed, onRecoveryChange]);
    useEffect(() => {
        if (!access || access.owner !== user.username) return;
        if (access.allowed === false && location.pathname !== '/activate') navigate('/activate', { replace: true });
        if (allowed && location.pathname === '/activate') navigate('/', { replace: true });
    }, [access, allowed, location.pathname, navigate, user.username]);
    if (allowed || held) return <AccessHold active={held} silent={suspended} cause="license" checking={pending} user={user} onRetry={() => void check()}>
        {allowed && failed && <div role="status" className="flex flex-wrap items-center justify-center gap-3 border-b border-border bg-surface px-4 py-3 text-sm text-fg">
            <p className="max-w-prose">{t('license.connectionRetry')}</p>
            <Button variant="secondary" onClick={() => void check()}>{t('license.refresh')}</Button>
            <Button variant="secondary" onClick={() => window.location.reload()}>{t('common.reloadPage')}</Button>
        </div>}
        {children}
    </AccessHold>;
    const loading = <div className="min-h-screen flex items-center justify-center bg-bg"><Spinner /></div>;
    if (screensFailed) return <RecoveryAccess user={user} cause="bundle" onRetry={() => void check()} />;
    if (failed && !allowed) return <RecoveryAccess user={user} cause="license" onRetry={() => void check()} />;
    if (!screensReady) return loading;
    return <Suspense fallback={loading}><LicenseLockScreen checking={!access || access.owner !== user.username || access.allowed === null} failed={failed} onCheck={() => void check()} /></Suspense>;
}
