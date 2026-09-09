import type { Role } from '../auth/AuthContext';
import type { TranslationKey } from '../i18n/en';
import type { DashboardMailTruthProfile } from './dashboardMailTruth';

export function hasMailActivity(profiles: DashboardMailTruthProfile[]): boolean {
    return profiles.some((p) => p.latest_attempt_status !== 'none'
        || p.status === 'partial' || p.status === 'complete');
}

export function dnsStartReady(fresh: boolean, identity: boolean, running: boolean): boolean {
    return fresh && identity && running;
}

export function accountStart(role: Role): { title: TranslationKey; hint: TranslationKey; action: TranslationKey; to: string } | null {
    if (role === 'reseller') return { title: 'start.reseller.title', hint: 'start.reseller.hint', action: 'start.reseller.action', to: '/users' };
    if (role === 'customer') return { title: 'start.customer.title', hint: 'start.customer.hint', action: 'nav.domains', to: '/domains' };
    return null;
}

// A website's certificate never supplies evidence for the panel certificate.
export function panelCertificateReady(value: unknown, now = Date.now()): boolean | null {
    if (!value || typeof value !== 'object') return null;
    const c = value as Record<string, unknown>;
    if (typeof c.https_enabled !== 'boolean' || typeof c.self_signed !== 'boolean') return null;
    if (!c.https_enabled || c.self_signed) return false;
    if (typeof c.expires_at !== 'string' || !Number.isFinite(Date.parse(c.expires_at))) return null;
    return Date.parse(c.expires_at) > now;
}
