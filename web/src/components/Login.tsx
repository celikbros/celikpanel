import { useState } from 'react';
import { api, type CurrentUser } from '../lib/api';

// Bilingual strings live here until the Phase 1 i18n system lands; this
// small dictionary previews that pattern (see docs/CONVENTIONS.md).
// İki dilli metinler, Faz 1 i18n sistemi gelene kadar burada; bu küçük
// sözlük o deseni önizler (bkz. docs/CONVENTIONS.md).
const strings = {
  tr: {
    subtitle: 'Kontrol Paneli',
    username: 'Kullanıcı adı',
    password: 'Parola',
    signIn: 'Giriş yap',
    signingIn: 'Giriş yapılıyor…',
    invalid: 'Kullanıcı adı ya da parola hatalı.',
    failed: 'Giriş başarısız. Lütfen tekrar deneyin.',
  },
  en: {
    subtitle: 'Control Panel',
    username: 'Username',
    password: 'Password',
    signIn: 'Sign in',
    signingIn: 'Signing in…',
    invalid: 'Invalid username or password.',
    failed: 'Login failed. Please try again.',
  },
};

type Lang = keyof typeof strings;

export function Login({ onSuccess }: { onSuccess: (user: CurrentUser) => void }) {
  const [lang, setLang] = useState<Lang>('tr');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const t = strings[lang];

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setSubmitting(true);
    try {
      const user = await api.login(username, password);
      onSuccess(user);
    } catch (err) {
      setError((err as Error).message === 'invalid_credentials' ? t.invalid : t.failed);
      setSubmitting(false);
    }
  };

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-950 text-gray-100 px-4">
      <div className="w-full max-w-sm">
        <div className="flex justify-end mb-4">
          <button
            type="button"
            onClick={() => setLang(lang === 'tr' ? 'en' : 'tr')}
            className="text-xs text-gray-400 hover:text-gray-200 border border-gray-700 rounded px-2 py-1"
          >
            {lang === 'tr' ? 'EN' : 'TR'}
          </button>
        </div>

        <div className="text-center mb-8">
          <h1 className="text-3xl font-bold tracking-tight">CelikPanel</h1>
          <p className="text-gray-400 mt-1">{t.subtitle}</p>
        </div>

        <form onSubmit={handleSubmit} className="bg-gray-900 border border-gray-800 rounded-xl p-6 space-y-4">
          <div>
            <label className="block text-sm text-gray-400 mb-1">{t.username}</label>
            <input
              type="text"
              autoFocus
              autoComplete="username"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 focus:outline-none focus:ring-2 focus:ring-blue-500"
              required
            />
          </div>

          <div>
            <label className="block text-sm text-gray-400 mb-1">{t.password}</label>
            <input
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 focus:outline-none focus:ring-2 focus:ring-blue-500"
              required
            />
          </div>

          {error && (
            <p className="text-sm text-red-400 bg-red-950/50 border border-red-900 rounded-lg px-3 py-2">
              {error}
            </p>
          )}

          <button
            type="submit"
            disabled={submitting}
            className="w-full bg-blue-600 hover:bg-blue-500 disabled:opacity-60 disabled:cursor-not-allowed rounded-lg px-3 py-2 font-medium transition-colors"
          >
            {submitting ? t.signingIn : t.signIn}
          </button>
        </form>
      </div>
    </div>
  );
}
