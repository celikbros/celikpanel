// Every state-changing request carries one identity; a replay never runs twice
// (D-029). Measured in a real browser: when the connection is reset while a
// POST is being sent, the browser sends the POST again by itself, and one
// click reached the server three times. The server answers every arrival of
// the same identity from the first one's result, so the identity is made here,
// once per user action, and travels in a header.
//
// Durum değiştiren her istek tek bir kimlik taşır; yineleme asla ikinci kez
// çalışmaz (D-029). Kimlik burada, kullanıcı eylemi başına bir kez üretilir ve
// bir başlıkta taşınır.

export const REQUEST_ID_HEADER = 'X-CelikPanel-Request-Id';

// 32 lowercase hexadecimal characters: the product's operation-id format.
export function newRequestId(): string {
    const bytes = new Uint8Array(16);
    crypto.getRandomValues(bytes);
    return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('');
}

// Batch 1: the eight routes the server refuses without an identity. For these,
// a lost answer is asked for again once, with the same identity.
// Birinci grup: sunucunun kimliksiz reddettiği sekiz rota. Bunlarda kaybolan
// yanıt, aynı kimlikle bir kez daha istenir.
const REASK_ROUTES = [
    /^\/api\/v1\/domains\/\d+\/backups\/restore$/,
    /^\/api\/v1\/domains\/\d+\/backups$/,
    /^\/api\/v1\/domains\/\d+\/ssl\/letsencrypt$/,
    /^\/api\/v1\/domains\/\d+\/databases$/,
    /^\/api\/v1\/database-servers\/\d+\/databases$/,
    /^\/api\/v1\/database-servers\/\d+\/admin-account$/,
    /^\/api\/v1\/vpn\/peers$/,
    /^\/api\/v1\/import\/cpanel\/apply$/,
    // D-031 (2026-10-10): the owner's three choices about a site's
    // configuration file; the server guards them the same way.
    // Sahibin site yapılandırma dosyası hakkındaki üç seçimi.
    /^\/api\/v1\/domains\/\d+\/site-config\/(?:keep|take|recreate)$/,
];

// An answer that says nothing about the change: a gateway between the browser
// and the Panel answers 408, 429, 502, 503 or 504 with its own page when it
// lost the Panel's answer or never passed the request on. The Panel's own
// refusals are JSON, also when their status is one of these (an Agent that
// could not be reached is a 502 with a sentence and a code): that is the
// answer, and it is shown, not asked for again. This is the one definition of
// a lost answer; lib/lostAnswer.ts and the import page use it too.
// Değişiklik hakkında hiçbir şey söylemeyen yanıt: tarayıcı ile Panel
// arasındaki geçit, Panel'in yanıtını yitirdiğinde ya da isteği hiç
// iletmediğinde kendi sayfasıyla 408, 429, 502, 503 ya da 504 yanıtlar.
// Panel'in kendi retleri, durum kodu bunlardan biri olsa da JSON'dur: o yanıtın
// kendisidir; gösterilir, yeniden sorulmaz. Kaybolan yanıtın tek tanımı budur.
const LOST_ANSWER_STATUSES = new Set([408, 429, 502, 503, 504]);

export function answerWasLost(answer: Response): boolean {
    if (!LOST_ANSWER_STATUSES.has(answer.status)) return false;
    return !(answer.headers.get('Content-Type') ?? '').toLowerCase().includes('json');
}

export const REASK_DELAY_MS = 1500;

// The path of a call to this Panel, or null for any other address: the header
// is never sent to another origin.
// Bu Panel'e yapılan çağrının yolu; başka her adres için null. Başlık asla
// başka bir kökene gönderilmez.
function panelPath(url: string): string | null {
    try {
        const here = typeof location === 'undefined' ? 'http://panel.invalid' : location.origin;
        const target = new URL(url, here);
        return target.origin === new URL(here).origin ? target.pathname : null;
    } catch {
        return null;
    }
}

export function isReaskRoute(method: string, url: string): boolean {
    if (method.toUpperCase() !== 'POST') return false;
    const path = panelPath(url);
    return path !== null && REASK_ROUTES.some((route) => route.test(path));
}

// A body that already names its request (the operation-row subsystems: service
// install, setup, DNS engine switch, panel update) keeps that identity.
function bodyCarriesRequestId(body: unknown): boolean {
    if (typeof body !== 'string' || !body.includes('"request_id"')) return false;
    try {
        const parsed: unknown = JSON.parse(body);
        return !!parsed && typeof parsed === 'object' && !Array.isArray(parsed)
            && typeof (parsed as { request_id?: unknown }).request_id === 'string'
            && (parsed as { request_id: string }).request_id !== '';
    } catch {
        return false;
    }
}

export interface IdentifiedRequest {
    init: RequestInit | undefined;
    // The identity the request carries in the header, or null when none is added.
    id: string | null;
    reask: boolean;
}

// identifyRequest adds the header to a state-changing /api/ call that has no
// identity yet. A page that set the header itself keeps its own value.
// identifyRequest, henüz kimliği olmayan durum değiştiren /api/ çağrısına
// başlığı ekler. Başlığı kendisi koyan sayfanın değeri korunur.
export function identifyRequest(input: RequestInfo | URL, init?: RequestInit): IdentifiedRequest {
    const request = typeof input === 'object' && 'url' in input ? input : null;
    const url = request ? request.url : String(input);
    const method = (init?.method ?? request?.method ?? 'GET').toUpperCase();
    if (method === 'GET' || method === 'HEAD' || method === 'OPTIONS' || !panelPath(url)?.startsWith('/api/')) {
        return { init, id: null, reask: false };
    }
    const headers = new Headers(init?.headers ?? request?.headers);
    const reask = isReaskRoute(method, url);
    const own = headers.get(REQUEST_ID_HEADER);
    if (own) return { init, id: own, reask };
    if (bodyCarriesRequestId(init?.body)) return { init, id: null, reask: false };
    const id = newRequestId();
    headers.set(REQUEST_ID_HEADER, id);
    return { init: { ...init, headers }, id, reask };
}

const pause = (ms: number) => new Promise<void>((resolve) => { setTimeout(resolve, ms); });

function aborted(error: unknown, init?: RequestInit): boolean {
    return !!init?.signal?.aborted || (error instanceof Error && error.name === 'AbortError');
}

// sendIdentified sends one user action. On the eight routes a lost answer (the
// connection failed, or a gateway answered in the Panel's place) is asked for
// again once, after a short delay, with the same identity; the server answers
// it from the first arrival and never runs the change twice. When that also
// fails the caller sees what it saw before: the failure, or the gateway's
// answer.
//
// sendIdentified tek bir kullanıcı eylemini gönderir. Sekiz rotada kaybolan
// yanıt, kısa bir beklemeden sonra aynı kimlikle bir kez daha istenir; sunucu
// onu ilk gelişten yanıtlar ve değişikliği asla iki kez çalıştırmaz.
export async function sendIdentified(
    send: typeof fetch,
    input: RequestInfo | URL,
    init?: RequestInit,
    wait: (ms: number) => Promise<void> = pause,
): Promise<Response> {
    const identified = identifyRequest(input, init);
    // A Request object can be read once; a second send needs its own copy.
    const again = identified.reask && typeof input === 'object' && 'clone' in input ? input.clone() : input;
    if (!identified.reask) return send(input, identified.init);

    let lost: Response | null = null;
    let failure: unknown = null;
    try {
        const answer = await send(input, identified.init);
        if (!answerWasLost(answer)) return answer;
        lost = answer;
    } catch (error) {
        if (aborted(error, init)) throw error;
        failure = error;
    }
    await wait(REASK_DELAY_MS);
    if (init?.signal?.aborted) {
        if (lost) return lost;
        throw failure;
    }
    try {
        return await send(again, identified.init);
    } catch (error) {
        if (lost) return lost;
        throw failure ?? error;
    }
}
