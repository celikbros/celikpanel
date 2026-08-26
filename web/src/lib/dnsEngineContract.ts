export const DNS_ENGINE_IDS = ['pdns', 'bind'] as const;
export type DNSEngineID = (typeof DNS_ENGINE_IDS)[number];

export const DNS_ENGINE_STATES = [
    'unconfigured',
    'ready',
    'unmanaged',
    'conflict',
    'switching',
    'degraded',
] as const;
export type DNSEngineState = (typeof DNS_ENGINE_STATES)[number];

export const DNS_ENGINE_STATUSES = [
    'active',
    'installed_standby',
    'available',
    'unmanaged',
    'conflict',
] as const;
export type DNSEngineStatus = (typeof DNS_ENGINE_STATUSES)[number];

export type DNSTopology = 'unconfigured' | 'standalone' | 'paired';
export type DNSPairRole = 'primary' | 'secondary';

export interface DNSEngineEntry {
    id: DNSEngineID;
    installed: boolean;
    running: boolean;
    managed: boolean;
    status: DNSEngineStatus;
    detail_code?: string;
}

export type DNSEngineOperationStatus =
    | 'running'
    | 'rolling_back'
    | 'recovery_required'
    | 'succeeded'
    | 'rolled_back'
    | 'failed';

export interface DNSEngineOperation {
    id: string;
    target_engine: DNSEngineID;
    phase: string;
    status: DNSEngineOperationStatus;
    started_at: string;
    updated_at: string;
    last_error?: string;
}

export interface DNSEngineSnapshot {
    revision: number;
    engine_epoch: number;
    active_engine: DNSEngineID | null;
    state: DNSEngineState;
    topology: DNSTopology;
    pair_role?: DNSPairRole;
    pair_ready?: boolean;
    dnssec_zone_count: number;
    zone_count: number;
    pending_zone_count: number;
    operation_id?: string;
    operation?: DNSEngineOperation;
    engines: DNSEngineEntry[];
}

export type DNSEnginePreviewAction = 'install' | 'switch' | 'adopt' | 'reconfigure';

export interface DNSEnginePreviewBlocker {
    code: string;
}

export interface DNSEngineSwitchPreview {
    preview_token: string;
    source_engine: DNSEngineID | null;
    target_engine: DNSEngineID;
    expected_revision: number;
    action: DNSEnginePreviewAction;
    topology: DNSTopology;
    zone_count: number;
    pending_zone_count: number;
    dnssec_zone_count: number;
    estimated_downtime_seconds: number;
    requires_downtime_acknowledgement: boolean;
    blockers: DNSEnginePreviewBlocker[];
    impacts: string[];
}

const engineIDs = new Set<string>(DNS_ENGINE_IDS);
const engineStates = new Set<string>(DNS_ENGINE_STATES);
const engineStatuses = new Set<string>(DNS_ENGINE_STATUSES);
const topologies = new Set<string>(['unconfigured', 'standalone', 'paired']);
const previewActions = new Set<string>(['install', 'switch', 'adopt', 'reconfigure']);
const codePattern = /^[a-z][a-z0-9_]{0,63}$/;
const operationIDPattern = /^[a-f0-9]{32}$/;
const operationPhases = new Set<string>([
    'planned', 'staging', 'staged', 'activating', 'verifying',
    'rolling_back', 'committed', 'rolled_back', 'failed',
]);
const operationStatuses = new Set<string>([
    'running', 'rolling_back', 'recovery_required', 'succeeded', 'rolled_back', 'failed',
]);
const dnsEngineEstimatedDowntimeSeconds = 15;

function isRecord(value: unknown): value is Record<string, unknown> {
    return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isNonNegativeInteger(value: unknown): value is number {
    return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0;
}

function isEngineID(value: unknown): value is DNSEngineID {
    return typeof value === 'string' && engineIDs.has(value);
}

function isTopology(value: unknown): value is DNSTopology {
    return typeof value === 'string' && topologies.has(value);
}

function decodeEngineEntry(value: unknown): DNSEngineEntry | null {
    if (!isRecord(value)
        || !isEngineID(value.id)
        || typeof value.installed !== 'boolean'
        || typeof value.running !== 'boolean'
        || typeof value.managed !== 'boolean'
        || typeof value.status !== 'string'
        || !engineStatuses.has(value.status)
        || (value.detail_code !== undefined
            && (typeof value.detail_code !== 'string' || !codePattern.test(value.detail_code)))) {
        return null;
    }

    if (value.status === 'active' && (!value.installed || !value.running || !value.managed)) return null;
    if (value.status === 'installed_standby'
        && (!value.installed || value.running || !value.managed)) return null;
    if (value.status === 'available'
        && (value.installed || value.running || value.managed)) return null;

    return {
        id: value.id,
        installed: value.installed,
        running: value.running,
        managed: value.managed,
        status: value.status as DNSEngineStatus,
        ...(typeof value.detail_code === 'string' ? { detail_code: value.detail_code } : {}),
    };
}

function decodeDNSEngineOperation(value: unknown): DNSEngineOperation | null {
    if (!isRecord(value)
        || typeof value.id !== 'string'
        || !operationIDPattern.test(value.id)
        || !isEngineID(value.target_engine)
        || typeof value.phase !== 'string'
        || !operationPhases.has(value.phase)
        || typeof value.status !== 'string'
        || !operationStatuses.has(value.status)
        || typeof value.started_at !== 'string'
        || typeof value.updated_at !== 'string'
        || !Number.isFinite(Date.parse(value.started_at))
        || !Number.isFinite(Date.parse(value.updated_at))
        || Date.parse(value.updated_at) < Date.parse(value.started_at)
        || (value.last_error !== undefined
            && (typeof value.last_error !== 'string'
                || value.last_error.length < 1
                || value.last_error.length > 2048))) {
        return null;
    }
    const expectedStatus: Record<string, DNSEngineOperationStatus> = {
        planned: 'running',
        staging: 'running',
        staged: 'running',
        activating: 'running',
        verifying: 'running',
        rolling_back: 'rolling_back',
        committed: 'succeeded',
        rolled_back: 'rolled_back',
        failed: 'failed',
    };
    const recoveryRequired = value.status === 'recovery_required'
        && ['planned', 'staging', 'staged', 'activating', 'verifying', 'rolling_back'].includes(value.phase);
    if (expectedStatus[value.phase] !== value.status && !recoveryRequired) return null;
    if (value.status === 'recovery_required' && value.last_error === undefined) return null;
    if ((value.status === 'running' || value.status === 'succeeded')
        && value.last_error !== undefined) return null;

    return {
        id: value.id,
        target_engine: value.target_engine,
        phase: value.phase,
        status: value.status as DNSEngineOperationStatus,
        started_at: value.started_at,
        updated_at: value.updated_at,
        ...(typeof value.last_error === 'string' ? { last_error: value.last_error } : {}),
    };
}

// This endpoint controls whether the page may expose privileged DNS actions.
// Any malformed or internally contradictory payload therefore decodes to null:
// the UI shows a verification error and never guesses an engine state.
export function decodeDNSEngineSnapshot(value: unknown): DNSEngineSnapshot | null {
    if (!isRecord(value)
        || !isNonNegativeInteger(value.revision)
        || !isNonNegativeInteger(value.engine_epoch)
        || (value.active_engine !== null && !isEngineID(value.active_engine))
        || typeof value.state !== 'string'
        || !engineStates.has(value.state)
        || !isTopology(value.topology)
        || (value.pair_role !== undefined
            && value.pair_role !== 'primary' && value.pair_role !== 'secondary')
        || (value.pair_ready !== undefined && typeof value.pair_ready !== 'boolean')
        || !isNonNegativeInteger(value.dnssec_zone_count)
        || !isNonNegativeInteger(value.zone_count)
        || !isNonNegativeInteger(value.pending_zone_count)
        || value.dnssec_zone_count > value.zone_count
        || value.pending_zone_count > value.zone_count
        || !Array.isArray(value.engines)
        || value.engines.length !== DNS_ENGINE_IDS.length
        || (value.operation_id !== undefined
            && (typeof value.operation_id !== 'string' || !operationIDPattern.test(value.operation_id)))
        || (value.operation !== undefined && !isRecord(value.operation))) {
        return null;
    }

    const operation = value.operation === undefined
        ? undefined
        : decodeDNSEngineOperation(value.operation);
    if (value.operation !== undefined && operation === null) return null;

    const entries = value.engines.map(decodeEngineEntry);
    if (entries.some((entry) => entry === null)) return null;
    const engines = entries as DNSEngineEntry[];
    if (new Set(engines.map((entry) => entry.id)).size !== DNS_ENGINE_IDS.length
        || !DNS_ENGINE_IDS.every((id) => engines.some((entry) => entry.id === id))) {
        return null;
    }

    const activeEntries = engines.filter((entry) => entry.status === 'active');
    if (activeEntries.length > 1) return null;
    if (value.active_engine === null && activeEntries.length !== 0) return null;
    if (value.active_engine !== null
        && value.state === 'ready'
        && (activeEntries.length !== 1 || activeEntries[0].id !== value.active_engine)) {
        return null;
    }
    if (activeEntries.some((entry) => entry.id !== value.active_engine)) return null;
    if ((value.active_engine === null) !== (value.engine_epoch === 0)) return null;
    if (value.state === 'ready' && value.active_engine === null) return null;
    if (value.state === 'switching'
        && (typeof value.operation_id !== 'string'
            || !operation
            || operation.id !== value.operation_id
            || !['running', 'rolling_back', 'recovery_required'].includes(operation.status))) return null;
    if (value.state !== 'switching' && value.operation_id !== undefined) return null;
    if (value.state !== 'switching'
        && operation
        && ['running', 'rolling_back', 'recovery_required'].includes(operation.status)) return null;
    const stagedPair = value.active_engine === null && value.topology === 'paired';
    const activePair = value.active_engine !== null && value.topology === 'paired';
    if (stagedPair
        && ((value.pair_role !== 'primary' && value.pair_role !== 'secondary')
            || value.pair_ready !== undefined)) return null;
    if (value.active_engine === 'bind' && activePair
        && value.pair_role !== 'primary' && value.pair_role !== 'secondary') return null;
    if (activePair && typeof value.pair_ready !== 'boolean') return null;
    if (!stagedPair && !activePair
        && (value.pair_role !== undefined || value.pair_ready !== undefined)) return null;
    if (value.pair_ready === true && value.pair_role !== 'primary') return null;
    if (activePair && value.pair_role === 'secondary' && value.pair_ready !== false) return null;

    return {
        revision: value.revision,
        engine_epoch: value.engine_epoch,
        active_engine: value.active_engine as DNSEngineID | null,
        state: value.state as DNSEngineState,
        topology: value.topology,
        ...(value.pair_role === 'primary' || value.pair_role === 'secondary'
            ? { pair_role: value.pair_role }
            : {}),
        ...(typeof value.pair_ready === 'boolean' ? { pair_ready: value.pair_ready } : {}),
        dnssec_zone_count: value.dnssec_zone_count,
        zone_count: value.zone_count,
        pending_zone_count: value.pending_zone_count,
        ...(typeof value.operation_id === 'string' ? { operation_id: value.operation_id } : {}),
        ...(operation ? { operation } : {}),
        engines,
    };
}

function decodeCodeList(value: unknown): string[] | null {
    if (!Array.isArray(value) || value.length > 32) return null;
    const result: string[] = [];
    for (const item of value) {
        if (typeof item !== 'string' || !codePattern.test(item)) return null;
        result.push(item);
    }
    return result;
}

export function decodeDNSEngineSwitchPreview(
    value: unknown,
    source: DNSEngineID | null,
    target: DNSEngineID,
    revision: number,
): DNSEngineSwitchPreview | null {
    const reconfigure = isRecord(value) && value.action === 'reconfigure';
    const requiresInterruption = source !== null || reconfigure;
    if (!isRecord(value)
        || typeof value.preview_token !== 'string'
        || !operationIDPattern.test(value.preview_token)
        || value.source_engine !== source
        || value.target_engine !== target
        || value.expected_revision !== revision
        || typeof value.action !== 'string'
        || !previewActions.has(value.action)
        || !isTopology(value.topology)
        || !isNonNegativeInteger(value.zone_count)
        || !isNonNegativeInteger(value.pending_zone_count)
        || value.pending_zone_count > value.zone_count
        || !isNonNegativeInteger(value.dnssec_zone_count)
        || value.dnssec_zone_count > value.zone_count
        || !isNonNegativeInteger(value.estimated_downtime_seconds)
        || value.estimated_downtime_seconds > 86400
        || typeof value.requires_downtime_acknowledgement !== 'boolean'
        || (reconfigure && (source !== null || target !== 'pdns'))
        || value.requires_downtime_acknowledgement !== requiresInterruption
        || value.estimated_downtime_seconds !== (requiresInterruption
            ? dnsEngineEstimatedDowntimeSeconds
            : 0)
        || !Array.isArray(value.blockers)
        || value.blockers.length > 32) {
        return null;
    }

    const blockers: DNSEnginePreviewBlocker[] = [];
    for (const item of value.blockers) {
        if (!isRecord(item)
            || typeof item.code !== 'string'
            || !codePattern.test(item.code)
            || (item.message !== undefined
                && (typeof item.message !== 'string' || item.message.length > 1024))) {
            return null;
        }
        // Backend message text is deliberately discarded. Only a translated,
        // allow-listed code or a safe generic fallback reaches the page.
        blockers.push({ code: item.code });
    }
    const impacts = decodeCodeList(value.impacts);
    if (impacts === null || impacts.length === 0) return null;

    return {
        preview_token: value.preview_token,
        source_engine: source,
        target_engine: target,
        expected_revision: revision,
        action: value.action as DNSEnginePreviewAction,
        topology: value.topology,
        zone_count: value.zone_count,
        pending_zone_count: value.pending_zone_count,
        dnssec_zone_count: value.dnssec_zone_count,
        estimated_downtime_seconds: value.estimated_downtime_seconds,
        requires_downtime_acknowledgement: value.requires_downtime_acknowledgement,
        blockers,
        impacts,
    };
}
