export const setupPurposes = ['web', 'web_mail', 'application', 'dns'] as const;
export type SetupPurpose = typeof setupPurposes[number];
export type SetupDNSMode = 'local' | 'existing' | 'external';
export interface ServerSetupDraft {
    purpose: SetupPurpose;
    panel_domain: string;
    mail_hostname: string;
    dns_mode: SetupDNSMode;
    remote_dns_connection_id: string;
    dns_engine: 'bind' | 'pdns';
    dns_role: 'primary' | 'secondary';
    ns1: string;
    ns2: string;
    local_ip: string;
    peer_ip: string;
    peer_ns: string;
    node_version: string;
    database: string;
}
export interface ServerSetupCheck {
    id: string;
    state: 'ready' | 'action_required' | 'unknown';
    code: string;
}
export interface ServerSetupSnapshot {
    version: number;
    revision: number;
    origin: 'fresh' | 'legacy';
    status: 'new' | 'legacy' | 'draft' | 'running' | 'waiting' | 'failed' | 'ready';
    required: boolean;
    draft: ServerSetupDraft;
    checks: ServerSetupCheck[];
    server_ip?: string;
}
const draftStrings = ['remote_dns_connection_id', 'panel_domain', 'mail_hostname', 'ns1', 'ns2', 'local_ip', 'peer_ip', 'peer_ns', 'node_version', 'database'] as const;
function record(value: unknown): value is Record<string, unknown> {
    return !!value && typeof value === 'object' && !Array.isArray(value);
}
export function decodeServerSetup(value: unknown): ServerSetupSnapshot | null {
    if (!record(value) || value.version !== 1 || !Number.isSafeInteger(value.revision) || (value.revision as number) < 0
        || !['fresh', 'legacy'].includes(String(value.origin))
        || !['new', 'legacy', 'draft', 'running', 'waiting', 'failed', 'ready'].includes(String(value.status))
        || typeof value.required !== 'boolean' || !record(value.draft) || !Array.isArray(value.checks)) return null;
    const draft = value.draft;
    if (!setupPurposes.includes(draft.purpose as SetupPurpose)
        || !['local', 'existing', 'external'].includes(String(draft.dns_mode))
        || !['bind', 'pdns'].includes(String(draft.dns_engine))
        || !['primary', 'secondary'].includes(String(draft.dns_role))
        || draftStrings.some(key => typeof draft[key] !== 'string')) return null;
    if (value.checks.some(check => !record(check) || typeof check.id !== 'string'
        || !['ready', 'action_required', 'unknown'].includes(String(check.state)) || typeof check.code !== 'string')) return null;
    if (value.server_ip !== undefined && typeof value.server_ip !== 'string') return null;
    if (value.status === 'ready' && value.required) return null;
    return value as unknown as ServerSetupSnapshot;
}
export function isSetupRecoveryPath(pathname: string): boolean {
    return pathname === '/settings' || pathname === '/services' || pathname.startsWith('/services/');
}
export function shouldOpenServerSetup(snapshot: ServerSetupSnapshot, pathname: string): boolean {
    return snapshot.required && pathname !== '/setup' && !isSetupRecoveryPath(pathname);
}
export function setupNextPath(purpose: SetupPurpose): string {
    return purpose === 'dns' ? '/settings?section=dns' : '/domains';
}
export function chooseSetupPurpose(draft: ServerSetupDraft, purpose: SetupPurpose): ServerSetupDraft {
    return {
        ...draft, purpose,
        dns_mode: purpose === 'dns' ? 'local' : draft.dns_mode,
        database: purpose === 'dns' ? '' : draft.database || 'mariadb',
    };
}
