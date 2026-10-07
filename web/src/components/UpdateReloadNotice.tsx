import { useEffect, useRef, useState } from 'react';
import { useI18n } from '../i18n';
import { useAccessGuidance } from '../lib/accessGuidance';

export const UPDATE_RELOAD_EVENT = 'celikpanel:update-reload';
/** Long enough to read one sentence before the page is replaced. */
export const UPDATE_RELOAD_DELAY_MS = 4000;

// A tab kept open across an update can ask for a part of the interface that the
// server no longer has. The entry point then reloads the page once. That used to
// happen without a word, also on a page with unsaved input. This component
// claims the reload when it can say why: it shows one line, then reloads. Before
// the interface and its wording have arrived there is nothing typed to lose and
// nothing to say it with, so the entry point reloads at once, as before.
//
// Guncelleme sirasinda acik kalan sekme, sunucuda artik bulunmayan bir arayuz
// parcasini isteyebilir. Giris noktasi o zaman sayfayi bir kez yeniden yukler.
// Bu bilesen, nedenini soyleyebildiginde yeniden yuklemeyi ustlenir: tek satir
// gosterir, sonra yukler. Arayuz ve metni gelmeden once giris noktasi eskisi
// gibi hemen yeniden yukler.
export function UpdateReloadNotice() {
    const { screensReady } = useI18n();
    const guidance = useAccessGuidance();
    const [reloading, setReloading] = useState(false);
    const able = useRef(false);
    able.current = !!guidance && screensReady;
    useEffect(() => {
        let timer: number | undefined;
        const announce = (event: Event) => {
            if (!able.current) return;
            event.preventDefault();
            if (timer !== undefined) return;
            setReloading(true);
            timer = window.setTimeout(() => window.location.reload(), UPDATE_RELOAD_DELAY_MS);
        };
        window.addEventListener(UPDATE_RELOAD_EVENT, announce);
        return () => { window.removeEventListener(UPDATE_RELOAD_EVENT, announce); window.clearTimeout(timer); };
    }, []);
    return reloading && guidance ? <guidance.UpdateReloadLine /> : null;
}
