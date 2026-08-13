import { useCallback, useEffect, useMemo, useState } from 'react';
import { AlertTriangle, CheckCircle2, HelpCircle, Loader2, RefreshCw, ShieldCheck, XCircle } from 'lucide-react';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import { Button } from './ui';

type AuditStatus = 'pass' | 'warning' | 'fail' | 'unknown';

interface AuditCheck {
    status: AuditStatus;
    code: string;
}

interface ListenerFinding {
    protocol: 'tcp' | 'udp';
    port: number;
    status: 'warning' | 'fail';
    code: 'listener_not_allowed' | 'allowed_no_listener';
}

interface SecurityAuditResponse {
    contract_version: 1;
    generated_at: string;
    agent: {
        contract_version: 1;
        capability: 'security_audit_v1';
        build_version: string;
        build_commit: string;
        generated_at: string;
        firewall: {
            engine: AuditCheck;
            default_drop: AuditCheck;
            persistence: AuditCheck;
            tcp_allowlist: number[];
            udp_allowlist: number[];
        };
        listeners: { check: AuditCheck; findings: ListenerFinding[] };
        ssh: {
            check: AuditCheck;
            password_authentication: string;
            keyboard_interactive_authentication: string;
            permit_root_login: string;
            pubkey_authentication: string;
            hostbased_authentication: string;
            gssapi_authentication: string;
        };
        reboot: { check: AuditCheck; required: boolean };
		signed_update: { check: AuditCheck; enrolled: boolean; sequence?: string; version?: string; key_fingerprint?: string };
    };
    tls: {
        certificate: AuditCheck;
        self_signed: AuditCheck;
        expiry: AuditCheck;
        key_match: AuditCheck;
        is_self_signed: boolean;
        expires_at?: string;
    };
}

const CODE_KEYS: Record<string, TranslationKey> = {
    platform_unsupported: 'securityAudit.code.platformUnsupported',
    firewall_engine_available: 'securityAudit.code.firewallEngineAvailable',
    firewall_engine_unavailable: 'securityAudit.code.firewallEngineUnavailable',
    firewall_state_unreadable: 'securityAudit.code.firewallStateUnreadable',
    firewall_disabled: 'securityAudit.code.firewallDisabled',
    firewall_policy_drop: 'securityAudit.code.firewallPolicyDrop',
    firewall_policy_not_drop: 'securityAudit.code.firewallPolicyNotDrop',
    firewall_policy_ambiguous: 'securityAudit.code.firewallPolicyAmbiguous',
    firewall_persistence_ready: 'securityAudit.code.firewallPersistenceReady',
    firewall_persistence_missing: 'securityAudit.code.firewallPersistenceMissing',
    firewall_persistence_stale: 'securityAudit.code.firewallPersistenceStale',
    firewall_persistence_invalid: 'securityAudit.code.firewallPersistenceInvalid',
    firewall_persistence_unverified: 'securityAudit.code.firewallPersistenceUnverified',
    listeners_match_allowlist: 'securityAudit.code.listenersMatchAllowlist',
    listener_not_allowed: 'securityAudit.code.listenerNotAllowed',
    allowed_no_listener: 'securityAudit.code.allowedNoListener',
    listener_state_unreadable: 'securityAudit.code.listenerStateUnreadable',
    listener_state_ambiguous: 'securityAudit.code.listenerStateAmbiguous',
    finding_limit_exceeded: 'securityAudit.code.findingLimitExceeded',
    ssh_key_only: 'securityAudit.code.sshKeyOnly',
    ssh_password_auth_enabled: 'securityAudit.code.sshPasswordAuthEnabled',
    ssh_non_key_auth_enabled: 'securityAudit.code.sshNonKeyAuthEnabled',
    ssh_root_login_unrestricted: 'securityAudit.code.sshRootLoginUnrestricted',
    ssh_policy_unreadable: 'securityAudit.code.sshPolicyUnreadable',
    ssh_policy_ambiguous: 'securityAudit.code.sshPolicyAmbiguous',
    ssh_policy_live_unverified: 'securityAudit.code.sshPolicyLiveUnverified',
    reboot_not_required: 'securityAudit.code.rebootNotRequired',
    reboot_required: 'securityAudit.code.rebootRequired',
    reboot_state_unknown: 'securityAudit.code.rebootStateUnknown',
	signed_update_identity_unverified: 'securityAudit.code.signedUpdateIdentityUnverified',
    signed_update_trust_not_enrolled: 'securityAudit.code.signedUpdateNotEnrolled',
    signed_update_trust_unsafe: 'securityAudit.code.signedUpdateUnsafe',
    signed_update_trust_unreadable: 'securityAudit.code.signedUpdateUnreadable',
    panel_tls_certificate_valid: 'securityAudit.code.tlsCertificateValid',
    panel_tls_not_managed: 'securityAudit.code.tlsNotManaged',
    panel_tls_incomplete: 'securityAudit.code.tlsIncomplete',
    panel_tls_unreadable: 'securityAudit.code.tlsUnreadable',
    panel_tls_invalid: 'securityAudit.code.tlsInvalid',
	panel_tls_metadata_unsafe: 'securityAudit.code.tlsMetadataUnsafe',
	panel_tls_live_unverified: 'securityAudit.code.tlsLiveUnverified',
	panel_tls_live_mismatch: 'securityAudit.code.tlsLiveMismatch',
    panel_tls_unknown: 'securityAudit.code.tlsUnknown',
    panel_tls_self_signed: 'securityAudit.code.tlsSelfSigned',
    panel_tls_not_self_signed: 'securityAudit.code.tlsNotSelfSigned',
	panel_tls_chain_unverified: 'securityAudit.code.tlsChainUnverified',
    panel_tls_valid: 'securityAudit.code.tlsValid',
    panel_tls_expiring: 'securityAudit.code.tlsExpiring',
    panel_tls_expired: 'securityAudit.code.tlsExpired',
    panel_tls_not_yet_valid: 'securityAudit.code.tlsNotYetValid',
    panel_tls_key_match: 'securityAudit.code.tlsKeyMatch',
    panel_tls_key_mismatch: 'securityAudit.code.tlsKeyMismatch',
};

const CODE_STATUSES: Record<string, AuditStatus> = {
	platform_unsupported: 'unknown', firewall_engine_available: 'pass', firewall_engine_unavailable: 'fail',
	firewall_state_unreadable: 'unknown', firewall_disabled: 'fail', firewall_policy_drop: 'pass',
	firewall_policy_not_drop: 'fail', firewall_policy_ambiguous: 'unknown', firewall_persistence_ready: 'pass',
	firewall_persistence_missing: 'fail', firewall_persistence_stale: 'fail', firewall_persistence_invalid: 'fail',
	firewall_persistence_unverified: 'unknown', listeners_match_allowlist: 'pass', listener_not_allowed: 'fail',
	allowed_no_listener: 'warning', listener_state_unreadable: 'unknown', listener_state_ambiguous: 'unknown',
	finding_limit_exceeded: 'unknown', ssh_key_only: 'pass', ssh_password_auth_enabled: 'fail',
	ssh_non_key_auth_enabled: 'fail', ssh_root_login_unrestricted: 'warning', ssh_policy_unreadable: 'unknown',
	ssh_policy_ambiguous: 'unknown', ssh_policy_live_unverified: 'unknown',
	reboot_not_required: 'pass', reboot_required: 'warning', reboot_state_unknown: 'unknown',
	signed_update_trust_not_enrolled: 'fail', signed_update_trust_unsafe: 'fail',
	signed_update_identity_unverified: 'warning',
	signed_update_trust_unreadable: 'unknown', panel_tls_certificate_valid: 'pass', panel_tls_not_managed: 'unknown',
	panel_tls_incomplete: 'fail', panel_tls_unreadable: 'unknown', panel_tls_invalid: 'fail',
	panel_tls_metadata_unsafe: 'fail', panel_tls_unknown: 'unknown', panel_tls_self_signed: 'warning',
	panel_tls_live_unverified: 'unknown', panel_tls_live_mismatch: 'fail',
	panel_tls_not_self_signed: 'pass', panel_tls_chain_unverified: 'unknown', panel_tls_valid: 'pass',
	panel_tls_expiring: 'warning', panel_tls_expired: 'fail', panel_tls_not_yet_valid: 'fail',
	panel_tls_key_match: 'pass', panel_tls_key_mismatch: 'fail',
};

const CHECK_CODES = {
    firewallEngine: new Set(['firewall_engine_available', 'firewall_engine_unavailable', 'firewall_state_unreadable', 'platform_unsupported']),
    firewallDefaultDrop: new Set(['firewall_policy_drop', 'firewall_policy_not_drop', 'firewall_policy_ambiguous', 'firewall_disabled', 'firewall_state_unreadable', 'platform_unsupported']),
    firewallPersistence: new Set(['firewall_persistence_missing', 'firewall_persistence_stale', 'firewall_persistence_invalid', 'firewall_persistence_unverified', 'platform_unsupported']),
    listeners: new Set(['listeners_match_allowlist', 'listener_not_allowed', 'allowed_no_listener', 'listener_state_unreadable', 'listener_state_ambiguous', 'finding_limit_exceeded', 'platform_unsupported']),
    ssh: new Set(['ssh_password_auth_enabled', 'ssh_non_key_auth_enabled', 'ssh_root_login_unrestricted', 'ssh_policy_unreadable', 'ssh_policy_ambiguous', 'ssh_policy_live_unverified', 'platform_unsupported']),
    reboot: new Set(['reboot_not_required', 'reboot_required', 'reboot_state_unknown', 'platform_unsupported']),
    signedUpdate: new Set(['signed_update_identity_unverified', 'signed_update_trust_not_enrolled', 'signed_update_trust_unsafe', 'signed_update_trust_unreadable', 'platform_unsupported']),
    tlsCertificate: new Set(['panel_tls_certificate_valid', 'panel_tls_not_managed', 'panel_tls_incomplete', 'panel_tls_unreadable', 'panel_tls_invalid', 'panel_tls_metadata_unsafe', 'panel_tls_live_unverified', 'panel_tls_live_mismatch']),
    tlsSelfSigned: new Set(['panel_tls_self_signed', 'panel_tls_chain_unverified', 'panel_tls_unknown']),
    tlsExpiry: new Set(['panel_tls_valid', 'panel_tls_expiring', 'panel_tls_expired', 'panel_tls_not_yet_valid', 'panel_tls_unknown']),
    tlsKeyMatch: new Set(['panel_tls_key_match', 'panel_tls_key_mismatch', 'panel_tls_unknown']),
} as const;

const VERSION_PATTERN = /^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-(?:[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$/;
const FINGERPRINT_PATTERN = /^sha256:[a-f0-9]{64}$/;

function isCheckFor(value: unknown, codes: ReadonlySet<string>): value is AuditCheck {
    return isCheck(value) && codes.has(value.code);
}

function isCanonicalTimestamp(value: unknown): value is string {
    if (typeof value !== 'string' || !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/.test(value)) return false;
    const parsed = new Date(value);
    return !Number.isNaN(parsed.getTime()) && parsed.toISOString().replace('.000Z', 'Z') === value;
}

function isSSHValue(value: unknown, root = false): value is string {
    if (root) return value === 'yes' || value === 'no' || value === 'prohibit-password' ||
        value === 'without-password' || value === 'forced-commands-only' || value === 'unknown';
    return value === 'yes' || value === 'no' || value === 'unknown';
}

function isCheck(value: unknown): value is AuditCheck {
    if (!value || typeof value !== 'object') return false;
    const check = value as Partial<AuditCheck>;
	return typeof check.code === 'string' && CODE_STATUSES[check.code] === check.status;
}

function isPortList(value: unknown): value is number[] {
    return Array.isArray(value) && value.length <= 4096 && value.every((port, index) =>
        Number.isInteger(port) && port >= 1 && port <= 65535 && (index === 0 || port > value[index - 1]));
}

function isListenerFinding(value: unknown): value is ListenerFinding {
    if (!value || typeof value !== 'object') return false;
    const finding = value as Partial<ListenerFinding>;
    const failure = finding.code === 'listener_not_allowed' && finding.status === 'fail';
    const warning = finding.code === 'allowed_no_listener' && finding.status === 'warning';
    return (finding.protocol === 'tcp' || finding.protocol === 'udp') &&
        Number.isInteger(finding.port) && Number(finding.port) >= 1 && Number(finding.port) <= 65535 &&
        (failure || warning);
}

function isSecurityAuditResponse(value: unknown): value is SecurityAuditResponse {
    if (!value || typeof value !== 'object') return false;
    const response = value as Partial<SecurityAuditResponse>;
    const agent = response.agent;
    const tls = response.tls;
    if (response.contract_version !== 1 || !agent || agent.contract_version !== 1 || agent.capability !== 'security_audit_v1' || !tls) return false;
    if (!isCheckFor(agent.firewall?.engine, CHECK_CODES.firewallEngine) ||
        !isCheckFor(agent.firewall?.default_drop, CHECK_CODES.firewallDefaultDrop) ||
        !isCheckFor(agent.firewall?.persistence, CHECK_CODES.firewallPersistence) ||
        !isCheckFor(agent.listeners?.check, CHECK_CODES.listeners) ||
        !isCheckFor(agent.ssh?.check, CHECK_CODES.ssh) ||
        !isCheckFor(agent.reboot?.check, CHECK_CODES.reboot) ||
        !isCheckFor(agent.signed_update?.check, CHECK_CODES.signedUpdate) ||
        !isCheckFor(tls.certificate, CHECK_CODES.tlsCertificate) ||
        !isCheckFor(tls.self_signed, CHECK_CODES.tlsSelfSigned) ||
        !isCheckFor(tls.expiry, CHECK_CODES.tlsExpiry) ||
        !isCheckFor(tls.key_match, CHECK_CODES.tlsKeyMatch) ||
        !isPortList(agent.firewall?.tcp_allowlist) || !isPortList(agent.firewall?.udp_allowlist) ||
        !Array.isArray(agent.listeners?.findings) || agent.listeners.findings.length > 512 ||
        !agent.listeners.findings.every(isListenerFinding)) return false;
    const endpoints = agent.listeners.findings.map((finding) => finding.protocol + ':' + String(finding.port).padStart(5, '0'));
    if (new Set(endpoints).size !== endpoints.length ||
        endpoints.some((endpoint, index) => index > 0 && endpoint <= endpoints[index - 1])) return false;
    if (!isCanonicalTimestamp(response.generated_at) || !isCanonicalTimestamp(agent.generated_at) ||
        response.generated_at !== agent.generated_at ||
        !VERSION_PATTERN.test(agent.build_version) || !/^[a-f0-9]{40}$/.test(agent.build_commit) ||
        !agent.ssh || !isSSHValue(agent.ssh.password_authentication) ||
        !isSSHValue(agent.ssh.keyboard_interactive_authentication) ||
        !isSSHValue(agent.ssh.permit_root_login, true) ||
        !isSSHValue(agent.ssh.pubkey_authentication) ||
        !isSSHValue(agent.ssh.hostbased_authentication) ||
        !isSSHValue(agent.ssh.gssapi_authentication) ||
        typeof agent.reboot?.required !== 'boolean' ||
        typeof agent.signed_update?.enrolled !== 'boolean' ||
        typeof tls.is_self_signed !== 'boolean') return false;
    if (agent.firewall.default_drop.status === 'pass' && agent.firewall.engine.status !== 'pass') return false;
    if (agent.firewall.default_drop.status !== 'pass' &&
        (agent.firewall.tcp_allowlist.length !== 0 || agent.firewall.udp_allowlist.length !== 0)) return false;
    if (agent.listeners.check.status !== 'unknown' && agent.firewall.default_drop.status !== 'pass') return false;
    const hasListenerFailure = agent.listeners.findings.some((finding) => finding.status === 'fail');
    const hasListenerWarning = agent.listeners.findings.some((finding) => finding.status === 'warning');
    if ((agent.listeners.check.status === 'pass' && agent.listeners.findings.length !== 0) ||
        (agent.listeners.check.status === 'fail' && !hasListenerFailure) ||
        (agent.listeners.check.status === 'warning' && (hasListenerFailure || !hasListenerWarning)) ||
        (agent.listeners.check.status === 'unknown' && agent.listeners.findings.length !== 0)) return false;
    const ssh = agent.ssh;
    if (ssh.check.code === 'ssh_password_auth_enabled' &&
        ssh.password_authentication !== 'yes' && ssh.keyboard_interactive_authentication !== 'yes') return false;
    if (ssh.check.code === 'ssh_non_key_auth_enabled' &&
        ssh.hostbased_authentication !== 'yes' && ssh.gssapi_authentication !== 'yes') return false;
    if (ssh.check.code === 'ssh_root_login_unrestricted' && (ssh.permit_root_login !== 'yes' ||
        ssh.password_authentication !== 'no' || ssh.keyboard_interactive_authentication !== 'no' ||
        ssh.pubkey_authentication !== 'yes' || ssh.hostbased_authentication !== 'no' ||
        ssh.gssapi_authentication !== 'no')) return false;
    if (ssh.check.code === 'ssh_policy_live_unverified') {
        const rootSafe = ssh.permit_root_login === 'no' || ssh.permit_root_login === 'prohibit-password' ||
            ssh.permit_root_login === 'without-password' || ssh.permit_root_login === 'forced-commands-only';
        if (ssh.password_authentication !== 'no' || ssh.keyboard_interactive_authentication !== 'no' ||
            ssh.pubkey_authentication !== 'yes' || ssh.hostbased_authentication !== 'no' ||
            ssh.gssapi_authentication !== 'no' || !rootSafe) return false;
    }
    if ((ssh.check.code === 'ssh_policy_unreadable' || ssh.check.code === 'platform_unsupported') &&
        [ssh.password_authentication, ssh.keyboard_interactive_authentication, ssh.permit_root_login,
            ssh.pubkey_authentication, ssh.hostbased_authentication, ssh.gssapi_authentication]
            .some((field) => field !== 'unknown')) return false;
    if (agent.signed_update.enrolled) {
        if (agent.signed_update.check.code !== 'signed_update_identity_unverified' ||
            !/^[1-9][0-9]*$/.test(agent.signed_update.sequence ?? '') ||
            agent.signed_update.version !== agent.build_version ||
            !FINGERPRINT_PATTERN.test(agent.signed_update.key_fingerprint ?? '')) return false;
    } else if (agent.signed_update.check.status === 'pass' || agent.signed_update.check.status === 'warning' ||
        agent.signed_update.sequence !== undefined || agent.signed_update.version !== undefined ||
        agent.signed_update.key_fingerprint !== undefined) return false;
    if (agent.reboot.check.status === 'pass' && agent.reboot.required) return false;
    if (agent.reboot.check.status === 'warning' && !agent.reboot.required) return false;
    if (agent.reboot.check.status === 'unknown' && agent.reboot.required) return false;
    if (tls.certificate.status !== 'pass') {
        if (tls.expires_at !== undefined || tls.is_self_signed || tls.self_signed.status !== 'unknown' ||
            tls.expiry.status !== 'unknown' || tls.key_match.status !== 'unknown') return false;
    } else {
        if (!isCanonicalTimestamp(tls.expires_at) || tls.expiry.status === 'unknown' || tls.key_match.status === 'unknown') return false;
        if (tls.is_self_signed) {
            if (tls.self_signed.code !== 'panel_tls_self_signed' || tls.self_signed.status !== 'warning') return false;
        } else if (tls.self_signed.code !== 'panel_tls_chain_unverified' || tls.self_signed.status !== 'unknown') return false;
    }
    return true;
}

function statusClasses(status: AuditStatus) {
    switch (status) {
        case 'pass': return 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300';
        case 'warning': return 'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300';
        case 'fail': return 'border-red-500/30 bg-red-500/10 text-red-700 dark:text-red-300';
        default: return 'border-border bg-surface-subtle text-fg-muted';
    }
}

function StatusIcon({ status }: { status: AuditStatus }) {
    if (status === 'pass') return <CheckCircle2 className="h-5 w-5 text-emerald-500" aria-hidden="true" />;
    if (status === 'warning') return <AlertTriangle className="h-5 w-5 text-amber-500" aria-hidden="true" />;
    if (status === 'fail') return <XCircle className="h-5 w-5 text-red-500" aria-hidden="true" />;
    return <HelpCircle className="h-5 w-5 text-fg-muted" aria-hidden="true" />;
}

export function SecurityAuditCard() {
    const { t, locale } = useI18n();
    const [audit, setAudit] = useState<SecurityAuditResponse | null>(null);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState('');

    const loadAudit = useCallback(async (signal?: AbortSignal) => {
        setLoading(true);
        setError('');
        try {
            const response = await fetch('/api/v1/security/audit', {
                method: 'GET', cache: 'no-store', credentials: 'same-origin', signal,
            });
            if (!response.ok) throw new Error(t('securityAudit.loadFailed'));
            const payload: unknown = await response.json();
            if (!isSecurityAuditResponse(payload)) throw new Error(t('securityAudit.invalidResponse'));
            setAudit(payload);
        } catch (loadError) {
            if (loadError instanceof DOMException && loadError.name === 'AbortError') return;
            setAudit(null);
            setError(loadError instanceof Error ? loadError.message : t('securityAudit.loadFailed'));
        } finally {
            if (!signal?.aborted) setLoading(false);
        }
    }, [t]);

    useEffect(() => {
        const controller = new AbortController();
        void loadAudit(controller.signal);
        return () => controller.abort();
    }, [loadAudit]);

    const rows = useMemo(() => audit ? [
        { key: 'firewallEngine', title: t('securityAudit.check.firewallEngine'), check: audit.agent.firewall.engine },
        { key: 'firewallDefaultDrop', title: t('securityAudit.check.firewallDefaultDrop'), check: audit.agent.firewall.default_drop },
        { key: 'firewallPersistence', title: t('securityAudit.check.firewallPersistence'), check: audit.agent.firewall.persistence },
        { key: 'listeners', title: t('securityAudit.check.listeners'), check: audit.agent.listeners.check },
        { key: 'ssh', title: t('securityAudit.check.ssh'), check: audit.agent.ssh.check },
        { key: 'reboot', title: t('securityAudit.check.reboot'), check: audit.agent.reboot.check },
        { key: 'signedUpdate', title: t('securityAudit.check.signedUpdate'), check: audit.agent.signed_update.check },
        { key: 'tlsCertificate', title: t('securityAudit.check.tlsCertificate'), check: audit.tls.certificate },
        { key: 'tlsSelfSigned', title: t('securityAudit.check.tlsSelfSigned'), check: audit.tls.self_signed },
        { key: 'tlsExpiry', title: t('securityAudit.check.tlsExpiry'), check: audit.tls.expiry },
        { key: 'tlsKeyMatch', title: t('securityAudit.check.tlsKeyMatch'), check: audit.tls.key_match },
    ] : [], [audit, t]);

    const counts = useMemo(() => rows.reduce<Record<AuditStatus, number>>((result, row) => {
        result[row.check.status] += 1;
        return result;
    }, { pass: 0, warning: 0, fail: 0, unknown: 0 }), [rows]);

    const reason = (code: string) => t(CODE_KEYS[code] ?? 'securityAudit.code.unknown');

    return (
        <section className="rounded-xl border border-border bg-surface p-5 shadow-card" aria-labelledby="security-audit-title">
            <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
                <div className="flex min-w-0 gap-3">
                    <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
                        <ShieldCheck className="h-5 w-5" aria-hidden="true" />
                    </span>
                    <div>
                        <h3 id="security-audit-title" className="font-semibold text-fg">{t('securityAudit.title')}</h3>
                        <p className="mt-1 text-sm text-fg-muted">{t('securityAudit.description')}</p>
                    </div>
                </div>
                <Button type="button" onClick={() => void loadAudit()} disabled={loading}>
                    {loading ? <Loader2 className="h-4 w-4 animate-spin" aria-hidden="true" /> : <RefreshCw className="h-4 w-4" aria-hidden="true" />}
                    {loading ? t('securityAudit.checking') : t('securityAudit.refresh')}
                </Button>
            </div>

            <div className="mt-4 rounded-lg border border-border bg-surface-subtle p-3 text-sm text-fg-muted" role="note">
                {t('securityAudit.readOnly')}
            </div>

            {error && <div className="mt-4 rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-700 dark:text-red-300" role="alert">{error}</div>}

            {audit && (
                <>
                    <div className={`mt-4 rounded-lg border p-3 text-sm ${counts.fail === 0 && counts.warning === 0 && counts.unknown === 0 ? statusClasses('pass') : statusClasses(counts.fail > 0 ? 'fail' : 'warning')}`} role="status" aria-live="polite">
                        {counts.fail === 0 && counts.warning === 0 && counts.unknown === 0
                            ? t('securityAudit.summaryHealthy')
                            : t('securityAudit.summaryIssues', { fail: counts.fail, warning: counts.warning, unknown: counts.unknown })}
                    </div>

                    <div className="mt-4 grid gap-3 lg:grid-cols-2">
                        {rows.map((row) => (
                            <div key={row.key} className="flex items-start gap-3 rounded-lg border border-border p-3">
                                <StatusIcon status={row.check.status} />
                                <div className="min-w-0">
                                    <div className="flex flex-wrap items-center gap-2">
                                        <h4 className="text-sm font-semibold text-fg">{row.title}</h4>
                                        <span className={`rounded-full border px-2 py-0.5 text-xs font-medium ${statusClasses(row.check.status)}`}>
                                            {t(`securityAudit.status.${row.check.status}` as TranslationKey)}
                                        </span>
                                    </div>
                                    <p className="mt-1 text-xs leading-5 text-fg-muted">{reason(row.check.code)}</p>
                                </div>
                            </div>
                        ))}
                    </div>

                    {audit.agent.listeners.findings.length > 0 && (
                        <div className="mt-4 rounded-lg border border-border p-3">
                            <h4 className="text-sm font-semibold text-fg">{t('securityAudit.listenerFindings')}</h4>
                            <ul className="mt-2 space-y-1 text-sm text-fg-muted">
                                {audit.agent.listeners.findings.map((finding) => (
                                    <li key={`${finding.protocol}:${finding.port}:${finding.code}`}>
                                        {t('securityAudit.listenerFinding', {
                                            protocol: finding.protocol.toUpperCase(), port: finding.port, detail: reason(finding.code),
                                        })}
                                    </li>
                                ))}
                            </ul>
                        </div>
                    )}

                    <dl className="mt-4 grid gap-2 rounded-lg border border-border bg-surface-subtle p-3 text-xs sm:grid-cols-2">
                        <div><dt className="text-fg-muted">{t('securityAudit.tcpAllowlist')}</dt><dd className="mt-0.5 font-mono text-fg">{audit.agent.firewall.tcp_allowlist.join(', ') || t('securityAudit.none')}</dd></div>
                        <div><dt className="text-fg-muted">{t('securityAudit.udpAllowlist')}</dt><dd className="mt-0.5 font-mono text-fg">{audit.agent.firewall.udp_allowlist.join(', ') || t('securityAudit.none')}</dd></div>
                        <div><dt className="text-fg-muted">{t('securityAudit.generatedAt')}</dt><dd className="mt-0.5 text-fg">{new Date(audit.generated_at).toLocaleString(locale === 'tr' ? 'tr-TR' : 'en-US')}</dd></div>
                        <div><dt className="text-fg-muted">{t('securityAudit.build')}</dt><dd className="mt-0.5 break-all font-mono text-fg">{audit.agent.build_version} / {audit.agent.build_commit.slice(0, 12)}</dd></div>
						{audit.agent.signed_update.key_fingerprint && <div className="sm:col-span-2"><dt className="text-fg-muted">{t('securityAudit.keyFingerprint')}</dt><dd className="mt-0.5 break-all font-mono text-fg">{audit.agent.signed_update.key_fingerprint}</dd></div>}
                    </dl>
                </>
            )}
        </section>
    );
}
