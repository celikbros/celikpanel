import { useEffect, useState } from 'react';

/**
 * A first read that answers within this time is not shown at all. The same
 * quiet time as ACCESS_HOLD_QUIET_MS in components/AccessHold.tsx; a test keeps
 * the two equal.
 */
export const QUIET_READ_MS = 1500;

// Seventh native record, cell 5 (2026-10-10): on a cold full page load the
// full-page "Checking panel access" screen was painted before any session read
// had answered, for 80-100 ms on a fast link and about 1.7 s on a slow one, on
// every load. The rule it broke (D-024, the known-state rule): a read that has
// not answered yet is not shown as a page. Nothing is drawn but the empty page
// background until the quiet time has passed or the read has answered.
//
// useQuietRead is true while `waiting` holds and the quiet time since this
// component first waited has not passed. Once it has passed it stays passed for
// the component: a wait that was already explained is not hidden again. Nor is
// it hidden by the next gate of the same load: a component that starts waiting
// while another one's wait is on screen (the interface arriving under the
// recovery fallback, the license read after the session read) continues the
// explained wait instead of going blank again. A browser run of 2026-10-10
// (mock, session read held 2.5 s) saw "Checking panel access" give way to an
// empty page for 0.3 s at exactly that handover before this was added.
//
// Yedinci yerel kayit, hucre 5: sayfa ilk yuklenirken oturum okumasi yanit
// vermeden tam sayfa "Panel erisimi kontrol ediliyor" ciziliyordu. Yanit
// vermemis okuma sayfa olarak gosterilmez; sessiz sure gecene ya da okuma yanit
// verene kadar yalnizca bos sayfa zemini cizilir. Aciklanmis bekleme, sonraki
// kapida yeniden bosluga donmez.
let explained = 0;
export function useQuietRead(waiting: boolean): boolean {
    const [passed, setPassed] = useState(() => explained > 0);
    useEffect(() => {
        if (!waiting || passed) return;
        const timer = window.setTimeout(() => setPassed(true), QUIET_READ_MS);
        return () => window.clearTimeout(timer);
    }, [waiting, passed]);
    useEffect(() => {
        if (!waiting || !passed) return;
        explained += 1;
        return () => { explained -= 1; };
    }, [waiting, passed]);
    return waiting && !passed;
}
