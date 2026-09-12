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
    dns_publisher_endpoint?: string;
    dns_hosting_management?: 'manual' | 'panel';
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
    if (draft.dns_hosting_management !== undefined && !['manual', 'panel'].includes(String(draft.dns_hosting_management))) return null;
    if (draft.dns_publisher_endpoint !== undefined && typeof draft.dns_publisher_endpoint !== 'string') return null;
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

// Slot names are the setup protocol's primary/secondary order, not hostname prefixes.
export function setupDNSNames(draft: ServerSetupDraft) {
    const localKey = draft.dns_role === 'primary' ? 'ns1' : 'ns2';
    const peerKey = localKey === 'ns1' ? 'ns2' : 'ns1';
    const canonical = (value: string) => value.trim().toLowerCase().replace(/\.$/, '');
    return { localKey, peerKey, mismatch: canonical(draft.peer_ns) !== canonical(draft[peerKey]) } as const;
}
export function changeSetupDNSRole(draft: ServerSetupDraft, role: ServerSetupDraft['dns_role']): ServerSetupDraft {
    // Changing roles keeps the name and IP attached to the same physical server.
    return role === draft.dns_role ? draft : { ...draft, dns_role: role, ns1: draft.ns2, ns2: draft.ns1 };
}
export function setupDetectedIPv4(value?: string): string {
    if (!value || !/^(?:0|[1-9]\d{0,2})(?:\.(?:0|[1-9]\d{0,2})){3}$/.test(value)) return '';
    const [a, b, ...rest] = value.split('.').map(Number);
    if ([a, b, ...rest].some(n => n > 255) || a === 0 || a === 10 || a === 127 || a >= 224
        || (a === 169 && b === 254) || (a === 172 && b >= 16 && b <= 31)
        || (a === 192 && b === 168) || (a === 100 && b >= 64 && b <= 127)) return '';
    return value;
}

// Tab-local editing state only. Server revisions and operation admission remain
// authoritative; neither a reviewed plan nor permission to start is stored here.
// Yalniz sekmenin duzenleme durumu tutulur; sunucu surumu ve islem kabul
// denetimleri gecerliligini korur. Plan veya baslatma izni burada saklanmaz.
export type SetupEditorStep = 'purpose' | 'components' | 'access' | 'review';
export interface SetupEditorCheckpoint {
    version: 1;
    revision: number;
    step: SetupEditorStep;
    draft: ServerSetupDraft;
}
export function decodeSetupEditorCheckpoint(raw: string | null, snapshot: ServerSetupSnapshot): SetupEditorCheckpoint | null {
    try {
        if (!['new', 'legacy', 'draft'].includes(snapshot.status)) return null;
        const value: unknown = JSON.parse(raw || 'null');
        if (!record(value) || value.version !== 1 || value.revision !== snapshot.revision
            || !['purpose', 'components', 'access', 'review'].includes(String(value.step))) return null;
        const restored = decodeServerSetup({ ...snapshot, draft: value.draft });
        if (!restored || (value.step === 'components' && !restored.draft.customization && restored.draft.purpose !== 'custom')) return null;
        // Review can only be recovered from exactly the server-saved inputs.
        // Inceleme yalniz sunucuda kayitli girdilerle ayniysa geri yuklenir.
        if (value.step === 'review' && JSON.stringify(restored.draft) !== JSON.stringify(snapshot.draft)) return null;
        return { version: 1, revision: snapshot.revision, step: value.step as SetupEditorStep, draft: restored.draft };
    } catch { return null; }
}
