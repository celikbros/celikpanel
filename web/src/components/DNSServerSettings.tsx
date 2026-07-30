import { useCallback, useEffect, useState } from 'react';
import { Network, Server, Check, AlertTriangle, Circle, Loader2 } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { Button, ErrorBanner, Field, inputClass, StatusDot } from './ui';
import { readApiError, apiErrorText, type ApiError } from '../lib/apiError';
import { HelpButton } from './HelpDrawer';

type DNSRole = 'standalone' | 'paired';
type DraftDNSRole = DNSRole | '';

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
type BusyAction = 'names' | 'cluster' | null;
type ClusterBlocker = 'busy' | 'saveNames' | 'chooseRole' | 'peerIp' | 'peerNs' | 'noChanges' | null;

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
    const { t } = useI18n();
    const [saved, setSaved] = useState<SavedSettings | null>(null);
    const [draft, setDraft] = useState<SettingsDraft | null>(null);
    const [busy, setBusy] = useState<BusyAction>(null);
    const [apiError, setApiError] = useState<ApiError | null>(null);

    const load = useCallback(async (preserveCluster?: ClusterDraft) => {
        try {
            const [namesResponse, clusterResponse] = await Promise.all([
                fetch('/api/v1/settings/nameservers'),
                fetch('/api/v1/settings/dns-cluster'),
            ]);
            if (!namesResponse.ok) {
                setApiError(await readApiError(namesResponse));
                return;
            }
            if (!clusterResponse.ok) {
                setApiError(await readApiError(clusterResponse));
                return;
            }
            const names = await namesResponse.json() as NameserverResponse;
            const cluster = await clusterResponse.json() as ClusterResponse;

            const role = normalizeRole(cluster.role);
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

            const savedPeerNames = snapshot.namesDerived ? [] : [ns1, ns2].filter(Boolean);
            const preservedPeerNS = cleanHostname(preserveCluster?.peer_ns ?? '');
            const requestedPeerNS = preservedPeerNS || cleanHostname(cluster.peer_ns ?? '');
            setDraft({
                ns1,
                ns2,
                role: preserveCluster?.role ?? (snapshot.configured ? role : ''),
                peer_ip: preserveCluster?.peer_ip ?? cluster.peer_ip ?? '',
                peer_ns: savedPeerNames.includes(requestedPeerNS) ? requestedPeerNS : '',
            });
            setApiError(null);
        } catch {
            const error = { message: t('common.error') };
            setApiError(error);
            showToast('error', error.message);
        }
    }, [t]);

    useEffect(() => {
        void load();
    }, [load]);

    const saveNS = async () => {
        if (!draft) return;
        setBusy('names');
        setApiError(null);
        const preserveCluster: ClusterDraft = {
            role: draft.role,
            peer_ip: draft.peer_ip,
            peer_ns: draft.peer_ns,
        };
        try {
            const res = await fetch('/api/v1/settings/nameservers', {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ ns1: cleanHostname(draft.ns1), ns2: cleanHostname(draft.ns2) }),
            });
            if (!res.ok) {
                const error = await readApiError(res);
                setApiError(error);
                showToast('error', apiErrorText(error, t));
                return;
            }
            showToast('success', t('dnssrv.namesSaved'));
            await load(preserveCluster);
        } catch {
            const error = { message: t('common.error') };
            setApiError(error);
            showToast('error', error.message);
        } finally {
            setBusy(null);
        }
    };

    const saveCluster = async () => {
        if (!draft || draft.role === '') return;
        setBusy('cluster');
        setApiError(null);
        try {
            const paired = draft.role === 'paired';
            const res = await fetch('/api/v1/settings/dns-cluster', {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    role: draft.role,
                    peer_ip: paired ? canonicalIPv4(draft.peer_ip) || draft.peer_ip.trim() : '',
                    peer_ns: paired ? cleanHostname(draft.peer_ns) : '',
                }),
            });
            if (!res.ok) {
                const error = await readApiError(res);
                setApiError(error);
                showToast('error', apiErrorText(error, t));
                return;
            }
            showToast('success', t('dnssrv.roleSaved'));
            await load();
        } catch {
            const error = { message: t('common.error') };
            setApiError(error);
            showToast('error', error.message);
        } finally {
            setBusy(null);
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
    const nameserverNames = saved.namesDerived ? [] : Array.from(new Set([saved.ns1, saved.ns2].filter(Boolean)));
    const peerSelectionValid = nameserverNames.includes(effectivePeerNS);
    const localSelectionValid = nameserverNames.includes(effectiveLocalNS);
    const serverIPv4 = canonicalIPv4(saved.server_ip);
    const peerIPInvalid = draft.role === 'paired' && effectivePeerIP !== '' && effectivePeerIPv4 === '';
    const peerIPSame =
        draft.role === 'paired' && effectivePeerIPv4 !== '' && serverIPv4 !== '' && effectivePeerIPv4 === serverIPv4;
    const canSaveNames = draftNS1 !== '' && draftNS2 !== '' && draftNS1 !== draftNS2;

    let suggestedLocalNS = nameserverNames.includes(cleanHostname(saved.suggested_local_ns ?? ''))
        ? cleanHostname(saved.suggested_local_ns ?? '')
        : '';
    let suggestedPeerNS = nameserverNames.includes(cleanHostname(saved.suggested_peer_ns ?? ''))
        ? cleanHostname(saved.suggested_peer_ns ?? '')
        : '';
    if (!suggestedLocalNS && suggestedPeerNS) suggestedLocalNS = otherNameserver(suggestedPeerNS, saved.ns1, saved.ns2);
    if (!suggestedPeerNS && suggestedLocalNS) suggestedPeerNS = otherNameserver(suggestedLocalNS, saved.ns1, saved.ns2);
    if (suggestedLocalNS === suggestedPeerNS) {
        suggestedLocalNS = '';
        suggestedPeerNS = '';
    }
    const suggestedPeerIPv4 = canonicalIPv4(saved.suggested_peer_ip ?? '');
    const safeSuggestedPeerIP =
        suggestedPeerIPv4 !== '' && (serverIPv4 === '' || suggestedPeerIPv4 !== serverIPv4)
            ? suggestedPeerIPv4
            : '';

    let clusterBlocker: ClusterBlocker = null;
    if (busy !== null) clusterBlocker = 'busy';
    else if (namesDirty || saved.namesDerived) clusterBlocker = 'saveNames';
    else if (draft.role === '') clusterBlocker = 'chooseRole';
    else if (draft.role === 'paired' && (effectivePeerIPv4 === '' || peerIPSame)) clusterBlocker = 'peerIp';
    else if (draft.role === 'paired' && (!peerSelectionValid || !localSelectionValid)) clusterBlocker = 'peerNs';
    else if (!clusterDirty && saved.configured) clusterBlocker = 'noChanges';

    const blockerText = clusterBlocker === 'busy'
        ? t('dnssrv.blocker.busy')
        : clusterBlocker === 'saveNames'
          ? t('dnssrv.blocker.saveNames')
          : clusterBlocker === 'chooseRole'
            ? t('dnssrv.blocker.chooseRole')
            : clusterBlocker === 'peerIp'
              ? t('dnssrv.blocker.peerIp')
              : clusterBlocker === 'peerNs'
                ? t('dnssrv.blocker.peerNs')
                : clusterBlocker === 'noChanges'
                  ? t('dnssrv.blocker.noChanges')
                  : t('dnssrv.readyToSave');

    const selectRole = (role: DNSRole) => {
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
                peer_ip: mayReplacePeerIP && safeSuggestedPeerIP ? safeSuggestedPeerIP : current.peer_ip,
                peer_ns: nameserverNames.includes(currentPeerNS) ? current.peer_ns : suggestedPeerNS || current.peer_ns,
            };
        });
    };

    const selectLocalNameserver = (localNS: string) => {
        const peerNS = otherNameserver(localNS, saved.ns1, saved.ns2);
        setDraft((current) => (current ? { ...current, peer_ns: peerNS } : current));
    };

    const useDetectedPeer = () => {
        setDraft((current) => current ? {
            ...current,
            role: 'paired',
            peer_ip: safeSuggestedPeerIP || current.peer_ip,
            peer_ns: suggestedPeerNS || current.peer_ns,
        } : current);
    };

    const factLocation = (fact: NSFact) => {
        if (fact.ips.length === 0) return t('dnssrv.whereNowhere');
        if (fact.points_here) return t('dnssrv.whereHere');
        const pointsToDraftPeer =
            draft.role === 'paired' && effectivePeerIPv4 !== '' &&
            fact.ips.some((ip) => canonicalIPv4(ip) === effectivePeerIPv4);
        if (pointsToDraftPeer) return checksCurrent ? t('dnssrv.wherePeer') : t('dnssrv.whereIntendedPeer');
        if (saved.role === 'paired' && saved.peer_ip && fact.ips.includes(saved.peer_ip)) return t('dnssrv.wherePeer');
        return t('dnssrv.whereOther');
    };

    const clusterSaveLabel = busy === 'cluster'
        ? t('dnssrv.saving')
        : draft.role === 'paired'
          ? t('dnssrv.savePaired')
          : draft.role === 'standalone'
            ? t('dnssrv.saveStandalone')
            : t('dnssrv.saveRole');

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

            <ErrorBanner error={apiError} className="mb-4" />

            <div className="space-y-5">
                <div className="min-w-0 rounded-xl border border-border bg-surface-2/30 p-4">
                    <SetupStep
                        number={1}
                        title={t('dnssrv.setup.step1.title')}
                        description={t('dnssrv.setup.step1.desc')}
                        complete={!namesDirty && !saved.namesDerived}
                    />
                    <div className="mb-2 flex flex-wrap items-center gap-2">
                        <span
                            className={`rounded-md px-2 py-0.5 text-xs font-medium ${
                                namesDirty
                                    ? 'bg-primary/10 text-primary'
                                    : saved.namesDerived
                                      ? 'bg-warning/10 text-warning'
                                      : 'bg-success/10 text-success'
                            }`}
                        >
                            {namesDirty
                                ? t('dnssrv.stateUnsaved')
                                : saved.namesDerived
                                  ? t('dnssrv.namesSuggested')
                                  : t('dnssrv.stateSaved')}
                        </span>
                    </div>
                    <p className="mb-1 text-xs leading-relaxed text-fg-muted">{t('dnssrv.namesHint')}</p>
                    {draft.role !== '' && (
                        <p className="mb-1 text-xs leading-relaxed text-fg-muted">
                            {draft.role === 'paired'
                                ? t('dnssrv.namesHintPaired', { ip: saved.server_ip })
                                : t('dnssrv.namesHintStandalone', { ip: saved.server_ip })}
                        </p>
                    )}
                    <p className="mb-3 text-xs leading-relaxed text-fg-subtle">{t('dnssrv.namesRegistrarHint')}</p>

                    <div className="mb-3 space-y-3">
                        <Field label={t('dnssrv.ns1Label')} htmlFor="dns-ns1">
                            <input
                                id="dns-ns1"
                                className={inputClass}
                                value={draft.ns1}
                                onChange={(e) => setDraft((current) => (current ? { ...current, ns1: e.target.value } : current))}
                                placeholder="ns1.example.com"
                            />
                        </Field>
                        <Field label={t('dnssrv.ns2Label')} htmlFor="dns-ns2">
                            <input
                                id="dns-ns2"
                                className={inputClass}
                                value={draft.ns2}
                                onChange={(e) => setDraft((current) => (current ? { ...current, ns2: e.target.value } : current))}
                                placeholder="ns2.example.com"
                            />
                        </Field>
                    </div>
                    <p className="mb-3 rounded-lg border border-primary/15 bg-primary/5 px-3 py-2 text-xs leading-relaxed text-fg-muted">
                        {t('dnssrv.namesOrderHint')}
                    </p>

                    <div className="mb-3 rounded-lg border border-border bg-surface-2/50 p-3">
                            <div className="mb-2 flex flex-wrap items-center gap-2">
                                <p className="text-xs font-medium text-fg">{t('dnssrv.liveNamesTitle')}</p>
                                <span
                                    className={`rounded-md px-2 py-0.5 text-xs font-medium ${
                                        saved.namesUsable ? 'bg-success/10 text-success' : 'bg-warning/10 text-warning'
                                    }`}
                                >
                                    {saved.namesUsable ? t('dnssrv.namesReady') : t('dnssrv.namesPending')}
                                </span>
                            </div>
                            <ul className="space-y-1">
                                {saved.facts?.map((fact) => (
                                    <li key={fact.host} className="flex flex-wrap items-center gap-x-2 font-mono text-xs">
                                        <span className="text-fg">{fact.host}</span>
                                        <span className="text-fg-muted">
                                            → {fact.ips.length ? fact.ips.join(', ') : t('conn.none')}
                                        </span>
                                        <span className="text-fg-subtle">{factLocation(fact)}</span>
                                    </li>
                                ))}
                            </ul>
                        {!checksCurrent && (
                            <p className="mt-2 rounded-md bg-primary/5 px-2.5 py-2 text-xs leading-relaxed text-fg-muted">
                                {t('dnssrv.publicDnsDraft')}
                            </p>
                        )}
                    </div>

                    <Button onClick={saveNS} disabled={busy !== null || !canSaveNames || (!namesDirty && !saved.namesDerived)}>
                        {busy === 'names' ? <Loader2 className="h-4 w-4 animate-spin" /> : <Check className="h-4 w-4" />}
                        {busy === 'names' ? t('dnssrv.saving') : t('dnssrv.saveNames')}
                    </Button>
                </div>

                <div className="min-w-0 rounded-xl border border-border bg-surface-2/30 p-4">
                    <SetupStep
                        number={2}
                        title={t('dnssrv.setup.step2.title')}
                        description={t('dnssrv.setup.step2.desc')}
                        complete={draft.role !== ''}
                    />
                    <div className="mb-2 flex flex-wrap items-center gap-2">
                        <span
                            className={`rounded-md px-2 py-0.5 text-xs font-medium ${
                                clusterDirty
                                    ? 'bg-primary/10 text-primary'
                                    : saved.configured
                                      ? 'bg-success/10 text-success'
                                      : 'bg-warning/10 text-warning'
                            }`}
                        >
                            {clusterDirty
                                ? t('dnssrv.stateUnsaved')
                                : saved.configured
                                  ? t('dnssrv.stateSaved')
                                  : t('dnssrv.roleUnconfigured')}
                        </span>
                    </div>
                    <p className="mb-3 text-xs leading-relaxed text-fg-muted">{t('dnssrv.roleHint')}</p>

                    {!saved.configured && !clusterDirty && (
                        <div className="mb-3 rounded-lg border border-warning/30 bg-warning/5 p-3 text-xs leading-relaxed text-fg-muted">
                            {t('dnssrv.roleSetupHint')}
                        </div>
                    )}

                    <fieldset className="mb-3 space-y-2">
                        <legend className="sr-only">{t('dnssrv.roleTitle')}</legend>
                        {(['standalone', 'paired'] as const).map((role) => (
                            <label
                                key={role}
                                className={`flex cursor-pointer items-start gap-2.5 rounded-lg border p-3 ${
                                    draft.role === role ? 'border-primary bg-primary/5' : 'border-border'
                                }`}
                            >
                                <input
                                    type="radio"
                                    name="dns-role"
                                    className="mt-0.5"
                                    checked={draft.role === role}
                                    onChange={() => selectRole(role)}
                                />
                                <span className="min-w-0">
                                    <span className="block text-sm font-medium text-fg">
                                        {t(`dnssrv.role.${role}` as Parameters<typeof t>[0])}
                                    </span>
                                    <span className="block text-xs leading-relaxed text-fg-muted">
                                        {t(`dnssrv.role.${role}.desc` as Parameters<typeof t>[0])}
                                    </span>
                                </span>
                            </label>
                        ))}
                    </fieldset>

                    {draft.role === 'paired' && (
                        <>
                            <SetupStep
                                number={3}
                                title={t('dnssrv.setup.step3.title')}
                                description={t('dnssrv.setup.step3.desc')}
                                complete={effectivePeerIPv4 !== '' && !peerIPSame && peerSelectionValid && localSelectionValid}
                            />

                            {safeSuggestedPeerIP && suggestedLocalNS && suggestedPeerNS && (
                                <div className="mb-3 rounded-lg border border-primary/20 bg-primary/5 p-3">
                                    <div className="flex flex-wrap items-center justify-between gap-3">
                                        <div className="min-w-0 text-xs text-fg-muted">
                                            <p className="mb-1 font-medium text-fg">{t('dnssrv.detected')}</p>
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
                                            className="rounded-lg border border-primary/30 px-3 py-1.5 text-xs font-medium text-primary transition-colors hover:bg-primary/10"
                                        >
                                            {t('dnssrv.useDetectedPeer')}
                                        </button>
                                    </div>
                                </div>
                            )}

                            <div className="mb-3 space-y-3">
                                <Field label={t('dnssrv.peerIpLabel')} htmlFor="dns-peer-ip">
                                    <input
                                        id="dns-peer-ip"
                                        className={inputClass}
                                        value={draft.peer_ip}
                                        onChange={(e) =>
                                            setDraft((current) => (current ? { ...current, peer_ip: e.target.value } : current))
                                        }
                                        placeholder={t('dnssrv.peerIpPlaceholder')}
                                        aria-invalid={peerIPInvalid || peerIPSame}
                                    />
                                    {peerIPInvalid && (
                                        <p className="mt-1.5 text-xs text-danger">{t('dnssrv.peerIpInvalid')}</p>
                                    )}
                                    {peerIPSame && (
                                        <p className="mt-1.5 text-xs text-danger">
                                            {t('dnssrv.peerIpSame', { ip: saved.server_ip })}
                                        </p>
                                    )}
                                </Field>
                                <Field label={t('dnssrv.localNsLabel')} hint={t('dnssrv.localNsHint')} htmlFor="dns-local-ns">
                                    <select
                                        id="dns-local-ns"
                                        className={inputClass}
                                        value={effectiveLocalNS}
                                        disabled={saved.namesDerived || namesDirty}
                                        onChange={(e) => selectLocalNameserver(e.target.value)}
                                    >
                                        <option value="">{t('dnssrv.localNsPlaceholder')}</option>
                                        {nameserverNames.map((name) => (
                                            <option key={name} value={name}>
                                                {name}{name === suggestedLocalNS ? ` — ${t('dnssrv.recommended')}` : ''}
                                            </option>
                                        ))}
                                    </select>
                                    {!localSelectionValid && !namesDirty && !saved.namesDerived && (
                                        <p className="mt-1.5 text-xs text-danger">{t('dnssrv.localNsRequired')}</p>
                                    )}
                                </Field>
                            </div>

                            <div className="mb-3">
                                <p className="mb-2 text-xs font-medium text-fg">{t('dnssrv.identityTitle')}</p>
                                <div className="space-y-2">
                                    <div className="min-w-0 rounded-lg border border-primary/25 bg-primary/5 p-3">
                                        <div className="mb-1 flex flex-wrap items-center justify-between gap-2">
                                            <span className="text-xs font-semibold uppercase tracking-wide text-primary">
                                                {t('dnssrv.thisServer')}
                                            </span>
                                            <span className="font-mono text-xs text-fg-muted">{saved.server_ip || '—'}</span>
                                        </div>
                                        <p className="break-all font-mono text-sm font-semibold text-fg">{effectiveLocalNS || '—'}</p>
                                    </div>
                                    <div className="min-w-0 rounded-lg border border-border bg-surface p-3">
                                        <div className="mb-1 flex flex-wrap items-center justify-between gap-2">
                                            <span className="text-xs font-semibold uppercase tracking-wide text-fg-muted">
                                                {t('dnssrv.peerServer')}
                                            </span>
                                            <span className="font-mono text-xs text-fg-muted">{effectivePeerIP || '—'}</span>
                                        </div>
                                        <p className="break-all font-mono text-sm font-semibold text-fg">{effectivePeerNS || '—'}</p>
                                    </div>
                                </div>
                            </div>

                            {(saved.namesDerived || namesDirty) && (
                                <p className="mb-3 rounded-lg bg-surface-2/60 p-2.5 text-xs leading-relaxed text-fg-muted">
                                    {t('dnssrv.saveNamesFirst')}
                                </p>
                            )}

                            {checksCurrent && saved.configured && saved.role === 'paired' && saved.peer_ip && (
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
                    <SetupStep
                        number={draft.role === 'paired' ? 4 : 3}
                        title={t('dnssrv.reviewTitle')}
                        description={blockerText}
                        complete={checksCurrent && saved.configured}
                    />
                    {draft.role === 'standalone' && (
                        <div className="mb-3 rounded-lg border border-primary/20 bg-primary/5 p-3">
                            <p className="mb-1 text-xs font-semibold uppercase tracking-wide text-primary">{t('dnssrv.thisServer')}</p>
                            <p className="font-mono text-xs text-fg-muted">{saved.server_ip || '—'}</p>
                            <p className="mt-1 text-sm font-medium text-fg">{t('dnssrv.bothNames')}</p>
                        </div>
                    )}
                    {draft.role === 'standalone' && (saved.namesDerived || namesDirty) && (
                        <p className="mb-3 rounded-lg bg-surface-2/60 p-2.5 text-xs leading-relaxed text-fg-muted">
                            {t('dnssrv.saveNamesFirst')}
                        </p>
                    )}

                    <div
                        id="dns-cluster-readiness"
                        aria-live="polite"
                        className={`mb-3 rounded-lg border p-3 text-xs leading-relaxed ${
                            clusterBlocker === null
                                ? 'border-success/25 bg-success/5 text-success'
                                : 'border-warning/25 bg-warning/5 text-fg-muted'
                        }`}
                    >
                        {blockerText}
                    </div>
                    <Button
                        variant="primary"
                        onClick={saveCluster}
                        disabled={clusterBlocker !== null}
                        aria-describedby="dns-cluster-readiness"
                    >
                        {busy === 'cluster' ? <Loader2 className="h-4 w-4 animate-spin" /> : <Check className="h-4 w-4" />}
                        {clusterSaveLabel}
                    </Button>

                    {!checksCurrent && (
                        <div className="mt-4 rounded-lg border border-primary/20 bg-primary/5 p-3 text-xs leading-relaxed text-fg-muted">
                            {t('dnssrv.checksStale')}
                        </div>
                    )}

                    {checksCurrent && saved.configured && saved.steps?.length > 0 && (
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
                </div>
            </div>
        </section>
    );
}
