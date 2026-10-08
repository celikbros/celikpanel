import { useRemote, type RemoteHandle } from './remote';

// The firewall of this server as the Agent reports it: GET /api/v1/firewall.
//
// This is the shared reader of that address (the dashboard reads through it;
// the firewall panel of the Components page still reads on its own and is on
// the ratchet's list). A failed status read is neither "on"
// nor "off": an Agent, permission or network failure must never be mistaken
// for an open firewall, and a screen must not offer to turn the firewall on,
// or say it is off, from the lack of an answer. The decoder refuses an answer
// without a boolean `enabled` and an answer that carries the Agent's own
// `error`; both are unknown, not "off".
//
// Bu sunucunun güvenlik duvarı, Agent'ın bildirdiği hâliyle. Bu adresin paylaşılan
// okuyucusu budur (Bileşenler sayfasının güvenlik duvarı paneli hâlâ kendi
// okur). Başarısız durum okuması ne "açık" ne de "kapalı"dır; yanıt
// yokluğundan güvenlik duvarı kapalı sayılmaz ve açma önerilmez. Çözücü,
// boolean `enabled` taşımayan ya da Agent'ın kendi `error` alanını taşıyan
// yanıtı reddeder; ikisi de bilinmeyendir, "kapalı" değildir.
export const FIREWALL_URL = '/api/v1/firewall';

export interface FirewallStatus {
    enabled: boolean;
    engine_available?: boolean;
    tcp_ports?: number[];
    udp_ports?: number[];
    ssh_ports?: number[];
    persistence_state?: string;
    persistence_error?: string;
    snapshot_version?: number;
    /** Why `ssh_ports` is empty; absent when an SSH listener was proven. */
    ssh_discovery_reason?: string;
}

export function decodeFirewallStatus(raw: unknown): FirewallStatus {
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) throw new Error('shape');
    const body = raw as Record<string, unknown>;
    if (typeof body.error === 'string' && body.error.trim() !== '') throw new Error('agent');
    if (typeof body.enabled !== 'boolean') throw new Error('field');
    return body as unknown as FirewallStatus;
}

// `enabled: false` is for a signed-in user who may not read the server's
// firewall: nothing is requested and the caller must not draw from the handle.
// `enabled: false`, sunucunun güvenlik duvarını okuyamayan kullanıcı içindir.
export function useFirewallStatus(options: { enabled?: boolean } = {}): RemoteHandle<FirewallStatus> {
    return useRemote(options.enabled === false ? null : FIREWALL_URL, decodeFirewallStatus, { init: { cache: 'no-store' } });
}
