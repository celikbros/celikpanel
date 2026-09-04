export interface ConfigFile {
    path: string;
    is_managed: boolean;
    Content?: string;
    Parsed?: string;
}

export interface ServicesScan {
    scanned_at: string | null;
    services: Service[];
}

export interface Service {
    id: string;
    name: string;
    description: string;
    icon: string;
    category: string;
    versions: string[];
    status: string;
    /** null = this panel has never observed this host for this service. */
    /** null = bu panel bu makineyi bu servis için hiç gözlemedi. */
    is_installed: boolean | null;
    config_files: ConfigFile[];
}

export interface ConfigResponse {
    Content: string;
    Parsed: string;
}

const API_BASE = '/api/v1';

export type EffectiveRole = 'admin' | 'reseller' | 'customer' | 'additional_user';
export type AccountType = 'account' | 'additional_user';

export interface CurrentUser {
    username: string;
    role: EffectiveRole;
    effective_role: EffectiveRole;
    account_type: AccountType;
    email?: string;
    impersonating: boolean;
    features: {
        team_members: boolean;
        [key: string]: unknown;
    };
}

export const TEAM_CAPABILITIES = [
    'files',
    'databases',
    'mail',
    'dns',
    'ssl',
    'cron',
    'backups',
    'php',
    'statistics',
] as const;

export type TeamCapability = (typeof TEAM_CAPABILITIES)[number];
export type TeamPermissionMode = 'view' | 'manage';
export type TeamMemberStatus = 'active' | 'suspended';

export interface TeamSubscriptionPermission {
    subscription_id: number;
    subscription_name?: string;
    capability: TeamCapability;
    mode: TeamPermissionMode;
}

export interface TeamDomainPermission {
    domain_id: number;
    domain_name?: string;
    capability: TeamCapability;
    mode: TeamPermissionMode;
}

export interface TeamMemberAccess {
    subscription_permissions: TeamSubscriptionPermission[];
    domain_permissions: TeamDomainPermission[];
}

export interface TeamMember {
    id: number;
    owner_id: number;
    username: string;
    email: string;
    status: TeamMemberStatus;
    access: TeamMemberAccess;
    created_at: string;
    updated_at: string;
}

export interface TeamMemberCreateInput {
    username: string;
    email: string;
    password: string;
    status?: TeamMemberStatus;
    access: TeamMemberAccess;
}

export interface TeamMemberUpdateInput {
    username?: string;
    email?: string;
    password?: string;
    status?: TeamMemberStatus;
    access?: TeamMemberAccess;
}

export interface TeamMemberSubscriptionScope {
    id: number;
    name: string;
    owner: string;
}

export interface TeamMemberDomainScope {
    id: number;
    domain_name: string;
}

export class ApiResponseError extends Error {
    readonly response: Response;

    constructor(response: Response) {
        super('API request failed with status ' + response.status);
        this.name = 'ApiResponseError';
        this.response = response;
    }
}

function isRecord(value: unknown): value is Record<string, unknown> {
    return typeof value === 'object' && value !== null && !Array.isArray(value);
}

const effectiveRoles = new Set<EffectiveRole>([
    'admin',
    'reseller',
    'customer',
    'additional_user',
]);

function isEffectiveRole(value: unknown): value is EffectiveRole {
    return typeof value === 'string' && effectiveRoles.has(value as EffectiveRole);
}

function unsupportedAuthIdentity(): never {
    throw new Error('unsupported_auth_identity');
}

const teamCapabilitySet = new Set<string>(TEAM_CAPABILITIES);

function malformedTeamMemberResponse(): never {
    throw new Error('malformed_team_member_response');
}

function isPositiveInteger(value: unknown): value is number {
    return typeof value === 'number' && Number.isInteger(value) && value > 0;
}

function isTeamCapability(value: unknown): value is TeamCapability {
    return typeof value === 'string' && teamCapabilitySet.has(value);
}

function isTeamPermissionMode(value: unknown): value is TeamPermissionMode {
    return value === 'view' || value === 'manage';
}

function parseTeamMemberAccess(raw: unknown): TeamMemberAccess {
    if (!isRecord(raw)) malformedTeamMemberResponse();
    if (!Array.isArray(raw.subscription_permissions) || !Array.isArray(raw.domain_permissions)) {
        malformedTeamMemberResponse();
    }

    const subscriptionPermissions = raw.subscription_permissions.map((permission): TeamSubscriptionPermission => {
        if (
            !isRecord(permission)
            || !isPositiveInteger(permission.subscription_id)
            || !isTeamCapability(permission.capability)
            || !isTeamPermissionMode(permission.mode)
            || (permission.subscription_name !== undefined && typeof permission.subscription_name !== 'string')
        ) {
            malformedTeamMemberResponse();
        }
        return {
            subscription_id: permission.subscription_id,
            subscription_name: permission.subscription_name,
            capability: permission.capability,
            mode: permission.mode,
        };
    });

    const domainPermissions = raw.domain_permissions.map((permission): TeamDomainPermission => {
        if (
            !isRecord(permission)
            || !isPositiveInteger(permission.domain_id)
            || !isTeamCapability(permission.capability)
            || !isTeamPermissionMode(permission.mode)
            || (permission.domain_name !== undefined && typeof permission.domain_name !== 'string')
        ) {
            malformedTeamMemberResponse();
        }
        return {
            domain_id: permission.domain_id,
            domain_name: permission.domain_name,
            capability: permission.capability,
            mode: permission.mode,
        };
    });

    return {
        subscription_permissions: subscriptionPermissions,
        domain_permissions: domainPermissions,
    };
}

function parseTeamMember(raw: unknown): TeamMember {
    if (
        !isRecord(raw)
        || !isPositiveInteger(raw.id)
        || !isPositiveInteger(raw.owner_id)
        || typeof raw.username !== 'string'
        || raw.username.trim() === ''
        || typeof raw.email !== 'string'
        || (raw.status !== 'active' && raw.status !== 'suspended')
        || typeof raw.created_at !== 'string'
        || typeof raw.updated_at !== 'string'
    ) {
        malformedTeamMemberResponse();
    }

    return {
        id: raw.id,
        owner_id: raw.owner_id,
        username: raw.username,
        email: raw.email,
        status: raw.status,
        access: parseTeamMemberAccess(raw.access),
        created_at: raw.created_at,
        updated_at: raw.updated_at,
    };
}

function teamMemberOwnerQuery(ownerID?: number): string {
    if (ownerID === undefined) return '';
    if (!Number.isInteger(ownerID) || ownerID <= 0) throw new Error('invalid_team_member_owner');
    return '?owner_id=' + encodeURIComponent(String(ownerID));
}

function parseTeamMemberSubscriptionScopes(raw: unknown): TeamMemberSubscriptionScope[] {
    if (!isRecord(raw) || !Array.isArray(raw.subscriptions)) malformedTeamMemberResponse();
    return raw.subscriptions.map((scope): TeamMemberSubscriptionScope => {
        if (
            !isRecord(scope)
            || !isPositiveInteger(scope.id)
            || typeof scope.name !== 'string'
            || scope.name.trim() === ''
            || typeof scope.owner !== 'string'
        ) {
            malformedTeamMemberResponse();
        }
        return { id: scope.id, name: scope.name, owner: scope.owner };
    });
}

function parseTeamMemberDomainScopes(raw: unknown): TeamMemberDomainScope[] {
    if (!Array.isArray(raw)) malformedTeamMemberResponse();
    return raw.map((scope): TeamMemberDomainScope => {
        if (
            !isRecord(scope)
            || !isPositiveInteger(scope.id)
            || typeof scope.domain_name !== 'string'
            || scope.domain_name.trim() === ''
        ) {
            malformedTeamMemberResponse();
        }
        return { id: scope.id, domain_name: scope.domain_name };
    });
}

// parseCurrentUser is the single fail-closed parser for every authentication
// response. It accepts the previous server's omitted canonical fields and its
// historical role-valued account_type only when that legacy value is
// self-consistent; unknown or contradictory identities are rejected.
export function parseCurrentUser(raw: unknown): CurrentUser {
    if (!isRecord(raw)) unsupportedAuthIdentity();

    const username = raw.username;
    if (typeof username !== 'string' || username.trim() === '') unsupportedAuthIdentity();

    const role = raw.role;
    if (!isEffectiveRole(role)) unsupportedAuthIdentity();

    const effectiveRole = raw.effective_role === undefined ? role : raw.effective_role;
    if (!isEffectiveRole(effectiveRole) || effectiveRole !== role) unsupportedAuthIdentity();

    let accountType: AccountType;
    if (raw.account_type === 'account' || raw.account_type === 'additional_user') {
        accountType = raw.account_type;
    } else if (raw.account_type === undefined) {
        accountType = effectiveRole === 'additional_user' ? 'additional_user' : 'account';
    } else if (
        (raw.account_type === 'admin' || raw.account_type === 'reseller' || raw.account_type === 'customer')
        && raw.account_type === effectiveRole
    ) {
        accountType = 'account';
    } else {
        unsupportedAuthIdentity();
    }

    if ((effectiveRole === 'additional_user') !== (accountType === 'additional_user')) {
        unsupportedAuthIdentity();
    }
    if (raw.email !== undefined && typeof raw.email !== 'string') unsupportedAuthIdentity();
    if (raw.impersonating !== undefined && typeof raw.impersonating !== 'boolean') unsupportedAuthIdentity();

    const rawFeatures = raw.features === undefined ? {} : raw.features;
    if (!isRecord(rawFeatures)) unsupportedAuthIdentity();

    return {
        username,
        role,
        effective_role: effectiveRole,
        account_type: accountType,
        email: raw.email,
        impersonating: raw.impersonating ?? false,
        features: {
            ...rawFeatures,
            team_members: rawFeatures.team_members === true
                && accountType === 'account'
                && effectiveRole === 'customer',
        },
    };
}

export interface PanelUser {
    id: number;
    username: string;
    email: string;
    role: string;
    status: string;
    parent_id?: number;
    parent_name?: string;
    subscriptions: number;
    domains: number;
    created_at: string;
}

export interface ServicePlan {
    id: number;
    name: string;
    max_domains: number;
    max_databases: number;
    max_email_accounts: number;
    disk_quota_mb: number;
    bandwidth_quota_mb: number;
    subscribers?: number;
}

export interface DemoAccount {
    username: string;
    password: string;
    role: string;
}

export interface SystemStats {
    hostname: string;
    os: string;
    kernel: string;
    arch: string;
    ipv4: string;
    uptime_seconds: number;
    cpu_percent: number;
    cpu_cores: number;
    load_avg: number[];
    mem_used_bytes: number;
    mem_total_bytes: number;
    disk_used_bytes: number;
    disk_total_bytes: number;
}

class API {
    private async checkedFetch(path: string, init?: RequestInit): Promise<Response> {
        const response = await fetch(API_BASE + path, init);
        if (!response.ok) throw new ApiResponseError(response);
        return response;
    }

    // me returns the current user, or null when unauthenticated (401).
    // me, mevcut kullanıcıyı döndürür; kimlik doğrulanmamışsa (401) null.
    async me(): Promise<CurrentUser | null> {
        const res = await fetch(`${API_BASE}/auth/me`);
        if (res.status === 401) return null;
        if (!res.ok) throw new Error('Failed to fetch current user');
        return parseCurrentUser(await res.json());
    }

    // login authenticates and, on success, the server sets the session
    // cookie. Returns the user on success, throws on failure.
    // login kimlik doğrular; başarılıysa sunucu oturum çerezini ayarlar.
    // login returns either the signed-in user, or a two-factor challenge
    // (the account has 2FA on) — the caller then finishes with loginTOTP.
    // login ya oturum açan kullanıcıyı ya da iki-faktör istemini döndürür
    // (hesabın 2FA'sı açık) — çağıran loginTOTP ile tamamlar.
    async login(username: string, password: string): Promise<CurrentUser | { totp_required: true; pending_token: string }> {
        const res = await fetch(`${API_BASE}/auth/login`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ username, password }),
        });
        if (!res.ok) {
            if (res.status === 401) throw new Error('invalid_credentials');
            if (res.status === 429) throw new Error('too_many');
            throw new Error('login_failed');
        }
        const payload: unknown = await res.json();
        if (
            isRecord(payload)
            && payload.totp_required === true
            && typeof payload.pending_token === 'string'
            && payload.pending_token !== ''
        ) {
            return { totp_required: true, pending_token: payload.pending_token };
        }
        return parseCurrentUser(payload);
    }

    async loginTOTP(pendingToken: string, code: string): Promise<CurrentUser> {
        const res = await fetch(`${API_BASE}/auth/login/totp`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ pending_token: pendingToken, code }),
        });
        if (!res.ok) {
            if (res.status === 401) throw new Error('invalid_code');
            if (res.status === 429) throw new Error('too_many');
            throw new Error('login_failed');
        }
        return parseCurrentUser(await res.json());
    }

    async logout(): Promise<void> {
        await fetch(`${API_BASE}/auth/logout`, { method: 'POST' });
    }

    // demoAccounts returns the dev quick-login credentials. The list is empty
    // unless the server was started with --demo, so it is safe to always call.
    // demoAccounts, geliştirme hızlı-giriş bilgilerini döndürür. Sunucu --demo
    // ile başlatılmadıkça liste boştur; bu yüzden her zaman çağrılması güvenlidir.
    async demoAccounts(): Promise<DemoAccount[]> {
        try {
            const res = await fetch(`${API_BASE}/auth/demo`);
            if (!res.ok) return [];
            return (await res.json()) || [];
        } catch {
            return [];
        }
    }

    // getServices reads the CACHED scan (instant — never probes the system).
    // A fresh probe is an explicit user action: scanServices().
    // getServices ÖNBELLEKTEKİ taramayı okur (anlık — sistemi asla yoklamaz).
    // Taze yoklama açık bir kullanıcı eylemidir: scanServices().
    async getServices(): Promise<Service[]> {
        const data = await this.getServicesScan();
        return data.services;
    }

    async getServicesScan(): Promise<ServicesScan> {
        const res = await fetch(`${API_BASE}/managed-services`);
        if (!res.ok) throw new Error('Failed to fetch services');
        const data = await res.json();
        return { scanned_at: data?.scanned_at ?? null, services: data?.services || [] };
    }

    async scanServices(): Promise<ServicesScan> {
        const res = await fetch(`${API_BASE}/managed-services/scan`, { method: 'POST' });
        if (!res.ok) throw new Error('Service scan failed');
        const data = await res.json();
        return { scanned_at: data?.scanned_at ?? null, services: data?.services || [] };
    }

    async getSystemStats(): Promise<SystemStats> {
        const res = await fetch(`${API_BASE}/system/stats`);
        if (!res.ok) throw new Error('Failed to fetch system stats');
        return res.json();
    }

    async getConfig(path: string): Promise<ConfigResponse> {
        const res = await fetch(`${API_BASE}/config?path=${encodeURIComponent(path)}`);
        if (!res.ok) throw new Error('Failed to fetch config');
        return res.json();
    }

    async saveConfig(path: string, content: string) {
        const res = await fetch(`${API_BASE}/config?path=${encodeURIComponent(path)}`, {
            method: 'POST',
            headers: { 'Content-Type': 'text/plain' },
            body: content,
        });
        if (!res.ok) throw new Error('Failed to save config');
    }

    async serviceAction(serviceName: string, action: string) {
        const res = await fetch(`${API_BASE}/service/action`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ service_name: serviceName, action }),
        });
        if (!res.ok) throw new Error(`Failed to ${action} service`);
    }

    async getTeamMembers(ownerID?: number): Promise<TeamMember[]> {
        const response = await this.checkedFetch('/team-members' + teamMemberOwnerQuery(ownerID), {
            cache: 'no-store',
        });
        const payload: unknown = await response.json();
        if (!isRecord(payload) || !Array.isArray(payload.team_members)) malformedTeamMemberResponse();
        return payload.team_members.map(parseTeamMember);
    }

    async createTeamMember(input: TeamMemberCreateInput, ownerID?: number): Promise<TeamMember> {
        const body: TeamMemberCreateInput & { owner_id?: number } = { ...input };
        if (ownerID !== undefined) {
            if (!Number.isInteger(ownerID) || ownerID <= 0) throw new Error('invalid_team_member_owner');
            body.owner_id = ownerID;
        }
        const response = await this.checkedFetch('/team-members', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        });
        const payload: unknown = await response.json();
        if (!isRecord(payload)) malformedTeamMemberResponse();
        return parseTeamMember(payload.team_member);
    }

    async updateTeamMember(
        memberID: number,
        input: TeamMemberUpdateInput,
        ownerID?: number,
    ): Promise<TeamMember> {
        if (!Number.isInteger(memberID) || memberID <= 0) throw new Error('invalid_team_member');
        const response = await this.checkedFetch(
            '/team-members/' + memberID + teamMemberOwnerQuery(ownerID),
            {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(input),
            },
        );
        const payload: unknown = await response.json();
        if (!isRecord(payload)) malformedTeamMemberResponse();
        return parseTeamMember(payload.team_member);
    }

    async deleteTeamMember(memberID: number, ownerID?: number): Promise<void> {
        if (!Number.isInteger(memberID) || memberID <= 0) throw new Error('invalid_team_member');
        await this.checkedFetch('/team-members/' + memberID + teamMemberOwnerQuery(ownerID), {
            method: 'DELETE',
        });
    }

    async getTeamMemberSubscriptionScopes(): Promise<TeamMemberSubscriptionScope[]> {
        const response = await this.checkedFetch('/subscriptions', { cache: 'no-store' });
        return parseTeamMemberSubscriptionScopes(await response.json());
    }

    async getTeamMemberDomainScopes(): Promise<TeamMemberDomainScope[]> {
        const response = await this.checkedFetch('/domains', { cache: 'no-store' });
        return parseTeamMemberDomainScopes(await response.json());
    }
}

export const api = new API();
