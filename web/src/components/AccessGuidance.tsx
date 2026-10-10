import { useEffect } from 'react';
import { useI18n } from '../i18n';
import { Button, Dialog } from './ui';

// Loaded ahead of need by lib/accessGuidance.ts; never imported statically, so
// its wording stays in the screen half of the catalogue. Callers draw these only
// after the screen copy has arrived.
// lib/accessGuidance.ts tarafindan onceden yuklenir; statik olarak ice aktarilmaz.

type Translate = ReturnType<typeof useI18n>['t'];
/**
 * update: the Panel did not answer while an update started from this browser
 * has not recorded its end; the restart an update makes is the likely reason,
 * and the license is not named (seventh native record, cell 1, 2026-10-10).
 */
export type AccessHoldCause = 'license' | 'availability' | 'starting' | 'auth' | 'update';

/**
 * What the layer over a held page says. waiting: the quiet time has passed and
 * the first read has not answered; nothing has failed yet. Otherwise a read has
 * answered without confirming access: the reason that read showed, then that
 * nobody needs to act and how the page resumes.
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

// Shown for the moment before the page reloads to load the current version: the
// shared dialogue, above everything, with no dismissal. It covers the page
// instead of sitting on a part of it: on a phone a line at the top hid the
// header of the dialogue underneath, and the page below can no longer be used
// in any case, so nothing more is typed into a form that is about to be
// replaced. It says what is lost and offers the reload at once. index.css keeps
// it the only scrim on the page.
// Sayfa guncel surumu yuklemek icin yenilenmeden hemen once gosterilir: ortak
// pencere, her seyin ustunde, kapatilamaz. Alttaki sayfa zaten kullanilamaz;
// neyin kayboldugunu soyler ve hemen yeniden yuklemeyi sunar.
export function UpdateReloadLine() {
    const { t } = useI18n();
    useEffect(() => { (document.activeElement as HTMLElement | null)?.blur?.(); }, []);
    return <div data-top-layer="reload" className="relative z-[130]">
        <Dialog id="update-reload" dismissible={false} title={t('accessHold.updateReloadTitle')}
            description={<span role="status">{t('accessHold.updateReload')}</span>}
            actions={<Button variant="primary" onClick={() => window.location.reload()}>{t('app.reload')}</Button>} />
    </div>;
}
