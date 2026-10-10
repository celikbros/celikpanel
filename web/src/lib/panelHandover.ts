// Planned certificate handover during server setup (2026-10-08, D-024).
//
// The setup step "Secure panel access" obtains the panel certificate for the
// reviewed host name; the Agent then restarts the Panel once so it serves that
// certificate. The restart follows the step's certificate work, so it lands
// while the step is running or just after it. Every browser page loses its
// connection for that restart. This is known before setup starts, so the wizard
// says it in advance and names the restart when the connection drops, instead
// of showing an unknown result. Pure decisions only; no requests, no storage
// writes, no translation keys.
//
// Kurulumun "Panel erişimini güvenceye al" adımı sertifikayı alır; Agent paneli
// yeni sertifikayı sunması için bir kez yeniden başlatır. Bu önceden bilinir;
// sihirbaz bunu önceden söyler ve bağlantı koptuğunda bilinmeyen sonuç yerine
// planlı yeniden başlatmayı anlatır. Yalnız saf kararlar.

export interface HandoverStep { kind: string; target: string; status?: string }
export interface HandoverExecution { status: string; steps: readonly HandoverStep[] }
export interface SetupHandover {
    host: string;
    // ahead: the step has not started; now: it is running; done: it finished.
    phase: 'ahead' | 'now' | 'done';
    // The browser is on another address (an IP or another name) than the host
    // being secured, so the secure address is worth linking.
    elsewhere: boolean;
}

// A public host name, lower-cased, without a trailing dot. IP addresses and
// single labels are not host names the panel certificate can carry.
export function handoverHost(raw: unknown): string | null {
    if (typeof raw !== 'string') return null;
    const name = raw.trim().toLowerCase().replace(/\.$/, '');
    if (name.length === 0 || name.length > 253 || !name.includes('.')) return null;
    const labels = name.split('.');
    if (!labels.every(label => /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(label))) return null;
    if (/^\d+$/.test(labels[labels.length - 1])) return null;
    return name;
}

export function setupHandover(steps: readonly HandoverStep[] | undefined, currentHost: unknown): SetupHandover | null {
    const step = steps?.find(item => item.kind === 'panel_certificate');
    const host = handoverHost(step?.target);
    if (!step || !host || step.status === 'failed') return null;
    return {
        host,
        phase: step.status === 'succeeded' ? 'done' : step.status === 'running' ? 'now' : 'ahead',
        elsewhere: handoverHost(currentHost) !== host,
    };
}

// The connection dropped. It is the planned restart only while the last state
// the wizard read puts the certificate step at the front: running, next to run
// (every earlier step finished), or finished with no later step finished yet.
// Anything else stays an unknown result.
export function plannedHandoverDrop(execution: HandoverExecution | null | undefined, currentHost: unknown): SetupHandover | null {
    if (!execution || execution.status !== 'running') return null;
    const handover = setupHandover(execution.steps, currentHost);
    if (!handover) return null;
    if (handover.phase === 'now') return handover;
    if (handover.phase === 'done') return handoverSettled(execution) ? null : handover;
    const index = execution.steps.findIndex(item => item.kind === 'panel_certificate');
    return execution.steps.slice(0, index).every(item => item.status === 'succeeded') ? handover : null;
}

// Once a step after the certificate step has finished, or setup itself has
// ended, a later restart is no longer this handover.
export function handoverSettled(execution: HandoverExecution | null | undefined): boolean {
    if (!execution) return false;
    if (execution.status === 'succeeded' || execution.status === 'failed') return true;
    const index = execution.steps.findIndex(item => item.kind === 'panel_certificate');
    if (index < 0) return true;
    if (execution.steps[index].status === 'failed') return true;
    return execution.steps[index].status === 'succeeded'
        && execution.steps.slice(index + 1).some(item => item.kind !== 'verify' && (item.status === 'succeeded' || item.status === 'failed'));
}

// https://<host>[:<port>] for the secure address. The server's own URL is used
// when it names exactly that host over HTTPS without credentials; otherwise the
// port of the page the owner is already on (the same Panel listener).
export function handoverAddress(host: string, currentPort: unknown, serverURL?: string | null): string {
    if (serverURL) {
        try {
            const url = new URL(serverURL);
            if (url.protocol === 'https:' && url.hostname === host && !url.username && !url.password) return url.origin;
        } catch { /* fall through to the current port */ }
    }
    const port = typeof currentPort === 'string' && /^\d{1,5}$/.test(currentPort) && currentPort !== '443' ? `:${currentPort}` : '';
    return `https://${host}${port}`;
}

// Where the wizard keeps its start marker; the recovery page reads the same key.
export const setupStartMarkerKey = (username: string): string => `celikpanel.setup.start.${username}`;

// The setup start marker this browser saved, read for the recovery page. It
// supplies identity only: the host a setup started here was going to secure.
// `handover` is set at start when the reviewed plan contained the step.
export function savedSetupHandoverHost(raw: unknown): string | null {
    if (typeof raw !== 'string' || raw.length > 4096) return null;
    try {
        const value: unknown = JSON.parse(raw);
        if (!value || typeof value !== 'object' || Array.isArray(value)) return null;
        const marker = value as Record<string, unknown>;
        return marker.handover === true ? handoverHost(marker.panel_domain) : null;
    } catch { return null; }
}

// The recovery page may name the handover only when the server itself reports
// that the Panel now serves a managed certificate for the host this browser's
// setup was securing. A saved marker alone proves nothing about the server.
export function recoveryHandover(savedHost: string | null, servedHost: unknown, currentHost: unknown): { host: string; elsewhere: boolean } | null {
    const served = handoverHost(servedHost);
    if (!savedHost || !served || served !== savedHost) return null;
    return { host: served, elsewhere: handoverHost(currentHost) !== served };
}

// The Panel refuses the catalogue scan while a server setup run owns the host
// (409 server_setup_busy). That is an answer from a reachable Panel, not a lost
// connection, and it does not clear until the whole setup ends.
// Kurulum makineyi tutarken tarama reddedilir; bu bir bağlantı kaybı değildir.
export async function scanRefusedBySetup(response: { status: number; clone(): { json(): Promise<unknown> } }): Promise<boolean> {
    if (response.status !== 409) return false;
    try {
        const body = await response.clone().json();
        return !!body && typeof body === 'object' && (body as { code?: unknown }).code === 'server_setup_busy';
    } catch { return false; }
}
