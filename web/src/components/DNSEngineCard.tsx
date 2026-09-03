import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
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
import { apiErrorText, readApiError, type ApiError } from '../lib/apiError';
import { dnsEngineIdentityReviewLocked } from '../lib/dnsIdentityPlan';
import {
    DNS_ENGINE_IDS,
    decodeDNSEngineSnapshot,
    decodeDNSEngineSwitchPreview,
    type DNSEngineEntry,
    type DNSEngineID,
    type DNSEngineOperation,
    type DNSEngineSnapshot,
    type DNSEngineSwitchPreview,
} from '../lib/dnsEngineContract';
import { Button } from './ui';
import { showToast } from './Toast';
import {
    useComponentOperation,
    type InteractionBlockLease,
} from './ComponentOperation';

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

interface DNSEngineSwitchRequestBody {
    request_id: string;
    target_engine: DNSEngineID;
    expected_source: DNSEngineID | null;
    expected_revision: number;
    preview_token: string;
    downtime_acknowledged: boolean;
}

type DNSEngineSwitchHTTPResult =
    | { ok: true; payload: unknown }
    | { ok: false; status: number; error: ApiError };

interface OperationGuardState {
    requestID: string;
    target: DNSEngineID;
    mode: 'submitting' | 'verifying' | 'stalled' | 'deadline' | 'recovery_required';
    startedAt: number;
    attempts: number;
    reconcileAttempts: number;
    operation: DNSEngineOperation | null;
    trackingMessage?: string;
    completionToast?: {
        type: 'error' | 'warning';
        message: string;
    };
}

interface DNSOperationRecoveryMarker {
    version: 1;
    requestID: string;
    target: DNSEngineID;
    request: DNSEngineSwitchRequestBody;
    createdAt: number;
}

const DNS_OPERATION_RECOVERY_KEY = 'celikpanel.dns-engine.operation-recovery';
const dnsOperationIDPattern = /^[a-f0-9]{32}$/;

function validSwitchRequest(value: unknown): value is DNSEngineSwitchRequestBody {
    if (!value || typeof value !== 'object') return false;
    const request = value as Record<string, unknown>;
    return typeof request.request_id === 'string'
        && dnsOperationIDPattern.test(request.request_id)
        && (request.target_engine === 'pdns' || request.target_engine === 'bind')
        && (request.expected_source === null
            || request.expected_source === 'pdns'
            || request.expected_source === 'bind')
        && typeof request.expected_revision === 'number'
        && Number.isSafeInteger(request.expected_revision)
        && request.expected_revision >= 0
        && typeof request.preview_token === 'string'
        && dnsOperationIDPattern.test(request.preview_token)
        && typeof request.downtime_acknowledged === 'boolean';
}

function readDNSOperationMarker(): DNSOperationRecoveryMarker | null {
    try {
        const parsed = JSON.parse(sessionStorage.getItem(DNS_OPERATION_RECOVERY_KEY) ?? 'null');
        if (!parsed || typeof parsed !== 'object') return null;
        const marker = parsed as Record<string, unknown>;
        if (marker.version !== 1 || !validSwitchRequest(marker.request)
            || marker.requestID !== marker.request.request_id
            || marker.target !== marker.request.target_engine) return null;
        const createdAt = typeof marker.createdAt === 'number'
            && Number.isSafeInteger(marker.createdAt)
            && marker.createdAt > 0
            && marker.createdAt <= Date.now()
            ? marker.createdAt
            : Date.now();
        return {
            version: 1,
            requestID: marker.requestID,
            target: marker.target,
            request: marker.request,
            createdAt,
        } as DNSOperationRecoveryMarker;
    } catch {
        return null;
    }
}

function storeDNSOperationMarker(request: DNSEngineSwitchRequestBody, createdAt: number): boolean {
    const marker: DNSOperationRecoveryMarker = {
        version: 1,
        requestID: request.request_id,
        target: request.target_engine,
        request,
        createdAt,
    };
    try {
        sessionStorage.setItem(DNS_OPERATION_RECOVERY_KEY, JSON.stringify(marker));
        return true;
    } catch {
        return false;
    }
}

function clearDNSOperationMarker(requestID: string): void {
    try {
        if (readDNSOperationMarker()?.requestID === requestID) {
            sessionStorage.removeItem(DNS_OPERATION_RECOVERY_KEY);
        }
    } catch {
        // Keep the marker when its exact ownership cannot be proven.
    }
}

const knownBlockerKeys = {
    dns_identity_required: 'dnsEngine.blocker.identityRequired',
    paired_topology_unsupported: 'dnsEngine.blocker.pairedTopology',
    dnssec_unsupported: 'dnsEngine.blocker.dnssec',
    pending_zone_sync: 'dnsEngine.blocker.pendingZones',
    operation_running: 'dnsEngine.blocker.operationRunning',
    unmanaged_dns_detected: 'dnsEngine.blocker.unmanaged',
    mutations_held: 'dnsEngine.blocker.mutationsHeld',
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

// A paired-primary snapshot can spend up to 15 seconds proving the peer
// catalog. The browser deadline must be longer than that backend proof.
const dnsEngineStatusRequestTimeoutMs = 30_000;
const dnsEngineGuardPollDelayMs = 3_000;
const dnsEngineGuardSlowPollDelayMs = 15_000;
const dnsEngineGuardStalledAfterMs = 2 * 60_000;
const dnsEngineGuardMaxElapsedMs = 31 * 60_000;
const dnsEngineGuardMaxAttempts = 180;
const dnsEngineGuardMaxReconcileAttempts = 3;
const dnsEngineGuardReconcileDelayMs = 60_000;

function createRequestID(): string | null {
    try {
        const bytes = new Uint8Array(16);
        crypto.getRandomValues(bytes);
        return Array.from(bytes, (value) => value.toString(16).padStart(2, '0')).join('');
    } catch {
        return null;
    }
}

async function submitDNSEngineSwitch(
    request: DNSEngineSwitchRequestBody,
): Promise<DNSEngineSwitchHTTPResult> {
    const requestController = new AbortController();
    const requestTimeout = setTimeout(
        () => requestController.abort(),
        dnsEngineStatusRequestTimeoutMs,
    );
    try {
        const response = await fetch('/api/v1/dns/engine/switch', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(request),
            signal: requestController.signal,
        });
        // Keep the deadline active until the complete body is consumed. If
        // body reading is interrupted, no partial error envelope may be used
        // as proof that a mutation never persisted.
        const bodyText = await response.text();
        if (!response.ok) {
            return {
                ok: false,
                status: response.status,
                error: await readApiError(new Response(bodyText)),
            };
        }
        let payload: unknown;
        try {
            payload = JSON.parse(bodyText);
        } catch {
            payload = null;
        }
        return { ok: true, payload };
    } finally {
        clearTimeout(requestTimeout);
    }
}

function engineName(id: DNSEngineID): string {
    return id === 'pdns' ? 'PowerDNS' : 'BIND';
}

function engineIcon(id: DNSEngineID) {
    return id === 'pdns' ? Database : Network;
}

function operationElapsedSeconds(operation: DNSEngineOperation): number {
    const started = Date.parse(operation.started_at);
    const finished = ['running', 'rolling_back'].includes(operation.status)
        ? Date.now()
        : Date.parse(operation.updated_at);
    return Math.max(0, Math.floor((finished - started) / 1000));
}

function elapsedText(
    seconds: number,
    text: (key: DNSEngineCopyKey, vars?: Record<string, string | number>) => string,
): string {
    if (seconds < 60) return text('dnsEngine.operation.elapsedSeconds', { seconds });
    if (seconds < 3600) {
        return text('dnsEngine.operation.elapsedMinutes', { minutes: Math.floor(seconds / 60) });
    }
    return text('dnsEngine.operation.elapsedHours', {
        hours: Math.floor(seconds / 3600),
        minutes: Math.floor((seconds % 3600) / 60),
    });
}

function operationTimestamp(value: string, locale: 'en' | 'tr'): string {
    return new Date(value).toLocaleString(locale === 'tr' ? 'tr-TR' : 'en-US');
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
    const { acquireInteractionBlock } = useComponentOperation();
    const et = (key: DNSEngineCopyKey, vars?: Record<string, string | number>) =>
        dnsEngineText(locale, key, vars);
    const [snapshot, setSnapshot] = useState<DNSEngineSnapshot | null>(null);
    const [loading, setLoading] = useState(true);
    const [loadError, setLoadError] = useState('');
    const [trackingError, setTrackingError] = useState('');
    const [trackingReadError, setTrackingReadError] = useState('');
    const [trackingDelayed, setTrackingDelayed] = useState(false);
    const [review, setReview] = useState<ReviewState | null>(null);
    const [initialMarker] = useState(readDNSOperationMarker);
    const [operationGuard, setOperationGuard] = useState<OperationGuardState | null>(() => (
        initialMarker
            ? {
                requestID: initialMarker.requestID,
                target: initialMarker.target,
                mode: 'verifying',
                startedAt: initialMarker.createdAt,
                attempts: 0,
                reconcileAttempts: 0,
                operation: null,
            }
            : null
    ));
    const operationGuardRef = useRef<OperationGuardState | null>(operationGuard);
    const operationLeaseRef = useRef<InteractionBlockLease | null>(null);
    operationGuardRef.current = operationGuard;
    const identityReviewLocked = dnsEngineIdentityReviewLocked(identityPlanCurrent, snapshot);

    const guardView = useCallback((guard: OperationGuardState) => {
        const operation = guard.operation;
        const elapsedSeconds = operation
            ? operationElapsedSeconds(operation)
            : Math.max(0, Math.floor((Date.now() - guard.startedAt) / 1000));
        const phase = operation
            ? et(`dnsEngine.operation.phase.${operation.phase}` as DNSEngineCopyKey)
            : et('dnsEngine.guard.awaitingPhase');
        const status = guard.mode === 'stalled'
            ? et('dnsEngine.guard.stalledStatus')
            : guard.mode === 'deadline'
              ? et('dnsEngine.guard.deadlineStatus')
            : guard.mode === 'recovery_required'
              ? et('dnsEngine.guard.recoveryStatus')
              : guard.mode === 'submitting' && operation === null
                ? et('dnsEngine.guard.submitting')
                : operation
                  ? phase
                  : et('dnsEngine.guard.verifying');
        return {
            title: et('dnsEngine.guard.title', { engine: engineName(guard.target) }),
            status,
            hint: et('dnsEngine.guard.navigationLocked'),
            operationID: guard.requestID,
            busy: !['stalled', 'deadline', 'recovery_required'].includes(guard.mode),
            severity: ['deadline', 'recovery_required'].includes(guard.mode)
                ? 'error' as const
                : guard.mode === 'stalled' || guard.trackingMessage
                  ? 'warning' as const
                  : undefined,
            message: guard.trackingMessage,
            details: [
                { label: et('dnsEngine.operation.phase'), value: phase },
                { label: et('dnsEngine.operation.elapsed'), value: elapsedText(elapsedSeconds, et) },
                {
                    label: et('dnsEngine.operation.updated'),
                    value: operation
                        ? operationTimestamp(operation.updated_at, locale)
                        : et('dnsEngine.guard.awaitingUpdate'),
                },
            ],
        };
    }, [locale]);

    const holdOperationGuard = useCallback((guard: OperationGuardState) => {
        operationGuardRef.current = guard;
        setOperationGuard(guard);
        const globallyBlocking = guard.mode !== 'deadline' && guard.mode !== 'recovery_required';
        if (!globallyBlocking) {
            operationLeaseRef.current?.release();
            operationLeaseRef.current = null;
        } else if (operationLeaseRef.current) {
            operationLeaseRef.current.update(guardView(guard));
        } else {
            operationLeaseRef.current = acquireInteractionBlock(guardView(guard));
        }
    }, [acquireInteractionBlock, guardView]);

    useLayoutEffect(() => {
        if (!operationGuard) return;
        if (operationGuard.mode === 'deadline' || operationGuard.mode === 'recovery_required') {
            operationLeaseRef.current?.release();
            operationLeaseRef.current = null;
            return;
        }
        if (operationLeaseRef.current) operationLeaseRef.current.update(guardView(operationGuard));
        else operationLeaseRef.current = acquireInteractionBlock(guardView(operationGuard));
    }, [acquireInteractionBlock, guardView, operationGuard]);

    useEffect(() => () => {
        operationLeaseRef.current?.release();
        operationLeaseRef.current = null;
    }, []);

    const refresh = useCallback(async (automaticTracking = false): Promise<DNSEngineSnapshot | null> => {
        setLoading(true);
        setLoadError('');
        if (!automaticTracking) setTrackingReadError('');
        const requestController = new AbortController();
        const requestTimeout = setTimeout(
            () => requestController.abort(),
            dnsEngineStatusRequestTimeoutMs,
        );
        const failRefresh = (message: string) => {
            if (automaticTracking) {
                setTrackingReadError(et('dnsEngine.operation.trackingReadFailed'));
                return;
            }
            // Keep the last verified state visible as evidence, but fail-close
            // every mutation action until a fresh authoritative read succeeds.
            setLoadError(message);
        };
        try {
            const response = await fetch('/api/v1/dns/engine', {
                method: 'GET',
                cache: 'no-store',
                signal: requestController.signal,
            });
            if (!response.ok) {
                await readApiError(response);
                failRefresh(et('dnsEngine.stateUnavailable'));
                return null;
            }
            let payload: unknown;
            try {
                payload = await response.json();
            } catch {
                payload = null;
            }
            const decoded = decodeDNSEngineSnapshot(payload);
            if (decoded === null) {
                failRefresh(et('dnsEngine.stateInvalid'));
                return null;
            }
            if (automaticTracking) setTrackingReadError('');
            setLoadError('');
            setSnapshot(decoded);
            onSnapshotChange?.(decoded);
            if (decoded.operation && operationGuardRef.current === null
                && (
                    decoded.state === 'switching'
                    || decoded.operation.status === 'recovery_required'
                )) {
                const recoveryRequired = decoded.operation.status === 'recovery_required';
                if (recoveryRequired) clearDNSOperationMarker(decoded.operation.request_id);
                if (operationGuardRef.current === null) {
                    holdOperationGuard({
                        requestID: decoded.operation.request_id,
                        target: decoded.operation.target_engine,
                        mode: recoveryRequired ? 'recovery_required' : 'verifying',
                        startedAt: Date.parse(decoded.operation.started_at),
                        attempts: 0,
                        reconcileAttempts: 0,
                        operation: decoded.operation,
                        trackingMessage: recoveryRequired
                            ? decoded.operation.last_error
                                || et('dnsEngine.operation.status.recovery_required')
                            : undefined,
                    });
                }
            }
            return decoded;
        } catch {
            failRefresh(et('dnsEngine.stateUnavailable'));
            return null;
        } finally {
            clearTimeout(requestTimeout);
            setLoading(false);
        }
    }, [holdOperationGuard, locale, onSnapshotChange]);

    const reconcileAndRefresh = useCallback(async (automaticTracking = false): Promise<DNSEngineSnapshot | null> => {
        setReview(null);
        setLoading(true);
        setLoadError('');
        const requestController = new AbortController();
        const requestTimeout = setTimeout(
            () => requestController.abort(),
            dnsEngineStatusRequestTimeoutMs,
        );
        try {
            const response = await fetch('/api/v1/dns/engine/reconcile', {
                method: 'POST',
                signal: requestController.signal,
            });
            if (!response.ok) {
                const apiError = await readApiError(response);
                setTrackingError(apiError.code
                    ? apiErrorText(apiError, t)
                    : et('dnsEngine.trackingReconcileFailed'));
            } else {
                setTrackingError('');
            }
        } catch {
            setTrackingError(et('dnsEngine.trackingReconcileFailed'));
        } finally {
            clearTimeout(requestTimeout);
        }
        return await refresh(automaticTracking);
    }, [locale, refresh, t]);

    useEffect(() => {
        void refresh();
    }, [refresh]);

    const completeGuardedVerification = useCallback((
        decoded: DNSEngineSnapshot,
        guard: OperationGuardState,
    ): boolean => {
        const exactOperation = decoded.operation?.request_id === guard.requestID
            && decoded.operation.target_engine === guard.target
            ? decoded.operation
            : null;
        if (exactOperation?.status === 'recovery_required') {
            setSnapshot(decoded);
            onSnapshotChange?.(decoded);
            setTrackingDelayed(false);
            setTrackingError('');
            setTrackingReadError('');
            clearDNSOperationMarker(guard.requestID);
            holdOperationGuard({
                ...guard,
                mode: 'recovery_required',
                operation: exactOperation,
                trackingMessage: exactOperation.last_error
                    || et('dnsEngine.operation.status.recovery_required'),
            });
            return true;
        }
        const operationSucceeded = exactOperation?.status === 'succeeded';
        const operationTerminated = operationSucceeded
            || exactOperation?.status === 'rolled_back'
            || exactOperation?.status === 'failed';
        if (!operationTerminated) return false;
        if (operationGuardRef.current?.requestID !== guard.requestID) return true;
        setSnapshot(decoded);
        onSnapshotChange?.(decoded);
        setLoadError('');
        setTrackingDelayed(false);
        setTrackingError('');
        setTrackingReadError('');
        clearDNSOperationMarker(guard.requestID);
        operationGuardRef.current = null;
        setOperationGuard((current) => current?.requestID === guard.requestID ? null : current);
        operationLeaseRef.current?.release();
        operationLeaseRef.current = null;
        if (operationSucceeded) {
            showToast('success', et('dnsEngine.switchCompleted'));
        } else if (guard.completionToast) {
            showToast(guard.completionToast.type, guard.completionToast.message);
        } else {
            showToast('error', decoded.operation?.last_error || et('dnsEngine.switchFailed'));
        }
        return true;
    }, [holdOperationGuard, locale, onSnapshotChange]);

    useEffect(() => {
        if (!operationGuard
            || operationGuard.mode === 'deadline'
            || operationGuard.mode === 'recovery_required') return;
        const requestID = operationGuard.requestID;
        const target = operationGuard.target;
        let attempts = operationGuard.attempts;
        let reconcileAttempts = operationGuard.reconcileAttempts;
        let lastReconcileAt = 0;
        let cancelled = false;
        let timer: ReturnType<typeof setTimeout> | undefined;

        const schedule = (delay: number) => {
            if (!cancelled) timer = setTimeout(() => void verify(), delay);
        };
        const currentGuard = (): OperationGuardState | null => {
            const current = operationGuardRef.current;
            return current?.requestID === requestID && current.target === target ? current : null;
        };
        const stopAtDeadline = (guard: OperationGuardState) => {
            setTrackingDelayed(true);
            holdOperationGuard({
                ...guard,
                mode: 'deadline',
                attempts,
                reconcileAttempts,
                trackingMessage: et('dnsEngine.guard.deadline'),
            });
        };
        const verify = async () => {
            let guard = currentGuard();
            if (!guard) return;
            const elapsedMs = Date.now() - guard.startedAt;
            // A persisted deadline marker must get one fresh authoritative
            // read after reload before the local guard returns to deadline.
            if (attempts > 0 && (
                attempts >= dnsEngineGuardMaxAttempts
                || elapsedMs >= dnsEngineGuardMaxElapsedMs
            )) {
                stopAtDeadline(guard);
                return;
            }
            attempts += 1;
            let decoded = await refresh(true);
            if (cancelled) return;
            guard = currentGuard();
            if (!guard) return;
            let exactOperation = decoded?.operation?.request_id === requestID
                && decoded.operation.target_engine === target
                ? decoded.operation
                : null;
            if (decoded !== null && completeGuardedVerification(decoded, guard)) return;
            if (Date.now() - guard.startedAt >= dnsEngineGuardMaxElapsedMs
                || attempts >= dnsEngineGuardMaxAttempts) {
                stopAtDeadline({
                    ...guard,
                    operation: exactOperation ?? guard.operation,
                });
                return;
            }

            let durableStalled = exactOperation !== null
                ? Date.now() - Date.parse(exactOperation.updated_at) >= dnsEngineGuardStalledAfterMs
                : Date.now() - guard.startedAt >= dnsEngineGuardStalledAfterMs;
            if (durableStalled
                && exactOperation !== null
                && reconcileAttempts < dnsEngineGuardMaxReconcileAttempts
                && Date.now() - lastReconcileAt >= dnsEngineGuardReconcileDelayMs) {
                reconcileAttempts += 1;
                lastReconcileAt = Date.now();
                decoded = await reconcileAndRefresh(true);
                if (cancelled) return;
                guard = currentGuard();
                if (!guard) return;
                exactOperation = decoded?.operation?.request_id === requestID
                    && decoded.operation.target_engine === target
                    ? decoded.operation
                    : exactOperation;
                if (decoded !== null && completeGuardedVerification(decoded, guard)) return;
                durableStalled = exactOperation !== null
                    ? Date.now() - Date.parse(exactOperation.updated_at) >= dnsEngineGuardStalledAfterMs
                    : Date.now() - guard.startedAt >= dnsEngineGuardStalledAfterMs;
            }

            const trackingMessage = durableStalled
                ? et(exactOperation
                    ? 'dnsEngine.guard.stalled'
                    : 'dnsEngine.guard.awaitingStalled')
                : undefined;
            setTrackingDelayed(durableStalled);
            holdOperationGuard({
                ...guard,
                mode: durableStalled ? 'stalled' : 'verifying',
                attempts,
                reconcileAttempts,
                operation: exactOperation ?? guard.operation,
                trackingMessage,
            });
            schedule(durableStalled ? dnsEngineGuardSlowPollDelayMs : dnsEngineGuardPollDelayMs);
        };

        timer = setTimeout(() => void verify(), 500);
        return () => {
            cancelled = true;
            if (timer !== undefined) clearTimeout(timer);
        };
        // The exact request owns one polling loop. Progress updates must not
        // recreate it and must never replay the mutation POST.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [operationGuard?.requestID]);

    useEffect(() => {
        if (actionsLocked) setReview(null);
    }, [actionsLocked]);

    const requestPreview = async (target: DNSEngineID) => {
        if (actionsLocked || loading || operationGuardRef.current !== null) return;
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
        if (actionsLocked || loading || operationGuardRef.current !== null) return;
        if (actionsLocked || !current || !preview || current.loading
            || current.committing || preview.blockers.length > 0) return;
        if (preview.requires_downtime_acknowledgement && !current.acknowledged) return;
        const requestID = current.requestID;
        if (requestID === null) return;

        const requestBody: DNSEngineSwitchRequestBody = {
            request_id: requestID,
            target_engine: current.target,
            expected_source: current.base.active_engine,
            expected_revision: current.base.revision,
            preview_token: preview.preview_token,
            downtime_acknowledged: current.acknowledged,
        };
        const startedAt = Date.now();
        if (!storeDNSOperationMarker(requestBody, startedAt)) {
            setReview({ ...current, error: et('dnsEngine.recoveryUnavailable') });
            return;
        }
        holdOperationGuard({
            requestID,
            target: current.target,
            mode: 'submitting',
            startedAt,
            attempts: 0,
            reconcileAttempts: 0,
            operation: null,
        });
        setReview({ ...current, committing: true, error: '' });
        try {
            const result = await submitDNSEngineSwitch(requestBody);
            // The read-only status loop can prove a terminal result while the
            // original POST response is still in flight. Never recreate a
            // released guard from a late response.
            const returnedGuard = operationGuardRef.current as OperationGuardState | null;
            if (returnedGuard?.requestID !== requestID) return;
            if (!result.ok) {
                const apiError = result.error;
                const mutationApplied = apiError.mutationApplied === true;
                const partialSuccess = apiError.partialSuccess === true;
                // The commit endpoint consumes the preview authority before it
                // starts a mutation. Close the stale dialog for every terminal
                // HTTP response; a retry must begin with a fresh state and
                // preview instead of reusing the old token.
                setReview(null);
                let completionToast: OperationGuardState['completionToast'];
                if (mutationApplied) {
                    completionToast = {
                        type: 'warning',
                        message: et('dnsEngine.switchAppliedNeedsRefresh'),
                    };
                } else if (partialSuccess) {
                    completionToast = {
                        type: 'warning',
                        message: et('dnsEngine.switchPartialUnverified'),
                    };
                } else {
                    completionToast = {
                        type: 'error',
                        message: apiError.code
                            ? apiErrorText(apiError, t)
                            : et('dnsEngine.switchFailed'),
                    };
                }
                const prePersistRefusal = !apiError.code
                    && !mutationApplied
                    && !partialSuccess
                    && (result.status === 400 || result.status === 409);
                if (prePersistRefusal) {
                    clearDNSOperationMarker(requestID);
                    operationGuardRef.current = null;
                    setOperationGuard(null);
                    operationLeaseRef.current?.release();
                    operationLeaseRef.current = null;
                    showToast('error', completionToast.message);
                    void refresh();
                    return;
                }
                holdOperationGuard({
                    ...returnedGuard,
                    mode: 'verifying',
                    completionToast,
                });
                return;
            }

            const decoded = decodeDNSEngineSnapshot(result.payload);
            if (decoded === null) {
                setReview(null);
                holdOperationGuard({
                    ...returnedGuard,
                    mode: 'verifying',
                    completionToast: {
                        type: 'warning',
                        message: et('dnsEngine.switchAmbiguous'),
                    },
                });
                return;
            }
            if (decoded.state === 'switching') {
                setSnapshot(decoded);
                onSnapshotChange?.(decoded);
                setReview(null);
                const returnedOperation = decoded.operation;
                holdOperationGuard({
                    ...returnedGuard,
                    mode: 'verifying',
                    operation: returnedOperation?.request_id === requestID
                        && returnedOperation.target_engine === current.target
                        ? returnedOperation
                        : returnedGuard.operation,
                });
                return;
            }

            // The successful POST already carries the verified terminal
            // snapshot. Adopt it before unlocking; a redundant GET can only
            // reintroduce a timeout and stale-state race.
            setReview(null);
            if (!completeGuardedVerification(decoded, {
                ...returnedGuard,
                mode: 'verifying',
            })) {
                holdOperationGuard({
                    ...returnedGuard,
                    mode: 'verifying',
                    completionToast: {
                        type: 'warning',
                        message: et('dnsEngine.switchAmbiguous'),
                    },
                });
            }
        } catch {
            // A lost response can hide an accepted mutation and the preview
            // token may already be consumed. Keep the page blocked and use
            // only the read-only status loop; never replay the switch POST.
            const returnedGuard = operationGuardRef.current as OperationGuardState | null;
            if (returnedGuard?.requestID !== requestID) return;
            setReview(null);
            holdOperationGuard({
                ...returnedGuard,
                mode: 'verifying',
                completionToast: {
                    type: 'warning',
                    message: et('dnsEngine.switchAmbiguous'),
                },
            });
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
                        disabled={loading || review?.committing === true || operationGuard !== null}
                        onClick={() => void reconcileAndRefresh()}
                    >
                        {et('dnsEngine.refresh')}
                    </Button>
                </div>

                {loading && !snapshot && !loadError && (
                    <div className="mt-5 flex min-h-24 items-center justify-center text-fg-muted" role="status" aria-label={t('common.loading')}>
                        <Loader2 className="h-5 w-5 animate-spin" />
                    </div>
                )}
                {loading && snapshot && (
                    <div className={'mt-4 flex items-center gap-2 rounded-lg border border-primary/20 bg-primary/5 px-3 py-2 text-sm text-primary'} role={'status'} aria-live={'polite'}>
                        <Loader2 className={'h-4 w-4 animate-spin'} />
                        <span>{et('dnsEngine.refreshing')}</span>
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

                        {snapshot.operation && (
                            <DNSEngineOperationProgress
                                operation={snapshot.operation}
                                trackingError={trackingError || trackingReadError}
                                trackingDelayed={trackingDelayed}
                            />
                        )}

                        <div className="mt-4 grid gap-3 md:grid-cols-2">
                            {DNS_ENGINE_IDS.map((id) => {
                                const engine = snapshot.engines.find((candidate) => candidate.id === id)!;
                                const Icon = engineIcon(id);
                                const canReview = !actionsLocked
                                    && !loading
                                    && !loadError
                                    && operationGuard === null
                                    && !identityReviewLocked
                                    && snapshot.state !== 'switching'
                                    && (engine.status === 'available'
                                        || engine.status === 'installed_standby'
                                        || engine.status === 'unmanaged');
                                // The panel's own record says this engine is
                                // serving and the server has no copy of it. One
                                // button, and it says what it does: put it back.
                                const reinstallActive = snapshot.active_engine === id
                                    && !engine.running
                                    && snapshot.topology === 'standalone'
                                    && id === 'bind';
                                const reviewLabel = reinstallActive
                                    ? et('dnsEngine.reviewReinstall')
                                    : engine.status === 'available'
                                    || (engine.status === 'installed_standby'
                                        && snapshot.active_engine === null
                                        && snapshot.state === 'unconfigured'
                                        && id === 'bind')
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
                                                    data-testid={reinstallActive ? 'dns-engine-reinstall' : undefined}
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

function DNSEngineOperationProgress({
    operation,
    trackingError,
    trackingDelayed,
}: {
    operation: DNSEngineOperation;
    trackingError: string;
    trackingDelayed: boolean;
}) {
    const { locale } = useI18n();
    const et = (key: DNSEngineCopyKey, vars?: Record<string, string | number>) =>
        dnsEngineText(locale, key, vars);
    const active = ['running', 'rolling_back'].includes(operation.status);
    const spinning = operation.status === 'running' || operation.status === 'rolling_back';
    const elapsedSeconds = operationElapsedSeconds(operation);
    const elapsed = elapsedText(elapsedSeconds, et);
    const StatusIcon = spinning
        ? Loader2
        : operation.status === 'succeeded'
          ? CheckCircle2
          : AlertTriangle;

    return (
        <div
            className="mt-4 rounded-xl border border-primary/20 bg-primary/5 p-4"
            data-testid="dns-engine-operation-progress"
            role="status"
            aria-live="polite"
        >
            <div className="flex items-start gap-3">
                <StatusIcon className={`mt-0.5 h-5 w-5 shrink-0 ${
                    spinning ? 'animate-spin text-primary' : operation.status === 'succeeded' ? 'text-success' : 'text-warning'
                }`} />
                <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center justify-between gap-2">
                        <div>
                            <h3 className="text-sm font-semibold text-fg">
                                {et('dnsEngine.operation.title')}
                            </h3>
                            <p className="mt-0.5 text-xs text-fg-muted">
                                {et(`dnsEngine.operation.status.${operation.status}` as DNSEngineCopyKey)}
                            </p>
                        </div>
                        {active && (
                            <span className="rounded-full border border-primary/25 bg-surface px-2.5 py-1 text-xs font-semibold text-primary">
                                {et('dnsEngine.operation.autoTracking')}
                            </span>
                        )}
                    </div>
                    <dl className="mt-3 grid gap-3 text-xs sm:grid-cols-2 lg:grid-cols-4">
                        <div>
                            <dt className="text-fg-muted">{et('dnsEngine.operation.target')}</dt>
                            <dd className="mt-1 font-semibold text-fg">{engineName(operation.target_engine)}</dd>
                        </div>
                        <div>
                            <dt className="text-fg-muted">{et('dnsEngine.operation.phase')}</dt>
                            <dd className="mt-1 font-semibold text-fg">
                                {et(`dnsEngine.operation.phase.${operation.phase}` as DNSEngineCopyKey)}
                            </dd>
                        </div>
                        <div>
                            <dt className="text-fg-muted">{et('dnsEngine.operation.elapsed')}</dt>
                            <dd className="mt-1 font-semibold text-fg">{elapsed}</dd>
                        </div>
                        <div>
                            <dt className="text-fg-muted">{et('dnsEngine.operation.updated')}</dt>
                            <dd className="mt-1 font-semibold text-fg">
                                {operationTimestamp(operation.updated_at, locale)}
                            </dd>
                        </div>
                    </dl>
                    <div className="mt-3">
                        <span className="text-xs text-fg-muted">{et('dnsEngine.operation.id')}</span>
                        <code className="mt-1 block break-all rounded bg-surface px-2 py-1.5 text-xs text-fg">
                            {operation.id}
                        </code>
                    </div>
                    {operation.last_error && (
                        <div className="mt-3 rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-xs leading-relaxed text-danger" role="alert">
                            <strong>{et('dnsEngine.operation.lastError')}</strong>
                            <span className="ml-1 break-words">{operation.last_error}</span>
                        </div>
                    )}
                    {trackingError && active && (
                        <div className="mt-3 rounded-lg border border-warning/30 bg-warning/10 px-3 py-2 text-xs leading-relaxed text-warning" role="alert">
                            {trackingError}
                        </div>
                    )}
                    {trackingDelayed && active && (
                        <div className="mt-3 rounded-lg border border-warning/30 bg-warning/10 px-3 py-2 text-xs leading-relaxed text-warning" role="alert">
                            {et('dnsEngine.operation.trackingDelayed')}
                        </div>
                    )}
                </div>
            </div>
        </div>
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
