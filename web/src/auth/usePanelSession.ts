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
    const markUnavailable = useCallback((authentication = false) => {
        pending.current?.abort(); pending.current = null; generation.current++;
        setChecking(false);
        if (authentication) setUser(null);
        setState(authentication ? 'auth_unavailable' : 'availability_unavailable');
    }, []);
    useEffect(() => { void retry(); return () => { generation.current++; pending.current?.abort(); pending.current = null; }; }, [retry]);
    useEffect(() => {
        if (state === 'ready' || state === 'unauthenticated' || state === 'checking') return;
        const refresh = () => { if (document.visibilityState === 'visible') void retry(); };
        window.addEventListener('focus', refresh);
        const interval = state === 'auth_unavailable' ? undefined : window.setInterval(refresh, 10000);
        return () => { window.removeEventListener('focus', refresh); window.clearInterval(interval); };
    }, [state, retry]);
    return { user, state, checking, generation, retry, transitionAuthentication, markUnavailable };
}
