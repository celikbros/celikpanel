import { useState } from 'react';
import { useAuth } from '../auth/AuthContext';
import { useI18n } from '../i18n';
import { BrandMark } from './BrandMark';
import { LanguageSwitcher } from './LanguageSwitcher';
import { ThemeSwitcher } from './ThemeSwitcher';
import { ChangePasswordModal } from './ChangePasswordModal';
import { LicensePanel } from './LicensePanel';
import { ToastContainer } from './Toast';
import { Button, Spinner } from './ui';

export function LicenseLockScreen({ checking, failed, onCheck }: { checking: boolean; failed: boolean; onCheck: () => void }) {
    const { role, user, logout } = useAuth();
    const { t } = useI18n();
    const [password, setPassword] = useState(false);
    const [leaving, setLeaving] = useState(false);
    const [error, setError] = useState(false);
    async function signOut() {
        setLeaving(true); setError(false);
        try {
            const response = await fetch('/api/v1/auth/logout', { method: 'POST' });
            if (!response.ok && response.status !== 401) throw new Error('logout');
            logout();
        } catch { setError(true); } finally { setLeaving(false); }
    }
    return <div className="min-h-screen bg-bg text-fg">
        <header className="border-b border-border bg-surface px-4 py-5 sm:px-8">
            <div className="mx-auto flex max-w-3xl flex-wrap items-center justify-between gap-4">
                <div className="flex items-center gap-3 font-semibold"><BrandMark className="h-7 w-7 text-primary" />CelikPanel</div>
                <div className="flex items-center gap-2"><LanguageSwitcher /><ThemeSwitcher /><Button variant="secondary" disabled={leaving} onClick={() => void signOut()}>{t('user.logout')}</Button></div>
            </div>
        </header>
        <main className="mx-auto max-w-3xl px-4 py-10 sm:px-8 sm:py-16">
            <p className="mb-5 break-words text-sm text-fg-muted">{user.username}</p>
            {error && <p role="alert" className="mb-4 text-danger">{t('common.error')}</p>}
            {failed && <div role="alert" className="mb-5 space-y-3"><p>{t('license.lockError')}</p><Button variant="secondary" onClick={onCheck}>{t('license.refresh')}</Button></div>}
            {checking ? <div className="flex items-center gap-3"><Spinner /><p>{t('license.lockCheck')}</p></div>
                : role === 'admin' ? <LicensePanel locked onContinue={onCheck} />
                    : <section className="space-y-5" aria-labelledby="license-lock-heading"><h1 id="license-lock-heading" className="text-2xl font-semibold">{t('license.tenantTitle')}</h1><p className="max-w-prose">{t('license.tenantHelp')}</p><p className="max-w-prose text-sm text-fg-muted">{t('license.restricted')}</p><Button onClick={onCheck}>{t('license.refresh')}</Button></section>}
            <div className="mt-8"><Button variant="secondary" onClick={() => setPassword(true)}>{t('profile.changePassword')}</Button></div>
        </main>
        {password && <ChangePasswordModal onClose={() => setPassword(false)} />}
        <ToastContainer />
    </div>;
}
