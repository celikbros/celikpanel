import { useCallback, useEffect, useState } from 'react';
import { Network, Server, Check, AlertTriangle, Circle } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { Button, Field, inputClass, StatusDot } from './ui';
import { readApiError, apiErrorText } from '../lib/apiError';
import { HelpButton } from './HelpDrawer';

type DNSRole = 'standalone' | 'paired';

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
    role: DNSRole;
    peer_ip: string;
    peer_ns: string;
}

type ClusterDraft = Pick<SettingsDraft, 'role' | 'peer_ip' | 'peer_ns'>;

function cleanHostname(value: string): string {
    return value.trim().toLowerCase().replace(/\.$/, '');
}

function normalizeRole(role: string): DNSRole {
    return role === 'paired' || role === 'primary' || role === 'secondary' ? 'paired' : 'standalone';
}

export function DNSServerSettings() {
    const { t } = useI18n();
    const [saved, setSaved] = useState<SavedSettings | null>(null);
    const [draft, setDraft] = useState<SettingsDraft | null>(null);
    const [busy, setBusy] = useState(false);

    const load = useCallback(async (preserveCluster?: ClusterDraft) => {
        const [names, cluster] = await Promise.all([
            fetch('/api/v1/settings/nameservers').then((r) => (r.ok ? (r.json() as Promise<NameserverResponse>) : null)),
            fetch('/api/v1/settings/dns-cluster').then((r) => (r.ok ? (r.json() as Promise<ClusterResponse>) : null)),
        ]);
        if (!names || !cluster) return;

        const role = normalizeRole(cluster.role);
        const ns1 = cleanHostname(names.ns1 || cluster.ns1 || '');
        const ns2 = cleanHostname(names.ns2 || cluster.ns2 || '');
        const snapshot: SavedSettings = {
            ...cluster,
            configured: cluster.configured === true,
            role,
            ns1,
            ns2,
            server_ip: cluster.server_ip || names.server_ip,
            facts: cluster.facts || names.facts || [],
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
            role: preserveCluster?.role ?? role,
            peer_ip: preserveCluster?.peer_ip ?? cluster.peer_ip ?? '',
            peer_ns: savedPeerNames.includes(requestedPeerNS) ? requestedPeerNS : '',
        });
    }, []);

    useEffect(() => {
        void load();
    }, [load]);

    const saveNS = async () => {
        if (!draft) return;
        setBusy(true);
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
                showToast('error', apiErrorText(await readApiError(res), t));
                return;
            }
            showToast('success', t('dnssrv.namesSaved'));
            await load(preserveCluster);
        } catch {
            showToast('error', t('common.error'));
        } finally {
            setBusy(false);
        }
    };

    const saveCluster = async () => {
        if (!draft) return;
        setBusy(true);
        try {
            const paired = draft.role === 'paired';
            const res = await fetch('/api/v1/settings/dns-cluster', {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    role: draft.role,
                    peer_ip: paired ? draft.peer_ip.trim() : '',
                    peer_ns: paired ? cleanHostname(draft.peer_ns) : '',
                }),
            });
            if (!res.ok) {
                showToast('error', apiErrorText(await readApiError(res), t));
                return;
            }
            showToast('success', t('dnssrv.roleSaved'));
            await load();
        } catch {
            showToast('error', t('common.error'));
        } finally {
            setBusy(false);
        }
    };

    if (!saved || !draft) return null;

    const draftNS1 = cleanHostname(draft.ns1);
    const draftNS2 = cleanHostname(draft.ns2);
    const namesDirty = draftNS1 !== saved.ns1 || draftNS2 !== saved.ns2;
    const effectivePeerIP = draft.role === 'paired' ? draft.peer_ip.trim() : '';
    const effectivePeerNS = draft.role === 'paired' ? cleanHostname(draft.peer_ns) : '';
    const savedPeerIP = saved.role === 'paired' ? saved.peer_ip : '';
    const savedPeerNS = saved.role === 'paired' ? cleanHostname(saved.peer_ns) : '';
    const clusterDirty =
        draft.role !== saved.role || effectivePeerIP !== savedPeerIP || effectivePeerNS !== savedPeerNS;
    const checksCurrent = !namesDirty && !clusterDirty;
    const peerNames = saved.namesDerived ? [] : Array.from(new Set([saved.ns1, saved.ns2].filter(Boolean)));
    const peerSelectionValid = peerNames.includes(effectivePeerNS);
    const canSaveNames = draftNS1 !== '' && draftNS2 !== '' && draftNS1 !== draftNS2;
	const canSaveCluster =
		!namesDirty &&
		!saved.namesDerived &&
		(draft.role === 'standalone' ||
            (!saved.namesDerived && effectivePeerIP !== '' && effectivePeerNS !== '' && peerSelectionValid));

    const factLocation = (fact: NSFact) => {
        if (fact.ips.length === 0) return t('dnssrv.whereNowhere');
        if (fact.points_here) return t('dnssrv.whereHere');
        if (saved.role === 'paired' && saved.peer_ip && fact.ips.includes(saved.peer_ip)) return t('dnssrv.wherePeer');
        return t('dnssrv.whereOther');
    };

    return (
        <section className="rounded-xl border border-border bg-surface p-6">
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

            <div className="mb-2 flex flex-wrap items-center gap-2">
                <p className="text-sm font-medium text-fg">{t('dnssrv.namesTitle')}</p>
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
            <p className="mb-1 text-xs leading-relaxed text-fg-muted">
                {draft.role === 'paired'
                    ? t('dnssrv.namesHintPaired', { ip: saved.server_ip })
                    : t('dnssrv.namesHintStandalone', { ip: saved.server_ip })}
            </p>
            <p className="mb-3 text-xs leading-relaxed text-fg-subtle">{t('dnssrv.namesRegistrarHint')}</p>

            <div className="mb-3 grid gap-3 sm:grid-cols-2">
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

            {checksCurrent && (
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
                </div>
            )}

            <Button onClick={saveNS} disabled={busy || !canSaveNames || (!namesDirty && !saved.namesDerived)}>
                <Check className="h-4 w-4" /> {t('dnssrv.saveNames')}
            </Button>

            <hr className="my-6 border-border" />
            <div className="mb-2 flex flex-wrap items-center gap-2">
                <p className="text-sm font-medium text-fg">{t('dnssrv.roleTitle')}</p>
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

            <div className="mb-3 space-y-2">
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
                            onChange={() => setDraft((current) => (current ? { ...current, role } : current))}
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
            </div>

			{draft.role === 'paired' && (
                <>
                    <div className="mb-2 grid gap-3 sm:grid-cols-2">
                        <Field label={t('dnssrv.peerIpLabel')} htmlFor="dns-peer-ip">
                            <input
                                id="dns-peer-ip"
                                className={inputClass}
                                value={draft.peer_ip}
                                onChange={(e) =>
                                    setDraft((current) => (current ? { ...current, peer_ip: e.target.value } : current))
                                }
                                placeholder={t('dnssrv.peerIpPlaceholder')}
                            />
                        </Field>
                        <Field label={t('dnssrv.peerNsLabel')} hint={t('dnssrv.peerNsHint')} htmlFor="dns-peer-ns">
                            <select
                                id="dns-peer-ns"
                                className={inputClass}
                                value={draft.peer_ns}
                                disabled={saved.namesDerived || namesDirty}
                                onChange={(e) =>
                                    setDraft((current) => (current ? { ...current, peer_ns: e.target.value } : current))
                                }
                            >
                                <option value="">{t('dnssrv.peerNsPlaceholder')}</option>
                                {peerNames.map((name) => (
                                    <option key={name} value={name}>
                                        {name}
                                    </option>
                                ))}
                            </select>
                        </Field>
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
			{draft.role === 'standalone' && (saved.namesDerived || namesDirty) && (
				<p className="mb-3 rounded-lg bg-surface-2/60 p-2.5 text-xs leading-relaxed text-fg-muted">
					{t('dnssrv.saveNamesFirst')}
				</p>
			)}

			<Button
                onClick={saveCluster}
                disabled={busy || !canSaveCluster || (!clusterDirty && saved.configured)}
            >
                <Check className="h-4 w-4" /> {t('dnssrv.saveRole')}
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
        </section>
    );
}
