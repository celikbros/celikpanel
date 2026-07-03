import { useState } from 'react';
import { Server } from 'lucide-react';
import { api, type CurrentUser } from '../lib/api';
import { useI18n } from '../i18n';
import { ThemeSwitcher } from './ThemeSwitcher';
import { LanguageSwitcher } from './LanguageSwitcher';

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
            </div>
        </div>
    );
}
