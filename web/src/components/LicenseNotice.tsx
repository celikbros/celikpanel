import { useEffect, useState } from 'react';
import { BadgeCheck } from 'lucide-react';
import { Link } from '../router';
import { useI18n } from '../i18n';

/** Mounted only in the administrator dashboard. No tenant license request. */
export function LicenseNotice() {
    const { t } = useI18n();
    const [needsLicense, setNeedsLicense] = useState(false);
    useEffect(() => {
        const controller = new AbortController();
        fetch('/api/v1/panel/license', { signal: controller.signal })
            .then(async response => { if (!response.ok) return; const state = await response.json(); if (state?.can_provision === false) setNeedsLicense(true); })
            .catch(() => {});
        return () => controller.abort();
    }, []);
    if (!needsLicense) return null;
    return <section className="mb-5 flex flex-wrap items-center justify-between gap-4 rounded-lg border border-border bg-surface p-4" aria-label={t('license.title')}>
        <div className="flex min-w-0 items-start gap-3"><BadgeCheck className="mt-1 h-5 w-5 shrink-0 text-fg-muted" /><div>
            <h2 className="font-semibold text-fg">{t('license.title')}</h2>
            <p className="text-sm text-fg-muted">{t('license.restricted')}</p>
        </div></div>
        <Link to="/settings?section=license" className="shrink-0 text-sm font-medium text-primary underline underline-offset-4">{t('license.activate')}</Link>
    </section>;
}
