import { useCallback, useEffect, useRef, useState } from 'react';
import { api, type CurrentUser } from '../lib/api';

export type PanelSessionState = 'checking' | 'unauthenticated' | 'auth_unavailable' | 'availability_unavailable' | 'starting' | 'ready';

/** Only an established 401 means sign-in is required. All reads share an identity generation. */
export function usePanelSession() {
    const [user, setUser] = useState<CurrentUser | null>(null);
    const [state, setState] = useState<PanelSessionState>('checking');
    const [checking, setChecking] = useState(true);
    const generation = useRef(0);
    const pending = useRef<AbortController | null>(null);
    const current = useRef<PanelSessionState>(state);
    current.current = state;
    const availability = useCallback(async (identity: CurrentUser, request: AbortController, sequence: number) => {
        try {
            const response = await fetch('/api/v1/panel/availability', { signal: request.signal, cache: 'no-store' });
            if (response.status === 401) {
                if (generation.current === sequence && !request.signal.aborted) { setUser(null); setState('unauthenticated'); }
                return;
            }
            if (!response.ok) throw new Error('availability unavailable');
            const result = await response.json();
            if (result.schema !== 'celikpanel-panel-availability/v1' || !['starting', 'ready'].includes(result.state)) throw new Error('invalid availability');
            if (generation.current === sequence && !request.signal.aborted) { setUser(identity); setState(result.state); }
        } catch {
            if (generation.current === sequence && !request.signal.aborted) setState('availability_unavailable');
        }
    }, []);
    const retry = useCallback(async () => {
        if (pending.current) return;
        const request = new AbortController(); pending.current = request;
        const sequence = ++generation.current;
        setChecking(true);
        const timeout = window.setTimeout(() => request.abort(), 15000);
        let authenticated = false;
        try {
            const identity = await api.me(request.signal);
            if (generation.current !== sequence || request.signal.aborted) return;
            if (!identity) { setUser(null); setState('unauthenticated'); return; }
            authenticated = true; setUser(identity);
            await availability(identity, request, sequence);
        } catch {
            if (generation.current === sequence) { setUser(null); setState('auth_unavailable'); }
        } finally {
            window.clearTimeout(timeout);
            if (pending.current === request) {
                pending.current = null; setChecking(false);
                if (request.signal.aborted && generation.current === sequence) {
                    if (!authenticated) setUser(null);
                    setState(authenticated ? 'availability_unavailable' : 'auth_unavailable');
                }
            }
        }
    }, [availability]);
    const transitionAuthentication = useCallback((identity: CurrentUser | null) => {
        pending.current?.abort(); pending.current = null;
        const sequence = ++generation.current;
        setUser(identity);
        if (!identity) { setState('unauthenticated'); setChecking(false); return; }
        setState('checking'); setChecking(true);
        const request = new AbortController(); pending.current = request;
        const timeout = window.setTimeout(() => request.abort(), 15000);
        void availability(identity, request, sequence).finally(() => {
            window.clearTimeout(timeout);
            if (pending.current === request) {
                pending.current = null; setChecking(false);
                if (request.signal.aborted && generation.current === sequence) setState('availability_unavailable');
            }
        });
    }, [availability]);
    // A refused background request reports one condition, and mounted pages
    // repeat it with every poll. Only the first report of a ready session changes
    // the state, and it starts the read that decides what is true now. Later
    // reports cannot restart that read. A read, never a retry of the refused request.
    // Reddedilen arka plan istegi tek bir durumu bildirir ve acik sayfalar bunu
    // her sorguda yineler. Yalnizca ilk bildirim durumu degistirir ve guncel
    // durumu belirleyen okumayi baslatir.
    const markUnavailable = useCallback((authentication = false) => {
        if (current.current !== 'ready') return;
        current.current = authentication ? 'auth_unavailable' : 'availability_unavailable';
        pending.current?.abort(); pending.current = null; generation.current++;
        // State first, and the read is marked in flight at once: no render in
        // between may look like a ready session without an identity, or like a
        // read that has already answered.
        setState(current.current);
        if (authentication) setUser(null);
        void retry();
    }, [retry]);
    useEffect(() => { void retry(); return () => { generation.current++; pending.current?.abort(); pending.current = null; }; }, [retry]);
    useEffect(() => {
        if (state === 'ready' || state === 'unauthenticated' || state === 'checking') return;
        const refresh = () => { if (document.visibilityState === 'visible') void retry(); };
        window.addEventListener('focus', refresh);
        // Every unknown state is read again by itself, so "checks again by itself" is true for each of them.
        const interval = window.setInterval(refresh, 10000);
        return () => { window.removeEventListener('focus', refresh); window.clearInterval(interval); };
    }, [state, retry]);
    return { user, state, checking, generation, retry, transitionAuthentication, markUnavailable };
}
