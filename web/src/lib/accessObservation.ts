export type LicenseState = 'active' | 'missing' | 'expired' | 'invalid' | 'verification_unavailable' | 'status_unavailable';
export type AccessObservation = { allowed: boolean | null; until: number; state: LicenseState };

/** A negative boolean alone is not evidence of a missing or invalid license. */
export function parseAccessObservation(value: unknown, now = Date.now() / 1000): AccessObservation {
    if (!value || typeof value !== 'object') throw new Error('invalid access observation');
    const result = value as Record<string, unknown>;
    if (typeof result.can_use_panel !== 'boolean' || !Number.isSafeInteger(result.valid_until)) throw new Error('invalid access observation');
    const until = result.valid_until as number;
    if (result.can_use_panel) {
        if (until <= now || (result.state !== undefined && result.state !== 'active')
            || (result.observation !== undefined && result.observation !== 'known')) throw new Error('invalid positive access');
        return { allowed: true, until, state: 'active' };
    }
    if (result.observation === 'known' && ['missing', 'expired', 'invalid'].includes(String(result.state))) {
        return { allowed: false, until: 0, state: result.state as LicenseState };
    }
    return { allowed: null, until: 0, state: result.state === 'verification_unavailable' ? 'verification_unavailable' : 'status_unavailable' };
}
