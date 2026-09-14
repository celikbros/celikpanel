import { lazy, Suspense, useCallback, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { useLocation, useNavigate } from '../router';
import { useAuth } from '../auth/AuthContext';
import { useI18n } from '../i18n';
import { Button, Spinner } from './ui';
import { parseAccessObservation, type AccessObservation } from '../lib/accessObservation';
import { RecoveryAccess } from './RecoveryAccess';

const LicenseLockScreen = lazy(() => import('./LicenseLockScreen').then(module => ({ default: module.LicenseLockScreen })));

/** Never mount management pages before a positive, server-verified decision. */
export function LicenseOnboarding({ children, onAccessChange, onRecoveryChange }: { children?: ReactNode; onAccessChange?: (allowed: boolean) => void; onRecoveryChange?: (recovering: boolean) => void }) {
    const { user } = useAuth();
    const navigate = useNavigate();
    const location = useLocation();
    const { t, screensReady, screensFailed } = useI18n();
    const [access, setAccess] = useState<(AccessObservation & { owner: string }) | null>(null);
    const [failed, setFailed] = useState(false);
    const controller = useRef<AbortController | null>(null);
    const check = useCallback(async () => {
        // Focus and periodic checks share the pending request.
        // Odak ve zamanlayıcı kontrolleri sürmekte olan isteği paylaşır.
        if (controller.current) return;
        const request = new AbortController();
        controller.current = request;
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
            if (controller.current === request) controller.current = null;
        }
    }, [user.username]);

    useEffect(() => {
        void check();
        // Only a mounted authenticated browser checks; the server has no idle timer.
        const interval = window.setInterval(() => { if (document.visibilityState === 'visible') void check(); }, 60000);
        const focus = () => void check();
        const locked = () => {
            controller.current?.abort();
            controller.current = null;
            // A generic gate blocks management; only typed status can require activation.
            setFailed(true);
            setAccess({ owner: user.username, allowed: null, until: 0, state: 'status_unavailable' });
            void check();
        };
        window.addEventListener('focus', focus);
        window.addEventListener('celikpanel:license-locked', locked);
        return () => {
            controller.current?.abort(); controller.current = null;
            window.clearInterval(interval);
            window.removeEventListener('focus', focus);
            window.removeEventListener('celikpanel:license-locked', locked);
        };
    }, [check, user.username]);

    useEffect(() => {
        if (!access?.allowed) return;
        // Refresh shortly before the server's hard deadline so normal renewals
        // preserve the current page and form state. The deadline still locks
        // management if verification cannot finish in time.
        const refreshDelay = access.until * 1000 - Date.now() - 15000;
        const refreshTimer = refreshDelay > 0 ? window.setTimeout(() => {
            if (document.visibilityState === 'visible') void check();
        }, Math.min(2147483647, refreshDelay)) : undefined;
        const timer = window.setTimeout(() => {
            setAccess(previous => previous ? { ...previous, allowed: null } : null);
            setFailed(true);
            if (document.visibilityState === 'visible') void check();
        }, Math.min(2147483647, Math.max(0, access.until * 1000 - Date.now())));
        return () => { window.clearTimeout(timer); window.clearTimeout(refreshTimer); };
    }, [access, check]);

    const allowed = access?.owner === user.username && access.allowed;
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
    if (allowed) return <>
        {failed && <div role="status" className="flex flex-wrap items-center justify-center gap-3 border-b border-border bg-surface px-4 py-3 text-sm text-fg">
            <p className="max-w-prose">{t('license.connectionRetry')}</p>
            <Button variant="secondary" onClick={() => void check()}>{t('license.refresh')}</Button>
            <Button variant="secondary" onClick={() => window.location.reload()}>{t('common.reloadPage')}</Button>
        </div>}
        {children}
    </>;
    const loading = <div className="min-h-screen flex items-center justify-center bg-bg"><Spinner /></div>;
    if (screensFailed) return <RecoveryAccess user={user} cause="bundle" onRetry={() => void check()} />;
    if (failed && !allowed) return <RecoveryAccess user={user} cause="license" onRetry={() => void check()} />;
    if (!screensReady) return loading;
    return <Suspense fallback={loading}><LicenseLockScreen checking={!access || access.owner !== user.username || access.allowed === null} failed={failed} onCheck={() => void check()} /></Suspense>;
}
