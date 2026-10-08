import { useCallback, useEffect, useState } from 'react';
import { readApiError, type ApiError } from './apiError';

// What a screen knows about something it reads from the server. The rule every
// screen follows from 9 Oct 2026 (D-024): NO NEGATIVE UI UNLESS KNOWN.
//
//   - loading: not known yet. The screen says it is checking. It shows no
//              "missing", "not ready", "none", "off" and no empty list, and it
//              offers no save or delete that depends on the answer.
//   - known:   the server answered and the answer was understood. Only now may
//              a screen say something is missing or empty, or build a save.
//   - unknown: the read failed, was refused, or could not be understood. The
//              screen says it could not check and offers Retry. If an earlier
//              answer exists it stays visible, marked as the earlier answer.
//
// A failed read is never turned into a value. There is no default here: not an
// empty list, not `null`, not `false`.
//
// `status` is the HTTP status of a refusal the server really sent. It is absent
// when no answer arrived or the answer was not the contract. A screen may read
// one meaning from it and only where the server defines one: "no such record"
// (404) for a record addressed by its exact identity.
//
// Bir ekranın sunucudan okuduğu şey hakkında bildiği. 9 Eki 2026'dan beri her
// ekranın uyduğu kural (D-024): BİLİNMEDEN OLUMSUZ ARAYÜZ YOK. Yükleniyor:
// henüz bilinmiyor, ekran kontrol ettiğini söyler. Biliniyor: ancak şimdi
// "eksik" ya da "boş" denebilir ve kayıt kurulabilir. Bilinmiyor: okuma
// başarısız; ekran kontrol edemediğini söyler ve Tekrar dene sunar. Başarısız
// okuma hiçbir zaman bir değere çevrilmez; burada varsayılan yoktur.
export type Remote<T> =
    | { state: 'loading' }
    | { state: 'known'; value: T; observedAt: number }
    | { state: 'unknown'; reason: ApiError; status?: number; previous?: Observed<T> };

/** A value the server really sent, and when it was read. */
export interface Observed<T> {
    value: T;
    observedAt: number;
}

export type Known<T> = Extract<Remote<T>, { state: 'known' }>;
export type Settled<T> = Exclude<Remote<T>, { state: 'loading' }>;

/**
 * Turns the raw JSON answer into the value a screen uses, or throws when the
 * answer is not the contract. A throw is "unknown"; it must not be replaced by
 * a default inside the decoder.
 */
export type Decode<T> = (raw: unknown) => T;

export const LOADING: { state: 'loading' } = Object.freeze({ state: 'loading' });

// The last answer the server really gave, whatever happened since.
// Sunucunun gerçekten verdiği son yanıt.
export function lastKnown<T>(remote: Remote<T>): Observed<T> | undefined {
    if (remote.state === 'known') return { value: remote.value, observedAt: remote.observedAt };
    return remote.state === 'unknown' ? remote.previous : undefined;
}

// readRemote never throws: a refused, failed, dropped or malformed answer is
// unknown, carrying the earlier answer when the caller had one.
// readRemote hata fırlatmaz: reddedilen, başarısız ya da bozuk yanıt
// bilinmeyendir; çağıranın önceki yanıtı varsa onu taşır.
export async function readRemote<T>(
    url: string,
    decode: Decode<T>,
    earlier?: Remote<T>,
    init?: RequestInit,
): Promise<Settled<T>> {
    const previous = earlier && lastKnown(earlier);
    const failed = (reason: ApiError, status?: number): Settled<T> => ({
        state: 'unknown',
        reason,
        ...(status === undefined ? {} : { status }),
        ...(previous ? { previous } : {}),
    });
    try {
        const res = init ? await fetch(url, init) : await fetch(url);
        if (!res.ok) return failed(await readApiError(res), res.status);
        return { state: 'known', value: decode(await res.json()), observedAt: Date.now() };
    } catch {
        return failed({ message: '' });
    }
}

// mapRemote reads one fact out of an answer without changing what is known
// about it: loading stays loading, unknown stays unknown.
// mapRemote, bir yanıttan tek olguyu, onun hakkında bilineni değiştirmeden
// okur: yükleniyor yükleniyor kalır, bilinmeyen bilinmeyen kalır.
export function mapRemote<T, U>(remote: Remote<T>, pick: (value: T) => U): Remote<U> {
    if (remote.state === 'loading') return remote;
    if (remote.state === 'known') return { state: 'known', value: pick(remote.value), observedAt: remote.observedAt };
    const { previous, ...rest } = remote;
    return previous
        ? { ...rest, previous: { value: pick(previous.value), observedAt: previous.observedAt } }
        : rest;
}

// decodeList is the decoder of an endpoint that answers with a list. A Go
// handler encodes a list it never appended to as `null`; that is the server
// saying "none", so it is the one non-array accepted. Anything else is not the
// contract and is unknown.
// decodeList, liste döndüren uç noktanın çözücüsüdür. Go, hiç eklenmemiş bir
// listeyi `null` olarak yazar; bu sunucunun "yok" demesidir. Başka her şey
// sözleşme değildir ve bilinmeyendir.
export function decodeList<T>(raw: unknown): T[] {
    if (raw === null) return [];
    if (!Array.isArray(raw)) throw new Error('list');
    return raw as T[];
}

// decodeListIn is decodeList for an endpoint that answers `{field: [...]}`. An
// answer without the field is not "none": it is not the contract.
// decodeListIn, `{alan: [...]}` yanıtlayan uç nokta içindir. Alanı taşımayan
// yanıt "yok" değildir; sözleşme değildir.
export function decodeListIn<T>(raw: unknown, field: string): T[] {
    if (!raw || typeof raw !== 'object' || Array.isArray(raw) || !(field in raw)) throw new Error('shape');
    return decodeList<T>((raw as Record<string, unknown>)[field]);
}

// countText is how a count beside a tab, a title or a card is written: the
// number once it is known, "…" while it is read and "–" when it could not be
// read. An earlier answer, when there is one, is still the number: a badge is
// not where a failed refresh is announced.
// countText, bir sekmenin, başlığın ya da kartın yanındaki sayının yazılışıdır:
// bilinince sayı, okunurken "…", okunamayınca "–".
export function countText(remote: Remote<number>): string {
    if (remote.state === 'known') return String(remote.value);
    if (remote.state === 'loading') return '…';
    return remote.previous ? String(remote.previous.value) : '–';
}

// What a control that depends on a server fact may do. `blocked` carries the
// reason and can only be built from a known value, so a screen cannot show a
// requirement as unmet before the answer exists.
// Sunucudaki bir olguya bağlı denetimin yapabileceği. `blocked` yalnız bilinen
// bir değerden kurulabilir.
export type Gate<R> =
    | { state: 'checking' }
    | { state: 'unknown' }
    | { state: 'open' }
    | { state: 'blocked'; reason: R };

export function gateOn<T, R>(remote: Remote<T>, blockedBy: (value: T) => R | null): Gate<R> {
    if (remote.state === 'loading') return { state: 'checking' };
    if (remote.state === 'unknown') return { state: 'unknown' };
    const reason = blockedBy(remote.value);
    return reason === null ? { state: 'open' } : { state: 'blocked', reason };
}

// --- One read per resource -------------------------------------------------
//
// Every component that reads the same address while another one is showing it
// shares that one answer and that one request: a dialog opened over a page
// does not ask again for what the page has, and two panels mounted together
// cause one request. The entry lives exactly as long as something on screen
// reads it; when the last reader leaves, the answer is dropped, so a later
// visit, or another signed-in user, never starts from an old one.
//
// Aynı adresi okuyan her bileşen, bir başkası onu gösterirken o tek yanıtı ve
// o tek isteği paylaşır. Kayıt, ekranda onu okuyan bir şey olduğu sürece
// yaşar; son okuyan ayrılınca yanıt bırakılır.
interface View<T> {
    remote: Remote<T>;
    reading: boolean;
}

interface Entry<T> {
    view: View<T>;
    decode: Decode<T>;
    init?: RequestInit;
    watchers: Set<() => void>;
    run: number;
    inflight: Promise<Settled<T>> | null;
}

const entries = new Map<string, Entry<unknown>>();
const IDLE: View<never> = { remote: LOADING, reading: false };

function publish<T>(entry: Entry<T>, view: View<T>) {
    entry.view = view;
    for (const watch of [...entry.watchers]) watch();
}

function read<T>(url: string, entry: Entry<T>, replace: boolean): Promise<Settled<T>> {
    if (entry.inflight && !replace) return entry.inflight;
    const run = ++entry.run;
    publish(entry, { remote: entry.view.remote, reading: true });
    const pending: Promise<Settled<T>> = readRemote(url, entry.decode, entry.view.remote, entry.init).then((next) => {
        // A read that was replaced, or whose readers have all left, changes
        // nothing: the newer read is the answer.
        // Yerini yenisine bırakan ya da okuyanı kalmayan okuma hiçbir şeyi
        // değiştirmez.
        if (entry.run !== run) return entry.inflight ?? next;
        entry.inflight = null;
        if (entries.get(url) === entry) publish(entry, { remote: next, reading: false });
        return next;
    });
    entry.inflight = pending;
    return pending;
}

export interface RemoteHandle<T> {
    remote: Remote<T>;
    /** A read is in flight: the first one, a retry or a refresh. */
    reading: boolean;
    /**
     * Reads again and replaces any read in flight, so it is also what a screen
     * calls after its own change. The value on screen stays until the answer
     * arrives. It only reads; it never throws.
     */
    retry: () => Promise<Remote<T>>;
}

export interface RemoteOptions {
    /**
     * How long an answer another mounted reader already holds is used without
     * asking again, in milliseconds. Default 0: a late reader shows that answer
     * at once and refreshes it behind the screen.
     */
    freshFor?: number;
    init?: RequestInit;
}

// useRemote reads `url` and reports the three states. `decode` must be the
// same module-level function for every reader of one address. A null address
// reads nothing and stays `loading`; a caller that passes null must not draw
// from the handle.
// useRemote `url`i okur ve üç durumu bildirir. Bir adresi okuyan herkes aynı
// `decode` işlevini verir. null adres hiçbir şey okumaz.
export function useRemote<T>(url: string | null, decode: Decode<T>, options: RemoteOptions = {}): RemoteHandle<T> {
    const { freshFor = 0, init } = options;
    const [held, setHeld] = useState<{ url: string | null; view: View<T> }>(() => ({ url, view: shown<T>(url) }));

    useEffect(() => {
        if (url === null) return;
        let entry = entries.get(url) as Entry<T> | undefined;
        if (!entry) {
            entry = { view: IDLE, decode, init, watchers: new Set(), run: 0, inflight: null };
            entries.set(url, entry as Entry<unknown>);
        }
        const mine = entry;
        const watch = () => setHeld({ url, view: mine.view });
        mine.watchers.add(watch);
        const current = mine.view.remote;
        const fresh = current.state === 'known' && Date.now() - current.observedAt <= freshFor;
        if (!mine.inflight && !fresh) void read(url, mine, false);
        watch();
        return () => {
            mine.watchers.delete(watch);
            if (mine.watchers.size === 0 && entries.get(url) === mine) entries.delete(url);
        };
        // `decode`, `init` and `freshFor` belong to the address, not to a render,
        // so only a new address starts a new subscription.
    }, [url]);

    const retry = useCallback((): Promise<Remote<T>> => {
        const entry = url === null ? undefined : (entries.get(url) as Entry<T> | undefined);
        return entry ? read(url as string, entry, true) : Promise.resolve<Remote<T>>(LOADING);
    }, [url]);

    // A render for another address than the one held shows what is known about
    // the new address, never the old address's answer for one frame.
    // Tutulandan başka bir adres için çizim, eski adresin yanıtını bir kare
    // bile göstermez.
    const view = held.url === url ? held.view : shown<T>(url);
    return { remote: view.remote, reading: view.reading, retry };
}

function shown<T>(url: string | null): View<T> {
    const entry = url === null ? undefined : (entries.get(url) as Entry<T> | undefined);
    return entry ? entry.view : IDLE;
}
