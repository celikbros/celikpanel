export const DOMAIN_CAPABILITIES = [
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

export type DomainCapability = (typeof DOMAIN_CAPABILITIES)[number];
export type DomainAccessMode = 'none' | 'view' | 'manage';
export type DomainAccess = Record<DomainCapability, DomainAccessMode>;

const MODES: readonly DomainAccessMode[] = ['none', 'view', 'manage'];

// Additional-user access arrives with every domain. A partial object, an
// unknown key or an unknown mode is rejected as a whole so a newer server
// contract cannot silently grant access in an older client.
export function normalizeDomainAccess(value: unknown): DomainAccess | null {
    if (!value || typeof value !== 'object' || Array.isArray(value)) return null;
    const record = value as Record<string, unknown>;
    const keys = Object.keys(record);
    if (keys.length !== DOMAIN_CAPABILITIES.length) return null;
    if (keys.some((key) => !DOMAIN_CAPABILITIES.includes(key as DomainCapability))) return null;

    const normalized = {} as DomainAccess;
    for (const capability of DOMAIN_CAPABILITIES) {
        const mode = record[capability];
        if (typeof mode !== 'string' || !MODES.includes(mode as DomainAccessMode)) return null;
        normalized[capability] = mode as DomainAccessMode;
    }
    return normalized;
}

export function hasDomainAccess(
    access: DomainAccess,
    capability: DomainCapability,
    minimum: Exclude<DomainAccessMode, 'none'> = 'view',
): boolean {
    const mode = access[capability];
    return minimum === 'manage' ? mode === 'manage' : mode === 'view' || mode === 'manage';
}

export function hasAnyDomainAccess(access: DomainAccess): boolean {
    return DOMAIN_CAPABILITIES.some((capability) => access[capability] !== 'none');
}

export function strongestDomainAccess(access: DomainAccess): DomainAccessMode {
    if (DOMAIN_CAPABILITIES.some((capability) => access[capability] === 'manage')) return 'manage';
    if (DOMAIN_CAPABILITIES.some((capability) => access[capability] === 'view')) return 'view';
    return 'none';
}
