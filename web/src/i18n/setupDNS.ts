import { enSetupDNS, type SetupDNSKey } from './setupDNS/en';
import { trSetupDNS } from './setupDNS/tr';

// Route-scoped copy: changing language still reads the same I18nProvider locale.
export function setupDNSTranslation(locale: string | undefined, key: string, values?: Record<string, string | number>): string | null {
    if ((locale !== 'en' && locale !== 'tr') || !(key in enSetupDNS)) return null;
    let value: string = (locale === 'tr' ? trSetupDNS : enSetupDNS)[key as SetupDNSKey];
    for (const [name, replacement] of Object.entries(values || {})) value = value.split(`{${name}}`).join(String(replacement));
    return value;
}
