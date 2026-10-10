// What the update notice and the update card say about an update this browser
// is following, from the last read of its exact record (owner report
// 2026-10-10: the card showed the new version installed while the notice still
// said the update was being applied).
//
// The Agent's record stays "running" after the new Panel has started, until the
// updater has exited and its final proof has passed. The Panel's status answer
// says "verifying" in that window: the Panel answering is already the target
// build (cmd/panel panelUpdateStatusPhase). A read that failed is unknown; it
// is never said as "being applied".
//
// Bu tarayicinin izledigi guncelleme icin bildirim ve kartin soyledigi: son
// okumadan gelir. Yeni Panel basladiktan sonra kayit, guncelleyici bitip son
// kanit gecene kadar "running" kalir; Panel bu aralikta "verifying" der.
// Basarisiz okuma bilinmiyor demektir, "uygulaniyor" denmez.

export type SystemUpdateTrackingPhase = 'reading' | 'queued' | 'applying' | 'verifying' | 'unknown';

export type SystemUpdateTracking = {
    version: string;
    phase: SystemUpdateTrackingPhase;
};

// The phase of a record that was read. A record that is not queued or running
// is terminal and handled by its own outcome, not by this phase.
export function systemUpdateOperationPhase(operation: { status?: string; phase?: string } | null | undefined): SystemUpdateTrackingPhase {
    if (!operation) return 'unknown';
    if (operation.status === 'queued') return 'queued';
    if (operation.status === 'running') return operation.phase === 'verifying' ? 'verifying' : 'applying';
    return 'unknown';
}

// The status answer's optional phase: only the one known value is kept.
export function decodeSystemUpdatePhase(value: unknown): 'verifying' | undefined {
    return value === 'verifying' ? 'verifying' : undefined;
}

// The notice's lines for a followed update. title null keeps the notice's own
// heading; message replaces the last read's sentence only where the phase says
// more than it; hint is why a read failed, said under the unknown line.
// Izlenen guncelleme icin bildirim satirlari.
type Translate = (key: 'panelUpdate.tracking.verifyingTitle' | 'panelUpdate.tracking.verifying' | 'panelUpdate.tracking.unknown', values?: Record<string, string>) => string;
export function systemUpdateTrackingText(tracking: SystemUpdateTracking, lastMessage: string, t: Translate): { title: string | null; message: string; hint: string | null } {
    if (tracking.phase === 'verifying') {
        return { title: t('panelUpdate.tracking.verifyingTitle'), message: t('panelUpdate.tracking.verifying', { version: tracking.version }), hint: null };
    }
    if (tracking.phase === 'unknown') return { title: null, message: t('panelUpdate.tracking.unknown'), hint: lastMessage || null };
    return { title: null, message: lastMessage, hint: null };
}

// A clock time on the update screens is the browser's local time, named with
// its zone, in the interface language (as the update card's other times).
// Guncelleme ekranlarindaki saat tarayicinin yerel saatidir ve dilimiyle yazilir.
export function systemUpdateClockTime(at: number, locale: string): string {
    const tag = locale === 'tr' ? 'tr-TR' : 'en-US';
    try {
        return new Date(at).toLocaleTimeString(tag, { timeZoneName: 'short' });
    } catch {
        return new Date(at).toLocaleTimeString(tag);
    }
}
