import type { TranslationKey } from '../i18n/en';
import type { ServerSetupExecution } from './serverSetupOperation';

export interface SetupGuidanceText { key: TranslationKey; values?: Record<string, string> }
export interface SetupExecutionGuidance {
    title: TranslationKey;
    messages: SetupGuidanceText[];
    details: SetupGuidanceText[];
}
const text = (key: TranslationKey, values?: Record<string, string>): SetupGuidanceText => ({ key, values });

// Present reviewed intent separately from observed results. A pending pair proof
// does not establish that the peer is offline, and a lost response is not failure.
// Incelenen amac ile gozlenen sonuc ayridir. Eksik es kaniti esin kapali oldugunu,
// kayip cevap ise islemin basarisiz oldugunu gostermez.
export function setupExecutionGuidance(execution: ServerSetupExecution): SetupExecutionGuidance | null {
    if (execution.status === 'succeeded') return null;
    const context = execution.context;
    const current = execution.steps.find(step => step.status === 'failed')
        || execution.steps.find(step => step.status === 'running');
    const phase = ['license', 'verification', 'dns_readiness', 'dns_publisher'].includes(execution.phase)
        ? execution.phase : current?.kind || execution.phase;
    const confirming = execution.status === 'running' && execution.error?.code.split(':')[0] === 'server_setup_reconciling';
    const result: SetupExecutionGuidance = {
        title: execution.status === 'failed' ? 'setup.guide.failedTitle' : confirming ? 'setup.guide.confirmTitle'
            : execution.status === 'waiting' ? 'setup.guide.waitTitle' : 'setup.guide.runningTitle',
        messages: [], details: [],
    };
    if (execution.status === 'failed' && execution.error?.code === 'server_setup_build_changed') {
        result.title = 'setup.guide.buildChangedTitle';
        result.messages.push(text('setup.guide.buildChanged'));
        return result;
    }
    if (phase === 'license') {
        result.title = 'setup.licenseWaiting';
        result.messages.push(text('setup.guide.license'));
        return result;
    }
    if (confirming) result.messages.push(text('setup.guide.confirm'));
    if (execution.status === 'failed') result.messages.push(text('setup.guide.failed'));

    if (['dns', 'dns_readiness'].includes(phase) && context?.dns_mode === 'local') {
        const values = { local: context.local_nameserver, localIP: context.local_ip, peer: context.peer_nameserver, peerIP: context.peer_ip };
        result.messages.push(text(context.dns_role === 'primary' ? 'setup.guide.primary' : 'setup.guide.secondary', values));
        result.messages.push(text(context.dns_role === 'primary' ? 'setup.guide.startSecondary' : 'setup.guide.startPrimary', values));
        if (phase === 'dns_readiness') result.messages.push(text('setup.guide.pairUnverified'));
        result.details.push(text('setup.guide.pairChecks'), text('setup.guide.nativeDNS'));
        if (context.dns_role === 'secondary' && ['manual', 'panel'].includes(context.dns_hosting_management)) result.details.push(text(context.dns_hosting_management === 'manual'
            ? 'setup.guide.manualRecords' : 'setup.guide.automaticRecords'));
    } else if (phase === 'dns_publisher') {
        result.messages.push(text('setup.guide.publisher'));
    } else if (phase === 'dns' && context?.dns_mode === 'existing') {
        result.messages.push(text('setup.guide.existingDNS'));
    } else if ((phase === 'dns' && context?.dns_mode === 'external') || ['panel_certificate', 'mail_certificate'].includes(phase)) {
        const domain = phase === 'mail_certificate' ? context?.mail_hostname : context?.panel_domain;
        if (domain) result.messages.push(text('setup.guide.certificateDNS', { domain }));
        result.details.push(text('setup.guide.certificateChecks'));
    } else if (['service', 'runtime', 'mail_profile'].includes(phase)) {
        result.messages.push(text('setup.guide.component'));
    } else if (phase === 'firewall') {
        result.messages.push(text('setup.guide.firewall'));
    } else if (phase === 'verification') {
        result.messages.push(text('setup.guide.verification'));
        const codes = new Set<string>();
        for (const check of execution.checks || []) {
            if (check.state === 'ready') continue;
            const key: TranslationKey = check.id === 'dns' ? 'setup.guide.pairChecks'
                : check.id === 'mail_identity' ? 'setup.guide.mailIdentity'
                    : check.id === 'mail_delivery' ? 'setup.guide.mailDelivery'
                        : ['panel_https', 'panel_renewal', 'mail_tls'].includes(check.id) ? 'setup.guide.certificateChecks'
                            : check.id === 'firewall' ? 'setup.guide.firewall' : 'setup.guide.checkUnknown';
            // External DNS does not require a local peer or a transfer policy.
            // Harici DNS yerel es veya aktarim ilkesi gerektirmez.
            const actual = check.id === 'dns' && context?.dns_mode === 'existing' ? 'setup.guide.existingDNS'
                : check.id === 'dns' && context?.dns_mode !== 'local' ? 'setup.guide.externalChecks' : key;
            if (!codes.has(actual)) result.details.push(text(actual));
            codes.add(actual);
        }
    }
    if (result.messages.length === 0) result.messages.push(text('setup.guide.unknown'));
    if (execution.status === 'running' || (execution.status === 'waiting' && ['dns_readiness', 'dns_publisher'].includes(phase))) {
        result.details.push(text('setup.guide.monitoring'));
    }
    return result;
}
