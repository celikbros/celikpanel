import { useCallback, useEffect, useState } from 'react';
import {
    AlertTriangle,
    ArrowRightLeft,
    CheckCircle2,
    Database,
    DownloadCloud,
    Loader2,
    Network,
    RefreshCw,
    ShieldAlert,
} from 'lucide-react';
import { useI18n } from '../i18n';
import { dnsEngineText, type DNSEngineCopyKey } from '../i18n/dnsEngine';
import { apiErrorText, readApiError } from '../lib/apiError';
import { dnsEngineIdentityReviewLocked } from '../lib/dnsIdentityPlan';
import {
    DNS_ENGINE_IDS,
    decodeDNSEngineSnapshot,
    decodeDNSEngineSwitchPreview,
    type DNSEngineEntry,
    type DNSEngineID,
    type DNSEngineSnapshot,
    type DNSEngineSwitchPreview,
} from '../lib/dnsEngineContract';
import { Button } from './ui';
import { showToast } from './Toast';

interface DNSEngineCardProps {
    onSnapshotChange?: (snapshot: DNSEngineSnapshot | null) => void;
    identityPlanCurrent?: boolean;
    actionsLocked?: boolean;
}

interface ReviewState {
    base: DNSEngineSnapshot;
    target: DNSEngineID;
    requestID: string | null;
    loading: boolean;
    committing: boolean;
    acknowledged: boolean;
    preview: DNSEngineSwitchPreview | null;
    error: string;
}

const knownBlockerKeys = {
    dns_identity_required: 'dnsEngine.blocker.identityRequired',
    paired_topology_unsupported: 'dnsEngine.blocker.pairedTopology',
    dnssec_unsupported: 'dnsEngine.blocker.dnssec',
    pending_zone_sync: 'dnsEngine.blocker.pendingZones',
    operation_running: 'dnsEngine.blocker.operationRunning',
    unmanaged_dns_detected: 'dnsEngine.blocker.unmanaged',
    port_53_conflict: 'dnsEngine.blocker.portConflict',
    source_degraded: 'dnsEngine.blocker.sourceDegraded',
    target_unavailable: 'dnsEngine.blocker.targetUnavailable',
    agent_incompatible: 'dnsEngine.blocker.agentIncompatible',
    target_already_active: 'dnsEngine.blocker.alreadyActive',
    stale_revision: 'dnsEngine.blocker.staleRevision',
} as const;

const knownImpactKeys = {
    install_target: 'dnsEngine.impact.installTarget',
    validate_target: 'dnsEngine.impact.validateTarget',
    publish_zones: 'dnsEngine.impact.publishZones',
    stop_source: 'dnsEngine.impact.stopSource',
    start_target: 'dnsEngine.impact.startTarget',
    brief_dns_interruption: 'dnsEngine.impact.briefInterruption',
    keep_source_standby: 'dnsEngine.impact.keepStandby',
    adopt_existing: 'dnsEngine.impact.adoptExisting',
    replace_existing: 'dnsEngine.impact.replaceExisting',
    restart_target: 'dnsEngine.impact.restartTarget',
    configure_secondary: 'dnsEngine.impact.configureSecondary',
} as const;

function createRequestID(): string | null {
    try {
        const bytes = new Uint8Array(16);
        crypto.getRandomValues(bytes);
        return Array.from(bytes, (value) => value.toString(16).padStart(2, '0')).join('');
    } catch {
        return null;
    }
}

function engineName(id: DNSEngineID): string {
    return id === 'pdns' ? 'PowerDNS' : 'BIND';
}

function engineIcon(id: DNSEngineID) {
    return id === 'pdns' ? Database : Network;
}

function statusStyle(status: DNSEngineEntry['status']): string {
    if (status === 'active') return 'border-success/30 bg-success/10 text-success';
    if (status === 'installed_standby') return 'border-primary/25 bg-primary/10 text-primary';
    if (status === 'available') return 'border-border bg-surface-2 text-fg-muted';
    return 'border-warning/35 bg-warning/10 text-warning';
}

export function DNSEngineCard({
    onSnapshotChange,
    identityPlanCurrent = true,
    actionsLocked = false,
}: DNSEngineCardProps) {
    const { t, locale } = useI18n();
    const et = (key: DNSEngineCopyKey, vars?: Record<string, string | number>) =>
        dnsEngineText(locale, key, vars);
    const [snapshot, setSnapshot] = useState<DNSEngineSnapshot | null>(null);
    const [loading, setLoading] = useState(true);
    const [loadError, setLoadError] = useState('');
    const [review, setReview] = useState<ReviewState | null>(null);
    const identityReviewLocked = dnsEngineIdentityReviewLocked(identityPlanCurrent, snapshot);

    const refresh = useCallback(async () => {
        setLoading(true);
        setLoadError('');
        try {
            const response = await fetch('/api/v1/dns/engine', {
                method: 'GET',
                cache: 'no-store',
            });
            if (!response.ok) {
                await readApiError(response);
                setSnapshot(null);
                onSnapshotChange?.(null);
                setLoadError(et('dnsEngine.stateUnavailable'));
                return;
            }
            let payload: unknown;
            try {
                payload = await response.json();
            } catch {
                payload = null;
            }
            const decoded = decodeDNSEngineSnapshot(payload);
            if (decoded === null) {
                setSnapshot(null);
                onSnapshotChange?.(null);
                setLoadError(et('dnsEngine.stateInvalid'));
                return;
            }
            setSnapshot(decoded);
            onSnapshotChange?.(decoded);
        } catch {
            setSnapshot(null);
            onSnapshotChange?.(null);
            setLoadError(et('dnsEngine.stateUnavailable'));
        } finally {
            setLoading(false);
        }
    }, [locale, onSnapshotChange]);

    useEffect(() => {
        void refresh();
    }, [refresh]);

    useEffect(() => {
        if (actionsLocked) setReview(null);
    }, [actionsLocked]);

    const requestPreview = async (target: DNSEngineID) => {
        if (actionsLocked || !snapshot || snapshot.state === 'switching' || identityReviewLocked) return;
        const base = snapshot;
        const requestID = createRequestID();
        setReview({
            base,
            target,
            requestID,
            loading: true,
            committing: false,
            acknowledged: false,
            preview: null,
            error: requestID === null ? et('dnsEngine.requestIDFailed') : '',
        });
        try {
            const response = await fetch('/api/v1/dns/engine/switch/preview', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    target_engine: target,
                    expected_source: base.active_engine,
                    expected_revision: base.revision,
                }),
            });
            if (!response.ok) {
                await readApiError(response);
                setReview((current) => current && current.base === base && current.target === target
                    ? { ...current, loading: false, error: et('dnsEngine.previewFailed') }
                    : current);
                return;
            }
            let payload: unknown;
            try {
                payload = await response.json();
            } catch {
                payload = null;
            }
            const preview = decodeDNSEngineSwitchPreview(
                payload,
                base.active_engine,
                target,
                base.revision,
            );
            setReview((current) => {
                if (!current || current.base !== base || current.target !== target) return current;
                if (preview === null) {
                    return { ...current, loading: false, error: et('dnsEngine.previewInvalid') };
                }
                return { ...current, loading: false, preview };
            });
        } catch {
            setReview((current) => current && current.base === base && current.target === target
                ? { ...current, loading: false, error: et('dnsEngine.previewFailed') }
                : current);
        }
    };

    const commitSwitch = async () => {
        const current = review;
        const preview = current?.preview;
        if (actionsLocked || !current || !preview || current.loading || current.committing || preview.blockers.length > 0) return;
        if (preview.requires_downtime_acknowledgement && !current.acknowledged) return;
        const requestID = current.requestID;
        if (requestID === null) return;

        setReview({ ...current, committing: true, error: '' });
        try {
            const response = await fetch('/api/v1/dns/engine/switch', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    request_id: requestID,
                    target_engine: current.target,
                    expected_source: current.base.active_engine,
                    expected_revision: current.base.revision,
                    preview_token: preview.preview_token,
                    downtime_acknowledged: current.acknowledged,
                }),
            });
            if (!response.ok) {
                const apiError = await readApiError(response);
                const mutationApplied = apiError.mutationApplied === true;
                const partialSuccess = apiError.partialSuccess === true;
                // The commit endpoint consumes the preview authority before it
                // starts a mutation. Close the stale dialog for every terminal
                // HTTP response; a retry must begin with a fresh state and
                // preview instead of reusing the old token.
                setReview(null);
                if (mutationApplied) {
                    showToast('warning', et('dnsEngine.switchAppliedNeedsRefresh'));
                } else if (partialSuccess) {
                    showToast('warning', et('dnsEngine.switchPartialUnverified'));
                } else {
                    showToast(
                        'error',
                        apiError.code
                            ? apiErrorText(apiError, t)
                            : et('dnsEngine.switchFailed'),
                    );
                }
                await refresh();
                return;
            }
            setReview(null);
            showToast('success', et('dnsEngine.switchAccepted'));
            await refresh();
        } catch {
            // A lost response can hide an accepted mutation and the preview
            // token may already be consumed. Never leave a retryable confirm
            // button on screen; refresh is the only truthful next step.
            setReview(null);
            showToast('warning', et('dnsEngine.switchAmbiguous'));
            await refresh();
        }
    };

    return (
        <>
            <section className="mb-4 rounded-xl border border-border bg-surface p-4 shadow-card sm:p-6">
                <div className="flex flex-wrap items-start gap-3">
                    <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
                        <ArrowRightLeft className="h-5 w-5" />
                    </span>
                    <div className="min-w-0 flex-1">
                        <h2 className="text-base font-semibold text-fg">{et('dnsEngine.title')}</h2>
                        <p className="mt-0.5 text-sm leading-relaxed text-fg-muted">{et('dnsEngine.subtitle')}</p>
                    </div>
                    <Button
                        variant="secondary"
                        icon={RefreshCw}
                        disabled={loading || review?.committing === true}
                        onClick={() => void refresh()}
                    >
                        {et('dnsEngine.refresh')}
                    </Button>
                </div>

                {loading && !snapshot && !loadError && (
                    <div className="mt-5 flex min-h-24 items-center justify-center text-fg-muted" role="status" aria-label={t('common.loading')}>
                        <Loader2 className="h-5 w-5 animate-spin" />
                    </div>
                )}
                {loadError && (
                    <div className="mt-4 flex items-start gap-2 rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
                        <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0" />
                        <span>{loadError}</span>
                    </div>
                )}

                {snapshot && (
                    <>
                        <div className="mt-4 flex flex-wrap items-center gap-2 text-xs">
                            <span className={`rounded-full border px-2.5 py-1 font-semibold ${
                                snapshot.state === 'ready'
                                    ? 'border-success/30 bg-success/10 text-success'
                                    : 'border-warning/35 bg-warning/10 text-warning'
                            }`}>
                                {et(`dnsEngine.state.${snapshot.state}` as DNSEngineCopyKey)}
                            </span>
                            <span className="text-fg-muted">
                                {et('dnsEngine.zoneSummary', {
                                    zones: snapshot.zone_count,
                                    pending: snapshot.pending_zone_count,
                                    dnssec: snapshot.dnssec_zone_count,
                                })}
                            </span>
                            {snapshot.topology === 'paired' && snapshot.active_engine !== null && (
                                <span
                                    data-testid="dns-pair-readiness"
                                    className={`rounded-full border px-2.5 py-1 font-semibold ${
                                        snapshot.pair_role === 'primary' && snapshot.pair_ready
                                            ? 'border-success/30 bg-success/10 text-success'
                                            : snapshot.pair_role === 'secondary'
                                              ? 'border-primary/25 bg-primary/5 text-primary'
                                              : 'border-warning/35 bg-warning/10 text-warning'
                                    }`}
                                >
                                    {snapshot.pair_role === 'secondary'
                                        ? et('dnsEngine.pair.secondaryReadOnly')
                                        : snapshot.pair_role === 'primary' && snapshot.pair_ready
                                          ? et('dnsEngine.pair.primaryReady')
                                          : snapshot.pair_role === 'primary'
                                            ? et('dnsEngine.pair.primaryWaiting')
                                            : et('dnsEngine.pair.roleUnresolved')}
                                </span>
                            )}
                        </div>

                        <div className="mt-4 grid gap-3 md:grid-cols-2">
                            {DNS_ENGINE_IDS.map((id) => {
                                const engine = snapshot.engines.find((candidate) => candidate.id === id)!;
                                const Icon = engineIcon(id);
                                const canReview = !actionsLocked
                                    && !identityReviewLocked
                                    && snapshot.state !== 'switching'
                                    && (engine.status === 'available'
                                        || engine.status === 'installed_standby'
                                        || engine.status === 'unmanaged');
                                const reviewLabel = engine.status === 'available'
                                    ? et('dnsEngine.reviewInstall')
                                    : engine.status === 'unmanaged'
                                      ? snapshot.active_engine === null
                                        && snapshot.topology === 'paired'
                                        && id === 'pdns'
                                          ? et('dnsEngine.reviewReconfigure')
                                          : et('dnsEngine.reviewAdopt')
                                      : et('dnsEngine.reviewSwitch');
                                return (
                                    <article key={id} className="rounded-xl border border-border bg-surface-2/30 p-4">
                                        <div className="flex items-start gap-3">
                                            <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-surface text-fg-muted">
                                                <Icon className="h-4.5 w-4.5" />
                                            </span>
                                            <div className="min-w-0 flex-1">
                                                <div className="flex flex-wrap items-center gap-2">
                                                    <h3 className="font-semibold text-fg">{engineName(id)}</h3>
                                                    <span className={`rounded-full border px-2 py-0.5 text-xs font-semibold ${statusStyle(engine.status)}`}>
                                                        {et(`dnsEngine.status.${engine.status}` as DNSEngineCopyKey)}
                                                    </span>
                                                </div>
                                                <p className="mt-1 text-xs leading-relaxed text-fg-muted">
                                                    {et(`dnsEngine.engine.${id}.description` as DNSEngineCopyKey)}
                                                </p>
                                            </div>
                                        </div>
                                        <div className="mt-4 flex min-h-9 items-center justify-end">
                                            {engine.status === 'active' ? (
                                                <span className="inline-flex items-center gap-1.5 text-sm font-semibold text-success">
                                                    <CheckCircle2 className="h-4 w-4" />
                                                    {et('dnsEngine.activeNow')}
                                                </span>
                                            ) : engine.status === 'conflict' ? (
                                                <span className="inline-flex items-center gap-1.5 text-sm font-medium text-warning">
                                                    <AlertTriangle className="h-4 w-4" />
                                                    {et('dnsEngine.resolveConflict')}
                                                </span>
                                            ) : actionsLocked ? null : (
                                                <Button
                                                    variant="secondary"
                                                    icon={engine.status === 'available' ? DownloadCloud : ArrowRightLeft}
                                                    disabled={!canReview || review !== null}
                                                    onClick={() => void requestPreview(id)}
                                                >
                                                    {reviewLabel}
                                                </Button>
                                            )}
                                        </div>
                                    </article>
                                );
                            })}
                        </div>

                        {identityReviewLocked && !actionsLocked && (
                            <p
                                className="mt-4 rounded-lg border border-warning/30 bg-warning/5 px-3 py-2 text-xs leading-relaxed text-fg-muted"
                                data-testid="dns-engine-identity-lock"
                                role="note"
                            >
                                {et('dnsEngine.identity.reviewLocked')}
                            </p>
                        )}

                        {!actionsLocked && (
                            <p className="mt-4 rounded-lg border border-primary/15 bg-primary/5 px-3 py-2 text-xs leading-relaxed text-fg-muted">
                                {et('dnsEngine.reviewSafety')}
                            </p>
                        )}
                    </>
                )}
            </section>

            {review && !actionsLocked && (
                <DNSEngineReviewDialog
                    review={review}
                    onAcknowledge={(acknowledged) => setReview((current) => current ? { ...current, acknowledged } : current)}
                    onCancel={() => {
                        if (!review.committing) setReview(null);
                    }}
                    onConfirm={() => void commitSwitch()}
                />
            )}
        </>
    );
}

function DNSEngineReviewDialog({
    review,
    onAcknowledge,
    onCancel,
    onConfirm,
}: {
    review: ReviewState;
    onAcknowledge: (acknowledged: boolean) => void;
    onCancel: () => void;
    onConfirm: () => void;
}) {
    const { t, locale } = useI18n();
    const et = (key: DNSEngineCopyKey, vars?: Record<string, string | number>) =>
        dnsEngineText(locale, key, vars);
    const { preview } = review;
    const blocked = !preview || preview.blockers.length > 0;
    const confirmationDisabled = review.loading
        || review.committing
        || review.requestID === null
        || blocked
        || (preview?.requires_downtime_acknowledgement === true && !review.acknowledged);

    useEffect(() => {
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.key === 'Escape' && !review.committing) onCancel();
        };
        document.addEventListener('keydown', onKeyDown);
        return () => document.removeEventListener('keydown', onKeyDown);
    }, [onCancel, review.committing]);

    return (
        <div
            className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
            onMouseDown={(event) => {
                if (event.currentTarget === event.target && !review.committing) onCancel();
            }}
        >
            <div
                role="dialog"
                aria-modal="true"
                aria-labelledby="dns-engine-review-title"
                aria-describedby="dns-engine-review-description"
                aria-busy={review.loading || review.committing}
                className="max-h-[90vh] w-full max-w-2xl overflow-y-auto rounded-2xl border border-border bg-surface p-5 shadow-xl sm:p-6"
            >
                <div className="flex items-start gap-3">
                    <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
                        <ArrowRightLeft className="h-5 w-5" />
                    </span>
                    <div className="min-w-0">
                        <h3 id="dns-engine-review-title" className="text-lg font-semibold text-fg">
                            {et('dnsEngine.reviewTitle', { engine: engineName(review.target) })}
                        </h3>
                        <p id="dns-engine-review-description" className="mt-1 text-sm leading-5 text-fg-muted">
                            {et('dnsEngine.reviewDescription')}
                        </p>
                    </div>
                </div>

                {review.loading && (
                    <div className="my-8 flex items-center justify-center gap-2 text-sm text-fg-muted" role="status">
                        <Loader2 className="h-5 w-5 animate-spin" />
                        {et('dnsEngine.reviewLoading')}
                    </div>
                )}

                {review.error && (
                    <div className="mt-4 flex items-start gap-2 rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
                        <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0" />
                        <span>{review.error}</span>
                    </div>
                )}

                {preview && (
                    <>
                        <dl className="mt-5 grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
                            <PreviewFact label={et('dnsEngine.preview.action')} value={et(`dnsEngine.action.${preview.action}` as DNSEngineCopyKey)} />
                            <PreviewFact label={et('dnsEngine.preview.zones')} value={String(preview.zone_count)} />
                            <PreviewFact label={et('dnsEngine.preview.pending')} value={String(preview.pending_zone_count)} warning={preview.pending_zone_count > 0} />
                            <PreviewFact label={et('dnsEngine.preview.dnssec')} value={String(preview.dnssec_zone_count)} warning={preview.dnssec_zone_count > 0} />
                            <PreviewFact label={et('dnsEngine.preview.topology')} value={et(`dnsEngine.topology.${preview.topology}` as DNSEngineCopyKey)} />
                            <PreviewFact
                                label={et('dnsEngine.preview.downtime')}
                                value={preview.estimated_downtime_seconds === 0
                                    ? et('dnsEngine.noDowntimeExpected')
                                    : et('dnsEngine.downtimeSeconds', { seconds: preview.estimated_downtime_seconds })}
                                warning={preview.estimated_downtime_seconds > 0}
                            />
                        </dl>

                        <div className="mt-5">
                            <h4 className="text-sm font-semibold text-fg">{et('dnsEngine.impactsTitle')}</h4>
                            <ul className="mt-2 space-y-2">
                                {preview.impacts.map((code, index) => {
                                    const key = knownImpactKeys[code as keyof typeof knownImpactKeys];
                                    return (
                                        <li key={`${code}-${index}`} className="flex items-start gap-2 text-sm leading-5 text-fg-muted">
                                            <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-primary" />
                                            <span>{et(key ?? 'dnsEngine.impact.unknown')}</span>
                                        </li>
                                    );
                                })}
                            </ul>
                        </div>

                        {preview.blockers.length > 0 && (
                            <div className="mt-5 rounded-xl border border-warning/35 bg-warning/10 p-4" role="alert">
                                <h4 className="flex items-center gap-2 text-sm font-semibold text-fg">
                                    <AlertTriangle className="h-4 w-4 text-warning" />
                                    {et('dnsEngine.blockersTitle')}
                                </h4>
                                <ul className="mt-2 list-disc space-y-1 pl-5 text-sm leading-5 text-fg-muted">
                                    {preview.blockers.map((blocker, index) => {
                                        const key = knownBlockerKeys[blocker.code as keyof typeof knownBlockerKeys];
                                        return <li key={`${blocker.code}-${index}`}>{et(key ?? 'dnsEngine.blocker.unknown')}</li>;
                                    })}
                                </ul>
                            </div>
                        )}

                        {preview.requires_downtime_acknowledgement && preview.blockers.length === 0 && (
                            <label className="mt-5 flex cursor-pointer items-start gap-3 rounded-xl border border-warning/35 bg-warning/5 p-4">
                                <input
                                    type="checkbox"
                                    checked={review.acknowledged}
                                    disabled={review.committing}
                                    onChange={(event) => onAcknowledge(event.target.checked)}
                                    className="mt-0.5 h-4 w-4 accent-primary"
                                />
                                <span className="text-sm leading-5 text-fg">{et('dnsEngine.downtimeAcknowledgement')}</span>
                            </label>
                        )}
                    </>
                )}

                <div className="mt-6 flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
                    <Button variant="secondary" autoFocus disabled={review.committing} onClick={onCancel}>
                        {t('common.cancel')}
                    </Button>
                    <Button
                        variant="primary"
                        icon={ArrowRightLeft}
                        disabled={confirmationDisabled}
                        onClick={onConfirm}
                    >
                        {review.committing ? et('dnsEngine.starting') : et('dnsEngine.confirm')}
                    </Button>
                </div>
            </div>
        </div>
    );
}

function PreviewFact({ label, value, warning = false }: { label: string; value: string; warning?: boolean }) {
    return (
        <div className="rounded-lg border border-border bg-surface-2/40 px-3 py-2.5">
            <dt className="text-xs font-medium text-fg-muted">{label}</dt>
            <dd className={`mt-1 text-sm font-semibold ${warning ? 'text-warning' : 'text-fg'}`}>{value}</dd>
        </div>
    );
}
