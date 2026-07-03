import { useI18n, type Locale } from '../i18n';

// TR / EN segmented toggle. The two primary locales are always one tap
// apart; more locales would turn this into a dropdown.
// TR / EN segment anahtarı. İki öncelikli yerel ayar her zaman tek dokunuş
// uzakta; daha fazla yerel ayar bunu bir açılır menüye dönüştürür.
const locales: Locale[] = ['tr', 'en'];

export function LanguageSwitcher() {
    const { locale, setLocale, t } = useI18n();

    return (
        <div
            className="inline-flex items-center gap-0.5 rounded-lg border border-border bg-surface-2 p-0.5"
            role="group"
            aria-label={t('lang.label')}
        >
            {locales.map((l) => {
                const active = locale === l;
                return (
                    <button
                        key={l}
                        type="button"
                        onClick={() => setLocale(l)}
                        aria-pressed={active}
                        className={`h-7 rounded-md px-2 text-xs font-semibold uppercase transition-colors ${
                            active ? 'bg-surface text-fg shadow-sm' : 'text-fg-subtle hover:text-fg-muted'
                        }`}
                    >
                        {l}
                    </button>
                );
            })}
        </div>
    );
}
