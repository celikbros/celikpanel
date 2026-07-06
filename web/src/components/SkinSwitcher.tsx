import { useEffect, useRef, useState } from 'react';
import { Palette, Check } from 'lucide-react';
import { useTheme, SKINS } from '../theme/ThemeProvider';
import { useI18n } from '../i18n';

// Skin picker: a palette button that opens a small list of color presets
// (Celik, Plesk, aaPanel, cPanel). Orthogonal to the light/dark switch next
// to it — every skin has both variants.
// Skin seçici: renk ön ayarlarının küçük listesini açan palet düğmesi
// (Celik, Plesk, aaPanel, cPanel). Yanındaki açık/koyu anahtarından
// bağımsızdır — her skin'in iki varyantı da vardır.
export function SkinSwitcher() {
    const { skin, setSkin } = useTheme();
    const { t } = useI18n();
    const [open, setOpen] = useState(false);
    const rootRef = useRef<HTMLDivElement>(null);

    useEffect(() => {
        if (!open) return;
        const close = (e: MouseEvent) => {
            if (!rootRef.current?.contains(e.target as Node)) setOpen(false);
        };
        document.addEventListener('mousedown', close);
        return () => document.removeEventListener('mousedown', close);
    }, [open]);

    return (
        <div ref={rootRef} className="relative">
            <button
                type="button"
                onClick={() => setOpen((o) => !o)}
                title={t('theme.skin')}
                aria-label={t('theme.skin')}
                aria-expanded={open}
                className="flex h-8 w-8 items-center justify-center rounded-lg border border-border bg-surface-2 text-fg-subtle transition-colors hover:text-fg"
            >
                <Palette className="h-4 w-4" />
            </button>

            {open && (
                <div className="absolute right-0 z-50 mt-1.5 w-44 rounded-xl border border-border bg-surface p-1.5 shadow-card">
                    <p className="px-2 pb-1 pt-0.5 text-xs font-semibold text-fg-subtle">{t('theme.skin')}</p>
                    {SKINS.map((sk) => {
                        const active = sk.id === skin;
                        return (
                            <button
                                key={sk.id}
                                type="button"
                                onClick={() => {
                                    setSkin(sk.id);
                                    setOpen(false);
                                }}
                                className={`flex w-full items-center gap-2.5 rounded-lg px-2 py-1.5 text-sm transition-colors ${
                                    active ? 'bg-surface-2 font-medium text-fg' : 'text-fg-muted hover:bg-surface-2'
                                }`}
                            >
                                <span
                                    aria-hidden
                                    className="h-3.5 w-3.5 rounded-full border border-border-strong"
                                    style={{ backgroundColor: sk.swatch }}
                                />
                                <span className="flex-1 text-left">{sk.name}</span>
                                {active && <Check className="h-4 w-4 text-primary" />}
                            </button>
                        );
                    })}
                </div>
            )}
        </div>
    );
}
