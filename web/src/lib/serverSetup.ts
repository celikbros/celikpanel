export const setupPurposes = ['web', 'web_mail', 'application', 'dns', 'custom'] as const;
export type SetupPurpose = typeof setupPurposes[number];
export type SetupDNSMode = 'local' | 'existing' | 'external';
export interface ServerSetupDraft {
    purpose: SetupPurpose;
    customization?: { components: string[] };
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
    guidance?: 'undecided' | 'guided' | 'manual';
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
    if (draft.customization !== undefined && (!record(draft.customization) || !Array.isArray(draft.customization.components)
        || draft.customization.components.length > 80 || draft.customization.components.some(id => typeof id !== 'string' || !/^[a-z][a-z0-9-]{0,63}$/.test(id))
        || new Set(draft.customization.components).size !== draft.customization.components.length)) return null;
    if (value.checks.some(check => !record(check) || typeof check.id !== 'string'
        || !['ready', 'action_required', 'unknown'].includes(String(check.state)) || typeof check.code !== 'string')) return null;
    if (value.server_ip !== undefined && typeof value.server_ip !== 'string') return null;
    if (value.status === 'ready' && value.required) return null;
    if (value.guidance !== undefined && !['undecided', 'guided', 'manual'].includes(String(value.guidance))) return null;
    return value as unknown as ServerSetupSnapshot;
}
export function isSetupRecoveryPath(pathname: string): boolean {
    return pathname === '/settings' || pathname === '/services' || pathname.startsWith('/services/');
}
export function shouldOpenServerSetup(snapshot: ServerSetupSnapshot, pathname: string): boolean {
    const active = snapshot.status === 'running' || snapshot.status === 'waiting';
    const needsChoice = snapshot.guidance === 'undecided' && snapshot.status !== 'ready';
    return (active || needsChoice || (snapshot.required && snapshot.guidance !== 'manual'))
        && pathname !== '/setup' && !isSetupRecoveryPath(pathname);
}
export function setupNextPath(purpose: SetupPurpose, selected?: ReadonlySet<string>): string {
    if (selected) {
        if (['nginx', 'node', 'phpmyadmin', 'phppgadmin', 'roundcube'].some(id => selected.has(id))) return '/domains';
        if (selected.size === 0) return '/settings?section=dns';
        return '/services';
    }
    return purpose === 'dns' ? '/settings?section=dns' : purpose === 'custom' ? '/services' : '/domains';
}
export function chooseSetupPurpose(draft: ServerSetupDraft, purpose: SetupPurpose): ServerSetupDraft {
    return {
        ...draft, purpose,
        customization: purpose === 'custom' ? { components: [] } : undefined,
        dns_mode: purpose === 'dns' ? 'local' : draft.dns_mode,
        database: purpose === 'dns' || purpose === 'custom' ? '' : draft.database || 'mariadb',
    };
}
