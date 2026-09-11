import { setupPurposes, type ServerSetupCheck, type SetupPurpose } from './serverSetup';

export interface SetupPlanStep { id: string; kind: 'dns' | 'service' | 'runtime' | 'mail_profile' | 'firewall' | 'panel_certificate' | 'mail_certificate' | 'verify'; target: string; qualifier?: string }
export interface ServerSetupPlan {
    id: string; version: number; revision: number; purpose: SetupPurpose;
    steps: SetupPlanStep[]; blockers: string[]; can_start: boolean;
    tcp_ports: number[]; udp_ports: number[]; preserve_ssh: boolean;
    persist_firewall: boolean; hostname_change?: string; contact_email: string;
    components?: { id: string; selected: boolean; required: boolean; installed: boolean }[];
    remote_dns_connection?: { id: string; endpoint: string; nameservers: string[] };
}
export interface ServerSetupExecution {
    id: string; request_id: string; plan_id: string;
    status: 'running' | 'waiting' | 'failed' | 'succeeded'; phase: string;
    steps: (SetupPlanStep & { status: 'pending' | 'running' | 'failed' | 'succeeded' })[];
    error?: { code: string; message: string }; panel_url?: string; checks?: ServerSetupCheck[];
}
const kinds = ['dns', 'service', 'runtime', 'mail_profile', 'firewall', 'panel_certificate', 'mail_certificate', 'verify'];
const isRecord = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value);
const identity = (value: unknown): value is string => typeof value === 'string' && /^[a-f0-9]{32}$/.test(value);
function isStep(value: unknown): value is SetupPlanStep {
    return isRecord(value) && typeof value.id === 'string' && value.id !== ''
        && kinds.includes(String(value.kind)) && typeof value.target === 'string'
        && (value.qualifier === undefined || typeof value.qualifier === 'string');
}
export function decodeSetupPlan(value: unknown, revision: number): ServerSetupPlan | null {
    if (!isRecord(value) || !identity(value.id) || value.version !== 1 || value.revision !== revision
        || !setupPurposes.includes(value.purpose as SetupPurpose) || !Array.isArray(value.steps) || !value.steps.every(isStep)
        || !Array.isArray(value.blockers) || !value.blockers.every(code => typeof code === 'string')
        || typeof value.can_start !== 'boolean' || (value.can_start && value.blockers.length > 0)
        || !Array.isArray(value.tcp_ports) || !Array.isArray(value.udp_ports)
        || [...value.tcp_ports, ...value.udp_ports].some(port => !Number.isInteger(port) || port < 1 || port > 65535)
        || value.preserve_ssh !== true || value.persist_firewall !== true
        || typeof value.contact_email !== 'string'
        || (value.remote_dns_connection !== undefined && (!isRecord(value.remote_dns_connection) || !identity(value.remote_dns_connection.id) || typeof value.remote_dns_connection.endpoint !== 'string' || !Array.isArray(value.remote_dns_connection.nameservers) || value.remote_dns_connection.nameservers.some(name => typeof name !== 'string')))
        || (value.hostname_change !== undefined && typeof value.hostname_change !== 'string')) return null;
    if (value.components !== undefined && (!Array.isArray(value.components) || value.components.length > 80
        || value.components.some(item => !isRecord(item) || typeof item.id !== 'string' || typeof item.selected !== 'boolean' || typeof item.required !== 'boolean' || typeof item.installed !== 'boolean'))) return null;
    return value as unknown as ServerSetupPlan;
}
export function decodeSetupExecution(value: unknown, marker?: SetupStartMarker | null): ServerSetupExecution | null {
    if (!isRecord(value) || !identity(value.id) || !identity(value.request_id) || !identity(value.plan_id)
        || !['running', 'waiting', 'failed', 'succeeded'].includes(String(value.status))
        || typeof value.phase !== 'string' || !Array.isArray(value.steps)
        || !value.steps.every(step => isStep(step) && isRecord(step) && ['pending', 'running', 'failed', 'succeeded'].includes(String(step.status)))
        || (marker && (value.request_id !== marker.request_id || value.plan_id !== marker.plan_id))
        || (value.error !== undefined && (!isRecord(value.error) || typeof value.error.code !== 'string' || typeof value.error.message !== 'string'))
        || (value.checks !== undefined && (!Array.isArray(value.checks) || value.checks.some(check => !isRecord(check) || typeof check.id !== 'string' || !['ready', 'action_required', 'unknown'].includes(String(check.state)) || typeof check.code !== 'string')))
        || (value.panel_url !== undefined && typeof value.panel_url !== 'string')) return null;
    return value as unknown as ServerSetupExecution;
}
export interface SetupStartMarker { request_id: string; plan_id: string; panel_domain: string }
export function decodeSetupMarker(raw: string | null): SetupStartMarker | null {
    try {
        const value: unknown = JSON.parse(raw || 'null');
        return isRecord(value) && identity(value.request_id) && identity(value.plan_id) && typeof value.panel_domain === 'string'
            ? value as unknown as SetupStartMarker : null;
    } catch { return null; }
}
export function newSetupRequestID(): string {
    return Array.from(crypto.getRandomValues(new Uint8Array(16)), value => value.toString(16).padStart(2, '0')).join('');
}
export function safeSetupPanelURL(raw: string | undefined, hostname: string): string | null {
    if (!raw || !hostname) return null;
    try {
        const url = new URL(raw);
        return url.protocol === 'https:' && url.hostname === hostname.toLowerCase().replace(/\.$/, '') && !url.username && !url.password
            ? url.href : null;
    } catch { return null; }
}
