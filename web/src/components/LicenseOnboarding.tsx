import { lazy, Suspense, useCallback, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { useLocation, useNavigate } from '../router';
import { useAuth } from '../auth/AuthContext';
import { useI18n } from '../i18n';
import { Button, Spinner } from './ui';

const LicenseLockScreen = lazy(() => import('./LicenseLockScreen').then(module => ({ default: module.LicenseLockScreen })));

/** Never mount management pages before a positive, server-verified decision. */
export function LicenseOnboarding({ children, onAccessChange }: { children?: ReactNode; onAccessChange?: (allowed: boolean) => void }) {
    const { user } = useAuth();
    const navigate = useNavigate();
    const location = useLocation();
    const { t, screensReady, screensFailed } = useI18n();
    const [access, setAccess] = useState<{ owner: string; allowed: boolean; until: number } | null>(null);
    const [failed, setFailed] = useState(false);
    const controller = useRef<AbortController | null>(null);
    const check = useCallback(async () => {
        controller.current?.abort();
        const request = new AbortController();
        controller.current = request;
        const timeout = window.setTimeout(() => request.abort(), 15000);
        setFailed(false);
        try {
            const response = await fetch('/api/v1/license/access', { cache: 'no-store', signal: request.signal });
            if (!response.ok) throw new Error('access unavailable');
            const result = await response.json();
            if (typeof result.can_use_panel !== 'boolean' || !Number.isSafeInteger(result.valid_until)
                || (result.can_use_panel && result.valid_until <= Date.now() / 1000)) throw new Error('invalid access');
            if (!request.signal.aborted) setAccess({ owner: user.username, allowed: result.can_use_panel, until: result.valid_until });
        } catch {
            if (controller.current === request) {
                setAccess({ owner: user.username, allowed: false, until: 0 });
                setFailed(true);
            }
        } finally { window.clearTimeout(timeout); }
    }, [user.username]);

    useEffect(() => {
        void check();
        // Only a mounted authenticated browser checks; the server has no idle timer.
        const interval = window.setInterval(() => { if (document.visibilityState === 'visible') void check(); }, 60000);
        const focus = () => void check();
        const locked = () => {
            controller.current?.abort();
            controller.current = null;
            setAccess({ owner: user.username, allowed: false, until: 0 });
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
            setAccess(previous => previous ? { ...previous, allowed: false } : null);
            if (document.visibilityState === 'visible') void check();
        }, Math.min(2147483647, Math.max(0, access.until * 1000 - Date.now())));
        return () => { window.clearTimeout(timer); window.clearTimeout(refreshTimer); };
    }, [access, check]);

    const allowed = access?.owner === user.username && access.allowed;
    // Pause the root update overlay while activation owns the screen. This
    // changes browser tracking only; the server's running operation continues.
    useLayoutEffect(() => { onAccessChange?.(!!allowed); }, [allowed, onAccessChange]);
    useEffect(() => {
        if (!access || access.owner !== user.username) return;
        if (!allowed && location.pathname !== '/activate') navigate('/activate', { replace: true });
        if (allowed && location.pathname === '/activate') navigate('/', { replace: true });
    }, [access, allowed, location.pathname, navigate, user.username]);
    if (allowed) return <>{children}</>;
    const loading = <div className="min-h-screen flex items-center justify-center bg-bg"><Spinner /></div>;
    if (screensFailed) return <div className="min-h-screen flex flex-col items-center justify-center gap-4 bg-bg"><p>{t('app.pageLoadFailed')}</p><Button onClick={() => window.location.reload()}>{t('app.reload')}</Button></div>;
    if (!screensReady) return loading;
    return <Suspense fallback={loading}><LicenseLockScreen checking={!access || access.owner !== user.username} failed={failed} onCheck={() => void check()} /></Suspense>;
}
