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
    request_id: string;
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

// The action set is the API's, not the UI's wish list. Every action the panel
// can return has to appear here, because an unlisted one makes the whole
// preview decode to null and the dialog says only that it could not be
// verified — the operator is told nothing about a change the server was
// perfectly willing to describe. `reinstall_active` shipped without this list
// being updated and did exactly that; the contract test below now fails the
// build if the two ever drift again.
export const DNS_ENGINE_PREVIEW_ACTIONS = [
    'install',
    'switch',
    'adopt',
    'adopt_unmanaged',
    'reconfigure',
    'reinstall_active',
] as const;
export type DNSEnginePreviewAction = (typeof DNS_ENGINE_PREVIEW_ACTIONS)[number];

export interface DNSEnginePreviewBlocker {
    code: string;
}

// The directives CelikPanel manages that this server already sets. The takeover
// replaces them, and the operator sees each one - the value found and the value
// CelikPanel will set - before agreeing to it. The directive names and the
// refusal codes are pinned lists for the same reason the action list is: the
// panel can only send what this file can render, and a contract test fails the
// build when the two drift.
//
// CelikPanel'in yonettigi ve bu sunucunun zaten koydugu direktifler. Devralma
// onlari degistirir ve operator, riza gostermeden once her birini - bulunan
// degeri ve CelikPanel'in koyacagi degeri - gorur. Direktif adlari ve ret
// kodlari, eylem listesiyle ayni sebeple sabitlenmis listelerdir: panel yalniz
// bu dosyanin cizebildigini gonderebilir ve ikisi ayrildiginda bir sozlesme
// testi derlemeyi dusurur.
export const DNS_MANAGED_BIND_OPTION_DIRECTIVES = [
    'recursion',
    'allow-recursion',
    'allow-query-cache',
    'allow-transfer',
] as const;
export type DNSManagedBINDOptionDirective =
    (typeof DNS_MANAGED_BIND_OPTION_DIRECTIVES)[number];

export const DNS_FOREIGN_OPTION_REFUSALS = [
    'nested_scope',
    'not_a_statement',
    'unterminated',
] as const;
export type DNSForeignOptionRefusal = (typeof DNS_FOREIGN_OPTION_REFUSALS)[number];

export interface DNSEngineAdoptedDirective {
    directive: DNSManagedBINDOptionDirective;
    found: string;
    replacement: string;
    unchanged: boolean;
    file: string;
    line: number;
    refusal?: DNSForeignOptionRefusal;
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
    requires_adoption_acknowledgement: boolean;
    blockers: DNSEnginePreviewBlocker[];
    impacts: string[];
    adopted_directives: DNSEngineAdoptedDirective[];
}

const engineIDs = new Set<string>(DNS_ENGINE_IDS);
const engineStates = new Set<string>(DNS_ENGINE_STATES);
const engineStatuses = new Set<string>(DNS_ENGINE_STATUSES);
const topologies = new Set<string>(['unconfigured', 'standalone', 'paired']);
const previewActions = new Set<string>(DNS_ENGINE_PREVIEW_ACTIONS);
const managedDirectives = new Set<string>(DNS_MANAGED_BIND_OPTION_DIRECTIVES);
const directiveRefusals = new Set<string>(DNS_FOREIGN_OPTION_REFUSALS);
// A directive value is the operator's own configuration text. The agent
// normalises it to one bounded printable line and the panel refuses anything
// else, so a value that does not look like that here is a bug upstream, not a
// server this page should describe.
//
// Bir direktif degeri, operatorun kendi yapilandirma metnidir. Agent onu tek,
// sinirli ve yazdirilabilir bir satira normallestirir ve panel baskasini
// reddeder; dolayisiyla burada boyle gorunmeyen bir deger, bu sayfanin
// anlatmasi gereken bir sunucu degil, yukarida bir hatadir.
const directiveValuePattern = /^[ -~]{1,200}$/;
const directiveFilePattern = /^\/[ -~]{0,511}$/;
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
        || typeof value.request_id !== 'string'
        || !operationIDPattern.test(value.request_id)
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
        request_id: value.request_id,
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

// A takeover's difference list. It is decoded strictly, and a payload that
// breaks its own rules takes the whole preview to null rather than reaching the
// screen half-understood: this list is what the operator reads before consenting
// to a change on a DNS server that is answering queries.
//
// Bir devralmanin fark listesi. Kati bicimde cozulur ve kendi kurallarini bozan
// bir yuk, yarim anlasilmis olarak ekrana ulasmak yerine tum onizlemeyi null'a
// goturur: bu liste, operatorun sorgu yanitlayan bir DNS sunucusundaki bir
// degisiklige riza gostermeden once okudugu seydir.
function decodeAdoptedDirectives(
    value: unknown,
    action: string,
): DNSEngineAdoptedDirective[] | null {
    if (value === undefined) return [];
    if (!Array.isArray(value) || value.length > 32) return null;
    // Only a takeover replaces a directive somebody else wrote. A list on any
    // other action is describing a change that action does not make.
    //
    // Yalniz bir devralma, baskasinin yazdigi bir direktifi degistirir. Baska
    // bir eylemde gelen liste, o eylemin yapmadigi bir degisikligi anlatiyordur.
    if (value.length !== 0 && action !== 'adopt_unmanaged') return null;
    const directives: DNSEngineAdoptedDirective[] = [];
    for (const item of value) {
        if (!isRecord(item)
            || typeof item.directive !== 'string'
            || !managedDirectives.has(item.directive)
            || typeof item.replacement !== 'string'
            || !directiveValuePattern.test(item.replacement)
            || typeof item.file !== 'string'
            || !directiveFilePattern.test(item.file)
            || !Number.isSafeInteger(item.line)
            || (item.line as number) < 1
            || typeof item.found !== 'string'
            || (item.refusal !== undefined
                && (typeof item.refusal !== 'string'
                    || !directiveRefusals.has(item.refusal)))) {
            return null;
        }
        const refusal = typeof item.refusal === 'string'
            ? item.refusal as DNSForeignOptionRefusal
            : undefined;
        // A directive that can be taken over has a value the operator can read
        // and a truthful "unchanged" flag. One that cannot has neither, because
        // the server could not read it either.
        //
        // Devralinabilen bir direktifin, operatorun okuyabilecegi bir degeri ve
        // dogru bir "degismiyor" isareti vardir. Devralinamayanin ikisi de
        // yoktur, cunku sunucu da onu okuyamamistir.
        if (refusal === undefined) {
            if (!directiveValuePattern.test(item.found)
                || typeof item.unchanged !== 'boolean'
                || item.unchanged !== (item.found === item.replacement)) {
                return null;
            }
        } else if (item.found !== '' || item.unchanged === true) {
            return null;
        }
        directives.push({
            directive: item.directive as DNSManagedBINDOptionDirective,
            found: item.found,
            replacement: item.replacement,
            unchanged: item.unchanged === true,
            file: item.file,
            line: item.line as number,
            ...(refusal ? { refusal } : {}),
        });
    }
    return directives;
}

export function decodeDNSEngineSwitchPreview(
    value: unknown,
    source: DNSEngineID | null,
    target: DNSEngineID,
    revision: number,
): DNSEngineSwitchPreview | null {
    const reconfigure = isRecord(value) && value.action === 'reconfigure';
    // A reinstall names the source engine because that engine owns this host,
    // but nothing of it is running: there is no service to interrupt and so no
    // outage to acknowledge. A takeover has no source at all.
    const reinstall = isRecord(value) && value.action === 'reinstall_active';
    const adoptUnmanaged = isRecord(value) && value.action === 'adopt_unmanaged';
    const requiresInterruption = (source !== null || reconfigure) && !reinstall;
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
        || typeof value.requires_adoption_acknowledgement !== 'boolean'
        || (reconfigure && (source !== null || target !== 'pdns'))
        || (reinstall && (source === null || source !== target))
        || (adoptUnmanaged && (source !== null || target !== 'bind'))
        // Taking over a DNS server the panel did not install is the one action
        // that carries this acknowledgement, and it carries it always. A
        // preview that claims otherwise in either direction is not describing
        // the change the operator is about to consent to.
        || value.requires_adoption_acknowledgement !== adoptUnmanaged
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
    const adoptedDirectives = decodeAdoptedDirectives(
        value.adopted_directives, value.action,
    );
    if (adoptedDirectives === null) return null;

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
        requires_adoption_acknowledgement: value.requires_adoption_acknowledgement,
        blockers,
        impacts,
        adopted_directives: adoptedDirectives,
    };
}
