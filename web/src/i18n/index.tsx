import { createContext, useContext, useState, type ReactNode } from 'react';
import { en, type TranslationKey } from './en';
import { tr } from './tr';

// A small, dependency-free i18n layer. Two primary locales (tr, en) ship
// complete; adding another is just another catalog. Strings never live in
// components — they come from a key here (see docs/CONVENTIONS.md).
//
// Küçük, bağımlılıksız bir i18n katmanı. İki öncelikli yerel ayar (tr, en)
// eksiksiz gelir; başka bir dil eklemek yalnızca yeni bir katalogdur.
// Metinler bileşenlerde yaşamaz — buradaki bir anahtardan gelir
// (bkz. docs/CONVENTIONS.md).
export type Locale = 'tr' | 'en';

const catalogs: Record<Locale, Record<TranslationKey, string>> = { en, tr };

const STORAGE_KEY = 'celikpanel.lang';

function detectLocale(): Locale {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (stored === 'tr' || stored === 'en') return stored;
    return navigator.language?.toLowerCase().startsWith('tr') ? 'tr' : 'en';
}

interface I18nContextValue {
    locale: Locale;
    setLocale: (l: Locale) => void;
    t: (key: TranslationKey, vars?: Record<string, string | number>) => string;
}

const I18nContext = createContext<I18nContextValue | null>(null);

export function I18nProvider({ children }: { children: ReactNode }) {
    const [locale, setLocaleState] = useState<Locale>(detectLocale);

    const setLocale = (l: Locale) => {
        localStorage.setItem(STORAGE_KEY, l);
        setLocaleState(l);
    };

    const t = (key: TranslationKey, vars?: Record<string, string | number>) => {
        let text = catalogs[locale][key] ?? en[key] ?? key;
        if (vars) {
            for (const [name, value] of Object.entries(vars)) {
                text = text.replace(new RegExp(`\\{${name}\\}`, 'g'), String(value));
            }
        }
        return text;
    };

    return <I18nContext.Provider value={{ locale, setLocale, t }}>{children}</I18nContext.Provider>;
}

export function useI18n(): I18nContextValue {
    const ctx = useContext(I18nContext);
    if (!ctx) throw new Error('useI18n must be used within I18nProvider');
    return ctx;
}
