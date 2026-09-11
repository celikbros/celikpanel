import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react';
import { useAuth } from '../auth/AuthContext';
import { useI18n } from '../i18n';
import { Link, Navigate, useLocation } from '../router';
import { decodeServerSetup, isSetupRecoveryPath, shouldOpenServerSetup, type ServerSetupSnapshot } from '../lib/serverSetup';
import { BrandMark } from './BrandMark';
import { LanguageSwitcher } from './LanguageSwitcher';
import { ThemeSwitcher } from './ThemeSwitcher';
import { ChangePasswordModal } from './ChangePasswordModal';
import { Button, Spinner } from './ui';

interface ServerSetupContextValue {
    snapshot: ServerSetupSnapshot | null;
    reload: (checks?: boolean) => Promise<ServerSetupSnapshot>;
    accept: (snapshot: ServerSetupSnapshot) => void;
}
const ServerSetupContext = createContext<ServerSetupContextValue | null>(null);
export function useServerSetup() { return useContext(ServerSetupContext); }

export function ServerSetupShell({ children }: { children: ReactNode }) {
    const { logout } = useAuth();
    const { t } = useI18n();
    const [leaving, setLeaving] = useState(false);
    const [logoutError, setLogoutError] = useState(false);
    const [password, setPassword] = useState(false);
    async function signOut() {
        setLeaving(true); setLogoutError(false);
        try {
            const response = await fetch('/api/v1/auth/logout', { method: 'POST' });
            if (!response.ok && response.status !== 401) throw new Error('logout');
            logout();
        } catch { setLogoutError(true); } finally { setLeaving(false); }
    }
    return <div className="min-h-screen bg-bg text-fg">
        <header className="border-b border-border bg-surface px-4 py-4 sm:px-8">
            <div className="mx-auto flex max-w-5xl flex-wrap items-center justify-between gap-4">
                <div className="flex items-center gap-3 font-semibold"><BrandMark className="h-7 w-7 text-primary" />CelikPanel</div>
                <div className="flex flex-wrap items-center gap-2"><LanguageSwitcher /><ThemeSwitcher /><Button variant="secondary" disabled={leaving} onClick={() => void signOut()}>{t('user.logout')}</Button></div>
            </div>
        </header>
        <main className="mx-auto max-w-5xl px-4 py-8 sm:px-8 sm:py-12">
            {logoutError && <p role="alert" className="mb-4 text-danger">{t('common.error')}</p>}
            {children}
            <details className="mt-10 border-t border-border pt-5 text-sm">
                <summary className="cursor-pointer font-semibold text-primary focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary">{t('setup.recovery')}</summary>
                <div className="mt-4 flex flex-wrap items-center gap-5">
                    <Link to="/settings?section=panel" className="text-primary underline underline-offset-4">{t('setup.recoveryAccess')}</Link>
                    <Link to="/settings?section=dns" className="text-primary underline underline-offset-4">{t('setup.recoveryDNS')}</Link>
                    <Link to="/services" className="text-primary underline underline-offset-4">{t('nav.services')}</Link>
                    <Link to="/settings?section=updates" className="text-primary underline underline-offset-4">{t('start.updates.action')}</Link>
                    <Button variant="secondary" onClick={() => setPassword(true)}>{t('profile.changePassword')}</Button>
                </div>
            </details>
        </main>
        {password && <ChangePasswordModal onClose={() => setPassword(false)} />}
    </div>;
}

export function ServerSetupGate({ children }: { children: ReactNode }) {
    const { role, user } = useAuth();
    const { t, screensReady, screensFailed } = useI18n();
    const location = useLocation();
    const [snapshot, setSnapshot] = useState<ServerSetupSnapshot | null>(null);
    const [failed, setFailed] = useState(false);
    const generation = useRef(0);
    const controller = useRef<AbortController | null>(null);
    const reload = useCallback(async (checks = false) => {
        const current = ++generation.current;
        controller.current?.abort();
        const pending = new AbortController(); controller.current = pending;
        const timeout = window.setTimeout(() => pending.abort(), checks ? 25000 : 15000);
        try {
            const response = await fetch(`/api/v1/setup${checks ? '?check=1' : ''}`, { cache: 'no-store', signal: pending.signal });
            if (!response.ok) throw new Error('setup status');
            const next = decodeServerSetup(await response.json());
            if (!next) throw new Error('setup contract');
            if (current === generation.current) { setSnapshot(next); setFailed(false); }
            return next;
        } catch (error) {
            if (current === generation.current) setFailed(true);
            throw error;
        } finally { window.clearTimeout(timeout); }
    }, []);
    useEffect(() => {
        setSnapshot(null); setFailed(false);
        if (role === 'admin') void reload().catch(() => {});
        return () => { generation.current++; controller.current?.abort(); };
    }, [role, user.username, reload]);
    if (role !== 'admin') return location.pathname === '/setup' ? <Navigate to="/" replace /> : <>{children}</>;
    if (!screensReady) return <div className="min-h-screen flex items-center justify-center bg-bg">{screensFailed ? <Button onClick={() => window.location.reload()}>{t('app.reload')}</Button> : <Spinner />}</div>;
    const value = { snapshot, reload, accept: setSnapshot };
    // Recovery pages stay available after an assessment error. No failed read
    // is interpreted as a fresh host, a completed setup, or install permission.
    if (isSetupRecoveryPath(location.pathname)) return <ServerSetupContext.Provider value={value}>{children}</ServerSetupContext.Provider>;
    if (!snapshot) return <ServerSetupShell><div className="max-w-2xl space-y-5" role={failed ? 'alert' : undefined}>
        <h1 className="text-2xl font-semibold">{t(failed ? 'setup.loadFailedTitle' : 'setup.loading')}</h1>
        {failed ? <><p className="text-fg-muted">{t('setup.loadFailed')}</p><Button variant="primary" onClick={() => void reload().catch(() => {})}>{t('common.retry')}</Button></> : <Spinner />}
    </div></ServerSetupShell>;
    return <ServerSetupContext.Provider value={value}>
        {shouldOpenServerSetup(snapshot, location.pathname) ? <Navigate to="/setup" replace /> : children}
    </ServerSetupContext.Provider>;
}

export function ServerSetupDashboardNotice() {
    const setup = useServerSetup();
    const { t } = useI18n();
    if (!setup?.snapshot) return null;
    const snapshot = setup.snapshot;
    if (snapshot.guidance === 'manual' && !['running', 'waiting'].includes(snapshot.status)) return null;
    return <section className="mt-6 flex flex-wrap items-center justify-between gap-4 rounded-xl border border-border bg-surface px-5 py-5" aria-labelledby="setup-dashboard-title">
        <div className="min-w-0 flex-1 basis-72"><h2 id="setup-dashboard-title" className="font-semibold">{t(snapshot.status === 'ready' ? 'setup.dashboardReady' : 'setup.dashboardTitle')}</h2>
            <p className="mt-1 max-w-2xl text-sm text-fg-muted">{snapshot.status === 'ready' ? t(snapshot.draft.customization ? 'setup.purpose.custom' : `setup.purpose.${snapshot.draft.purpose}`) : t(snapshot.origin === 'legacy' ? 'setup.dashboardLegacy' : 'setup.dashboardResume')}</p></div>
        {snapshot.status !== 'ready' && <Link to="/setup" className="rounded-lg border border-border-strong px-4 py-2 text-sm font-semibold text-primary hover:bg-surface-2 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary">{t(snapshot.status === 'legacy' ? 'setup.dashboardAction' : 'setup.resume')}</Link>}
    </section>;
}
