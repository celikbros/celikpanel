import { useEffect, useState } from 'react';

type AccessGuidance = typeof import('../components/AccessGuidance');

let loaded: AccessGuidance | null = null;
let loading: Promise<AccessGuidance | null> | null = null;

// The wording for an access hold, an ended session and a reload after an update
// is needed only once pages are mounted, so it travels in the screen half of the
// catalogue and in a part of its own, not in the boot payload (register R-060).
// It is fetched as soon as the application starts, while the server is known to
// answer, and is never fetched at the moment it is needed: that moment is the
// one in which the server may not answer. Until it has arrived the callers fall
// back to the shell's own neutral wording, or to saying nothing.
//
// Erisim bekletmesi, sona eren oturum ve guncelleme sonrasi yeniden yukleme
// metinleri yalnizca sayfalar baglandiktan sonra gerekir; bu yuzden acilis
// yukunde degil, ekran katalogunda ve kendi parcasinda tasinir. Uygulama
// baslarken getirilir, gerektigi anda asla getirilmez: o an sunucunun yanit
// vermeyebilecegi andir.
export function loadAccessGuidance(): Promise<AccessGuidance | null> {
    loading ??= import('../components/AccessGuidance').then(
        module => { loaded = module; return module; },
        () => { loading = null; return null; },
    );
    return loading;
}

/** The guidance part once it has arrived; null before that. Mounting a caller starts the fetch. */
export function useAccessGuidance(): AccessGuidance | null {
    const [guidance, setGuidance] = useState(loaded);
    useEffect(() => {
        if (guidance) return;
        let current = true;
        void loadAccessGuidance().then(module => { if (current && module) setGuidance(module); });
        return () => { current = false; };
    }, [guidance]);
    return guidance;
}
