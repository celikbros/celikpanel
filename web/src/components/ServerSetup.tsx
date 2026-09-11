import { useCallback, useEffect, useRef, useState, type FormEvent, type ReactNode } from 'react';
import { ArrowRight, Check, Circle, Loader2 } from 'lucide-react';
import { useAuth } from '../auth/AuthContext';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import { Link, Navigate } from '../router';
import { chooseSetupPurpose, decodeServerSetup, setupNextPath, setupPurposes, type ServerSetupDraft, type ServerSetupSnapshot } from '../lib/serverSetup';
import { decodeSetupExecution, decodeSetupMarker, decodeSetupPlan, newSetupRequestID, safeSetupPanelURL, type ServerSetupExecution, type ServerSetupPlan, type SetupStartMarker } from '../lib/serverSetupOperation';
import { ServerSetupShell, useServerSetup } from './ServerSetupGate';
import { ServerSetupDNSConnection } from './ServerSetupDNSConnections';
import { ServerSetupChoice, ServerSetupManualAction } from './ServerSetupChoice';
import { Button, inputClass, Spinner } from './ui';

type Step = 'purpose' | 'access' | 'review' | 'progress';
const steps: Step[] = ['purpose', 'access', 'review', 'progress'];
const requestOptions = (body: unknown) => ({ method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
async function setupFetch(url: string, options?: RequestInit) {
    const controller = new AbortController();
    const timeout = window.setTimeout(() => controller.abort(), 20000);
    try { return await fetch(url, { ...options, signal: controller.signal }); }
    finally { window.clearTimeout(timeout); }
}
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
    server_setup_hosting_requires_dns_publisher: 'setup.blocker.dnsIdentity',
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
    server_setup_dns_mode_unsupported: 'setup.blocker.existing',
    server_setup_dns_purpose_requires_local: 'setup.blocker.dnsIdentity',
    server_setup_service_unknown: 'setup.blocker.services',
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
    const [snapshot, setSnapshot] = useState(initial);
    const [draft, setDraft] = useState(initial.draft);
    const [step, setStep] = useState<Step>(initial.status === 'new' || initial.status === 'legacy' ? 'purpose' : 'access');
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
        if (key === 'dns_mode' || key === 'remote_dns_connection_id') setRemoteVerified(false);
        setDraft(previous => ({ ...previous, [key]: value })); setPlan(null); setAcknowledged(false); setError('');
    }
    async function saveDraft(): Promise<ServerSetupSnapshot> {
        const response = await setupFetch('/api/v1/setup', { ...requestOptions({ revision: snapshot.revision, draft }), method: 'PUT' });
        if (response.status === 409) throw new Error(t('setup.conflict'));
        if (!response.ok) throw new Error(t('setup.saveFailed'));
        const next = decodeServerSetup(await response.json());
        if (!next) throw new Error(t('setup.loadFailed'));
        if (alive.current) { accept(next); setDraft(next.draft); }
        return next;
    }
    async function next(event: FormEvent) {
        event.preventDefault();
        if (step === 'access' && draft.dns_mode === 'existing' && !remoteVerified) { setError(t('setup.remote.proof.failed')); return; }
        if (pendingRef.current) return;
        pendingRef.current = true; setBusy(true); setError('');
        try {
            const saved = await saveDraft();
            if (!alive.current) return;
            if (step === 'purpose') { setStep('access'); return; }
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
        if (execution?.status !== 'waiting' || execution.phase !== 'verification' || pendingRef.current) return;
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
    const waitingLicense = execution?.status === 'waiting' && execution.phase === 'license';
    const confirmingPrevious = execution?.status === 'running' && execution.error?.code.split(':')[0] === 'server_setup_reconciling';
    const completed = snapshot.status === 'ready';
    const hasOperation = resolving || !!execution || !!marker || reconnecting;
    const certificateReady = execution?.steps.some(item => item.kind === 'panel_certificate' && item.status === 'succeeded') || execution?.status === 'succeeded';
    const panelURL = certificateReady ? safeSetupPanelURL(execution?.panel_url, marker?.panel_domain || draft.panel_domain) : null;
    const currentStep = steps.indexOf(step);
    const isDNS = draft.purpose === 'dns';
    const isMail = draft.purpose === 'web_mail';
    const progressChecks = (execution?.checks || snapshot.checks).filter(check => check.state !== 'ready');

    if (manualExit) return <Navigate to="/" replace />;
    return <ServerSetupShell>
        <div className="max-w-3xl">
            <h1 ref={heading} tabIndex={-1} className="text-2xl font-semibold leading-tight outline-none focus-visible:outline-none sm:text-3xl">{t(completed ? 'setup.completeTitle' : 'setup.title')}</h1>
            <p className="mt-3 max-w-2xl text-fg-muted">{t(completed ? 'setup.completeHelp' : 'setup.intro')}</p>
        </div>
        {completed ? <section className="mt-8 max-w-3xl space-y-6" aria-labelledby="setup-ready-title">
            <div className="flex items-start gap-3"><Check className="mt-1 h-5 w-5 shrink-0 text-success" aria-hidden="true" /><div><h2 id="setup-ready-title" className="text-lg font-semibold">{t(`setup.purpose.${snapshot.draft.purpose}`)}</h2><p className="mt-1 text-sm text-fg-muted">{t('setup.completeServices')}</p></div></div>
            <Link to={setupNextPath(snapshot.draft.purpose)} className="inline-flex items-center gap-3 rounded-lg bg-primary px-5 py-3 font-semibold text-primary-fg hover:bg-primary/90 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary">{t(snapshot.draft.purpose === 'dns' ? 'setup.nextDNS' : snapshot.draft.purpose === 'application' ? 'setup.nextApplication' : 'setup.nextWebsite')}<ArrowRight className="h-4 w-4" aria-hidden="true" /></Link>
        </section> : <>
            <ol className="my-8 grid grid-cols-2 gap-x-5 gap-y-3 border-b border-border pb-6 text-sm sm:grid-cols-4" aria-label={t('setup.steps')}>
                {steps.map((item, index) => <li key={item} aria-current={step === item ? 'step' : undefined} className={`flex items-center gap-2 ${index === currentStep ? 'font-semibold text-primary' : 'text-fg-muted'}`}><span className="tabular-nums">{index + 1}.</span>{t(`setup.step.${item}`)}</li>)}
            </ol>
            {error && <p role="alert" className="mb-5 max-w-3xl rounded-lg border border-danger/40 bg-danger/5 p-4 text-sm text-danger">{error}</p>}
            {resolving ? <div className="flex items-center gap-3"><Spinner label={t('setup.resuming')} /><p>{t('setup.resuming')}</p></div>
                : step === 'progress' || hasOperation ? <section className="max-w-3xl" aria-labelledby="setup-progress-title">
                    <h2 id="setup-progress-title" className="text-xl font-semibold">{t(reconnecting ? 'setup.reconnecting' : execution?.status === 'failed' ? 'setup.operationFailed' : waitingLicense ? 'setup.licenseWaiting' : execution?.status === 'waiting' ? 'setup.waiting' : execution?.status === 'succeeded' ? 'setup.verifying' : 'setup.installing')}</h2>
                    <p className="mt-3 text-sm leading-6 text-fg-muted">{t(reconnecting ? 'setup.uncertain' : execution?.status === 'failed' ? 'setup.failedHelp' : 'setup.progressHelp')}</p>
                    <ol className="mt-6 divide-y divide-border" aria-live="polite">
                        {execution?.steps.map(item => <li key={item.id} className="flex items-center justify-between gap-4 py-4"><div className="min-w-0"><p className="font-medium">{t(`setup.kind.${item.kind}`, { target: stepTarget(item.kind, item.target) })}</p>{item.qualifier && <p className="mt-1 break-words text-sm text-fg-muted">{item.qualifier}</p>}</div><span className="flex shrink-0 items-center gap-2 text-sm text-fg-muted">{item.status === 'succeeded' ? <Check className="h-4 w-4 text-success" aria-hidden="true" /> : item.status === 'running' ? <Loader2 className="h-4 w-4 motion-safe:animate-spin" aria-hidden="true" /> : <Circle className="h-3 w-3" aria-hidden="true" />}{t(`setup.operation.${item.status}`)}</span></li>)}
                    </ol>
                    {execution?.error && <div role={confirmingPrevious ? 'status' : 'alert'} className="mt-5 space-y-2 text-sm"><p className={confirmingPrevious ? 'text-fg-muted' : 'text-danger'}>{failureText(execution.error.code)}</p><details><summary className="cursor-pointer text-primary">{t('setup.details')}</summary><p className="mt-2 break-words text-fg-muted">{execution.error.message}</p></details></div>}
                    {progressChecks.length > 0 && waitingVerification && <ul className="mt-5 list-disc space-y-2 pl-5 text-sm text-fg-muted">{progressChecks.map(check => <li key={check.id}>{failureText(check.code)}</li>)}</ul>}
                    {waitingVerification && <p className="mt-5 text-sm text-fg-muted">{t('setup.reviseHelp')}</p>}
                    {completionFailed && <p role="alert" className="mt-5 text-sm text-danger">{t('setup.verificationFailed')}</p>}
                    {panelURL && new URL(panelURL).hostname !== window.location.hostname && <p className="mt-6"><a href={new URL('/setup', panelURL).href} className="font-semibold text-primary underline underline-offset-4">{t('setup.secureAddress')}</a></p>}
                    {waitingLicense && <Link to="/settings?section=license" className="mt-5 inline-flex text-primary underline underline-offset-4">{t('license.activate')}</Link>}
                    <div className="mt-6 flex flex-wrap gap-3">
                        {waitingVerification && <Button variant="secondary" disabled={busy} onClick={() => void reviseWaiting()}>{t('setup.editPlan')}</Button>}
                        {execution?.status === 'failed' ? <Button variant="primary" onClick={editPlan}>{t('setup.revise')}</Button>
                            : waitingVerification ? <Button variant="primary" disabled={busy} onClick={() => void verifyManual()}>{t('setup.verify')}</Button>
                                : (reconnecting || completionFailed) && <Button variant="primary" disabled={busy} onClick={() => void (marker && !execution ? resumeUnconfirmed() : reconcile())}>{t(marker && !execution ? 'setup.resumeConfirmed' : 'setup.reconnect')}</Button>}
                    </div>
                </section> : <form onSubmit={next} className="max-w-3xl">
                    {step === 'purpose' && <fieldset disabled={busy}>
                        <legend className="text-xl font-semibold">{t('setup.purposeTitle')}</legend>
                        <div className="mt-5 divide-y divide-border rounded-xl border border-border bg-surface">
                            {setupPurposes.map(purpose => <label key={purpose} className={`flex cursor-pointer items-start gap-4 p-5 hover:bg-surface-2 ${draft.purpose === purpose ? 'bg-surface-2' : ''}`}><input type="radio" name="setup-purpose" value={purpose} checked={draft.purpose === purpose} onChange={() => { setDraft(previous => chooseSetupPurpose(previous, purpose)); setPlan(null); }} className="mt-1 h-4 w-4 shrink-0 accent-primary" /><span><span className="font-semibold">{t(`setup.purpose.${purpose}`)}{purpose === 'web' && <span className="ml-2 text-xs font-normal text-fg-muted">{t('setup.recommended')}</span>}</span><span className="mt-1 block text-sm leading-6 text-fg-muted">{t(`setup.purpose.${purpose}.help`)}</span></span></label>)}
                        </div>
                        <p className="mt-4 text-sm text-fg-muted">{t('setup.purposeHelp')}</p>
                    </fieldset>}
                    {step === 'access' && <fieldset disabled={busy} className="space-y-8">
                        <legend className="mb-5 text-xl font-semibold">{t('setup.accessTitle')}</legend>
                        <div className="space-y-4"><SetupInput name="panel_domain" label={t('setup.panelDomain')} value={draft.panel_domain} onChange={value => change('panel_domain', value)} placeholder="panel.example.com" required /><p className="max-w-2xl text-sm leading-6 text-fg-muted">{t('setup.panelDomainHelp')}</p>
                            {snapshot.server_ip && draft.panel_domain && <p className="break-words rounded-lg bg-surface-2 px-4 py-3 text-sm">{t('setup.panelRecord', { name: draft.panel_domain, ip: snapshot.server_ip })}</p>}
                        </div>
                        {isMail && <div className="space-y-3"><SetupInput name="mail_hostname" label={t('setup.mailHostname')} value={draft.mail_hostname} onChange={value => change('mail_hostname', value)} placeholder="mail.example.com" required /><p className="text-sm leading-6 text-fg-muted">{t('setup.mailHostnameHelp')}</p></div>}
                        <fieldset><legend className="font-semibold">{t('setup.dnsTitle')}</legend><p className="mt-2 text-sm text-fg-muted">{t('setup.dnsHelp')}</p>
                            <div className="mt-4 space-y-3">{(['local', 'external', 'existing'] as const).map(mode => <label key={mode} className={`flex items-start gap-3 rounded-lg border border-border p-4 ${isDNS && mode !== 'local' ? 'text-fg-muted' : 'cursor-pointer hover:bg-surface-2'}`}><input type="radio" name="setup-dns" value={mode} checked={draft.dns_mode === mode} disabled={isDNS && mode !== 'local'} onChange={() => change('dns_mode', mode)} className="mt-1 h-4 w-4 shrink-0 accent-primary" /><span><span className="font-medium">{t(`setup.dns.${mode}`)}</span><span className="mt-1 block text-sm leading-6 text-fg-muted">{t(`setup.dns.${mode}.help`)}</span></span></label>)}</div>
                        </fieldset>
                        {draft.dns_mode === 'existing' && <ServerSetupDNSConnection value={draft.remote_dns_connection_id} onChange={value => change('remote_dns_connection_id', value)} onValidityChange={setRemoteVerified} />}
                        {draft.dns_mode === 'local' && <div className="space-y-5">
                            <div className="grid gap-5 sm:grid-cols-2"><SetupSelect name="dns_engine" label={t('setup.engine')} value={draft.dns_engine} onChange={value => change('dns_engine', value as 'bind' | 'pdns')}><option value="pdns">PowerDNS</option><option value="bind">BIND</option></SetupSelect><SetupSelect name="dns_role" label={t('setup.dnsRole')} value={draft.dns_role} onChange={value => change('dns_role', value as 'primary' | 'secondary')}><option value="primary">{t('setup.primary')}</option><option value="secondary" disabled={!isDNS}>{t('setup.secondary')}</option></SetupSelect></div>
                            <p className="text-sm leading-6 text-fg-muted">{t(draft.dns_role === 'secondary' ? 'setup.secondaryHelp' : 'setup.primaryHelp')}</p>
                            <p className="text-sm leading-6 text-fg-muted">{t('setup.dnsPairOrder')}</p>
                            <div className="grid gap-5 sm:grid-cols-2"><SetupInput name="ns1" label={t('setup.ns1')} value={draft.ns1} onChange={value => change('ns1', value)} placeholder="ns1.example.com" required /><SetupInput name="ns2" label={t('setup.ns2')} value={draft.ns2} onChange={value => change('ns2', value)} placeholder="ns2.example.com" required /><SetupInput name="local_ip" label={t('setup.localIP')} value={draft.local_ip} onChange={value => change('local_ip', value)} placeholder={snapshot.server_ip || ''} required /><SetupInput name="peer_ip" label={t('setup.peerIP')} value={draft.peer_ip} onChange={value => change('peer_ip', value)} required /><SetupInput name="peer_ns" label={t('setup.peerNS')} value={draft.peer_ns} onChange={value => change('peer_ns', value)} required /></div>
                        </div>}
                        {draft.purpose === 'application' && <SetupNodeVersion value={draft.node_version} onChange={value => change('node_version', value)} />}
                        {draft.purpose === 'application' && <SetupSelect name="database" label={t('setup.database')} value={draft.database} onChange={value => change('database', value)}><option value="">{t('setup.databaseNone')}</option><option value="mariadb">MariaDB</option><option value="postgresql">PostgreSQL</option></SetupSelect>}
                    </fieldset>}
                    {step === 'review' && plan && <section aria-labelledby="setup-plan-title">
                        <h2 id="setup-plan-title" className="text-xl font-semibold">{t('setup.reviewTitle')}</h2><p className="mt-3 text-sm leading-6 text-fg-muted">{t('setup.reviewHelp')}</p>
                        <ol className="mt-5 divide-y divide-border">{plan.steps.map(item => <li key={item.id} className="py-4"><p className="font-medium">{t(`setup.kind.${item.kind}`, { target: stepTarget(item.kind, item.target) })}</p>{item.qualifier && <p className="mt-1 text-sm text-fg-muted">{item.qualifier}</p>}</li>)}</ol>
                        <dl className="mt-5 space-y-3 rounded-lg bg-surface-2 p-4 text-sm"><div><dt className="font-semibold">{t('setup.firewallReview')}</dt><dd className="mt-1 leading-6 text-fg-muted">{t('setup.firewallHelp')}</dd></div><div><dt className="font-semibold">TCP</dt><dd className="mt-1 break-words tabular-nums">{plan.tcp_ports.join(', ') || t('setup.noPorts')}</dd></div><div><dt className="font-semibold">UDP</dt><dd className="mt-1 break-words tabular-nums">{plan.udp_ports.join(', ') || t('setup.noPorts')}</dd></div><div><dt className="font-semibold">{t('setup.certificateContact')}</dt><dd className="mt-1 break-all">{plan.contact_email}</dd></div>{plan.hostname_change && <div><dt className="font-semibold">{t('setup.hostnameChange')}</dt><dd className="mt-1 break-all">{plan.hostname_change}</dd></div>}</dl>
                        {plan.remote_dns_connection && <div className="mt-5 rounded-lg border border-border p-4 text-sm"><p className="font-semibold">{t('setup.remote.reviewTitle')}</p><p className="mt-2 break-all">{plan.remote_dns_connection.endpoint}</p><p className="mt-1 break-words text-fg-muted">{plan.remote_dns_connection.nameservers.join(', ')}</p><p className="mt-2 leading-6 text-fg-muted">{t('setup.remote.reviewHelp')}</p></div>}
                        {draft.dns_mode === 'external' && <p className="mt-5 text-sm leading-6 text-fg-muted">{t('setup.externalAfter')}</p>}
                        {isMail && <p className="mt-4 text-sm leading-6 text-fg-muted">{t('setup.mailAfter')}</p>}
                        {plan.blockers.length > 0 && <div role="alert" className="mt-5 rounded-lg border border-warning-mark/40 bg-warning-mark/10 p-4"><p className="font-semibold">{t('setup.planBlocked')}</p><ul className="mt-3 list-disc space-y-2 pl-5 text-sm">{plan.blockers.map(code => <li key={code}>{failureText(code)}<details className="mt-1 text-xs text-fg-muted"><summary className="cursor-pointer">{t('setup.details')}</summary><code className="mt-1 block break-words">{code}</code></details></li>)}</ul></div>}
                        {plan.can_start && <label className="mt-6 flex cursor-pointer items-start gap-3 text-sm leading-6"><input type="checkbox" checked={acknowledged} onChange={event => setAcknowledged(event.target.checked)} className="mt-1 h-4 w-4 shrink-0 accent-primary" /><span>{t('setup.confirm')}</span></label>}
                    </section>}
                    <div className="mt-8 flex flex-wrap items-center gap-4 border-t border-border pt-6">
                        {step !== 'purpose' && <Button type="button" variant="secondary" disabled={busy} onClick={() => { setStep(step === 'review' ? 'access' : 'purpose'); setPlan(null); setAcknowledged(false); }}>{t('setup.back')}</Button>}
                        {step === 'review' ? <Button type="button" variant="primary" disabled={busy || !plan?.can_start || !acknowledged} onClick={() => void start()}>{t('setup.start')}</Button> : <button type="submit" disabled={busy || (step === 'access' && draft.dns_mode === 'existing' && !remoteVerified)} className="inline-flex items-center gap-3 rounded-lg bg-primary px-5 py-2.5 font-semibold text-primary-fg hover:bg-primary/90 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary disabled:cursor-not-allowed disabled:opacity-60">{busy ? t('common.loading') : t(step === 'purpose' ? 'setup.continue' : 'setup.review')}<ArrowRight className="h-4 w-4" aria-hidden="true" /></button>}
                    </div>
                </form>}
            {!completed && !hasOperation && <ServerSetupManualAction snapshot={snapshot} onChosen={next => { accept(next); setManualExit(true); }} />}
        </>}
    </ServerSetupShell>;
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
