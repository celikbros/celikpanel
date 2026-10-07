import type { TranslationKey } from '../i18n/en';
import type { ServerSetupExecution } from './serverSetupOperation';

export interface SetupGuidanceText { key: TranslationKey; values?: Record<string, string> }
export interface SetupExecutionGuidance {
    title: TranslationKey;
    messages: SetupGuidanceText[];
    details: SetupGuidanceText[];
}
const text = (key: TranslationKey, values?: Record<string, string>): SetupGuidanceText => ({ key, values });

const componentNames: Record<string, string> = { nginx: 'Nginx', 'php-fpm': 'PHP-FPM', mariadb: 'MariaDB', postgresql: 'PostgreSQL', node: 'Node.js', nftables: 'nftables', certbot: 'Certbot', postfix: 'Postfix', dovecot: 'Dovecot', rspamd: 'Rspamd', roundcube: 'Roundcube', bind: 'BIND', pdns: 'PowerDNS', webmail: 'Webmail', 'core-mail': 'Core Mail', 'protected-mail': 'Spam-Protected Mail' };
export const setupComponentName = (id: string): string => componentNames[id] || id;

// Setup review blocker for an owner's directory above the hosting root that
// the web server or the site users cannot pass (native finding P3):
// "server_setup_hosting_root_not_traversable:<mode>:<owner>:<group>:<directory>".
// The directory is last so it is read whole. Anything else yields null.
// Barındırma kökünün üstünde, web sunucusunun ya da site kullanıcılarının
// geçemediği sahip dizini için kurulum inceleme engeli (yerel bulgu P3).
export function setupHostingRootBlockerValues(code: string): Record<string, string> | null {
    const parts = code.split(':');
    if (parts[0] !== 'server_setup_hosting_root_not_traversable' || parts.length < 5) return null;
    const [, mode, owner, group] = parts;
    const directory = parts.slice(4).join(':');
    if (!/^[0-7]{4}$/.test(mode) || !owner || !group || !directory.startsWith('/')) return null;
    return { directory, mode, owner: `${owner}:${group}`, command: `sudo chmod 755 ${directory}` };
}
// A setup step refused with HOST_MUTATION_BUSY carries the Panel's typed reason
// (internal/transport HostMutationReason*), and the reason selects the headline.
// A record written before the reason existed has only the Panel's sentence
// (cmd/panel/httperr.go hostMutationBusyMessages); its opening is the fallback.
// Neither known: the text that covers every reason.
// tests/server-setup-host-busy.test.mjs pins both to the Panel.
// HOST_MUTATION_BUSY ile reddedilen adim tipli nedenini tasir; basligi neden
// secer. Eski kayitta yalniz cumle vardir; cumlenin basi yedek secicidir.
const hostBusyReasons: Record<string, TranslationKey> = {
    package_manager_active: 'setup.blocker.packageBusy',
    agent_mutation_active: 'setup.blocker.changeBusy',
    panel_operation_active: 'setup.blocker.changeBusy',
    host_lock_busy: 'setup.blocker.hostHeld',
};
const hostBusyOpenings: [string, TranslationKey][] = [
    ["This server's package manager is busy", 'setup.blocker.packageBusy'],
    ['Another CelikPanel change is still running', 'setup.blocker.changeBusy'],
    ['Another CelikPanel operation is still running', 'setup.blocker.changeBusy'],
    ['A change that did not finish is still holding this server', 'setup.blocker.hostHeld'],
];
export const setupHostBusyKey = (message = '', reason = ''): TranslationKey =>
    (Object.prototype.hasOwnProperty.call(hostBusyReasons, reason) ? hostBusyReasons[reason] : undefined)
    || hostBusyOpenings.find(([opening]) => message.startsWith(opening))?.[1] || 'setup.blocker.hostBusy';

// A screen passes its localized names (mail profiles, cron) so the guidance
// sentence names the component exactly as the step list above it does.
// Ekran yerel adlari verir; yonlendirme bileseni adim listesiyle ayni adlandirir.
export type SetupComponentNamer = (id: string) => string;

const installSteps = ['preflight', 'package_install', 'configure', 'unit_start', 'verify'] as const;
type InstallStep = typeof installSteps[number];
const mailComponents = ['postfix', 'dovecot', 'rspamd', 'roundcube', 'webmail', 'core-mail', 'protected-mail'];

// An install failure names the component, what stopped, the host's own line,
// who acts and how setup continues, before the step list (D-024).
// Kurulum hatasi bileseni, neyin durdugunu, makinenin kendi satirini, kimin
// ne yapacagini ve kurulumun nasil surecegini adim listesinden once soyler.
function installFailureMessages(execution: ServerSetupExecution, componentName: SetupComponentNamer): SetupGuidanceText[] | null {
    const error = execution.error;
    if (execution.status !== 'failed' || !error?.component || !installSteps.includes(error.step as InstallStep)) return null;
    const step = error.step as InstallStep;
    const component = componentName(error.component);
    const messages = [text(`setup.guide.installFailed.${step}`, { component })];
    if (error.detail) messages.push(text('setup.guide.installFailedDetail', { detail: error.detail }));
    messages.push(text(step === 'package_install' ? 'setup.guide.installFailedAction.package'
        : step === 'unit_start' ? 'setup.guide.installFailedAction.service' : 'setup.guide.installFailedAction.other'));
    messages.push(text('setup.guide.installFailedResume'));
    const failedStep = execution.steps.find(item => item.status === 'failed');
    if (failedStep?.kind === 'mail_profile' || mailComponents.includes(error.component)) messages.push(text('setup.guide.installFailedWithoutMail'));
    return messages;
}

// Present reviewed intent separately from observed results. A pending pair proof
// does not establish that the peer is offline, and a lost response is not failure.
// Incelenen amac ile gozlenen sonuc ayridir. Eksik es kaniti esin kapali oldugunu,
// kayip cevap ise islemin basarisiz oldugunu gostermez.
export function setupExecutionGuidance(execution: ServerSetupExecution, componentName: SetupComponentNamer = setupComponentName): SetupExecutionGuidance | null {
    if (execution.status === 'succeeded') return null;
    const context = execution.context;
    const current = execution.steps.find(step => step.status === 'failed')
        || execution.steps.find(step => step.status === 'running');
    const phase = ['license', 'verification', 'dns_readiness', 'dns_publisher', 'primary_dns', 'infrastructure_dns', 'access_dns'].includes(execution.phase)
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
    const installFailure = ['service', 'runtime', 'mail_profile'].includes(phase) ? installFailureMessages(execution, componentName) : null;
    if (installFailure) {
        result.messages.push(...installFailure);
        return result;
    }
    if (confirming) result.messages.push(text('setup.guide.confirm'));
    if (execution.status === 'failed') result.messages.push(text('setup.guide.failed'));

    if (phase === 'access_dns') {
        const domain = current?.kind === 'access_dns' ? current.target : context?.panel_domain;
        const ip = current?.kind === 'access_dns' && current.qualifier ? current.qualifier : context?.access_dns_ip || context?.local_ip;
        if (domain && ip) result.messages.push(text('setup.guide.accessDNSRecord', { domain, ip }));
        if (execution.error?.code === 'server_setup_access_dns_mismatch') result.messages.push(text('setup.guide.accessDNSMismatch'));
        else if (execution.status === 'waiting') result.messages.push(text('setup.guide.accessDNSUnknown'));
        if (context?.dns_mode === 'local' && context.dns_role === 'secondary') {
            result.messages.push(text('setup.guide.accessDNSSecondary', { primary: context.peer_nameserver, primaryIP: context.peer_ip }));
            if (domain === context.panel_domain) result.details.push(text('setup.guide.accessDNSPeerPanel'));
        } else if (context?.infrastructure_dns) {
            result.messages.push(text('setup.guide.accessDNSPrepared', { zone: context.infrastructure_dns.zone, primary: context.local_nameserver, secondary: context.peer_nameserver }));
        } else result.messages.push(text('setup.guide.accessDNSProvider'));
        result.details.push(text(context?.dns_mode === 'local' ? 'setup.guide.accessDNSChecks' : 'setup.guide.accessDNSExternalChecks'));
        if (execution.status === 'waiting') result.messages.push(text('setup.guide.accessDNSResume'));
    } else if (phase === 'infrastructure_dns') {
        if (context?.infrastructure_dns) result.messages.push(text('setup.guide.infrastructureDNS', { zone: context.infrastructure_dns.zone }));
        if (execution.error?.code === 'server_setup_infrastructure_dns_unknown') result.messages.push(text('setup.infrastructure.unknown'));
        else if (execution.status === 'waiting') {
            result.messages.push(text('setup.guide.infrastructureDNSWaiting'));
            if (context?.peer_nameserver && context.peer_ip) result.messages.push(text('setup.guide.infrastructureDNSPeer', { peer: context.peer_nameserver, peerIP: context.peer_ip }));
        }
    } else if (phase === 'primary_dns' && context?.dns_mode === 'local') {
        result.messages.push(text('setup.guide.primaryDNSWaiting', { primary: context.peer_nameserver, primaryIP: context.peer_ip }));
        result.details.push(text('setup.guide.nativeDNS'));
        if (execution.status === 'waiting') result.messages.push(text('setup.guide.accessDNSResume'));
    } else if (['dns', 'dns_readiness'].includes(phase) && context?.dns_mode === 'local') {
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
    } else if (phase === 'mail_enrollment') {
        const code = execution.error?.code;
        const key = code === 'server_setup_mail_enrollment_not_recorded' ? 'setup.guide.mailEnrollmentUnrecorded'
            : code === 'server_setup_mail_enrollment_rollback' ? 'setup.guide.mailEnrollmentRollback'
            : code === 'server_setup_mail_enrollment_running' ? 'setup.guide.mailEnrollmentRecorded'
                : execution.status === 'failed' ? 'setup.guide.mailEnrollmentFailed' : 'setup.guide.mailEnrollmentUnknown';
        result.messages.push(text(key));
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
    if (execution.status === 'running' || (execution.status === 'waiting' && ['dns_readiness', 'dns_publisher', 'access_dns', 'primary_dns', 'infrastructure_dns'].includes(phase))) {
        result.details.push(text('setup.guide.monitoring'));
    }
    return result;
}
