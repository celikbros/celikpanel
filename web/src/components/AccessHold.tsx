import { useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import type { CurrentUser } from '../lib/api';
import { useI18n } from '../i18n';
import { handoverAddress } from '../lib/panelHandover';
import { useAccessGuidance } from '../lib/accessGuidance';
import { savedUpdateUnfinished, UPDATE_MARKER_KEY } from '../lib/recoveryObservation';
import { RecoveryStatus, usePanelHandover } from './RecoveryAccess';
import { AddressLink } from './AddressLink';
import { Button, Dialog } from './ui';

/** A re-check that answers within this time is not shown at all. */
export const ACCESS_HOLD_QUIET_MS = 1500;
/** Still unknown after this long: the reload action is offered beside "check again". */
export const ACCESS_HOLD_PROLONGED_MS = 30000;

type HoldPhase = 'quiet' | 'waiting' | 'unknown';
const guardedEvents = ['click', 'dblclick', 'mousedown', 'pointerdown', 'keydown', 'submit', 'input', 'paste', 'drop'] as const;

// Owner report, 2026-10-08: leaving a page for a while replaced it with the
// full-screen "License status could not be checked" page, and returning rebuilt
// the application from nothing: the open dialogue, the typed input and the
// selected tab were gone, although the license had been fine the whole time.
//
// The rule this component carries: only a KNOWN negative decision may replace the
// screen. While access is unknown or merely not yet refreshed, the pages stay
// mounted exactly as they were and are made unreachable instead: the subtree is
// inert, events that still target it are stopped, and focus cannot enter it. No
// management control can be used while the server has not confirmed access, and
// the server refuses such requests on its own in any case. Nothing about the
// decision, its validity or what a refusal means is changed here.
//
// A read that answers promptly shows nothing. If it is slow, or fails, a modal
// layer over the page says what is known, that nobody needs to act yet, and that
// the page continues where it was. A hidden tab shows nothing and reads nothing;
// returning to it starts one quiet read.
//
// Sahip bildirimi, 8 Ekim 2026: sayfadan bir sure ayrilinca tum ekran "Lisans
// durumu kontrol edilemedi" sayfasina donuyor, geri gelince uygulama bastan
// kuruluyordu; acik pencere, yazilanlar ve secili sekme kayboluyordu.
// Kural: ekrani yalnizca BILINEN olumsuz karar degistirebilir. Erisim bilinmiyor
// ya da henuz yenilenmemisken sayfalar oldugu gibi bagli kalir ve erisilmez
// yapilir: alt agac etkisizdir, ona yonelen olaylar durdurulur, odak iceri
// giremez. Sunucu bu istekleri zaten kendisi reddeder. Kararin kendisi, gecerlilik
// suresi ve reddin anlami burada degismez.
export function AccessHold({ active, silent = false, cause, checking, user, onRetry, onUnauthorized, children }: {
    /** Access is not confirmed right now: the children are unreachable until it is. */
    active: boolean;
    /** An outer hold already explains the state; this one only blocks. */
    silent?: boolean;
    cause: 'license' | 'availability' | 'starting' | 'auth';
    /** A read is in flight. A hold whose first read has not answered yet is not a failure. */
    checking: boolean;
    /** Only a verified administrator may see a saved operation; null while the session itself is unknown. */
    user?: CurrentUser | null;
    onRetry: () => void;
    onUnauthorized?: () => void;
    children: ReactNode;
}) {
    const { t, screensReady } = useI18n();
    const guidance = useAccessGuidance();
    const wrapper = useRef<HTMLDivElement>(null);
    const layer = useRef<HTMLDivElement>(null);
    const heading = useRef<HTMLSpanElement>(null);
    const focusBefore = useRef<HTMLElement | null>(null);
    const latest = useRef({ active, onRetry });
    latest.current = { active, onRetry };
    const [visible, setVisible] = useState(() => document.visibilityState === 'visible');
    const [phase, setPhase] = useState<HoldPhase>('quiet');
    const [prolonged, setProlonged] = useState(false);
    // The automatic read repeats every few seconds; only a read the owner asked
    // for, or the first one still in flight, is drawn as busy.
    const [asked, setAsked] = useState(false);
    const shown = active && visible && !silent && phase !== 'quiet';

    // Returning to the tab is one quiet read, started before anything is drawn.
    useEffect(() => {
        const changed = () => {
            const now = document.visibilityState === 'visible';
            if (now && latest.current.active) latest.current.onRetry();
            setVisible(now);
        };
        document.addEventListener('visibilitychange', changed);
        return () => document.removeEventListener('visibilitychange', changed);
    }, []);

    // Each visible period of a hold starts quiet. It is explained once a read has
    // answered without confirming access, or once the quiet time has passed.
    useEffect(() => {
        if (!active || !visible) { setPhase('quiet'); setProlonged(false); return; }
        const timer = window.setTimeout(() => setPhase(current => current === 'quiet' ? 'waiting' : current), ACCESS_HOLD_QUIET_MS);
        return () => window.clearTimeout(timer);
    }, [active, visible]);
    useEffect(() => { if (active && visible && !checking) setPhase('unknown'); }, [active, visible, checking]);
    useEffect(() => {
        if (phase === 'quiet') return;
        const timer = window.setTimeout(() => setProlonged(true), ACCESS_HOLD_PROLONGED_MS);
        return () => window.clearTimeout(timer);
    }, [phase]);

    // The inert attribute is what makes the pages unreachable. The listeners below
    // cover a browser that does not implement it: nothing aimed at the held pages
    // is delivered, and focus that lands inside them is taken out again.
    useLayoutEffect(() => {
        if (!active) return;
        const node = wrapper.current;
        if (!node) return;
        const focused = document.activeElement;
        focusBefore.current = focused instanceof HTMLElement && node.contains(focused) ? focused : null;
        focusBefore.current?.blur();
        const stop = (event: Event) => { event.stopPropagation(); event.preventDefault(); };
        // Focus belongs to the layer while it is drawn: an overlay outside the held
        // pages (a running component or update operation) cannot take it either.
        const keepOut = (event: FocusEvent) => {
            if (!(event.target instanceof HTMLElement)) return;
            if (heading.current ? layer.current?.contains(event.target) : !node.contains(event.target)) return;
            if (heading.current) heading.current.focus(); else event.target.blur();
        };
        for (const type of guardedEvents) node.addEventListener(type, stop, { capture: true });
        document.addEventListener('focusin', keepOut);
        return () => {
            for (const type of guardedEvents) node.removeEventListener(type, stop, { capture: true });
            document.removeEventListener('focusin', keepOut);
            const target = focusBefore.current;
            focusBefore.current = null;
            const current = document.activeElement;
            // Give the caret back to the field it was in, unless the owner has moved on.
            if (target?.isConnected && (!current || current === document.body || layer.current?.contains(current))) target.focus({ preventScroll: true });
        };
    }, [active]);

    useEffect(() => { if (shown) heading.current?.focus(); }, [shown]);
    useEffect(() => { if (!checking) setAsked(false); }, [checking]);

    const waiting = phase === 'waiting';
    const busy = checking && (asked || waiting);
    const handover = usePanelHandover(user?.username, shown && !waiting && (cause === 'availability' || cause === 'starting'), checking);
    const address = handover?.elsewhere ? handoverAddress(handover.host, window.location.port) : '';
    // The wording arrives ahead of need. Should it be missing, the layer still
    // blocks and says the neutral thing the shell can say: access is being checked.
    // The Panel did not answer while this browser's update has not recorded its end: the
    // update's restart is named, not the license. The record only chooses the words.
    const updating = shown && cause === 'availability' && (() => { try { return savedUpdateUnfinished(localStorage.getItem(UPDATE_MARKER_KEY)); } catch { return false; } })();
    const copy = guidance && screensReady ? guidance.accessHoldCopy(t, updating ? 'update' : cause, waiting) : null;
    const title = handover ? t('recovery.handoverTitle') : copy?.title ?? t('recovery.checkingTitle');
    const help = handover ? t('recovery.handoverHelp', { host: handover.host }) : copy?.help ?? t('recovery.checkingHelp');
    const resume = !handover && copy?.resume;
    const later = prolonged && copy?.prolonged;
    const keepFocusInside = (event: ReactKeyboardEvent<HTMLDivElement>) => {
        if (event.key !== 'Tab') return;
        const stops = Array.from(layer.current?.querySelectorAll<HTMLElement>('button:not([disabled]), a[href], summary') ?? []);
        const at = stops.indexOf(document.activeElement as HTMLElement);
        if (stops.length === 0) { event.preventDefault(); return; }
        if (event.shiftKey ? at <= 0 : at === stops.length - 1) {
            event.preventDefault();
            stops[event.shiftKey ? stops.length - 1 : 0].focus();
        }
    };
    const held = active ? { inert: '', 'aria-hidden': true, 'data-access-hold': 'blocked' } : undefined;

    return <>
        <div ref={wrapper} className="contents" {...held}>{children}</div>
        {/* Above every other overlay (component operation 100, update 110) and below the
            reload dialogue (130). index.css keeps it the only scrim on the page. */}
        {shown && createPortal(
            <div ref={layer} className="relative z-[120]" data-top-layer="hold" onKeyDown={keepFocusInside}>
                <Dialog
                    id="access-hold"
                    width="lg"
                    dismissible={false}
                    busy={busy}
                    title={<span ref={heading} tabIndex={-1} className="outline-none focus-visible:outline-none">{title}</span>}
                    description={<span aria-live="polite">{help}</span>}
                    actions={<>
                        {prolonged && <Button onClick={() => window.location.reload()}>{t('app.reload')}</Button>}
                        <Button variant="primary" loading={busy} onClick={() => { setAsked(true); onRetry(); }}>{t(busy ? 'recovery.checking' : 'recovery.retry')}</Button>
                    </>}
                >
                    {/* Nothing more to say yet: the dialogue then has no body and no second hairline. */}
                    {(resume || address || later) && <div className="space-y-3 text-sm leading-relaxed text-fg" role="status" aria-live="polite">
                        {resume && <p>{resume}</p>}
                        {address && <p className="text-fg-muted">{t('recovery.handoverAddress')} <AddressLink href={`${address}/setup`} address={address} /></p>}
                        {later && <p className="text-fg-muted">{later}</p>}
                    </div>}
                    {/* Shown only for an operation that is still running or needs the owner; reads only. */}
                    {!waiting && user?.effective_role === 'admin' && <RecoveryStatus key={user.username} username={user.username}
                        onUnauthorized={onUnauthorized} unfinishedOnly heading="h4" />}
                </Dialog>
            </div>, document.body)}
    </>;
}
