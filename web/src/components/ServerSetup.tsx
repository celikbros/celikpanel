import { useCallback, useEffect, useRef, useState, type FormEvent, type ReactNode } from 'react';
import { ArrowRight, Check, Circle, Loader2 } from 'lucide-react';
import { useAuth } from '../auth/AuthContext';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import { Link, Navigate } from '../router';
import { decodeSetupEditorCheckpoint, changeSetupDNSRole, setupDNSNames, setupDetectedIPv4, chooseSetupPurpose, decodeServerSetup, setupNextPath, setupPurposes, type ServerSetupDraft, type ServerSetupSnapshot } from '../lib/serverSetup';
import { decodeSetupExecution, decodeSetupMarker, decodeSetupPlan, newSetupRequestID, safeSetupPanelURL, type ServerSetupExecution, type ServerSetupPlan, type SetupStartMarker } from '../lib/serverSetupOperation';
import { ServerSetupShell, useServerSetup } from './ServerSetupGate';
import { ServerSetupDNSConnection } from './ServerSetupDNSConnections';
import { remoteDNSEndpoint } from '../lib/remoteDNS';
import { ServerSetupChoice, ServerSetupManualAction } from './ServerSetupChoice';
import { Button, inputClass, Spinner } from './ui';
import { ServerSetupComponents, useSetupComponentCatalog } from './ServerSetupComponents';
import { setupEffectiveComponents, setupPresetComponents } from '../lib/serverSetupComponents';

type Step = 'purpose' | 'components' | 'access' | 'review' | 'progress';
const requestOptions = (body: unknown) => ({ method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
async function setupFetch(url: string, options?: RequestInit) {
    const controller = new AbortController();
    const timeout = window.setTimeout(() => controller.abort(), 20000);
    try { return await fetch(url, { ...options, signal: controller.signal }); }
    finally { window.clearTimeout(timeout); }
}
const editorKey = (username: string) => `celikpanel.setup.editor.${username}`;
const markerKey = (username: string) => `celikpanel.setup.start.${username}`;
const codeKey: Record<string, TranslationKey> = {
    license_required: 'license.restricted',
    server_setup_reconciling: 'setup.confirmingPrevious',
    REMOTE_DNS_UNAVAILABLE: 'setup.blocker.remote',
    REMOTE_DNS_AUTHORITY_NOT_READY: 'setup.remote.authorityNotReady',
    dns_peer_ipv6_unverified: 'setup.blocker.peerIPv6',
    mail_identity_required: 'setup.blocker.mailIdentity',
    mail_identity_unavailable: 'setup.blocker.mailIdentity',
    mail_delivery_required: 'setup.blocker.mailDelivery',
    mail_delivery_unavailable: 'setup.blocker.mailDelivery',
    server_setup_mail_certificate_failed: 'setup.blocker.mailDNS',
    server_setup_dns_identity_required: 'setup.blocker.dnsIdentity',
    server_setup_hosting_requires_dns_publisher: 'setup.publisher.endpointRequired',
    server_setup_dns_publisher_endpoint_required: 'setup.publisher.endpointRequired',
    server_setup_dns_publisher_required: 'setup.publisher.waitingHelp',
    server_setup_dns_readiness_required: 'setup.publisher.pairWaiting',
    server_setup_dns_engine_unsupported: 'setup.blocker.profile',
    server_setup_existing_dns_requires_migration: 'setup.blocker.migration',
    server_setup_dns_failed: 'setup.blocker.dnsIdentity',
    server_setup_panel_certificate_failed: 'setup.blocker.panelTLS',
    server_setup_firewall_failed: 'setup.blocker.firewall',
    server_setup_step_failed: 'setup.blocker.services',
    firewall_no_ssh_service: 'setup.blocker.ssh',
    firewall_ssh_not_listening: 'setup.blocker.ssh',
    firewall_ssh_unprovable: 'setup.blocker.ssh',
    firewall_no_engine: 'setup.blocker.firewall',
    server_setup_remote_dns_required: 'setup.blocker.remote',
    remote_dns_not_ready: 'setup.blocker.remote',
    server_setup_panel_domain_invalid: 'setup.blocker.panelDomain',
    panel_certificate_contact_email_invalid: 'setup.blocker.contact',
    server_setup_panel_certificate_unmanaged: 'setup.blocker.panelTLS',
    server_setup_panel_renewal_unavailable: 'setup.blocker.renewal',
    server_setup_dns_mode_unsupported: 'setup.blocker.existing',
    server_setup_dns_purpose_requires_local: 'setup.blocker.dnsIdentity',
    server_setup_service_unknown: 'setup.blocker.services',
    server_setup_inventory_unavailable: 'setup.components.inventoryUnknown',
    server_setup_components_required: 'setup.components.chooseRequired',
    server_setup_customization_invalid: 'setup.components.savedUnavailable',
    server_setup_service_unsupported: 'setup.blocker.profile',
    server_setup_dependency_missing: 'setup.blocker.services',
    server_setup_service_conflict: 'setup.blocker.services',
    server_setup_node_version_required: 'setup.blocker.node',
    server_setup_database_invalid: 'setup.blocker.database',
    server_setup_purpose_invalid: 'setup.blocker.profile',
    server_setup_mail_hostname_invalid: 'setup.blocker.mailHostname',
    panel_https_required: 'setup.blocker.panelTLS',
    panel_https_unavailable: 'setup.blocker.panelTLS',
    panel_renewal_required: 'setup.blocker.renewal',
    panel_renewal_unavailable: 'setup.blocker.renewal',
    firewall_required: 'setup.blocker.firewall',
    firewall_unavailable: 'setup.blocker.firewall',
    dns_required: 'setup.blocker.dnsIdentity',
    dns_unavailable: 'setup.blocker.dnsIdentity',
    services_required: 'setup.blocker.services',
    services_unavailable: 'setup.blocker.services',
    mail_tls_required: 'setup.blocker.mailDNS',
    mail_tls_unavailable: 'setup.blocker.mailDNS',
    setup_existing_dns_not_supported: 'setup.blocker.existing',
    existing_dns_not_supported: 'setup.blocker.existing',
    existing_dns_unavailable: 'setup.blocker.existing',
    panel_domain_required: 'setup.blocker.panelDomain',
    panel_dns_not_ready: 'setup.blocker.panelDNS',
    panel_certificate_untrusted: 'setup.blocker.panelTLS',
    panel_certificate_not_ready: 'setup.blocker.panelTLS',
    panel_renewal_not_ready: 'setup.blocker.renewal',
    firewall_not_ready: 'setup.blocker.firewall',
    dns_identity_required: 'setup.blocker.dnsIdentity',
    dns_redundancy_required: 'setup.blocker.redundancy',
    mail_hostname_required: 'setup.blocker.mailHostname',
    mail_dns_not_ready: 'setup.blocker.mailDNS',
    services_not_ready: 'setup.blocker.services',
    profile_not_supported: 'setup.blocker.profile',
    operation_running: 'setup.blocker.operation',
};

export function ServerSetup() {
    const setup = useServerSetup();
    const [chosen, setChosen] = useState<ServerSetupSnapshot | null>(null);
    const [manual, setManual] = useState(false);
    if (!setup?.snapshot) return <ServerSetupShell><Spinner /></ServerSetupShell>;
    if (manual) return <Navigate to="/" replace />;
    const snapshot = chosen || setup.snapshot;
    if (['undecided', 'manual'].includes(snapshot.guidance || '') && !['running', 'waiting', 'ready'].includes(snapshot.status)) {
        return <ServerSetupShell><ServerSetupChoice snapshot={snapshot} onChosen={next => {
            setup.accept(next); setChosen(next); setManual(next.guidance === 'manual');
        }} /></ServerSetupShell>;
    }
    return <SetupWizard initial={snapshot} />;
}

function SetupWizard({ initial }: { initial: ServerSetupSnapshot }) {
    const setup = useServerSetup()!;
    const { user } = useAuth();
    const { t } = useI18n();
    const { catalog, failed: catalogFailed, reload: reloadCatalog } = useSetupComponentCatalog(!['running', 'waiting', 'ready'].includes(initial.status));
    const [snapshot, setSnapshot] = useState(initial);
    const [restoredEditor] = useState(() => {
        try { return decodeSetupEditorCheckpoint(sessionStorage.getItem(editorKey(user.username)), initial); }
        catch { return null; }
    });
    const [draft, setDraft] = useState(restoredEditor?.draft || initial.draft);
    const restoreReview = useRef(restoredEditor?.step === 'review');
    const restoredSnapshot = useRef(initial);
    const localIPTouched = useRef(false);
    const detectedIP = setupDetectedIPv4(snapshot.server_ip);
    useEffect(() => {
        if (!detectedIP || localIPTouched.current || !['new', 'legacy', 'draft'].includes(snapshot.status)) return;
        setDraft(previous => previous.local_ip ? previous : { ...previous, local_ip: detectedIP });
    }, [detectedIP, snapshot.status]);
    const dnsNames = setupDNSNames(draft);
    const [step, setStep] = useState<Step>(restoredEditor?.step || (initial.status === 'new' || initial.status === 'legacy' ? 'purpose' : initial.draft.customization ? 'components' : 'access'));
    const [plan, setPlan] = useState<ServerSetupPlan | null>(null);
    const [execution, setExecution] = useState<ServerSetupExecution | null>(null);
    const [marker, setMarker] = useState<SetupStartMarker | null>(() => {
        try { return decodeSetupMarker(localStorage.getItem(markerKey(user.username))); } catch { return null; }
    });
    const [resolving, setResolving] = useState(true);
    const [busy, setBusy] = useState(false);
    const [acknowledged, setAcknowledged] = useState(false);
    const [remoteVerified, setRemoteVerified] = useState(false);
    const [error, setError] = useState('');
    const [reconnecting, setReconnecting] = useState(false);
    const [completionFailed, setCompletionFailed] = useState(false);
    const [manualExit, setManualExit] = useState(false);
    const pendingRef = useRef(false);
    const pollingRef = useRef(false);
    const pollEpoch = useRef(0);
    const alive = useRef(true);
    const heading = useRef<HTMLHeadingElement>(null);
    const markerRef = useRef(marker);
    markerRef.current = marker;
    const accept = useCallback((value: ServerSetupSnapshot) => { setSnapshot(value); setup.accept(value); }, [setup.accept]);
    const failureText = (code: string) => t(codeKey[code.split(':')[0]] || 'setup.blocker.unknown');
    const stepTarget = (kind: string, target: string) => {
        if (kind === 'dns' && ['local', 'external', 'existing'].includes(target)) return t(`setup.dns.${target}` as TranslationKey);
        if (kind === 'mail_profile') return t(target === 'protected-mail' ? 'dashboard.audit.profile.protectedMail' : target === 'core-mail' ? 'dashboard.audit.profile.coreMail' : 'dashboard.audit.profile.webmail');
        const names: Record<string, string> = { nginx: 'Nginx', 'php-fpm': 'PHP-FPM', mariadb: 'MariaDB', postgresql: 'PostgreSQL', node: 'Node.js', nftables: 'nftables', certbot: 'Certbot', postfix: 'Postfix', dovecot: 'Dovecot', rspamd: 'Rspamd', roundcube: 'Roundcube', bind: 'BIND', pdns: 'PowerDNS' };
        return names[target] || target;
    };

    useEffect(() => {
        heading.current?.focus();
    }, [step]);
    useEffect(() => {
        try {
            if (step === 'progress' || manualExit || snapshot.status === 'ready') {
                sessionStorage.removeItem(editorKey(user.username));
            } else if (['new', 'legacy', 'draft'].includes(snapshot.status)) {
                sessionStorage.setItem(editorKey(user.username), JSON.stringify({
                    version: 1, revision: snapshot.revision, step, draft,
                }));
            }
        } catch { /* Browser storage may be unavailable; normal setup still works. */ }
    }, [step, draft, snapshot.revision, snapshot.status, manualExit, user.username]);
    useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);

    async function readExecution() {
        const current = markerRef.current;
        const response = await setupFetch(`/api/v1/setup/operation${current ? `?request_id=${encodeURIComponent(current.request_id)}` : ''}`, { cache: 'no-store' });
        if (!response.ok) throw new Error('operation unavailable');
        const value: unknown = await response.json();
        if (value === null) {
            if (current) throw new Error('unconfirmed start');
            return null;
        }
        const next = decodeSetupExecution(value, current);
        if (!next) throw new Error('operation identity');
        return next;
    }
    const reconcile = useCallback(async () => {
        if (pollingRef.current) return;
        pollingRef.current = true;
        const epoch = pollEpoch.current;
        try {
            const next = await readExecution();
            if (!alive.current || epoch !== pollEpoch.current) return;
            // Rebuild the review after a remount/reload, only after ruling out
            // an accepted execution. Never restore the confirmation checkbox.
            // Kabul edilmis islem yoksa incelemeyi yeniden kur; baslatma
            // onayini geri yukleme.
            if (!next && restoreReview.current) {
                restoreReview.current = false;
                try {
                    const response = await setupFetch('/api/v1/setup/plan', requestOptions({ revision: restoredSnapshot.current.revision }));
                    const reviewed = response.ok ? decodeSetupPlan(await response.json(), restoredSnapshot.current.revision) : null;
                    if (!reviewed || (restoredSnapshot.current.draft.dns_mode === 'existing' && reviewed.can_start
                        && reviewed.remote_dns_connection?.id !== restoredSnapshot.current.draft.remote_dns_connection_id)) throw new Error('review unavailable');
                    if (!alive.current || epoch !== pollEpoch.current) return;
                    setPlan(reviewed); setAcknowledged(false);
                } catch {
                    if (!alive.current || epoch !== pollEpoch.current) return;
                    setStep('access'); setError(t('setup.planFailed'));
                }
            }
            setExecution(next); setResolving(false); setReconnecting(false);
            if (next) {
                setStep('progress');
                if (next.status === 'succeeded') {
                    try {
                        const state = await setup.reload(true);
                        if (!alive.current) return;
                        accept(state);
                        setCompletionFailed(state.status !== 'ready');
                        if (state.status === 'ready') localStorage.removeItem(markerKey(user.username));
                    } catch { if (alive.current) setCompletionFailed(true); }
                }
            }
        } catch {
            if (alive.current && epoch === pollEpoch.current) { setResolving(false); setReconnecting(true); }
        } finally { pollingRef.current = false; }
    }, [setup.reload, accept, user.username]);
    useEffect(() => {
        void reconcile();
        void setup.reload(true).then(value => { if (alive.current) setSnapshot(value); }).catch(() => {});
    }, [reconcile, setup.reload]);
    useEffect(() => {
        if (snapshot.status === 'ready' || (!reconnecting && !marker && !execution)) return;
        if (execution?.status === 'failed' && !reconnecting) return;
        const timer = window.setInterval(() => void reconcile(), 3000);
        return () => window.clearInterval(timer);
    }, [execution, marker, reconnecting, resolving, reconcile, snapshot.status]);

    function change<K extends keyof ServerSetupDraft>(key: K, value: ServerSetupDraft[K]) {
        if (key === 'local_ip') localIPTouched.current = true;
        if (key === 'dns_mode' || key === 'remote_dns_connection_id') setRemoteVerified(false);
        setDraft(previous => ({ ...previous, [key]: value })); setPlan(null); setAcknowledged(false); setError('');
    }
    async function saveDraft(): Promise<ServerSetupSnapshot> {
        const response = await setupFetch('/api/v1/setup', { ...requestOptions({ revision: snapshot.revision, draft: { ...draft, ...(secondaryHosting ? { dns_hosting_management: hostingDNSManagement, ...(hostingDNSManagement === 'manual' ? { dns_publisher_endpoint: '' } : {}) } : {}) } }), method: 'PUT' });
        if (response.status === 409) throw new Error(t('setup.conflict'));
        if (!response.ok) throw new Error(t('setup.saveFailed'));
        const next = decodeServerSetup(await response.json());
        if (!next) throw new Error(t('setup.loadFailed'));
        if (alive.current) { accept(next); setDraft(next.draft); }
        return next;
    }
    async function next(event: FormEvent) {
        event.preventDefault();
        if (step === 'components' && (!catalog || catalog.inventory_state !== 'ready')) { setError(t('setup.components.inventoryUnknown')); return; }
        if (step === 'components' && emptySelectionInvalid) { setError(t('setup.components.chooseRequired')); return; }
        if (step === 'access' && draft.dns_mode === 'local' && dnsNames.mismatch) { setError(t('setup.dnsMappingMismatch')); return; }
        if (step === 'access' && dnsSelectionError) { setError(t(dnsSelectionError)); return; }
        if (step === 'access' && draft.dns_mode === 'existing' && !remoteVerified) { setError(t('setup.remote.proof.failed')); return; }
        if (pendingRef.current) return;
        pendingRef.current = true; setBusy(true); setError('');
        try {
            const saved = await saveDraft();
            if (!alive.current) return;
            if (step === 'purpose') { setStep(saved.draft.customization || saved.draft.purpose === 'custom' ? 'components' : 'access'); return; }
            if (step === 'components') { setStep('access'); return; }
            const response = await setupFetch('/api/v1/setup/plan', requestOptions({ revision: saved.revision }));
            if (!response.ok) throw new Error(t('setup.planFailed'));
            const reviewed = decodeSetupPlan(await response.json(), saved.revision);
            if (!reviewed || (saved.draft.dns_mode === 'existing' && reviewed.can_start && reviewed.remote_dns_connection?.id !== saved.draft.remote_dns_connection_id)) throw new Error(t('setup.planFailed'));
            if (alive.current) { setPlan(reviewed); setAcknowledged(false); setStep('review'); }
        } catch (failure) { if (alive.current) setError(failure instanceof Error ? failure.message : t('setup.saveFailed')); }
        finally { pendingRef.current = false; if (alive.current) setBusy(false); }
    }
    async function start() {
        if (!plan?.can_start || !acknowledged || pendingRef.current) return;
        pendingRef.current = true; setBusy(true); setError('');
        let started: SetupStartMarker;
        try {
            started = { request_id: newSetupRequestID(), plan_id: plan.id, panel_domain: draft.panel_domain };
            localStorage.setItem(markerKey(user.username), JSON.stringify(started));
        } catch { setError(t('setup.storageFailed')); pendingRef.current = false; setBusy(false); return; }
        markerRef.current = started; setMarker(started); setStep('progress');
        try {
            const response = await setupFetch('/api/v1/setup/start', requestOptions({ plan_id: started.plan_id, request_id: started.request_id, confirmed: true }));
            if (!response.ok) {
                const problem = await response.clone().json().catch(() => null);
                if (response.status === 409 && ['server_setup_review_required', 'server_setup_review_stale', 'server_setup_plan_blocked'].includes(problem?.code)) {
                    // These codes are emitted only before an execution is created.
                    // The server checks a previously committed request first.
                    localStorage.removeItem(markerKey(user.username));
                    markerRef.current = null; setMarker(null); setPlan(null); setAcknowledged(false);
                    setStep('access'); setError(t('setup.conflict')); return;
                }
                await reconcile(); return;
            }
            const accepted = decodeSetupExecution(await response.json(), started);
            if (!accepted) { await reconcile(); return; }
            if (alive.current) { setExecution(accepted); setReconnecting(false); }
        } catch { await reconcile(); }
        finally { pendingRef.current = false; if (alive.current) setBusy(false); }
    }
    async function verifyManual() {
        if (pendingRef.current) return;
        pendingRef.current = true; setBusy(true); setError('');
        try {
            const latest = await setup.reload(true); accept(latest);
            await reconcile();
        } catch { if (alive.current) setError(t('setup.verificationFailed')); }
        finally { pendingRef.current = false; if (alive.current) setBusy(false); }
    }
    async function resumeUnconfirmed() {
        const saved = markerRef.current;
        if (!saved || execution || pendingRef.current) { await reconcile(); return; }
        pendingRef.current = true; setBusy(true);
        try {
            // Reuse the already confirmed plan and request. A lost response is
            // never permission to create another installation identity.
            const response = await setupFetch('/api/v1/setup/start', requestOptions({ plan_id: saved.plan_id, request_id: saved.request_id, confirmed: true }));
            if (response.ok) {
                const resumed = decodeSetupExecution(await response.json(), saved);
                if (resumed && alive.current) { setExecution(resumed); setReconnecting(false); }
            }
            await reconcile();
        } catch { if (alive.current) setReconnecting(true); }
        finally { pendingRef.current = false; if (alive.current) setBusy(false); }
    }
    function editPlan() {
        if (execution && execution.status !== 'failed') return;
        resetForReview();
    }
    function resetForReview() {
        pollEpoch.current++;
        try { localStorage.removeItem(markerKey(user.username)); } catch { /* next start still requires storage */ }
        markerRef.current = null; setMarker(null); setExecution(null); setPlan(null); setAcknowledged(false); setStep('access');
    }
    async function reviseWaiting() {
        if (execution?.status !== 'waiting' || !['verification', 'dns_publisher', 'dns_readiness'].includes(execution.phase) || pendingRef.current) return;
        pendingRef.current = true; setBusy(true); setError('');
        try {
            const latest = await setup.reload();
            const response = await setupFetch('/api/v1/setup/revise', requestOptions({ revision: latest.revision, execution_id: execution.id }));
            if (!response.ok) throw new Error(t('setup.reviseBlocked'));
            const revised = decodeServerSetup(await response.json());
            if (!revised || revised.status !== 'draft') throw new Error(t('setup.reviseBlocked'));
            if (alive.current) { accept(revised); setDraft(revised.draft); resetForReview(); }
        } catch (failure) { if (alive.current) setError(failure instanceof Error ? failure.message : t('setup.reviseBlocked')); }
        finally { pendingRef.current = false; if (alive.current) setBusy(false); }
    }
    const waitingVerification = execution?.status === 'waiting' && execution.phase === 'verification';
    const canReviseWaiting = execution?.status === 'waiting' && ['verification', 'dns_publisher', 'dns_readiness'].includes(execution.phase);
    const waitingLicense = execution?.status === 'waiting' && execution.phase === 'license';
    const confirmingPrevious = execution?.status === 'running' && execution.error?.code.split(':')[0] === 'server_setup_reconciling';
    const completed = snapshot.status === 'ready';
    const hasOperation = resolving || !!execution || !!marker || reconnecting;
    const certificateReady = execution?.steps.some(item => item.kind === 'panel_certificate' && item.status === 'succeeded') || execution?.status === 'succeeded';
    const panelURL = certificateReady ? safeSetupPanelURL(execution?.panel_url, marker?.panel_domain || draft.panel_domain) : null;
    const customized = !!draft.customization || draft.purpose === 'custom';
    const steps: Step[] = ['purpose', ...(customized ? ['components' as const] : []), 'access', 'review', 'progress'];
    const currentStep = steps.indexOf(step);
    const selectedComponents = setupEffectiveComponents(draft, catalog);
    const emptySelectionInvalid = customized && selectedComponents.size === 0 && !['dns', 'custom'].includes(draft.purpose);
    const isDNS = draft.purpose === 'dns' || (draft.purpose === 'custom' && selectedComponents.size === 0);
    const needsDNSPublisher = ['nginx', 'node', 'postfix', 'roundcube'].some(id => selectedComponents.has(id));
    const secondaryHosting = needsDNSPublisher && draft.dns_mode === 'local' && draft.dns_role === 'secondary';
    const hostingDNSManagement = draft.dns_hosting_management || (draft.dns_publisher_endpoint ? 'panel' : 'manual');
    const automaticPublisher = secondaryHosting && hostingDNSManagement === 'panel';
    const publisherEndpoint = remoteDNSEndpoint(draft.dns_publisher_endpoint || '');
    const dnsSelectionError: TranslationKey | null = isDNS && draft.dns_mode !== 'local'
        ? 'setup.components.localDNSRequired'
        : automaticPublisher && !publisherEndpoint
            ? 'setup.publisher.endpointRequired' : null;
    const isMail = ['postfix', 'dovecot', 'rspamd', 'roundcube'].some(id => selectedComponents.has(id));
    const isNode = selectedComponents.has('node');
    const nextPath = setupNextPath(snapshot.draft.purpose, customized ? selectedComponents : undefined);
    const nextLabel = nextPath === '/settings?section=dns' ? 'setup.nextDNS' : nextPath === '/services' ? 'setup.nextComponents' : isNode ? 'setup.nextApplication' : 'setup.nextWebsite';
    const progressChecks = (execution?.checks || snapshot.checks).filter(check => check.state !== 'ready');

    if (manualExit) return <Navigate to="/" replace />;
    return <ServerSetupShell>
        <div className="max-w-3xl">
            <h1 ref={heading} tabIndex={-1} className="text-2xl font-semibold leading-tight outline-none focus-visible:outline-none sm:text-3xl">{t(completed ? 'setup.completeTitle' : 'setup.title')}</h1>
            <p className="mt-3 max-w-2xl text-fg-muted">{t(completed ? 'setup.completeHelp' : 'setup.intro')}</p>
        </div>
        {completed ? <section className="mt-8 max-w-3xl space-y-6" aria-labelledby="setup-ready-title">
            <div className="flex items-start gap-3"><Check className="mt-1 h-5 w-5 shrink-0 text-success" aria-hidden="true" /><div><h2 id="setup-ready-title" className="text-lg font-semibold">{t(customized ? 'setup.purpose.custom' : `setup.purpose.${snapshot.draft.purpose}`)}</h2><p className="mt-1 text-sm text-fg-muted">{t('setup.completeServices')}</p></div></div>
            <Link to={nextPath} className="inline-flex items-center gap-3 rounded-lg bg-primary px-5 py-3 font-semibold text-primary-fg hover:bg-primary/90 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary">{t(nextLabel)}<ArrowRight className="h-4 w-4" aria-hidden="true" /></Link>
        </section> : <div className="mt-6 grid items-start gap-6 lg:grid-cols-12 lg:gap-8">
            <nav className="lg:sticky lg:top-6 lg:col-span-2" aria-label={t('setup.steps')}>
                <p className="mb-3 flex items-center justify-between gap-4 font-semibold lg:hidden"><span>{t(`setup.step.${step}`)}</span><span className="text-sm tabular-nums text-fg-muted">{currentStep + 1} / {steps.length}</span></p>
                <ol className={`grid gap-2 ${customized ? 'grid-cols-5' : 'grid-cols-4'} lg:flex lg:flex-col lg:gap-1`}>
                    {steps.map((item, index) => <li key={item} aria-current={step === item ? 'step' : undefined} className={`flex items-center justify-center gap-3 rounded-lg px-2 py-3 text-sm lg:justify-start lg:px-3 ${index === currentStep ? 'bg-surface-2 font-semibold text-primary' : 'text-fg-muted'}`}>
                        <span className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-full tabular-nums ${index === currentStep ? 'bg-primary text-primary-fg' : 'border border-border-strong'}`}>{index + 1}</span><span className="sr-only lg:not-sr-only">{t(`setup.step.${item}`)}</span>
                    </li>)}
                </ol>
            </nav>
            <div className="min-w-0 lg:col-span-10">
            {error && <p role="alert" className="mb-5 rounded-lg border border-danger/40 bg-danger/5 p-4 text-sm text-danger">{error}</p>}
            {resolving ? <div className="flex items-center gap-3"><Spinner label={t('setup.resuming')} /><p>{t('setup.resuming')}</p></div>
                : step === 'progress' || hasOperation ? <section aria-labelledby="setup-progress-title">
                    <h2 id="setup-progress-title" className="text-xl font-semibold">{t(reconnecting ? 'setup.reconnecting' : execution?.status === 'failed' ? 'setup.operationFailed' : waitingLicense ? 'setup.licenseWaiting' : execution?.status === 'waiting' ? 'setup.waiting' : execution?.status === 'succeeded' ? 'setup.verifying' : 'setup.installing')}</h2>
                    <p className="mt-3 text-sm leading-6 text-fg-muted">{t(reconnecting ? 'setup.uncertain' : execution?.status === 'failed' ? 'setup.failedHelp' : 'setup.progressHelp')}</p>
                    {execution?.status === 'waiting' && execution.phase === 'dns_publisher' && <SetupDNSPublisher key={execution.id} execution={execution} onBound={reconcile} />}
                    {execution?.status === 'waiting' && execution.phase === 'dns_readiness' && <p role="status" className="mt-5 text-sm leading-6 text-fg-muted">{t('setup.publisher.pairWaiting')}</p>}
                    <ol className="mt-6 divide-y divide-border" aria-live="polite">
                        {execution?.steps.map(item => <li key={item.id} className="flex items-center justify-between gap-4 py-4"><div className="min-w-0"><p className="font-medium">{t(`setup.kind.${item.kind}`, { target: stepTarget(item.kind, item.target) })}</p>{item.qualifier && <p className="mt-1 break-words text-sm text-fg-muted">{item.qualifier}</p>}</div><span className="flex shrink-0 items-center gap-2 text-sm text-fg-muted">{item.status === 'succeeded' ? <Check className="h-4 w-4 text-success" aria-hidden="true" /> : item.status === 'running' ? <Loader2 className="h-4 w-4 motion-safe:animate-spin" aria-hidden="true" /> : <Circle className="h-3 w-3" aria-hidden="true" />}{t(`setup.operation.${item.status}`)}</span></li>)}
                    </ol>
                    {execution?.error && !['dns_publisher', 'dns_readiness'].includes(execution.phase) && <div role={confirmingPrevious ? 'status' : 'alert'} className="mt-5 space-y-2 text-sm"><p className={confirmingPrevious ? 'text-fg-muted' : 'text-danger'}>{failureText(execution.error.code)}</p><details><summary className="cursor-pointer text-primary">{t('setup.details')}</summary><p className="mt-2 break-words text-fg-muted">{execution.error.message}</p></details></div>}
                    {progressChecks.length > 0 && waitingVerification && <ul className="mt-5 list-disc space-y-2 pl-5 text-sm text-fg-muted">{progressChecks.map(check => <li key={check.id}>{failureText(check.code)}</li>)}</ul>}
                    {waitingVerification && <p className="mt-5 text-sm text-fg-muted">{t('setup.reviseHelp')}</p>}
                    {completionFailed && <p role="alert" className="mt-5 text-sm text-danger">{t('setup.verificationFailed')}</p>}
                    {panelURL && new URL(panelURL).hostname !== window.location.hostname && <p className="mt-6"><a href={new URL('/setup', panelURL).href} className="font-semibold text-primary underline underline-offset-4">{t('setup.secureAddress')}</a></p>}
                    {waitingLicense && <Link to="/settings?section=license" className="mt-5 inline-flex text-primary underline underline-offset-4">{t('license.activate')}</Link>}
                    <div className="mt-6 flex flex-wrap gap-3">
                        {canReviseWaiting && <Button variant="secondary" disabled={busy} onClick={() => void reviseWaiting()}>{t('setup.editPlan')}</Button>}
                        {execution?.status === 'failed' ? <Button variant="primary" onClick={editPlan}>{t('setup.revise')}</Button>
                            : waitingVerification ? <Button variant="primary" disabled={busy} onClick={() => void verifyManual()}>{t('setup.verify')}</Button>
                                : (reconnecting || completionFailed) && <Button variant="primary" disabled={busy} onClick={() => void (marker && !execution ? resumeUnconfirmed() : reconcile())}>{t(marker && !execution ? 'setup.resumeConfirmed' : 'setup.reconnect')}</Button>}
                    </div>
                </section> : <form onSubmit={next} className="min-w-0">
                    {step === 'purpose' && <fieldset disabled={busy}>
                        <legend className="text-xl font-semibold">{t('setup.purposeTitle')}</legend>
                        <div className="mt-4 divide-y divide-border rounded-xl border border-border bg-surface">
                            {setupPurposes.map(purpose => <div key={purpose} className={`first:rounded-t-xl last:rounded-b-xl ${draft.purpose === purpose ? 'bg-surface-2' : 'hover:bg-surface-subtle'}`}>
                                <label className="flex cursor-pointer items-start gap-4 px-4 py-4 sm:px-5"><input type="radio" name="setup-purpose" value={purpose} checked={draft.purpose === purpose} onChange={() => { setDraft(previous => chooseSetupPurpose(previous, purpose)); setPlan(null); }} className="mt-1 h-4 w-4 shrink-0 accent-primary" /><span className="grid min-w-0 flex-1 gap-1 sm:grid-cols-3 sm:gap-5"><span className="font-semibold"><span id={`setup-purpose-title-${purpose}`}>{t(`setup.purpose.${purpose}`)}</span>{purpose === 'web' && <span className="mt-1 block text-xs font-normal text-fg-muted">{t('setup.recommended')}</span>}</span><span className="text-sm leading-6 text-fg-muted sm:col-span-2">{t(`setup.purpose.${purpose}.help`)}</span></span></label>
                                {draft.purpose === purpose && purpose !== 'custom' && <button type="button" disabled={!catalog || busy} aria-describedby={`setup-purpose-title-${purpose}`} className="mb-3 ml-12 inline-flex min-h-9 items-center text-sm font-medium text-primary underline underline-offset-4 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary disabled:cursor-not-allowed disabled:opacity-60 sm:ml-[3.25rem]" onClick={() => { if (!catalog) return; change('customization', { components: draft.customization?.components || setupPresetComponents(draft, catalog) }); setStep('components'); }}>{t('setup.components.customize')}</button>}
                            </div>)}
                        </div>
                        <p className="mt-4 text-sm text-fg-muted">{t('setup.purposeHelp')}</p>
                        {catalogFailed && <p role="alert" className="mt-3 text-sm text-fg-muted">{t('setup.components.loadFailed')} <Button type="button" onClick={reloadCatalog}>{t('common.retry')}</Button></p>}
                    </fieldset>}
                    {step === 'components' && (catalog ? <ServerSetupComponents catalog={catalog} selected={draft.customization?.components || []} disabled={busy} onChange={components => change('customization', { components })} /> : <div role={catalogFailed ? 'alert' : 'status'} className="space-y-3"><h2 className="text-xl font-semibold">{t('setup.components.title')}</h2><p className="text-sm text-fg-muted">{t(catalogFailed ? 'setup.components.loadFailed' : 'setup.components.loading')}</p>{catalogFailed && <Button type="button" onClick={reloadCatalog}>{t('common.retry')}</Button>}</div>)}
                    {step === 'components' && emptySelectionInvalid && <p role="alert" className="mt-4 text-sm text-danger">{t('setup.components.chooseRequired')}</p>}
                    {step === 'components' && catalog?.inventory_state === 'unknown' && <div className="mt-4"><Button type="button" disabled={busy} onClick={reloadCatalog}>{t('common.retry')}</Button></div>}
                    {step === 'access' && <fieldset disabled={busy} className="space-y-6">
                        <legend className="mb-0 text-xl font-semibold">{t('setup.accessTitle')}</legend>
                        <div className="space-y-4"><SetupInput name="panel_domain" label={t('setup.panelDomain')} value={draft.panel_domain} onChange={value => change('panel_domain', value)} placeholder="panel.example.com" required /><p className="max-w-2xl text-sm leading-6 text-fg-muted">{t('setup.panelDomainShort')}</p>
                            {snapshot.server_ip && draft.panel_domain && <p className="break-words rounded-lg bg-surface-2 px-4 py-3 text-sm">{t('setup.panelRecord', { name: draft.panel_domain, ip: snapshot.server_ip })}</p>}
                        </div>
                        {isMail && <div className="space-y-3"><SetupInput name="mail_hostname" label={t('setup.mailHostname')} value={draft.mail_hostname} onChange={value => change('mail_hostname', value)} placeholder="mail.example.com" required /><p className="text-sm leading-6 text-fg-muted">{t('setup.mailHostnameHelp')}</p></div>}
                        <fieldset><legend className="font-semibold">{t('setup.dnsTitle')}</legend><p className="mt-2 text-sm text-fg-muted">{t('setup.dnsHelp')}</p>
                            <div className="mt-4 grid gap-3 lg:grid-cols-3">{(['local', 'external', 'existing'] as const).map(mode => <label key={mode} className={`flex items-start gap-3 rounded-lg border border-border p-4 ${draft.dns_mode === mode ? 'bg-surface-2 ring-1 ring-primary' : 'bg-surface'} ${isDNS && mode !== 'local' ? 'text-fg-muted' : 'cursor-pointer hover:bg-surface-2'}`}><input type="radio" name="setup-dns" value={mode} checked={draft.dns_mode === mode} disabled={isDNS && mode !== 'local' && draft.dns_mode !== mode} onChange={() => change('dns_mode', mode)} className="mt-1 h-4 w-4 shrink-0 accent-primary" /><span><span className="font-medium">{t(`setup.dns.${mode}.shortTitle`)}</span><span className="mt-1 block text-sm leading-6 text-fg-muted">{t(`setup.dns.${mode}.shortHelp`)}</span></span></label>)}</div>
                        </fieldset>
                        {dnsSelectionError && <p role="alert" className="text-sm leading-6 text-danger">{t(dnsSelectionError)}</p>}
                        {draft.dns_mode === 'existing' && <ServerSetupDNSConnection value={draft.remote_dns_connection_id} onChange={value => change('remote_dns_connection_id', value)} onValidityChange={setRemoteVerified} />}
                        {draft.dns_mode === 'local' && <div className="space-y-5">
                            <p className="text-sm leading-6 text-fg-muted">{t(draft.dns_role === 'secondary' ? 'setup.secondaryHelp' : 'setup.primaryHelp')}</p>
                            <div className="grid gap-6 rounded-xl border border-border bg-surface p-4 sm:p-5 md:grid-cols-2 md:gap-8">
                                <fieldset className="min-w-0 space-y-4">
                                    <legend className="mb-0 font-semibold">{t('setup.thisServer')}</legend>
                                    <SetupSelect name="dns_role" label={t('setup.dnsRole')} value={draft.dns_role} onChange={value => { setDraft(previous => changeSetupDNSRole(previous, value as 'primary' | 'secondary')); setPlan(null); setAcknowledged(false); setError(''); }}><option value="primary">{t('setup.primary')}</option><option value="secondary">{t('setup.secondary')}</option></SetupSelect>
                                    {automaticPublisher && <p className="text-sm leading-6 text-fg-muted" role="note">{t('setup.publisher.roleHelp')}</p>}
                                    <SetupInput name={dnsNames.localKey} label={t('setup.nameserverName')} value={draft[dnsNames.localKey]} onChange={value => change(dnsNames.localKey, value)} placeholder="ns1.example.com" required />
                                    <SetupInput name="local_ip" label={t('setup.publicIPv4')} value={draft.local_ip} onChange={value => change('local_ip', value)} required />
                                    {detectedIP && draft.local_ip === detectedIP && <p className="text-xs leading-5 text-fg-muted">{t('setup.detectedIPHelp')}</p>}
                                </fieldset>
                                <fieldset className="min-w-0 space-y-4 border-t border-border md:border-l md:border-t-0 md:pl-8">
                                    <legend className="mb-0 font-semibold">{t('setup.otherDNSServer')}</legend>
                                    <div><p className="mb-2 text-sm font-medium">{t('setup.peerRole')}</p><p className={`${inputClass} flex items-center bg-surface-2 text-sm`} aria-live="polite">{t(draft.dns_role === 'primary' ? 'setup.secondary' : 'setup.primary')}</p></div>
                                    <SetupInput name={dnsNames.peerKey} label={t('setup.nameserverName')} value={draft[dnsNames.peerKey]} onChange={value => { setDraft(previous => ({ ...previous, [setupDNSNames(previous).peerKey]: value, peer_ns: value })); setPlan(null); setAcknowledged(false); setError(''); }} placeholder="ns2.example.com" required />
                                    <SetupInput name="peer_ip" label={t('setup.publicIPv4')} value={draft.peer_ip} onChange={value => change('peer_ip', value)} required />
                                </fieldset>
                            </div>
                            {dnsNames.mismatch && <div role="alert" className="space-y-2 rounded-lg border border-warning-mark/40 bg-warning-mark/10 p-4 text-sm"><p>{t('setup.dnsMappingMismatch')}</p><p className="break-all">{t('setup.savedPeerName')}: {draft.peer_ns || '—'}</p><Button type="button" disabled={!draft[dnsNames.peerKey].trim()} onClick={() => change('peer_ns', draft[dnsNames.peerKey])}>{t('setup.useDisplayedPeer')}</Button></div>}
                            <div className="max-w-sm"><SetupSelect name="dns_engine" label={t('setup.engine')} value={draft.dns_engine} onChange={value => change('dns_engine', value as 'bind' | 'pdns')}><option value="pdns">PowerDNS</option><option value="bind">BIND</option></SetupSelect></div>
                            <p className="text-sm leading-6 text-fg-muted">{t('setup.dnsStartPrimary')}</p>
                            <details className="text-sm text-fg-muted"><summary className="cursor-pointer font-medium text-primary">{t('setup.dnsPairDetails')}</summary><p className="mt-3 leading-6">{t('setup.dnsPairOrder')}</p><p className="mt-3 leading-6">{t('setup.dnsNativePeerHelp')}</p></details>
                        </div>}
                        {secondaryHosting && <div className="space-y-3">
                            <SetupSelect name="dns_hosting_management" label={t('setup.publisher.management')} value={hostingDNSManagement} onChange={value => change('dns_hosting_management', value as 'manual' | 'panel')}>
                                <option value="manual">{t('setup.publisher.manual')}</option>
                                <option value="panel">{t('setup.publisher.automatic')}</option>
                            </SetupSelect>
                            {automaticPublisher ? <>
                            <SetupInput name="dns_publisher_endpoint" label={t('setup.publisher.endpoint')} value={draft.dns_publisher_endpoint || ''} onChange={value => change('dns_publisher_endpoint', value)} placeholder="https://primary.example.com:2083" required />
                            <p className="text-sm leading-6 text-fg-muted">{t('setup.publisher.setupHelp')}</p>
                            </> : <p className="text-sm leading-6 text-fg-muted">{t('setup.publisher.manualHelp')}</p>}
                        </div>}
                        {isNode && <SetupNodeVersion value={draft.node_version} onChange={value => change('node_version', value)} />}
                        {draft.purpose === 'application' && !customized && <SetupSelect name="database" label={t('setup.database')} value={draft.database} onChange={value => change('database', value)}><option value="">{t('setup.databaseNone')}</option><option value="mariadb">MariaDB</option><option value="postgresql">PostgreSQL</option></SetupSelect>}
                    </fieldset>}
                    {step === 'review' && plan && <section aria-labelledby="setup-plan-title">
                        <h2 id="setup-plan-title" className="text-xl font-semibold">{t('setup.reviewTitle')}</h2><p className="mt-3 text-sm leading-6 text-fg-muted">{t('setup.reviewHelp')}</p>
                        {plan.components && <div className="mt-5"><h3 className="font-semibold">{t('setup.components.reviewTitle')}</h3><ul className="mt-2 divide-y divide-border">{plan.components.map(item => <li key={item.id} className="flex flex-wrap items-baseline justify-between gap-2 py-2 text-sm"><span>{catalog?.components.find(row => row.id === item.id)?.name || stepTarget('service', item.id)}</span><span className="text-fg-muted">{t(item.installed ? 'setup.components.keep' : item.required ? 'setup.components.dependency' : 'setup.components.toInstall')}</span></li>)}</ul><p className="mt-3 text-sm text-fg-muted">{t('setup.components.preserve')}</p></div>}
                        <ol className="mt-5 divide-y divide-border">{plan.steps.map(item => <li key={item.id} className="py-4"><p className="font-medium">{t(`setup.kind.${item.kind}`, { target: stepTarget(item.kind, item.target) })}</p>{item.qualifier && <p className="mt-1 text-sm text-fg-muted">{item.qualifier}</p>}</li>)}</ol>
                        <dl className="mt-5 space-y-3 rounded-lg bg-surface-2 p-4 text-sm"><div><dt className="font-semibold">{t('setup.firewallReview')}</dt><dd className="mt-1 leading-6 text-fg-muted">{t('setup.firewallHelp')}</dd></div><div><dt className="font-semibold">TCP</dt><dd className="mt-1 break-words tabular-nums">{plan.tcp_ports.join(', ') || t('setup.noPorts')}</dd></div><div><dt className="font-semibold">UDP</dt><dd className="mt-1 break-words tabular-nums">{plan.udp_ports.join(', ') || t('setup.noPorts')}</dd></div><div><dt className="font-semibold">{t('setup.certificateContact')}</dt><dd className="mt-1 break-all">{plan.contact_email}</dd></div>{plan.hostname_change && <div><dt className="font-semibold">{t('setup.hostnameChange')}</dt><dd className="mt-1 break-all">{plan.hostname_change}</dd></div>}</dl>
                        {automaticPublisher && publisherEndpoint && <div className="mt-5 space-y-2 border-t border-border pt-4 text-sm"><p className="font-semibold">{t('setup.publisher.reviewTitle')}</p><p className="break-all">{publisherEndpoint}</p><p className="leading-6 text-fg-muted">{t('setup.publisher.setupHelp')}</p></div>}
                        {plan.remote_dns_connection && <div className="mt-5 rounded-lg border border-border p-4 text-sm"><p className="font-semibold">{t('setup.remote.reviewTitle')}</p><p className="mt-2 break-all">{plan.remote_dns_connection.endpoint}</p><p className="mt-1 break-words text-fg-muted">{plan.remote_dns_connection.nameservers.join(', ')}</p><p className="mt-2 leading-6 text-fg-muted">{t('setup.remote.reviewHelp')}</p></div>}
                        {secondaryHosting && !automaticPublisher && <p className="mt-5 text-sm leading-6 text-fg-muted">{t('setup.publisher.manualHelp')}</p>}
                        {draft.dns_mode === 'external' && <p className="mt-5 text-sm leading-6 text-fg-muted">{t('setup.externalAfter')}</p>}
                        {isMail && <p className="mt-4 text-sm leading-6 text-fg-muted">{t('setup.mailAfter')}</p>}
                        {plan.blockers.length > 0 && <div role="alert" className="mt-5 rounded-lg border border-warning-mark/40 bg-warning-mark/10 p-4"><p className="font-semibold">{t('setup.planBlocked')}</p><ul className="mt-3 list-disc space-y-2 pl-5 text-sm">{plan.blockers.map(code => <li key={code}>{failureText(code)}<details className="mt-1 text-xs text-fg-muted"><summary className="cursor-pointer">{t('setup.details')}</summary><code className="mt-1 block break-words">{code}</code></details></li>)}</ul></div>}
                        {plan.can_start && <label className="mt-6 flex cursor-pointer items-start gap-3 text-sm leading-6"><input type="checkbox" checked={acknowledged} onChange={event => setAcknowledged(event.target.checked)} className="mt-1 h-4 w-4 shrink-0 accent-primary" /><span>{t('setup.confirm')}</span></label>}
                    </section>}
                    <div className="setup-actions sticky bottom-0 z-10 mt-6 flex flex-wrap items-center justify-between gap-3 border-t border-border bg-bg py-4">
                        {step !== 'purpose' && <Button type="button" variant="secondary" disabled={busy} onClick={() => { setStep(steps[Math.max(0, currentStep - 1)]); setPlan(null); setAcknowledged(false); }}>{t('setup.back')}</Button>}
                        <div className="ml-auto flex flex-wrap items-center justify-end gap-3">
                        {step === 'review' ? <Button type="button" variant="primary" disabled={busy || !plan?.can_start || !acknowledged} onClick={() => void start()}>{t('setup.start')}</Button> : <button type="submit" disabled={busy || (step === 'components' && (!catalog || catalog.inventory_state !== 'ready' || emptySelectionInvalid)) || (step === 'access' && (!!dnsSelectionError || (draft.dns_mode === 'local' && dnsNames.mismatch) || (draft.dns_mode === 'existing' && !remoteVerified)))} className="inline-flex items-center gap-3 rounded-lg bg-primary px-5 py-2.5 font-semibold text-primary-fg hover:bg-primary/90 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary disabled:cursor-not-allowed disabled:opacity-60">{busy ? t('common.loading') : t(step === 'purpose' || step === 'components' ? 'setup.continue' : 'setup.review')}<ArrowRight className="h-4 w-4" aria-hidden="true" /></button>}
                        </div>
                    </div>
                </form>}
            {!completed && !hasOperation && <ServerSetupManualAction compact snapshot={snapshot} onChosen={next => { accept(next); setManualExit(true); }} />}
            </div>
        </div>}
    </ServerSetupShell>;
}

function SetupDNSPublisher({ execution, onBound }: { execution: ServerSetupExecution; onBound: () => Promise<void> }) {
    const { t } = useI18n();
    const [connection, setConnection] = useState('');
    const [verified, setVerified] = useState(false);
    const [busy, setBusy] = useState(false);
    const [failed, setFailed] = useState(false);
    const [bound, setBound] = useState(false);
    const inFlight = useRef(false);
    const alive = useRef(true);
    useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
    const endpoint = remoteDNSEndpoint(execution.steps.find(item => item.kind === 'dns_publisher')?.target || '');
    async function bind() {
        if (!endpoint || !connection || !verified || inFlight.current) return;
        inFlight.current = true; setBusy(true); setFailed(false);
        try {
            const response = await setupFetch('/api/v1/setup/publisher', requestOptions({ execution_id: execution.id, connection_id: connection }));
            const value = response.ok ? decodeSetupExecution(await response.json()) : null;
            if (!value || value.id !== execution.id || value.plan_id !== execution.plan_id || value.request_id !== execution.request_id) throw new Error('binding');
            if (alive.current) setBound(true);
            await onBound();
        } catch {
            // A lost reply may follow a committed binding. Poll the existing
            // execution; retrying this exact choice never creates another grant.
            if (alive.current) setFailed(true);
            await onBound();
        } finally { inFlight.current = false; if (alive.current) setBusy(false); }
    }
    return <div className="mt-6 space-y-4 border-y border-border py-5" aria-labelledby="setup-publisher-title">
        <h3 id="setup-publisher-title" className="text-lg font-semibold">{t('setup.publisher.waitingTitle')}</h3>
        <p className="text-sm leading-6 text-fg-muted">{t('setup.publisher.waitingHelp')}</p>
        {endpoint ? <>
            <fieldset disabled={busy || bound}><ServerSetupDNSConnection value={connection} requiredEndpoint={endpoint} onChange={id => { setConnection(id); setVerified(false); setFailed(false); }} onValidityChange={setVerified} /></fieldset>
            {failed && <p role="alert" className="text-sm text-danger">{t('setup.publisher.bindFailed')}</p>}
            {bound && <p role="status" className="text-sm text-fg-muted">{t('setup.publisher.bound')}</p>}
            <Button type="button" variant="primary" disabled={!connection || !verified || busy || bound} onClick={() => void bind()}>{t('setup.publisher.continue')}</Button>
        </> : <p role="alert" className="text-sm text-danger">{t('setup.publisher.endpointRequired')}</p>}
    </div>;
}


function SetupInput({ name, label, value, onChange, placeholder, required }: { name: string; label: string; value: string; onChange: (value: string) => void; placeholder?: string; required?: boolean }) {
    return <label className="block" htmlFor={`setup-${name}`}><span className="mb-2 block text-sm font-medium">{label}</span><input id={`setup-${name}`} name={name} value={value} onChange={event => onChange(event.target.value)} placeholder={placeholder} required={required} autoCapitalize="none" spellCheck={false} className={`${inputClass} w-full`} /></label>;
}
function SetupSelect({ name, label, value, onChange, children }: { name: string; label: string; value: string; onChange: (value: string) => void; children: ReactNode }) {
    return <label className="block" htmlFor={`setup-${name}`}><span className="mb-2 block text-sm font-medium">{label}</span><select id={`setup-${name}`} name={name} value={value} onChange={event => onChange(event.target.value)} className={`${inputClass} w-full`}>{children}</select></label>;
}


function SetupNodeVersion({ value, onChange }: { value: string; onChange: (value: string) => void }) {
    const { t } = useI18n();
    const [releases, setReleases] = useState<{ version: string; name: string }[] | null>(null);
    const [failed, setFailed] = useState(false);
    const [attempt, setAttempt] = useState(0);
    useEffect(() => {
        const controller = new AbortController();
        const timeout = window.setTimeout(() => controller.abort(), 20000);
        let active = true;
        setFailed(false); setReleases(null);
        void fetch('/api/v1/runtimes/node/lts', { signal: controller.signal, cache: 'no-store' })
            .then(async response => { if (!response.ok) throw new Error('release list'); return response.json() as Promise<unknown>; })
            .then(body => {
                const list = (body as { releases?: unknown } | null)?.releases;
                if (!Array.isArray(list) || list.length === 0 || list.some(item => typeof item?.version !== 'string' || !/^\d+\.\d+\.\d+$/.test(item.version) || typeof item.name !== 'string')) throw new Error('invalid release list');
                if (active) setReleases(list);
            }).catch(() => { if (active) setFailed(true); })
            .finally(() => window.clearTimeout(timeout));
        return () => { active = false; controller.abort(); window.clearTimeout(timeout); };
    }, [attempt]);
    return <div className="space-y-3">
        <label className="block" htmlFor="setup-node_version"><span className="mb-2 block text-sm font-medium">{t('setup.nodeVersion')}</span><select id="setup-node_version" name="node_version" value={value} onChange={event => onChange(event.target.value)} required disabled={!releases} className={`${inputClass} w-full`}>
            <option value="">{t(releases ? 'setup.nodeChoose' : failed ? 'setup.nodeUnavailable' : 'common.loading')}</option>
            {value && !releases?.some(item => item.version === value) && <option value={value}>{value} — {t('setup.nodeSaved')}</option>}
            {releases?.map(item => <option key={item.version} value={item.version}>Node.js {item.version} · {item.name} LTS</option>)}
        </select></label>
        <p className="text-sm text-fg-muted">{t('setup.nodeHelp')}</p>
        {failed && <p role="alert" className="flex flex-wrap items-center gap-3 text-sm text-danger">{t('setup.nodeUnavailable')}<Button type="button" variant="secondary" onClick={() => setAttempt(value => value + 1)}>{t('common.retry')}</Button></p>}
    </div>;
}
