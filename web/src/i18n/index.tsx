import { createContext, useContext, useEffect, useState, type ReactNode } from 'react';
import type { TranslationKey } from './en';

// A small, dependency-free i18n layer. Two primary locales (tr, en) ship
// complete; adding another is just another catalog. Strings never live in
// components — they come from a key here (see docs/CONVENTIONS.md).
//
// Küçük, bağımlılıksız bir i18n katmanı. İki öncelikli yerel ayar (tr, en)
// eksiksiz gelir; başka bir dil eklemek yalnızca yeni bir katalogdur.
// Metinler bileşenlerde yaşamaz — buradaki bir anahtardan gelir
// (bkz. docs/CONVENTIONS.md).
export type Locale = 'tr' | 'en';
type Catalog = Record<TranslationKey, string>;

const STORAGE_KEY = 'celikpanel.lang';
const bootstrapLabels: Record<Locale, { loading: string; reload: string }> = {
    en: { loading: 'Loading…', reload: 'Reload CelikPanel' },
    tr: { loading: 'Yükleniyor…', reload: 'CelikPanel’i yeniden yükle' },
};

function detectLocale(): Locale {
    const stored = readStoredLocale();
    if (stored === 'tr' || stored === 'en') return stored;
    return navigator.language?.toLowerCase().startsWith('tr') ? 'tr' : 'en';
}

function readStoredLocale(): string | null {
    try {
        return window.localStorage.getItem(STORAGE_KEY);
    } catch {
        return null;
    }
}

function persistLocale(locale: Locale) {
    try {
        window.localStorage.setItem(STORAGE_KEY, locale);
    } catch {
        // Language switching must still work when storage is blocked.
    }
}

interface I18nContextValue {
    locale: Locale;
    setLocale: (l: Locale) => void;
    t: (key: TranslationKey, vars?: Record<string, string | number>) => string;
}

const I18nContext = createContext<I18nContextValue | null>(null);

async function loadCatalog(locale: Locale): Promise<Catalog> {
    if (locale === 'tr') {
        return (await import('./tr')).tr;
    }
    return (await import('./en')).en;
}

export function I18nProvider({ children }: { children: ReactNode }) {
    const [targetLocale, setTargetLocale] = useState<Locale>(detectLocale);
    const [loaded, setLoaded] = useState<{ locale: Locale; catalog: Catalog } | null>(null);
    const [loadFailed, setLoadFailed] = useState(false);
    const activeLocale = loaded?.locale;
    const boot = bootstrapLabels[targetLocale];

    useEffect(() => {
        if (activeLocale === targetLocale) return;

        let current = true;
        setLoadFailed(false);

        loadCatalog(targetLocale)
            .then((catalog) => {
                if (!current) return;
                persistLocale(targetLocale);
                setLoaded({ locale: targetLocale, catalog });
            })
            .catch((error) => {
                if (!current) return;
                if (activeLocale) {
                    console.error(`Could not load locale ${targetLocale}`, error);
                    setTargetLocale(activeLocale);
                    return;
                }
                if (targetLocale !== 'en') {
                    setTargetLocale('en');
                    return;
                }
                setLoadFailed(true);
            });

        return () => {
            current = false;
        };
    }, [activeLocale, targetLocale]);

    const setLocale = (l: Locale) => {
        setTargetLocale(l);
    };

    const t = (key: TranslationKey, vars?: Record<string, string | number>) => {
        let text = loaded?.catalog[key] ?? key;
        if (vars) {
            for (const [name, value] of Object.entries(vars)) {
                text = text.replace(new RegExp(`\\{${name}\\}`, 'g'), String(value));
            }
        }
        return text;
    };

    if (!loaded) {
        if (loadFailed) {
            return (
                <div className="flex min-h-screen items-center justify-center bg-bg" role="alert">
                    <button type="button" className="rounded-lg border border-border bg-surface px-4 py-2 text-sm text-fg" onClick={() => window.location.reload()}>
                        {boot.reload}
                    </button>
                </div>
            );
        }

        return (
            <div className="flex min-h-screen items-center justify-center bg-bg" role="status" aria-label={boot.loading}>
                <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary" />
            </div>
        );
    }

    return <I18nContext.Provider value={{ locale: loaded.locale, setLocale, t }}>{children}</I18nContext.Provider>;
}

export function useI18n(): I18nContextValue {
    const ctx = useContext(I18nContext);
    if (!ctx) throw new Error('useI18n must be used within I18nProvider');
    return ctx;
}
