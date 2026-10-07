import { useI18n } from '../i18n';

// Loaded ahead of need by lib/accessGuidance.ts; never imported statically, so
// its wording stays in the screen half of the catalogue. Callers draw these only
// after the screen copy has arrived.
// lib/accessGuidance.ts tarafindan onceden yuklenir; statik olarak ice aktarilmaz.

type Translate = ReturnType<typeof useI18n>['t'];
export type AccessHoldCause = 'license' | 'availability' | 'starting' | 'auth';

/**
 * What the layer over a held page says. waiting: the quiet time has passed and
 * the first read has not answered; nothing has failed yet. Otherwise a read has
 * answered without confirming access: the reason, then that nobody needs to act
 * and how the page resumes.
 */
export function accessHoldCopy(t: Translate, cause: AccessHoldCause, waiting: boolean) {
    if (waiting) return { title: t('recovery.checkingTitle'), help: t('accessHold.waitingHelp'), resume: '', prolonged: t('accessHold.prolonged') };
    return {
        title: cause === 'starting' ? t('recovery.startingTitle') : t(`accessHold.${cause}Title`),
        help: cause === 'starting' ? t('recovery.startingHelp') : t(`accessHold.${cause}Help`),
        resume: t('accessHold.resume'),
        prolonged: t('accessHold.prolonged'),
    };
}

/** Above the sign-in form after the server confirmed that the session of a page in use is over. */
export function SessionEndedNotice() {
    const { t } = useI18n();
    return <p role="status" className="mb-5 rounded-lg border border-border bg-surface-2 p-4 text-sm leading-relaxed text-fg">{t('accessHold.sessionEnded')}</p>;
}

/** One line, above everything, for the moment before the page reloads to load the current version. */
export function UpdateReloadLine() {
    const { t } = useI18n();
    return <p role="status" className="fixed inset-x-4 top-4 z-[130] mx-auto max-w-2xl rounded-lg border border-border-strong bg-surface px-4 py-3 text-sm font-medium leading-relaxed text-fg shadow-2xl">{t('accessHold.updateReload')}</p>;
}
