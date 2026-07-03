import { createContext, useContext, useEffect, useState, type ReactNode } from 'react';

// The user picks light, dark, or "system" (follow the OS). The choice is
// persisted; "system" also reacts live to OS changes. Default is "system",
// which resolves to light unless the OS prefers dark.
//
// Kullanıcı açık, koyu ya da "sistem"i (işletim sistemini izle) seçer. Seçim
// kalıcıdır; "sistem" ayrıca işletim sistemi değişikliklerine canlı tepki
// verir. Varsayılan "sistem"dir ve işletim sistemi koyu tercih etmedikçe
// açığa çözülür.
export type ThemePreference = 'light' | 'dark' | 'system';
type ResolvedTheme = 'light' | 'dark';

const STORAGE_KEY = 'celikpanel.theme';

interface ThemeContextValue {
    preference: ThemePreference;
    resolved: ResolvedTheme;
    setPreference: (p: ThemePreference) => void;
}

const ThemeContext = createContext<ThemeContextValue | null>(null);

function systemPrefersDark(): boolean {
    return window.matchMedia?.('(prefers-color-scheme: dark)').matches ?? false;
}

function readStoredPreference(): ThemePreference {
    const stored = localStorage.getItem(STORAGE_KEY);
    return stored === 'light' || stored === 'dark' || stored === 'system' ? stored : 'system';
}

export function ThemeProvider({ children }: { children: ReactNode }) {
    const [preference, setPreferenceState] = useState<ThemePreference>(readStoredPreference);
    const [resolved, setResolved] = useState<ResolvedTheme>(() =>
        readStoredPreference() === 'dark' || (readStoredPreference() === 'system' && systemPrefersDark())
            ? 'dark'
            : 'light',
    );

    // Apply the resolved theme to <html> and keep it in sync with the
    // preference and, for "system", with live OS changes.
    // Çözülmüş temayı <html>'e uygula ve tercihle, "sistem" içinse canlı
    // işletim sistemi değişiklikleriyle senkron tut.
    useEffect(() => {
        const apply = () => {
            const dark = preference === 'dark' || (preference === 'system' && systemPrefersDark());
            setResolved(dark ? 'dark' : 'light');
            document.documentElement.classList.toggle('dark', dark);
        };
        apply();

        if (preference !== 'system') return;
        const media = window.matchMedia('(prefers-color-scheme: dark)');
        media.addEventListener('change', apply);
        return () => media.removeEventListener('change', apply);
    }, [preference]);

    const setPreference = (p: ThemePreference) => {
        localStorage.setItem(STORAGE_KEY, p);
        setPreferenceState(p);
    };

    return (
        <ThemeContext.Provider value={{ preference, resolved, setPreference }}>
            {children}
        </ThemeContext.Provider>
    );
}

export function useTheme(): ThemeContextValue {
    const ctx = useContext(ThemeContext);
    if (!ctx) throw new Error('useTheme must be used within ThemeProvider');
    return ctx;
}
