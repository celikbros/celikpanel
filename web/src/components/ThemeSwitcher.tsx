import { Sun, Moon, Monitor } from 'lucide-react';
import { useTheme, type ThemePreference } from '../theme/ThemeProvider';
import { useI18n } from '../i18n';

// A compact three-way segmented control: light / system / dark. Always
// visible, no dropdown, one tap to switch.
// Kompakt üç yönlü segment kontrolü: açık / sistem / koyu. Her zaman görünür,
// açılır menü yok, değiştirmek için tek dokunuş.
const options: { value: ThemePreference; icon: typeof Sun; labelKey: 'theme.light' | 'theme.system' | 'theme.dark' }[] = [
    { value: 'light', icon: Sun, labelKey: 'theme.light' },
    { value: 'system', icon: Monitor, labelKey: 'theme.system' },
    { value: 'dark', icon: Moon, labelKey: 'theme.dark' },
];

export function ThemeSwitcher() {
    const { preference, setPreference } = useTheme();
    const { t } = useI18n();

    return (
        <div
            className="inline-flex items-center gap-0.5 rounded-lg border border-border bg-surface-2 p-0.5"
            role="group"
            aria-label={t('theme.label')}
        >
            {options.map(({ value, icon: Icon, labelKey }) => {
                const active = preference === value;
                return (
                    <button
                        key={value}
                        type="button"
                        onClick={() => setPreference(value)}
                        title={t(labelKey)}
                        aria-label={t(labelKey)}
                        aria-pressed={active}
                        className={`flex h-7 w-7 items-center justify-center rounded-md transition-colors ${
                            active ? 'bg-surface text-fg shadow-sm' : 'text-fg-subtle hover:text-fg-muted'
                        }`}
                    >
                        <Icon className="h-4 w-4" />
                    </button>
                );
            })}
        </div>
    );
}
