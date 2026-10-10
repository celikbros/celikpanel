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

/** The access read has not answered for this long: still unknown, and "Check now" is offered. */
export const ACCESS_WAIT_LONG_MS = 15000;
/** Half a minute without an answer: the reload is offered beside it (the committed rule, as in AccessHold). */
export const ACCESS_WAIT_PROLONGED_MS = 30000;

// Ninth native record, cell 2 (2026-10-10): with the session read held 35 s,
// the page's own 15 s read limit ended the explained wait and drew "Your session
// could not be checked" with the reload at 15.18 s; the automatic re-read at
// 25.2 s drew the older "Confirming your session…" sentence, and the half-minute
// sentence never appeared. A read that has not answered is still unknown, also
// after its own limit; only an answer ends the wait. The page's first wait is
// counted from the page load itself (navigation start, the clock's origin), a
// later one from its first read (after a sign-in): re-reads and the next gate of
// the same load continue it, they never restart it.
//
// Dokuzuncu yerel kayit, hucre 2: yanit vermeyen okuma kendi 15 sn sinirindan
// sonra da bilinmeyendir; bekleme yalnizca bir yanitla biter. Sayfanin ilk
// beklemesi sayfa yuklemesinden sayilir; yeniden okumalar onu sifirlamaz.
let waitBegan: number | null = null;
let pageWaited = false;
export const accessWaitClock = { now: () => performance.now() };
export function beginAccessWait(): void {
    if (waitBegan !== null) return;
    waitBegan = pageWaited ? accessWaitClock.now() : 0;
    pageWaited = true;
}
export function endAccessWait(): void { waitBegan = null; }
const accessWaitElapsed = () => waitBegan === null ? 0 : accessWaitClock.now() - waitBegan;

export type AccessWaitStage = 'waiting' | 'long' | 'prolonged';
/** The stage of the page's access wait while `active`; it re-renders when the next stage is due. */
export function useAccessWaitStage(active: boolean): AccessWaitStage {
    const [tick, setTick] = useState(0);
    useEffect(() => {
        if (!active) return;
        beginAccessWait();
        const elapsed = accessWaitElapsed();
        const next = elapsed < ACCESS_WAIT_LONG_MS ? ACCESS_WAIT_LONG_MS : elapsed < ACCESS_WAIT_PROLONGED_MS ? ACCESS_WAIT_PROLONGED_MS : null;
        if (next === null) return;
        const timer = window.setTimeout(() => setTick(value => value + 1), next - elapsed);
        return () => window.clearTimeout(timer);
    }, [active, tick]);
    if (!active) return 'waiting';
    const elapsed = accessWaitElapsed();
    return elapsed >= ACCESS_WAIT_PROLONGED_MS ? 'prolonged' : elapsed >= ACCESS_WAIT_LONG_MS ? 'long' : 'waiting';
}
