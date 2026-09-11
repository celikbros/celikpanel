export interface DNSConnection { id: string; endpoint: string; status: 'pending' | 'ready' | 'revoked'; nameservers: string[]; created_at: string }
export interface DNSClient { id: string; label: string; created_at: string; revoked: boolean }
export const remoteDNSIdentity = (value: unknown): value is string => typeof value === 'string' && /^[a-f0-9]{32}$/.test(value);
const record = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value);
export function remoteDNSEndpoint(value: string): string | null {
    try {
        const url = new URL(value.trim());
        if (url.protocol !== 'https:' || url.username || url.password || url.search || url.hash || !['', '/'].includes(url.pathname)
            || !/^[a-z0-9](?:[a-z0-9.-]*\.)[a-z0-9-]+$/i.test(url.hostname) || /^\d+(?:\.\d+){3}$/.test(url.hostname)) return null;
        return url.origin;
    } catch { return null; }
}
export function decodeDNSConnection(value: unknown): DNSConnection | null {
    if (!record(value) || !remoteDNSIdentity(value.id) || typeof value.endpoint !== 'string' || !remoteDNSEndpoint(value.endpoint)
        || !['pending', 'ready', 'revoked'].includes(String(value.status)) || typeof value.created_at !== 'string'
        || !Array.isArray(value.nameservers) || value.nameservers.some(item => typeof item !== 'string')) return null;
    return value as unknown as DNSConnection;
}
export function decodeDNSConnections(value: unknown): DNSConnection[] | null {
    if (!record(value) || !Array.isArray(value.connections)) return null;
    const list = value.connections.map(decodeDNSConnection);
    return list.some(item => !item) ? null : list as DNSConnection[];
}
export function decodeDNSClients(value: unknown): DNSClient[] | null {
    if (!record(value) || !Array.isArray(value.clients) || value.clients.some(item => !record(item) || !remoteDNSIdentity(item.id)
        || typeof item.label !== 'string' || typeof item.created_at !== 'string' || typeof item.revoked !== 'boolean')) return null;
    return value.clients as unknown as DNSClient[];
}
export class RemoteDNSError extends Error {
    constructor(readonly code: string) { super(code); }
}
export async function remoteDNSRequest(url: string, options?: RequestInit, signal?: AbortSignal): Promise<unknown> {
    const controller = new AbortController();
    const timeout = window.setTimeout(() => controller.abort(), 20000);
    const abort = () => controller.abort();
    signal?.addEventListener('abort', abort, { once: true });
    if (signal?.aborted) abort();
    try {
        const response = await fetch(url, { cache: 'no-store', ...options, signal: controller.signal });
        const value: unknown = response.status === 204 ? null : await response.json();
        if (!response.ok) {
            // Only a stable error code crosses into the UI; remote response
            // text must never expose submitted credentials.
            // Arayüze yalnız sabit hata kodu geçer; uzak yanıt metni girilen
            // kimlik bilgilerini açığa çıkarmamalıdır.
            const code = record(value) && typeof value.code === 'string' ? value.code : 'REMOTE_DNS_UNAVAILABLE';
            throw new RemoteDNSError(code);
        }
        return value;
    } finally { window.clearTimeout(timeout); signal?.removeEventListener('abort', abort); }
}
export const remoteDNSPost = (value: unknown): RequestInit => ({ method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(value) });
