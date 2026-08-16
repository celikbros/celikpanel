import { useCallback, useEffect, useState } from 'react';
import { Link } from '../router';
import { Network, Server, Check, AlertTriangle, Circle, Loader2 } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { dnsEngineText } from '../i18n/dnsEngine';
import { Button, ErrorBanner, Field, inputClass, StatusDot } from './ui';
import { readApiError, apiErrorText, type ApiError } from '../lib/apiError';
import type { DNSEngineSnapshot } from '../lib/dnsEngineContract';
import { HelpButton } from './HelpDrawer';
import { DNSEngineCard } from './DNSEngineCard';

type DNSRole = 'standalone' | 'paired';
type DraftDNSRole = DNSRole | '';
type ActiveDNSEngine = 'pdns' | 'bind';

interface NSFact {
    host: string;
    ips: string[];
    points_here: boolean;
}

interface Step {
    code: 'aloneNoBackup' | 'localName' | 'peerName' | 'peerPort' | 'samePairOnPeer';
    done: boolean;
    manual: boolean;
    args?: string[];
}

interface NameserverResponse {
    ns1: string;
    ns2: string;
    derived: boolean;
    server_ip: string;
    facts: NSFact[];
    usable: boolean;
}

interface ClusterResponse {
    configured: boolean;
    role: string;
    peer_ip: string;
    peer_ns: string;
    peer_reachable: boolean;
    server_ip: string;
    ns1: string;
    ns2: string;
    suggested_local_ns?: string;
    suggested_peer_ns?: string;
    suggested_peer_ip?: string;
    facts: NSFact[];
    steps: Step[];
    dns_service_known?: boolean;
    dns_service_ready?: boolean;
    dns_service_detail?: string;
}

interface SavedSettings extends Omit<ClusterResponse, 'role' | 'configured'> {
    configured: boolean;
    role: DNSRole;
    namesDerived: boolean;
    namesUsable: boolean;
}

interface SettingsDraft {
    ns1: string;
    ns2: string;
    role: DraftDNSRole;
    peer_ip: string;
    peer_ns: string;
}

type ClusterDraft = Pick<SettingsDraft, 'role' | 'peer_ip' | 'peer_ns'>;
type ClusterBlocker = 'busy' | 'dnsService' | 'names' | 'chooseRole' | 'peerIp' | 'peerIpSame' | 'peerNs' | null;
type WizardStep = 1 | 2 | 3;

// API responses produced before the fresh-install fix can contain
// `ips: null` when a nameserver does not resolve yet. Treat response data as
// untrusted at this boundary so one missing DNS answer cannot blank the whole
// Settings page.
function normalizeFacts(value: unknown): NSFact[] {
    if (!Array.isArray(value)) return [];

    return value.flatMap((item): NSFact[] => {
        if (!item || typeof item !== 'object') return [];
        const fact = item as Partial<NSFact>;
        if (typeof fact.host !== 'string') return [];
        return [{
            host: fact.host,
            ips: Array.isArray(fact.ips)
                ? fact.ips.filter((ip): ip is string => typeof ip === 'string')
                : [],
            points_here: fact.points_here === true,
        }];
    });
}

function cleanHostname(value: string): string {
    return value.trim().toLowerCase().replace(/\.$/, '');
}

function normalizeRole(role: string): DNSRole {
    return role === 'paired' || role === 'primary' || role === 'secondary' ? 'paired' : 'standalone';
}

function canonicalIPv4(value: string): string {
    const octets = value.trim().split('.');
    if (octets.length !== 4) return '';
    const parsed = octets.map((octet) => {
        if (!/^\d{1,3}$/.test(octet)) return -1;
        const number = Number(octet);
        return number >= 0 && number <= 255 ? number : -1;
    });
    return parsed.some((octet) => octet < 0) ? '' : parsed.join('.');
}

// Keep browser readiness aligned with Go's net.IP.IsGlobalUnicast check in
// the atomic DNS setup endpoint. Private addresses remain valid for peers on
// an operator-controlled network; unspecified, loopback, multicast,
// link-local and limited-broadcast addresses do not.
function isGlobalUnicastIPv4(value: string): boolean {
    const canonical = canonicalIPv4(value);
    if (canonical === '') return false;
    const [a, b] = canonical.split('.').map(Number);
    return canonical !== '0.0.0.0' &&
        canonical !== '255.255.255.255' &&
        a !== 127 &&
        !(a === 169 && b === 254) &&
        !(a >= 224 && a <= 239);
}

function isValidNameserver(value: string): boolean {
    const hostname = cleanHostname(value);
    if (hostname.length < 3 || hostname.length > 253 || !hostname.includes('.')) return false;
    return hostname.split('.').every((label) =>
        label.length > 0 &&
        label.length <= 63 &&
        /^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/.test(label),
    );
}

function otherNameserver(name: string, ns1: string, ns2: string): string {
    const cleanName = cleanHostname(name);
    if (cleanName === ns1) return ns2;
    if (cleanName === ns2) return ns1;
    return '';
}

function SetupStep({ number, title, description, complete = false }: {
    number: number;
    title: string;
    description: string;
    complete?: boolean;
}) {
    return (
        <div className="mb-4 flex items-start gap-3">
            <span className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-full text-sm font-semibold ${
                complete ? 'bg-success/10 text-success' : 'bg-primary/10 text-primary'
            }`}>
                {complete ? <Check className="h-4 w-4" /> : number}
            </span>
            <div className="min-w-0">
                <h3 className="text-sm font-semibold text-fg">{title}</h3>
                <p className="mt-0.5 text-xs leading-relaxed text-fg-muted">{description}</p>
            </div>
        </div>
    );
}

export function DNSServerSettings() {
    const { locale } = useI18n();
    const et = (key: Parameters<typeof dnsEngineText>[1]) => dnsEngineText(locale, key);
    const [engine, setEngine] = useState<DNSEngineSnapshot | null>(null);
    const activeEngine: ActiveDNSEngine | null = engine?.state === 'ready' &&
        (engine.active_engine === 'pdns' || engine.active_engine === 'bind')
        ? engine.active_engine
        : null;

    return (
        <div>
            <DNSEngineCard onSnapshotChange={setEngine} />
            {activeEngine ? (
                <DNSInfrastructureSettings
                    key={activeEngine}
                    activeEngine={activeEngine}
                />
            ) : (
                <section className="rounded-xl border border-border bg-surface p-4 sm:p-6">
                    <div className="flex items-start gap-3">
                        <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-warning/10 text-warning">
                            <AlertTriangle className="h-4.5 w-4.5" />
                        </span>
                        <div className="min-w-0">
                            <h2 className="text-base font-semibold text-fg">{et('dnsEngine.topologyEditorTitle')}</h2>
                            <p className="mt-1 text-sm leading-relaxed text-fg-muted">
                                {engine?.active_engine === 'bind'
                                    ? et('dnsEngine.topologyEditorBind')
                                    : engine?.state === 'switching'
                                      ? et('dnsEngine.topologyEditorSwitching')
                                      : engine?.state === 'unmanaged' || engine?.state === 'conflict' || engine?.state === 'degraded'
                                        ? et('dnsEngine.topologyEditorUnsafe')
                                        : et('dnsEngine.topologyEditorUnconfigured')}
                            </p>
                        </div>
                    </div>
                </section>
            )}
        </div>
    );
}

function DNSInfrastructureSettings({ activeEngine }: { activeEngine: ActiveDNSEngine }) {
    const { t, locale } = useI18n();
    const et = (key: Parameters<typeof dnsEngineText>[1]) => dnsEngineText(locale, key);
    const [saved, setSaved] = useState<SavedSettings | null>(null);
    const [draft, setDraft] = useState<SettingsDraft | null>(null);
    const [busy, setBusy] = useState(false);
    const [needsClusterRetry, setNeedsClusterRetry] = useState(false);
    const [apiError, setApiError] = useState<ApiError | null>(null);
    const [activeStep, setActiveStep] = useState<WizardStep>(1);
    const [detectedPeerStaged, setDetectedPeerStaged] = useState(false);

    const load = useCallback(async (preserveCluster?: ClusterDraft): Promise<boolean> => {
        try {
            const [namesResponse, clusterResponse] = await Promise.all([
                fetch('/api/v1/settings/nameservers'),
                fetch('/api/v1/settings/dns-cluster'),
            ]);
            if (!namesResponse.ok) {
                setApiError(await readApiError(namesResponse));
                return false;
            }
            if (!clusterResponse.ok) {
                setApiError(await readApiError(clusterResponse));
                return false;
            }
            const names = await namesResponse.json() as NameserverResponse;
            const cluster = await clusterResponse.json() as ClusterResponse;

            const role = activeEngine === 'bind' ? 'standalone' : normalizeRole(cluster.role);
            const ns1 = cleanHostname(names.ns1 || cluster.ns1 || '');
            const ns2 = cleanHostname(names.ns2 || cluster.ns2 || '');
            const snapshot: SavedSettings = {
                ...cluster,
                configured: cluster.configured === true,
                role,
                ns1,
                ns2,
                suggested_local_ns: cleanHostname(cluster.suggested_local_ns ?? ''),
                suggested_peer_ns: cleanHostname(cluster.suggested_peer_ns ?? ''),
                suggested_peer_ip: cluster.suggested_peer_ip?.trim() ?? '',
                server_ip: cluster.server_ip || names.server_ip,
                facts: normalizeFacts(cluster.facts ?? names.facts),
                namesDerived: names.derived === true,
                namesUsable: names.usable === true,
            };
            setSaved(snapshot);

            const savedPeerNames = [ns1, ns2].filter(Boolean);
            const preservedPeerNS = cleanHostname(preserveCluster?.peer_ns ?? '');
            const storedPeerNS = cleanHostname(cluster.peer_ns ?? '');
            const serverIPv4 = canonicalIPv4(snapshot.server_ip);
            const storedPeerIPv4 = canonicalIPv4(cluster.peer_ip ?? '');
            const suggestedLocalNS = cleanHostname(snapshot.suggested_local_ns ?? '');
            const suggestedPeerNS = cleanHostname(snapshot.suggested_peer_ns ?? '');
            const suggestedPeerIPv4 = canonicalIPv4(snapshot.suggested_peer_ip ?? '');
            const suggestionValid =
                savedPeerNames.length === 2 &&
                savedPeerNames.includes(suggestedLocalNS) &&
                savedPeerNames.includes(suggestedPeerNS) &&
                suggestedLocalNS !== suggestedPeerNS &&
                isGlobalUnicastIPv4(suggestedPeerIPv4) &&
                suggestedPeerIPv4 !== serverIPv4;
            const storedPeerValid =
                isGlobalUnicastIPv4(storedPeerIPv4) &&
                storedPeerIPv4 !== serverIPv4 &&
                savedPeerNames.includes(storedPeerNS) &&
                otherNameserver(storedPeerNS, ns1, ns2) !== '';
            const storedPeerMatchesSuggestion =
                storedPeerValid &&
                storedPeerIPv4 === suggestedPeerIPv4 &&
                storedPeerNS === suggestedPeerNS &&
                otherNameserver(storedPeerNS, ns1, ns2) === suggestedLocalNS;
            const autoStageDetectedPeer =
                preserveCluster === undefined &&
                snapshot.configured &&
                role === 'paired' &&
                !storedPeerMatchesSuggestion &&
                suggestionValid;
            const draftPeerNS = preserveCluster !== undefined
                ? (savedPeerNames.includes(preservedPeerNS) ? preservedPeerNS : '')
                : autoStageDetectedPeer
                  ? suggestedPeerNS
                  : savedPeerNames.includes(storedPeerNS)
                    ? storedPeerNS
                    : '';
            setDetectedPeerStaged(autoStageDetectedPeer);
            setDraft({
                ns1,
                ns2,
                role: activeEngine === 'bind'
                    ? 'standalone'
                    : preserveCluster?.role ?? (snapshot.configured ? role : ''),
                peer_ip: preserveCluster?.peer_ip ?? (autoStageDetectedPeer ? suggestedPeerIPv4 : cluster.peer_ip ?? ''),
                peer_ns: draftPeerNS,
            });
            setApiError(null);
            return true;
        } catch {
            const error = { message: t('common.error') };
            setApiError(error);
            showToast('error', error.message);
            return false;
        }
    }, [activeEngine, t]);

    useEffect(() => {
        void load();
    }, [load]);

    const saveAndPublish = async () => {
        if (!draft || draft.role === '') return;
        setBusy(true);
        setApiError(null);
        try {
            const draftNS1 = cleanHostname(draft.ns1);
            const draftNS2 = cleanHostname(draft.ns2);
            const effectiveRole: DNSRole = activeEngine === 'bind' ? 'standalone' : draft.role;
            const paired = effectiveRole === 'paired';
            const setupResponse = await fetch('/api/v1/settings/dns-setup', {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    ns1: draftNS1,
                    ns2: draftNS2,
                    role: effectiveRole,
                    peer_ip: paired ? canonicalIPv4(draft.peer_ip) || draft.peer_ip.trim() : '',
                    peer_ns: paired ? cleanHostname(draft.peer_ns) : '',
                }),
            });
            if (!setupResponse.ok) {
                const error = await readApiError(setupResponse);
                if (error.code === 'DNS_PUBLICATION_FAILED') {
                    const reloaded = await load();
                    setNeedsClusterRetry(true);
                    setActiveStep(3);
                    if (reloaded) setApiError(error);
                    showToast('warning', t('dnssrv.publicationPending'));
                } else {
                    setNeedsClusterRetry(false);
                    setApiError(error);
                    showToast('error', apiErrorText(error, t));
                }
                return;
            }
            setNeedsClusterRetry(false);
            if (await load()) {
                setActiveStep(3);
                showToast('success', t('dnssrv.setupSaved'));
            }
        } catch {
            const error = { message: t('dnssrv.applyIncomplete') };
            setNeedsClusterRetry(false);
            setApiError(error);
            showToast('error', error.message);
        } finally {
            setBusy(false);
        }
    };

    if (!saved || !draft) {
        return (
            <section className="rounded-xl border border-border bg-surface p-4 sm:p-6">
                <ErrorBanner error={apiError} />
                {!apiError && (
                    <div className="flex min-h-24 items-center justify-center text-fg-muted">
                        <Loader2 className="h-5 w-5 animate-spin" />
                    </div>
                )}
            </section>
        );
    }

    const draftNS1 = cleanHostname(draft.ns1);
    const draftNS2 = cleanHostname(draft.ns2);
    const namesDirty = draftNS1 !== saved.ns1 || draftNS2 !== saved.ns2;
    const effectivePeerIP = draft.role === 'paired' ? draft.peer_ip.trim() : '';
    const effectivePeerIPv4 = canonicalIPv4(effectivePeerIP);
    const effectivePeerNS = draft.role === 'paired' ? cleanHostname(draft.peer_ns) : '';
    const effectiveLocalNS = otherNameserver(effectivePeerNS, draftNS1, draftNS2);
    const savedPeerIP = saved.role === 'paired' ? saved.peer_ip : '';
    const savedPeerNS = saved.role === 'paired' ? cleanHostname(saved.peer_ns) : '';
    const savedDraftRole: DraftDNSRole = saved.configured ? saved.role : '';
    const clusterDirty =
        draft.role !== savedDraftRole || effectivePeerIP !== savedPeerIP || effectivePeerNS !== savedPeerNS;
    const checksCurrent = !namesDirty && !clusterDirty;
    const ns1Valid = isValidNameserver(draftNS1);
    const ns2Valid = isValidNameserver(draftNS2);
    const namesDistinct = draftNS1 !== draftNS2;
    const namesValid = ns1Valid && ns2Valid && namesDistinct;
    const namesReadyForSetup = namesValid;
    const nameserverNames = namesReadyForSetup ? [draftNS1, draftNS2] : [];
    const peerSelectionValid = nameserverNames.includes(effectivePeerNS);
    const localSelectionValid = nameserverNames.includes(effectiveLocalNS);
    const serverIPv4 = canonicalIPv4(saved.server_ip);
    const peerIPUsable = draft.role !== 'paired' || isGlobalUnicastIPv4(effectivePeerIPv4);
    const peerIPInvalid = draft.role === 'paired' && effectivePeerIP !== '' && !peerIPUsable;
    const peerIPSame =
        draft.role === 'paired' && effectivePeerIPv4 !== '' && serverIPv4 !== '' && effectivePeerIPv4 === serverIPv4;
    const hasChanges = namesDirty || saved.namesDerived || clusterDirty || !saved.configured;
    // This editor is rendered for BIND only from an exact `state=ready`
    // engine snapshot. A legacy PowerDNS-only readiness response must not turn
    // that already-proven authority back into a false missing-service state.
    const dnsServiceKnown = activeEngine === 'bind' || saved.dns_service_known === true;
    const dnsServiceReady = activeEngine === 'bind' || saved.dns_service_ready === true;
    const dnsServiceMissing = dnsServiceKnown && !dnsServiceReady;

    const savedNameserverNames = [saved.ns1, saved.ns2].filter(Boolean);
    const rawSuggestedLocalNS = cleanHostname(saved.suggested_local_ns ?? '');
    const rawSuggestedPeerNS = cleanHostname(saved.suggested_peer_ns ?? '');
    const suggestionMatchesSavedPair =
        !namesDirty &&
        savedNameserverNames.length === 2 &&
        savedNameserverNames.includes(rawSuggestedLocalNS) &&
        savedNameserverNames.includes(rawSuggestedPeerNS) &&
        rawSuggestedLocalNS !== rawSuggestedPeerNS;
    const suggestedLocalNS = suggestionMatchesSavedPair ? rawSuggestedLocalNS : '';
    const suggestedPeerNS = suggestionMatchesSavedPair ? rawSuggestedPeerNS : '';
    const suggestedPeerIPv4 = canonicalIPv4(saved.suggested_peer_ip ?? '');
    const safeSuggestedPeerIP =
        isGlobalUnicastIPv4(suggestedPeerIPv4) && (serverIPv4 === '' || suggestedPeerIPv4 !== serverIPv4)
            ? suggestedPeerIPv4
            : '';
    const detectedAssignmentAvailable =
        suggestionMatchesSavedPair && safeSuggestedPeerIP !== '' && suggestedLocalNS !== '' && suggestedPeerNS !== '';
    const detectedAssignmentNeedsApply = draft.role === 'paired' && detectedAssignmentAvailable && (
        effectivePeerIPv4 !== safeSuggestedPeerIP ||
        effectiveLocalNS !== suggestedLocalNS ||
        effectivePeerNS !== suggestedPeerNS
    );

    let clusterBlocker: ClusterBlocker = null;
    if (busy) clusterBlocker = 'busy';
    else if (dnsServiceMissing) clusterBlocker = 'dnsService';
    else if (!namesReadyForSetup) clusterBlocker = 'names';
    else if (draft.role === '') clusterBlocker = 'chooseRole';
    else if (draft.role === 'paired' && peerIPSame) clusterBlocker = 'peerIpSame';
    else if (draft.role === 'paired' && !peerIPUsable) clusterBlocker = 'peerIp';
    else if (draft.role === 'paired' && (!peerSelectionValid || !localSelectionValid)) clusterBlocker = 'peerNs';

    const blockerText = clusterBlocker === 'busy'
        ? t('dnssrv.blocker.busy')
        : clusterBlocker === 'dnsService'
          ? t('dnssrv.blocker.powerdns')
        : clusterBlocker === 'names'
          ? t('dnssrv.blocker.names')
          : clusterBlocker === 'chooseRole'
            ? t('dnssrv.blocker.chooseRole')
            : clusterBlocker === 'peerIp'
              ? t('dnssrv.blocker.peerIp')
              : clusterBlocker === 'peerIpSame'
                ? t('dnssrv.peerIpSame', { ip: saved.server_ip })
              : clusterBlocker === 'peerNs'
                ? t('dnssrv.blocker.peerNs')
                : needsClusterRetry
                  ? t('dnssrv.retryReady')
                : hasChanges
                  ? t('dnssrv.readyToSave')
                  : t('dnssrv.readyToVerify');

    const assignmentReady = draft.role === 'standalone' || (
        draft.role === 'paired' &&
        peerIPUsable &&
        !peerIPSame &&
        peerSelectionValid &&
        localSelectionValid
    );
    const stepTwoReady = namesReadyForSetup && draft.role !== '' && assignmentReady;
    const stepTwoBlockerText = !namesReadyForSetup
        ? t('dnssrv.blocker.names')
        : draft.role === ''
          ? t('dnssrv.blocker.chooseRole')
          : draft.role === 'paired' && peerIPSame
            ? t('dnssrv.peerIpSame', { ip: saved.server_ip })
            : draft.role === 'paired' && !peerIPUsable
              ? t('dnssrv.blocker.peerIp')
              : draft.role === 'paired' && (!peerSelectionValid || !localSelectionValid)
                ? t('dnssrv.blocker.peerNs')
                : t('dnssrv.stepContinueReady');

    const selectRole = (role: DNSRole) => {
        if (activeEngine === 'bind' && role === 'paired') return;
        setNeedsClusterRetry(false);
        setApiError(null);
        if (role === 'standalone') {
            setDetectedPeerStaged(false);
        }
        const selectedPeerIPv4 = canonicalIPv4(draft.peer_ip);
        const selectedPeerNS = cleanHostname(draft.peer_ns);
        const selectedLocalNS = otherNameserver(selectedPeerNS, draftNS1, draftNS2);
        const currentAssignmentStructurallyValid =
            isGlobalUnicastIPv4(selectedPeerIPv4) &&
            selectedPeerIPv4 !== serverIPv4 &&
            nameserverNames.includes(selectedPeerNS) &&
            nameserverNames.includes(selectedLocalNS);
        const currentAssignmentMatchesDetected =
            !detectedAssignmentAvailable ||
            (selectedPeerIPv4 === safeSuggestedPeerIP &&
                selectedPeerNS === suggestedPeerNS &&
                selectedLocalNS === suggestedLocalNS);
        const currentAssignmentValid =
            currentAssignmentStructurallyValid && currentAssignmentMatchesDetected;
        const useDetectedAssignment =
            role === 'paired' && detectedAssignmentAvailable && !currentAssignmentValid;
        if (useDetectedAssignment) {
            setDetectedPeerStaged(true);
        }
        setDraft((current) => {
            if (!current) return current;
            if (role === 'standalone') return { ...current, role };

            const currentPeerIP = current.peer_ip.trim();
            const currentPeerIPv4 = canonicalIPv4(currentPeerIP);
            const mayReplacePeerIP =
                currentPeerIP === '' || (currentPeerIPv4 !== '' && serverIPv4 !== '' && currentPeerIPv4 === serverIPv4);
            const currentPeerNS = cleanHostname(current.peer_ns);
            return {
                ...current,
                role,
                peer_ip: useDetectedAssignment
                    ? safeSuggestedPeerIP
                    : mayReplacePeerIP
                      ? safeSuggestedPeerIP
                      : current.peer_ip,
                peer_ns: useDetectedAssignment
                    ? suggestedPeerNS
                    : nameserverNames.includes(currentPeerNS)
                      ? current.peer_ns
                      : suggestedPeerNS || current.peer_ns,
            };
        });
    };

    const selectLocalNameserver = (localNS: string) => {
        setNeedsClusterRetry(false);
        setApiError(null);
        setDetectedPeerStaged(false);
        const peerNS = otherNameserver(localNS, draftNS1, draftNS2);
        setDraft((current) => (current ? { ...current, peer_ns: peerNS } : current));
    };

    const useDetectedPeer = () => {
        setNeedsClusterRetry(false);
        setApiError(null);
        setDetectedPeerStaged(true);
        setDraft((current) => current ? {
            ...current,
            role: 'paired',
            peer_ip: safeSuggestedPeerIP || current.peer_ip,
            peer_ns: suggestedPeerNS || current.peer_ns,
        } : current);
    };

    const primaryActionLabel = needsClusterRetry
        ? t('dnssrv.retryPublication')
        : t('dnssrv.saveAndPublish');
    const assignmentRows = draft.role === 'standalone'
        ? [
            { label: t('dnssrv.thisServer'), ip: saved.server_ip, name: draftNS1 },
            { label: t('dnssrv.thisServer'), ip: saved.server_ip, name: draftNS2 },
        ]
        : [
            { label: t('dnssrv.thisServer'), ip: saved.server_ip, name: effectiveLocalNS },
            { label: t('dnssrv.peerServer'), ip: effectivePeerIPv4, name: effectivePeerNS },
        ];

    return (
        <section className="rounded-xl border border-border bg-surface p-4 sm:p-6">
            <div className="mb-4 flex flex-wrap items-center gap-2">
                <span className="flex h-9 w-9 items-center justify-center rounded-lg bg-primary/10 text-primary">
                    <Network className="h-4.5 w-4.5" />
                </span>
                <div className="min-w-0">
                    <h2 className="text-base font-semibold text-fg">{t('dnssrv.title')}</h2>
                    <p className="text-sm text-fg-muted">{t('dnssrv.subtitle')}</p>
                </div>
                <div className="ml-auto">
                    <HelpButton serviceId="dns-server-settings" name={t('dnssrv.title')} />
                </div>
            </div>

            <ol className="mb-5 grid gap-2 sm:grid-cols-3" aria-label={t('dnssrv.wizardProgress')}>
                {([1, 2, 3] as const).map((step) => {
                    const reachable = step === 1 || (step === 2 && draft.role !== '') || (step === 3 && stepTwoReady);
                    const label = step === 1
                        ? t('dnssrv.modeChoiceTitle')
                        : step === 2
                          ? t('dnssrv.namesAndAssignmentTitle')
                          : t('dnssrv.reviewTitle');
                    return (
                        <li key={step}>
                            <button
                                type="button"
                                data-testid={`dns-wizard-step-${step}`}
                                disabled={!reachable}
                                onClick={() => setActiveStep(step)}
                                aria-current={activeStep === step ? 'step' : undefined}
                                className={`flex w-full items-center gap-3 rounded-lg border px-3 py-2.5 text-left transition-colors ${
                                    activeStep === step
                                        ? 'border-primary bg-primary/5 text-primary'
                                        : reachable
                                          ? 'border-border bg-surface text-fg hover:border-primary/40'
                                          : 'cursor-not-allowed border-border bg-surface-2/40 text-fg-subtle'
                                }`}
                            >
                                <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-current/10 text-xs font-semibold">
                                    {step}
                                </span>
                                <span className="text-xs font-semibold">{label}</span>
                            </button>
                        </li>
                    );
                })}
            </ol>

            <div className="flex flex-col gap-5">
                <div className={`${activeStep === 1 ? '' : 'hidden'} min-w-0 rounded-xl border border-border bg-surface-2/30 p-4`}>
                    <SetupStep
                        number={1}
                        title={t('dnssrv.modeChoiceTitle')}
                        description={t('dnssrv.modeChoiceDesc')}
                        complete={draft.role !== ''}
                    />
                    <fieldset className="grid gap-3 sm:grid-cols-2">
                        <legend className="sr-only">{t('dnssrv.roleTitle')}</legend>
                        {(['standalone', 'paired'] as const).map((role) => (
                            <label
                                key={role}
                                aria-disabled={activeEngine === 'bind' && role === 'paired'}
                                className={`flex items-start gap-3 rounded-xl border p-4 transition-colors ${
                                    activeEngine === 'bind' && role === 'paired'
                                        ? 'cursor-not-allowed border-border bg-surface-2/50 opacity-70'
                                        : draft.role === role
                                        ? 'border-primary bg-primary/5 ring-1 ring-primary/20'
                                        : 'cursor-pointer border-border bg-surface hover:border-primary/40'
                                }`}
                            >
                                <input
                                    type="radio"
                                    name="dns-role"
                                    className="mt-0.5"
                                    checked={draft.role === role}
                                    data-testid={`dns-role-${role}`}
                                    disabled={activeEngine === 'bind' && role === 'paired'}
                                    onChange={() => selectRole(role)}
                                />
                                <span className="min-w-0">
                                    <span className="block text-sm font-semibold text-fg">
                                        {t(`dnssrv.role.${role}` as Parameters<typeof t>[0])}
                                    </span>
                                    <span className="mt-1 block text-xs leading-relaxed text-fg-muted">
                                        {activeEngine === 'bind' && role === 'paired'
                                            ? et('dnsEngine.identity.bindPairedUnsupported')
                                            : t(`dnssrv.role.${role}.desc` as Parameters<typeof t>[0])}
                                    </span>
                                </span>
                            </label>
                        ))}
                    </fieldset>

                    {activeEngine === 'bind' && (
                        <div
                            className={'mt-4 rounded-xl border border-primary/25 bg-primary/5 p-3 text-xs leading-relaxed text-fg-muted'}
                            role={'note'}
                            data-testid={'bind-standalone-identity-note'}
                        >
                            <p className={'font-semibold text-fg'}>{et('dnsEngine.identity.bindTitle')}</p>
                            <p className={'mt-1'}>{et('dnsEngine.identity.bindDescription')}</p>
                        </div>
                    )}

                    {dnsServiceKnown && (
                        <div className={`mt-4 rounded-xl border p-3 ${
                            dnsServiceReady
                                ? 'border-success/25 bg-success/5'
                                : 'border-warning/30 bg-warning/5'
                        }`}>
                            <div className="flex flex-wrap items-center gap-3">
                                {dnsServiceReady
                                    ? <Check className="h-5 w-5 shrink-0 text-success" />
                                    : <AlertTriangle className="h-5 w-5 shrink-0 text-warning" />}
                                <div className="min-w-0 flex-1">
                                    <p className="text-sm font-semibold text-fg">
                                        {dnsServiceReady
                                            ? activeEngine === 'bind'
                                                ? et('dnsEngine.identity.bindReady')
                                                : t('dnssrv.requirement.powerdnsReady')
                                            : t('dnssrv.requirement.powerdnsTitle')}
                                    </p>
                                    {!dnsServiceReady && (
                                        <p className="mt-1 text-xs leading-relaxed text-fg-muted">
                                            {saved.dns_service_detail || t('dnssrv.requirement.powerdnsMissing')}
                                        </p>
                                    )}
                                </div>
                                {!dnsServiceReady && (
                                    <Link
                                        to="/services"
                                        className="rounded-lg border border-warning/40 bg-surface px-3 py-2 text-xs font-semibold text-fg hover:border-warning"
                                    >
                                        {t('dnssrv.requirement.openComponents')}
                                    </Link>
                                )}
                            </div>
                        </div>
                    )}

                    <div className="mt-5 flex justify-end">
                        <Button
                            variant="primary"
                            data-testid="dns-wizard-continue-mode"
                            onClick={() => setActiveStep(2)}
                            disabled={draft.role === ''}
                        >
                            {t('dnssrv.continue')}
                        </Button>
                    </div>
                    {draft.role === '' && (
                        <p className="mt-2 text-right text-xs text-warning">{t('dnssrv.blocker.chooseRole')}</p>
                    )}
                </div>

                <div className={`${activeStep === 2 ? '' : 'hidden'} min-w-0 rounded-xl border border-border bg-surface-2/30 p-4`}>
                    <SetupStep
                        number={2}
                        title={t('dnssrv.namesTitle')}
                        description={t('dnssrv.namesHint')}
                        complete={namesReadyForSetup}
                    />
                    {saved.namesDerived && !namesDirty && (
                        <p className="mb-3 rounded-lg border border-primary/20 bg-primary/5 px-3 py-2 text-xs leading-relaxed text-fg-muted">
                            {t('dnssrv.namesInferredReview')}
                        </p>
                    )}
                    <div className="grid gap-3 md:grid-cols-2">
                        <Field label={t('dnssrv.ns1Label')} htmlFor="dns-ns1">
                            <input
                                id="dns-ns1"
                                className={inputClass}
                                value={draft.ns1}
                                onChange={(e) => {
                                    setNeedsClusterRetry(false);
                                    setApiError(null);
                                    setDetectedPeerStaged(false);
                                    setDraft((current) => (current ? { ...current, ns1: e.target.value } : current));
                                }}
                                placeholder="ns1.example.com"
                                aria-invalid={!ns1Valid}
                            />
                            {!ns1Valid && <p className="mt-1.5 text-xs text-danger">{t('dnssrv.namesInvalid')}</p>}
                        </Field>
                        <Field label={t('dnssrv.ns2Label')} htmlFor="dns-ns2">
                            <input
                                id="dns-ns2"
                                className={inputClass}
                                value={draft.ns2}
                                onChange={(e) => {
                                    setNeedsClusterRetry(false);
                                    setApiError(null);
                                    setDetectedPeerStaged(false);
                                    setDraft((current) => (current ? { ...current, ns2: e.target.value } : current));
                                }}
                                placeholder="ns2.example.com"
                                aria-invalid={!ns2Valid || !namesDistinct}
                            />
                            {!ns2Valid && <p className="mt-1.5 text-xs text-danger">{t('dnssrv.namesInvalid')}</p>}
                            {ns2Valid && !namesDistinct && (
                                <p className="mt-1.5 text-xs text-danger">{t('dnssrv.namesMustDiffer')}</p>
                            )}
                        </Field>
                    </div>
                    <p className="mt-3 rounded-lg border border-primary/15 bg-primary/5 px-3 py-2 text-xs leading-relaxed text-fg-muted">
                        {t('dnssrv.namesOrderHint')}
                    </p>
                </div>

                <div className={`${activeStep === 2 || activeStep === 3 ? '' : 'hidden'} min-w-0 rounded-xl border border-border bg-surface-2/30 p-4`}>
                    {activeStep === 2 && (
                        <>
                            <div className="mb-4">
                                <h3 className="text-sm font-semibold text-fg">{t('dnssrv.assignmentTitle')}</h3>
                                <p className="mt-1 text-xs leading-relaxed text-fg-muted">
                                    {draft.role === 'paired'
                                        ? t('dnssrv.assignmentPairedDesc')
                                        : t('dnssrv.assignmentStandaloneDesc')}
                                </p>
                            </div>

                            {draft.role === 'paired' && (
                        <>
                            {detectedPeerStaged && (
                                <div className="mb-4 rounded-xl border border-success/30 bg-success/5 p-3 text-xs leading-relaxed text-fg-muted">
                                    <p className="font-semibold text-success">{t('dnssrv.peerCorrectionTitle')}</p>
                                    <p className="mt-1">
                                        {t('dnssrv.peerCorrectionStaged', {
                                            ip: safeSuggestedPeerIP,
                                            name: suggestedPeerNS,
                                        })}
                                    </p>
                                </div>
                            )}
                            {detectedAssignmentNeedsApply && (
                                <div className="mb-4 rounded-xl border border-warning/30 bg-warning/5 p-3">
                                    <div className="flex flex-wrap items-center justify-between gap-3">
                                        <div className="min-w-0 text-xs text-fg-muted">
                                            <p className="mb-1 font-semibold text-fg">{t('dnssrv.detectedAssignment')}</p>
                                            <p className="break-all font-mono">
                                                {t('dnssrv.thisServer')}: {saved.server_ip || '—'} → {suggestedLocalNS}
                                            </p>
                                            <p className="mt-1 break-all font-mono">
                                                {t('dnssrv.peerServer')}: {safeSuggestedPeerIP} → {suggestedPeerNS}
                                            </p>
                                        </div>
                                        <button
                                            type="button"
                                            onClick={useDetectedPeer}
                                            className="rounded-lg bg-primary px-3 py-2 text-xs font-semibold text-white transition-opacity hover:opacity-90"
                                        >
                                            {t('dnssrv.useDetectedPeer')}
                                        </button>
                                    </div>
                                </div>
                            )}

                            <div className="mb-4 space-y-3">
                                <div className="rounded-xl border border-primary/25 bg-primary/5 p-4">
                                    <p className="mb-3 text-xs font-semibold uppercase tracking-wide text-primary">
                                        {t('dnssrv.thisServer')}
                                    </p>
                                    <div className="grid gap-3 sm:grid-cols-2">
                                        <Field label={t('dnssrv.ipv4Label')} htmlFor="dns-local-ip">
                                            <input id="dns-local-ip" className={inputClass} value={saved.server_ip} readOnly />
                                        </Field>
                                        <Field label={t('dnssrv.localNsLabel')} hint={t('dnssrv.localNsHint')} htmlFor="dns-local-ns">
                                            <select
                                                id="dns-local-ns"
                                                className={inputClass}
                                                value={effectiveLocalNS}
                                                disabled={!namesReadyForSetup}
                                                onChange={(e) => selectLocalNameserver(e.target.value)}
                                                aria-invalid={namesReadyForSetup && !localSelectionValid}
                                                aria-describedby={namesReadyForSetup && !localSelectionValid ? 'dns-local-ns-error' : undefined}
                                            >
                                                <option value="">{t('dnssrv.localNsPlaceholder')}</option>
                                                {nameserverNames.map((name) => (
                                                    <option key={name} value={name}>
                                                        {name}{name === suggestedLocalNS ? ` — ${t('dnssrv.recommended')}` : ''}
                                                    </option>
                                                ))}
                                            </select>
                                            {!localSelectionValid && namesReadyForSetup && (
                                                <p id="dns-local-ns-error" className="mt-1.5 text-xs text-danger">
                                                    {t('dnssrv.localNsRequired')}
                                                </p>
                                            )}
                                        </Field>
                                    </div>
                                </div>

                                <div className="rounded-xl border border-border bg-surface p-4">
                                    <p className="mb-3 text-xs font-semibold uppercase tracking-wide text-fg-muted">
                                        {t('dnssrv.peerServer')}
                                    </p>
                                    <div className="grid gap-3 sm:grid-cols-2">
                                        <Field label={t('dnssrv.peerIpLabel')} htmlFor="dns-peer-ip">
                                            <input
                                                id="dns-peer-ip"
                                                data-testid="dns-peer-ip"
                                                className={inputClass}
                                                value={draft.peer_ip}
                                                onChange={(e) => {
                                                    setNeedsClusterRetry(false);
                                                    setApiError(null);
                                                    setDetectedPeerStaged(false);
                                                    setDraft((current) => (
                                                        current ? { ...current, peer_ip: e.target.value } : current
                                                    ));
                                                }}
                                                placeholder={t('dnssrv.peerIpPlaceholder')}
                                                aria-invalid={peerIPInvalid || peerIPSame}
                                                aria-describedby={
                                                    peerIPInvalid
                                                        ? 'dns-peer-ip-invalid'
                                                        : peerIPSame
                                                          ? 'dns-peer-ip-same'
                                                          : undefined
                                                }
                                            />
                                            {peerIPInvalid && (
                                                <p id="dns-peer-ip-invalid" className="mt-1.5 text-xs text-danger">
                                                    {t('dnssrv.peerIpInvalid')}
                                                </p>
                                            )}
                                            {peerIPSame && (
                                                <p id="dns-peer-ip-same" className="mt-1.5 text-xs text-danger">
                                                    {t('dnssrv.peerIpSame', { ip: saved.server_ip })}
                                                </p>
                                            )}
                                        </Field>
                                        <Field label={t('dnssrv.peerNsLabel')} hint={t('dnssrv.peerNameAutomatic')} htmlFor="dns-peer-ns">
                                            <input id="dns-peer-ns" className={inputClass} value={effectivePeerNS} readOnly />
                                        </Field>
                                    </div>
                                </div>
                            </div>

                            {saved.configured && saved.role === 'paired' && saved.peer_ip && (
                                <div className="mb-3 rounded-lg border border-border bg-surface-2/50 p-3">
                                    <p className="flex items-center gap-1.5 text-xs">
                                        <Server className="h-3.5 w-3.5 text-fg-muted" />
                                        <StatusDot ok={saved.peer_reachable} />
                                        <span className="text-fg-muted">
                                            {saved.peer_reachable
                                                ? t('dnssrv.peerTcpReachable', { ip: saved.peer_ip })
                                                : t('dnssrv.peerTcpUnreachable', { ip: saved.peer_ip })}
                                        </span>
                                    </p>
                                    <p className="mt-1 text-xs text-fg-subtle">{t('dnssrv.peerTcpOnly')}</p>
                                </div>
                            )}
                        </>
                            )}

                            <div
                                id="dns-step-two-readiness"
                                aria-live="polite"
                                className={`mt-4 rounded-lg border p-3 text-xs leading-relaxed ${
                                    stepTwoReady
                                        ? 'border-success/25 bg-success/5 text-success'
                                        : 'border-warning/25 bg-warning/5 text-fg-muted'
                                }`}
                            >
                                {stepTwoBlockerText}
                            </div>
                            <div className="mt-5 flex flex-wrap justify-between gap-3">
                                <Button variant="secondary" onClick={() => setActiveStep(1)}>
                                    {t('common.back')}
                                </Button>
                                <Button
                                    variant="primary"
                                    data-testid="dns-wizard-continue-assignment"
                                    onClick={() => setActiveStep(3)}
                                    disabled={!stepTwoReady}
                                    aria-describedby="dns-step-two-readiness"
                                >
                                    {t('dnssrv.continue')}
                                </Button>
                            </div>
                        </>
                    )}

                    {activeStep === 3 && (
                        <>
                            <SetupStep
                                number={3}
                                title={t('dnssrv.reviewTitle')}
                                description={blockerText}
                                complete={checksCurrent && saved.configured && !needsClusterRetry}
                            />
                    {draft.role === 'standalone' && (
                        <div className="mb-3 rounded-lg border border-primary/20 bg-primary/5 p-3">
                            <p className="mb-1 text-xs font-semibold uppercase tracking-wide text-primary">{t('dnssrv.thisServer')}</p>
                            <p className="font-mono text-xs text-fg-muted">{saved.server_ip || '—'}</p>
                            <p className="mt-1 text-sm font-medium text-fg">{t('dnssrv.bothNames')}</p>
                        </div>
                    )}
                    <div
                        aria-live="polite"
                        aria-label={t('dnssrv.assignmentSummary')}
                        className="mb-3 grid gap-2 md:grid-cols-2"
                    >
                        {assignmentRows.map((row, index) => (
                            <div key={`${row.label}-${index}`} className="rounded-lg border border-border bg-surface p-3">
                                <p className="text-xs font-semibold uppercase tracking-wide text-fg-muted">{row.label}</p>
                                <p className="mt-1 break-all font-mono text-xs text-fg">{row.name || '—'}</p>
                                <p className="mt-1 break-all font-mono text-xs text-fg-muted">{row.ip || '—'}</p>
                            </div>
                        ))}
                    </div>
                    <div
                        id="dns-setup-readiness"
                        aria-live="polite"
                        className={`mb-3 rounded-lg border p-3 text-xs leading-relaxed ${
                            clusterBlocker === null
                                ? 'border-success/25 bg-success/5 text-success'
                                : 'border-warning/25 bg-warning/5 text-fg-muted'
                        }`}
                    >
                        {blockerText}
                    </div>
                    {dnsServiceMissing && (
                        <div className="mb-3 flex flex-wrap items-center gap-3 rounded-lg border border-warning/30 bg-warning/5 p-3">
                            <AlertTriangle className="h-4 w-4 shrink-0 text-warning" />
                            <p className="min-w-0 flex-1 text-xs leading-relaxed text-fg-muted">
                                {t('dnssrv.requirement.powerdnsMissing')}
                            </p>
                            <Link
                                to="/services"
                                className="rounded-lg border border-warning/40 bg-surface px-3 py-2 text-xs font-semibold text-fg hover:border-warning"
                            >
                                {t('dnssrv.requirement.openComponents')}
                            </Link>
                        </div>
                    )}
                    {apiError && <ErrorBanner error={apiError} className="mb-3" />}
                    {needsClusterRetry && (
                        <p className="mb-3 rounded-lg border border-warning/25 bg-warning/5 px-3 py-2 text-xs leading-relaxed text-fg-muted">
                            {t('dnssrv.publicationPending')}
                        </p>
                    )}
                    <div className="flex flex-col-reverse gap-3 sm:flex-row sm:justify-between">
                        <Button variant="secondary" onClick={() => setActiveStep(2)} disabled={busy}>
                            {t('common.back')}
                        </Button>
                        <Button
                            variant="primary"
                            data-testid="dns-wizard-save"
                            className="justify-center py-2.5 sm:min-w-64"
                            onClick={saveAndPublish}
                            disabled={clusterBlocker !== null}
                            aria-describedby="dns-setup-readiness"
                        >
                            {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : <Check className="h-4 w-4" />}
                            {primaryActionLabel}
                        </Button>
                    </div>

                    {!checksCurrent && (
                        <div className="mt-4 rounded-lg border border-primary/20 bg-primary/5 p-3 text-xs leading-relaxed text-fg-muted">
                            {t('dnssrv.verificationPrevious')}
                        </div>
                    )}

                    {saved.configured && saved.steps?.length > 0 && (
                        <div className="mt-5 rounded-xl border border-border bg-surface-2/50 p-4">
                            <p className="mb-2 text-sm font-medium text-fg">{t('dnssrv.stepsTitle')}</p>
                            <ul className="space-y-2">
                                {saved.steps.map((step, index) => (
                                    <li key={`${step.code}-${index}`} className="flex items-start gap-2.5 text-xs leading-relaxed">
                                        {step.done ? (
                                            <Check className="mt-0.5 h-4 w-4 shrink-0 text-success" />
                                        ) : step.manual ? (
                                            <Circle className="mt-0.5 h-4 w-4 shrink-0 text-fg-subtle" />
                                        ) : (
                                            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
                                        )}
                                        <span className={step.done ? 'text-fg-muted line-through' : 'text-fg'}>
                                            {t(`dnssrv.step.${step.code}` as Parameters<typeof t>[0], {
                                                a: step.args?.[0] ?? '',
                                                b: step.args?.[1] ?? '',
                                            })}
                                        </span>
                                    </li>
                                ))}
                            </ul>
                        </div>
                    )}
                        </>
                    )}
                </div>
            </div>
        </section>
    );
}
