import { useState, useEffect } from 'react';
import { Server, ShieldCheck, Users, User } from 'lucide-react';
import { api, type CurrentUser, type DemoAccount } from '../lib/api';
import { useI18n } from '../i18n';
import { ThemeSwitcher } from './ThemeSwitcher';
import { SkinSwitcher } from './SkinSwitcher';
import { LanguageSwitcher } from './LanguageSwitcher';

const roleIcon: Record<string, typeof User> = {
    admin: ShieldCheck,
    reseller: Users,
    customer: User,
};

// The front door. Theme- and language-aware, it uses the shared i18n and
// theme systems so it looks like the rest of the panel from the first
// screen the user ever sees.
//
// Ön kapı. Tema- ve dil-farkında; paylaşılan i18n ve tema sistemlerini
// kullanır; böylece kullanıcının gördüğü ilk ekrandan itibaren panelin geri
// kalanı gibi görünür.
export function Login({ onSuccess }: { onSuccess: (user: CurrentUser) => void }) {
    const { t } = useI18n();
    const [username, setUsername] = useState('');
    const [password, setPassword] = useState('');
    const [error, setError] = useState('');
    const [submitting, setSubmitting] = useState(false);
    const [demo, setDemo] = useState<DemoAccount[]>([]);

    // Demo accounts only come back when the server runs with --demo, so this
    // panel simply never appears in production.
    // Demo hesapları yalnızca sunucu --demo ile çalışırken döner; bu yüzden
    // bu panel üretimde asla görünmez.
    useEffect(() => {
        api.demoAccounts().then(setDemo).catch(() => {});
    }, []);

    const fillDemo = (acc: DemoAccount) => {
        setUsername(acc.username);
        setPassword(acc.password);
        setError('');
    };

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        setError('');
        setSubmitting(true);
        try {
            const user = await api.login(username, password);
            onSuccess(user);
        } catch (err) {
            const code = (err as Error).message;
            setError(
                code === 'invalid_credentials'
                    ? t('login.invalid')
                    : code === 'too_many'
                      ? t('login.tooMany')
                      : t('login.failed'),
            );
            setSubmitting(false);
        }
    };

    return (
        <div className="relative flex min-h-screen items-center justify-center bg-bg px-4 text-fg">
            <div className="absolute right-4 top-4 flex items-center gap-2">
                <LanguageSwitcher />
                <SkinSwitcher />
                        <ThemeSwitcher />
            </div>

            <div className="w-full max-w-sm">
                <div className="mb-8 flex flex-col items-center text-center">
                    <div className="mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-primary text-primary-fg shadow-lg">
                        <Server className="h-7 w-7" />
                    </div>
                    <h1 className="text-2xl font-bold tracking-tight">{t('app.name')}</h1>
                    <p className="mt-1 text-sm text-fg-muted">{t('login.subtitle')}</p>
                </div>

                <form
                    onSubmit={handleSubmit}
                    className="space-y-4 rounded-2xl border border-border bg-surface p-6 shadow-card"
                >
                    <div>
                        <label htmlFor="username" className="mb-1.5 block text-sm font-medium text-fg-muted">
                            {t('login.username')}
                        </label>
                        <input
                            id="username"
                            type="text"
                            autoFocus
                            autoComplete="username"
                            value={username}
                            onChange={(e) => setUsername(e.target.value)}
                            className="w-full rounded-lg border border-border bg-surface-2 px-3 py-2 text-fg outline-none transition-shadow focus:border-primary focus:ring-2 focus:ring-primary/30"
                            required
                        />
                    </div>

                    <div>
                        <label htmlFor="password" className="mb-1.5 block text-sm font-medium text-fg-muted">
                            {t('login.password')}
                        </label>
                        <input
                            id="password"
                            type="password"
                            autoComplete="current-password"
                            value={password}
                            onChange={(e) => setPassword(e.target.value)}
                            className="w-full rounded-lg border border-border bg-surface-2 px-3 py-2 text-fg outline-none transition-shadow focus:border-primary focus:ring-2 focus:ring-primary/30"
                            required
                        />
                    </div>

                    {error && (
                        <p className="rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-sm text-danger">
                            {error}
                        </p>
                    )}

                    <button
                        type="submit"
                        disabled={submitting}
                        className="w-full rounded-lg bg-primary px-3 py-2.5 font-medium text-primary-fg transition-colors hover:bg-primary-hover disabled:cursor-not-allowed disabled:opacity-60"
                    >
                        {submitting ? t('login.signingIn') : t('login.signIn')}
                    </button>
                </form>

                {demo.length > 0 && (
                    <div className="mt-4 rounded-2xl border border-dashed border-border bg-surface/60 p-4">
                        <div className="mb-1 flex items-center gap-2 text-sm font-semibold text-fg">
                            <span className="inline-flex h-1.5 w-1.5 rounded-full bg-warning" />
                            {t('login.demoTitle')}
                        </div>
                        <p className="mb-3 text-xs text-fg-muted">{t('login.demoHint')}</p>
                        <div className="space-y-2">
                            {demo.map((acc) => {
                                const Icon = roleIcon[acc.role] ?? User;
                                return (
                                    <button
                                        key={acc.username}
                                        type="button"
                                        onClick={() => fillDemo(acc)}
                                        className="flex w-full items-center gap-3 rounded-lg border border-border bg-surface-2 px-3 py-2 text-left transition-colors hover:border-primary hover:bg-surface"
                                    >
                                        <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
                                            <Icon className="h-4 w-4" />
                                        </span>
                                        <span className="min-w-0 flex-1">
                                            <span className="block text-sm font-medium capitalize text-fg">{acc.role}</span>
                                            <span className="block truncate font-mono text-xs text-fg-muted">
                                                {acc.username} · {acc.password}
                                            </span>
                                        </span>
                                    </button>
                                );
                            })}
                        </div>
                    </div>
                )}
            </div>
        </div>
    );
}
