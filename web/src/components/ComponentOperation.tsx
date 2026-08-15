import {
    createContext,
    useContext,
    useEffect,
    useRef,
    useState,
    type ReactNode,
} from 'react';
import { createPortal } from 'react-dom';
import { Loader2 as LoaderCircle, WifiOff, X } from 'lucide-react';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import { readApiError, type ApiError } from '../lib/apiError';
import { showToast } from './Toast';
import { ErrorBanner } from './ui';

const OPERATION_ID_KEY = 'celikpanel.components.operation-id';
const OPERATION_LABEL_KEY = 'celikpanel.components.operation-label';
const OPERATION_RECOVERY_KEY = 'celikpanel.components.operation-recovery';
const OPERATION_RECOVERY_VERSION = 3;
const POLL_DELAY_MS = 1500;
const RETRY_DELAY_MS = 3000;
const RECOVERY_LOOKUP_GRACE_MS = 15000;
const BUSY_EXACT_LOOKUP_GRACE_MS = 3000;
const ACTIVE_DISCOVERY_TIMEOUT_MS = 5000;
const RECENT_OPERATION_MS = 2 * 60 * 1000;
const ERROR_CODE_OPERATION_BUSY = 'service_operation_busy';
const OPERATION_ID_RE = /^[a-f0-9]{32}$/;

type OperationStatus = 'queued' | 'running' | 'succeeded' | 'failed';
type InstallOperationKind = 'service_install' | 'runtime_install' | 'mail_profile_install';
type MailProfileStatus = 'unknown' | 'available' | 'partial' | 'complete' | 'blocked';

const MAIL_PROFILE_IDS = ['core-mail', 'webmail', 'protected-mail'] as const;
const MAIL_PROFILE_ID_SET: ReadonlySet<string> = new Set(MAIL_PROFILE_IDS);

function isMailProfileID(value: unknown): value is (typeof MAIL_PROFILE_IDS)[number] {
    return typeof value === 'string' && MAIL_PROFILE_ID_SET.has(value);
}

export interface ManagedMailProfile {
    id: (typeof MAIL_PROFILE_IDS)[number];
    name: string;
    description: string;
    services: string[];
    status: MailProfileStatus;
    available: boolean;
    blocked_reason?: string;
    warning?: string;
}

export interface ComponentOperation {
    id: string;
    request_id?: string;
    kind: string;
    service_id: string;
    package_name?: string;
    status: OperationStatus;
    phase: string;
    started_at: string;
    finished_at?: string;
    error?: unknown;
    result?: unknown;
}

export interface ManagedServicesSnapshot {
    services: Record<string, unknown>[];
    profiles: ManagedMailProfile[];
    dns_identity_ready: boolean;
    scanned_at?: string | null;
}

export interface InstallOperationRequest {
    serviceId: string;
    name: string;
    operationKind?: InstallOperationKind;
    package?: string;
    version?: string;
}

export interface OperationRecoveryMarker {
    version: 3;
    operation_kind: InstallOperationKind;
    request_id: string;
    service_id: string;
    label: string;
    package_name?: string;
    runtime_version?: string;
    created_at: number;
}

interface ComponentOperationContextValue {
    operation: ComponentOperation | null;
    locked: boolean;
    failure: ApiError | null;
    catalogSnapshot: ManagedServicesSnapshot | null;
    startInstall: (request: InstallOperationRequest) => Promise<boolean>;
    clearFailure: () => void;
}

const ComponentOperationContext = createContext<ComponentOperationContextValue | null>(null);

export function decodeManagedMailProfiles(
    value: unknown,
    serviceIDs: ReadonlySet<string>,
): ManagedMailProfile[] | null {
    if (!Array.isArray(value) || value.length !== MAIL_PROFILE_IDS.length) return null;
    const expectedIDs = new Set<string>(MAIL_PROFILE_IDS);
    const profileIDs = new Set<string>();
    const profiles: ManagedMailProfile[] = [];

    for (const candidate of value) {
        if (!candidate || typeof candidate !== 'object') return null;
        const profile = candidate as Record<string, unknown>;
        const id = profile.id;
        const status = profile.status;
        if (
            typeof id !== 'string'
            || !expectedIDs.has(id)
            || profileIDs.has(id)
            || typeof profile.name !== 'string'
            || profile.name.trim() === ''
            || typeof profile.description !== 'string'
            || profile.description.trim() === ''
            || (
                status !== 'unknown'
                && status !== 'available'
                && status !== 'partial'
                && status !== 'complete'
                && status !== 'blocked'
            )
            || typeof profile.available !== 'boolean'
            || profile.available !== (
                status === 'available' || status === 'partial' || status === 'complete'
            )
            || !Array.isArray(profile.services)
            || profile.services.length === 0
            || !profile.services.every((serviceID) => (
                typeof serviceID === 'string'
                && serviceID.trim() === serviceID
                && serviceID !== ''
                && serviceIDs.has(serviceID)
            ))
            || new Set(profile.services).size !== profile.services.length
            || (
                profile.blocked_reason !== undefined
                && (typeof profile.blocked_reason !== 'string' || profile.blocked_reason.trim() === '')
            )
            || (
                status === 'blocked'
                && (typeof profile.blocked_reason !== 'string' || profile.blocked_reason.trim() === '')
            )
            || (
                profile.warning !== undefined
                && (typeof profile.warning !== 'string' || profile.warning.trim() === '')
            )
        ) {
            return null;
        }
        profileIDs.add(id);
        profiles.push(profile as unknown as ManagedMailProfile);
    }
    return profileIDs.size === expectedIDs.size ? profiles : null;
}

// A terminal operation may unlock the page only after every field consumed by
// the Components screen has a valid shape. A bare array is not verification.
// Terminal işlem, sayfayı yalnız Bileşenler ekranının kullandığı her alan
// geçerli biçimdeyse açabilir. Yalnızca bir dizi gelmesi doğrulama değildir.
function decodeManagedServicesSnapshot(value: unknown): ManagedServicesSnapshot | null {
    if (!value || typeof value !== 'object') return null;
    const payload = value as Record<string, unknown>;
    if (
        !Array.isArray(payload.services)
        || !payload.services.every((entry) => {
            if (!entry || typeof entry !== 'object') return false;
            const service = entry as Record<string, unknown>;
            return (
                typeof service.id === 'string'
                && typeof service.name === 'string'
                && typeof service.description === 'string'
                && typeof service.icon === 'string'
                && typeof service.category === 'string'
                && typeof service.status === 'string'
                && typeof service.is_installed === 'boolean'
                && Array.isArray(service.versions)
                && service.versions.every((version) => typeof version === 'string')
                && (service.unit === undefined || typeof service.unit === 'string')
                && (
                    service.instances === undefined
                    || (
                        Array.isArray(service.instances)
                        && service.instances.every((candidate) => {
                            if (!candidate || typeof candidate !== 'object') return false;
                            const instance = candidate as Record<string, unknown>;
                            return (
                                typeof instance.version === 'string'
                                && typeof instance.managed === 'boolean'
                                && (instance.unit === undefined || typeof instance.unit === 'string')
                                && (instance.path === undefined || typeof instance.path === 'string')
                                && (instance.status === undefined || typeof instance.status === 'string')
                                && (instance.size_bytes === undefined || typeof instance.size_bytes === 'number')
                            );
                        })
                    )
                )
                && (service.conflict_with === undefined || typeof service.conflict_with === 'string')
                && (service.not_offered === undefined || typeof service.not_offered === 'boolean')
                && (
                    service.not_offered_kind === undefined
                    || service.not_offered_kind === 'integration'
                    || service.not_offered_kind === 'distribution'
                )
                && (service.not_offered_reason === undefined || typeof service.not_offered_reason === 'string')
                && (
                    service.requires_missing === undefined
                    || (
                        Array.isArray(service.requires_missing)
                        && service.requires_missing.every((requirement) => typeof requirement === 'string')
                    )
                )
                && (
                    service.kind === undefined
                    || service.kind === 'service'
                    || service.kind === 'runtime'
                    || service.kind === 'tool'
                )
                && (
                    service.packages === undefined
                    || (
                        Array.isArray(service.packages)
                        && service.packages.every((packageName) => typeof packageName === 'string')
                    )
                )
                && (service.repair_package === undefined || typeof service.repair_package === 'string')
                && (service.repair_available === undefined || typeof service.repair_available === 'boolean')
            );
        })
        || typeof payload.dns_identity_ready !== 'boolean'
        || typeof payload.scanned_at !== 'string'
        || !Number.isFinite(Date.parse(payload.scanned_at))
    ) {
        return null;
    }
    const services = payload.services as Record<string, unknown>[];
    const serviceIDs = new Set<string>();
    for (const service of services) {
        const id = service.id as string;
        if (id.trim() !== id || id === '' || serviceIDs.has(id)) return null;
        serviceIDs.add(id);
    }
    const profiles = decodeManagedMailProfiles(payload.profiles, serviceIDs);
    if (profiles === null) return null;
    return {
        services,
        profiles,
        dns_identity_ready: payload.dns_identity_ready,
        scanned_at: payload.scanned_at,
    };
}

function snapshotConfirmsTerminalOperation(
    snapshot: ManagedServicesSnapshot,
    operation: ComponentOperation,
): boolean {
    const scannedAt = Date.parse(snapshot.scanned_at || '');
    const finishedAt = Date.parse(operation.finished_at || '');
    if (!Number.isFinite(scannedAt) || !Number.isFinite(finishedAt) || scannedAt < finishedAt) {
        return false;
    }
    if (operation.status === 'failed') return true;
    if (operation.status !== 'succeeded') return false;

    if (operation.kind === 'mail_profile_install') {
        return decodeVerifiedMailProfileResult(snapshot, operation) !== null;
    }

    const service = snapshot.services.find((candidate) => candidate.id === operation.service_id);
    if (!service || service.is_installed !== true) return false;
    if (operation.kind !== 'runtime_install') return operation.kind === 'service_install';

    const expectedVersion = operation.package_name || '';
    if (!expectedVersion) return false;
    const versions = Array.isArray(service.versions) ? service.versions : [];
    const instances = Array.isArray(service.instances) ? service.instances : [];
    return versions.includes(expectedVersion)
        || instances.some((candidate) => (
            candidate
            && typeof candidate === 'object'
            && (candidate as Record<string, unknown>).version === expectedVersion
        ));
}

interface VerifiedMailProfileResult {
    fallbackOnly: boolean;
}

function stringArrayMatchesSet(value: unknown, expected: readonly string[]): boolean {
    if (!Array.isArray(value) || value.length !== expected.length) return false;
    if (!value.every((entry) => typeof entry === 'string' && entry !== '')) return false;
    const actual = new Set(value as string[]);
    return actual.size === value.length && expected.every((entry) => actual.has(entry));
}

function decodeVerifiedMailProfileResult(
    snapshot: ManagedServicesSnapshot,
    operation: ComponentOperation,
): VerifiedMailProfileResult | null {
    if (operation.kind !== 'mail_profile_install' || operation.status !== 'succeeded') return null;
    const profile = snapshot.profiles.find((candidate) => candidate.id === operation.service_id);
    if (!profile || profile.status !== 'complete') return null;
    if (!operation.result || typeof operation.result !== 'object') return null;

    const result = operation.result as Record<string, unknown>;
    const mailTLS = result.mail_tls;
    if (!mailTLS || typeof mailTLS !== 'object') return null;
    const tls = mailTLS as Record<string, unknown>;
    const warnings = result.warnings;
    if (
        result.success !== true
        || result.profile_id !== operation.service_id
        || !stringArrayMatchesSet(result.services, profile.services)
        || !stringArrayMatchesSet(result.completed_services, profile.services)
        || tls.configured !== true
        || typeof tls.sni_count !== 'number'
        || !Number.isSafeInteger(tls.sni_count)
        || tls.sni_count < 0
        || typeof tls.fallback_only !== 'boolean'
        || tls.fallback_only !== (tls.sni_count === 0)
        || result.submission_configured !== true
        || !Array.isArray(warnings)
        || !warnings.every((warning) => typeof warning === 'string' && warning.trim() !== '')
        || (tls.fallback_only && warnings.length === 0)
    ) {
        return null;
    }
    return { fallbackOnly: tls.fallback_only };
}

function readSessionValue(key: string): string {
    try {
        return sessionStorage.getItem(key) ?? '';
    } catch {
        return '';
    }
}

function storeOperation(id: string, label: string): boolean {
    try {
        sessionStorage.setItem(OPERATION_ID_KEY, id);
        sessionStorage.setItem(OPERATION_LABEL_KEY, label);
        return true;
    } catch {
        // Restricted storage does not stop the live operation. The current
        // page can still poll it; only reload reattachment is unavailable.
        // Kısıtlı depolama canlı işlemi durdurmaz. Geçerli sayfa işlemi
        // yoklamayı sürdürür; yalnız yenileme sonrası yeniden bağlanma yoktur.
        return false;
    }
}

function clearStoredOperation() {
    try {
        sessionStorage.removeItem(OPERATION_ID_KEY);
        sessionStorage.removeItem(OPERATION_LABEL_KEY);
        sessionStorage.removeItem(OPERATION_RECOVERY_KEY);
    } catch {
        // Nothing else is required when storage is unavailable.
        // Depolama kullanılamadığında başka bir işlem gerekmez.
    }
}

function boundedMarkerString(value: unknown, maxLength: number, required = false): string | null {
    if (value === undefined && !required) return '';
    if (typeof value !== 'string') return null;
    const normalized = value.trim();
    if ((required && !normalized) || normalized.length > maxLength) return null;
    return normalized;
}

function createOperationRequestID(): string | null {
    try {
        const bytes = new Uint8Array(16);
        crypto.getRandomValues(bytes);
        return Array.from(bytes, (value) => value.toString(16).padStart(2, '0')).join('');
    } catch {
        return null;
    }
}

export function createOperationRecoveryMarker(
    request: InstallOperationRequest,
    createdAt = Date.now(),
    requestID = createOperationRequestID(),
): OperationRecoveryMarker | null {
    const operationKind = request.operationKind
        ?? (request.version ? 'runtime_install' : 'service_install');
    const serviceID = boundedMarkerString(request.serviceId, 128, true);
    const label = boundedMarkerString(request.name, 256, true);
    const packageName = boundedMarkerString(request.package, 256);
    const runtimeVersion = boundedMarkerString(request.version, 128);
    if (
        requestID === null
        || !/^[a-f0-9]{32}$/.test(requestID)
        || serviceID === null
        || label === null
        || packageName === null
        || runtimeVersion === null
        || (packageName && runtimeVersion)
        || (
            operationKind !== 'service_install'
            && operationKind !== 'runtime_install'
            && operationKind !== 'mail_profile_install'
        )
        || (operationKind === 'service_install' && Boolean(runtimeVersion))
        || (operationKind === 'runtime_install' && (!runtimeVersion || Boolean(packageName)))
        || (
            operationKind === 'mail_profile_install'
            && (!isMailProfileID(serviceID) || Boolean(packageName || runtimeVersion))
        )
        || !Number.isFinite(createdAt)
        || createdAt <= 0
    ) {
        return null;
    }
    return {
        version: OPERATION_RECOVERY_VERSION,
        operation_kind: operationKind,
        request_id: requestID,
        service_id: serviceID,
        label,
        ...(packageName ? { package_name: packageName } : {}),
        ...(runtimeVersion ? { runtime_version: runtimeVersion } : {}),
        created_at: createdAt,
    };
}

export function decodeOperationRecoveryMarker(raw: string): OperationRecoveryMarker | null {
    let candidate: unknown;
    try {
        candidate = JSON.parse(raw);
    } catch {
        return null;
    }
    if (!candidate || typeof candidate !== 'object') return null;
    const value = candidate as Record<string, unknown>;
    if (value.version !== 2 && value.version !== OPERATION_RECOVERY_VERSION) return null;
    if (
        (value.package_name !== undefined && typeof value.package_name !== 'string')
        || (value.runtime_version !== undefined && typeof value.runtime_version !== 'string')
    ) {
        return null;
    }
    const operationKind = value.version === 2
        ? (value.runtime_version ? 'runtime_install' : 'service_install')
        : value.operation_kind;
    if (
        (value.version === 2 && value.operation_kind !== undefined)
        || (
            operationKind !== 'service_install'
            && operationKind !== 'runtime_install'
            && operationKind !== 'mail_profile_install'
        )
    ) {
        return null;
    }
    if (
        value.version === OPERATION_RECOVERY_VERSION
        && operationKind === 'mail_profile_install'
        && !isMailProfileID(value.service_id)
    ) {
        return null;
    }
    return createOperationRecoveryMarker({
        serviceId: typeof value.service_id === 'string' ? value.service_id : '',
        name: typeof value.label === 'string' ? value.label : '',
        operationKind,
        package: typeof value.package_name === 'string' ? value.package_name : undefined,
        version: typeof value.runtime_version === 'string' ? value.runtime_version : undefined,
    }, typeof value.created_at === 'number' ? value.created_at : Number.NaN,
    typeof value.request_id === 'string' ? value.request_id : null);
}

function readStoredRecoveryMarker(): OperationRecoveryMarker | null {
    const raw = readSessionValue(OPERATION_RECOVERY_KEY);
    if (!raw) return null;
    const marker = decodeOperationRecoveryMarker(raw);
    if (!marker) {
        try {
            sessionStorage.removeItem(OPERATION_RECOVERY_KEY);
        } catch {
            // An invalid marker is ignored even when restricted storage cannot be cleaned.
            // Kısıtlı depolama temizlenemese bile geçersiz işaretçi yok sayılır.
        }
    }
    return marker;
}

function storeRecoveryMarker(marker: OperationRecoveryMarker): boolean {
    try {
        sessionStorage.setItem(OPERATION_RECOVERY_KEY, JSON.stringify(marker));
        return true;
    } catch {
        return false;
    }
}

function requestFromRecoveryMarker(marker: OperationRecoveryMarker): InstallOperationRequest {
    return {
        serviceId: marker.service_id,
        name: marker.label,
        operationKind: marker.operation_kind,
        package: marker.package_name,
        version: marker.runtime_version,
    };
}

function decodeOperation(payload: unknown): ComponentOperation | null {
    const envelope = payload && typeof payload === 'object' ? payload as Record<string, unknown> : null;
    const candidate = envelope?.operation && typeof envelope.operation === 'object'
        ? envelope.operation as Record<string, unknown>
        : envelope;
    if (!candidate) return null;

    const status = candidate.status;
    if (
        typeof candidate.id !== 'string'
        || !OPERATION_ID_RE.test(candidate.id)
        || (
            candidate.request_id !== undefined
            && (typeof candidate.request_id !== 'string' || !OPERATION_ID_RE.test(candidate.request_id))
        )
        || (status !== 'queued' && status !== 'running' && status !== 'succeeded' && status !== 'failed')
    ) {
        return null;
    }

    return {
        id: candidate.id,
        request_id: typeof candidate.request_id === 'string' ? candidate.request_id : undefined,
        kind: typeof candidate.kind === 'string' ? candidate.kind : 'install',
        service_id: typeof candidate.service_id === 'string' ? candidate.service_id : '',
        package_name: typeof candidate.package_name === 'string' ? candidate.package_name : undefined,
        status,
        phase: typeof candidate.phase === 'string' ? candidate.phase : status,
        started_at: typeof candidate.started_at === 'string' ? candidate.started_at : '',
        finished_at: typeof candidate.finished_at === 'string' ? candidate.finished_at : undefined,
        error: candidate.error,
        result: candidate.result,
    };
}


function operationDisplayLabel(operation: ComponentOperation): string {
    const serviceID = operation.service_id.trim();
    const target = (operation.package_name || '').trim();
    if (operation.kind === 'runtime_install' && serviceID.toLowerCase() === 'node') {
        return target ? `Node.js ${target}` : 'Node.js';
    }
    if (serviceID && target && serviceID.toLowerCase() !== target.toLowerCase()) {
        return `${serviceID} (${target})`;
    }
    return serviceID || target || 'service';
}

function responseReferenceTime(response: Response): number {
    const serverDate = Date.parse(response.headers.get('Date') || '');
    return Number.isFinite(serverDate) ? serverDate : Date.now();
}

function isRecentTerminalOperation(operation: ComponentOperation, referenceTime: number): boolean {
    if (operation.status !== 'succeeded' && operation.status !== 'failed') return false;
    const timestamp = Date.parse(operation.finished_at || operation.started_at);
    return Number.isFinite(timestamp)
        && Math.abs(referenceTime - timestamp) <= RECENT_OPERATION_MS;
}

export function operationMatchesRecoveryMarker(
    operation: ComponentOperation,
    marker: OperationRecoveryMarker,
): boolean {
    const expectedTarget = marker.runtime_version || marker.package_name || '';
    return operation.request_id === marker.request_id
        && operation.kind === marker.operation_kind
        && operation.service_id === marker.service_id
        && (operation.package_name || '') === expectedTarget;
}

export function shouldAdoptOperationFromRecoveryMarker(
    operation: ComponentOperation,
    marker: OperationRecoveryMarker,
    referenceTime: number,
): boolean {
    if (!operationMatchesRecoveryMarker(operation, marker)) return false;
    return operation.status === 'queued'
        || operation.status === 'running'
        || isRecentTerminalOperation(operation, referenceTime);
}

// recoveryMarkerForOperation creates the durable marker required before a
// globally active operation can be adopted by a tab that did not start it.
// recoveryMarkerForOperation, işlemi başlatmamış bir sekmenin global etkin
// işlemi benimsemesinden önce gereken kalıcı işaretçiyi üretir.
function recoveryMarkerForOperation(operation: ComponentOperation): OperationRecoveryMarker | null {
    if (
        !operation.request_id
        || !OPERATION_ID_RE.test(operation.request_id)
        || (
            operation.kind !== 'service_install'
            && operation.kind !== 'runtime_install'
            && operation.kind !== 'mail_profile_install'
        )
        || !operation.service_id
        || (operation.kind === 'runtime_install' && !operation.package_name)
        || (
            operation.kind === 'mail_profile_install'
            && (!isMailProfileID(operation.service_id) || Boolean(operation.package_name))
        )
    ) {
        return null;
    }
    const startedAt = Date.parse(operation.started_at);
    return createOperationRecoveryMarker(
        {
            serviceId: operation.service_id,
            name: operationDisplayLabel(operation),
            operationKind: operation.kind,
            ...(operation.kind === 'runtime_install'
                ? { version: operation.package_name }
                : operation.kind === 'service_install'
                    ? { package: operation.package_name }
                    : {}),
        },
        Number.isFinite(startedAt) && startedAt > 0 ? startedAt : Date.now(),
        operation.request_id,
    );
}

function waitForRetry(): Promise<void> {
    return new Promise((resolve) => window.setTimeout(resolve, RETRY_DELAY_MS));
}

function operationError(value: unknown, fallback: string): ApiError {
    if (typeof value === 'string') {
        return { message: value || fallback };
    }
    if (value && typeof value === 'object') {
        const raw = value as Record<string, unknown>;
        const details = Array.isArray(raw.details)
            ? raw.details.filter((item): item is string => typeof item === 'string')
            : undefined;
        return {
            message:
                (typeof raw.message === 'string' && raw.message)
                || (typeof raw.error === 'string' && raw.error)
                || fallback,
            code: typeof raw.code === 'string' ? raw.code : undefined,
            action: typeof raw.action === 'string' ? raw.action : undefined,
            details,
        };
    }
    return { message: fallback };
}

function restoredOperation(): ComponentOperation | null {
    const id = readSessionValue(OPERATION_ID_KEY);
    if (!id) return null;
    if (!OPERATION_ID_RE.test(id)) {
        try {
            sessionStorage.removeItem(OPERATION_ID_KEY);
            sessionStorage.removeItem(OPERATION_LABEL_KEY);
        } catch {
            // Keep a separately valid recovery marker even when the operation id is corrupt.
            // İşlem kimliği bozuk olsa bile ayrıca geçerli olan kurtarma işaretçisini koru.
        }
        return null;
    }
    return {
        id,
        kind: 'install',
        service_id: '',
        status: 'queued',
        phase: 'queued',
        started_at: '',
    };
}

export function ComponentOperationProvider({ children }: { children: ReactNode }) {
    const { t } = useI18n();
    const [initialSession] = useState(() => ({
        operation: restoredOperation(),
        recoveryMarker: readStoredRecoveryMarker(),
    }));
    const [operation, setOperation] = useState<ComponentOperation | null>(initialSession.operation);
    const [label, setLabel] = useState(() => (
        readSessionValue(OPERATION_LABEL_KEY) || initialSession.recoveryMarker?.label || ''
    ));
    const [submitting, setSubmitting] = useState(
        () => initialSession.operation === null && initialSession.recoveryMarker !== null,
    );
    const [recoveringRequest, setRecoveringRequest] = useState(
        () => initialSession.operation === null && initialSession.recoveryMarker !== null,
    );
    const [discoveringActive, setDiscoveringActive] = useState(
        () => initialSession.operation === null && initialSession.recoveryMarker === null,
    );
    const [refreshingCatalog, setRefreshingCatalog] = useState(false);
    const [connectionInterrupted, setConnectionInterrupted] = useState(false);
    const [failure, setFailure] = useState<ApiError | null>(null);
    const [catalogSnapshot, setCatalogSnapshot] = useState<ManagedServicesSnapshot | null>(null);
    const locked = discoveringActive || submitting || operation !== null || refreshingCatalog;
    const lockedRef = useRef(locked);
    const recoveryGenerationRef = useRef(0);
    const recoveryMarkerRef = useRef<OperationRecoveryMarker | null>(initialSession.recoveryMarker);
    const adoptedOperationIDRef = useRef(initialSession.operation?.id || '');
    const activeSyncInFlightRef = useRef(false);
    const focusBeforeLockRef = useRef<HTMLElement | null>(null);

    useEffect(() => {
        lockedRef.current = locked;
    }, [locked]);

    useEffect(() => () => {
        recoveryGenerationRef.current += 1;
    }, []);

    // The overlay is portalled outside #root. Making #root inert therefore
    // blocks pointer, focus and keyboard interaction everywhere underneath
    // while the status dialog remains accessible.
    // Katman #root dışına taşınır. Bu yüzden #root'u inert yapmak, durum
    // iletişim kutusu erişilebilir kalırken alttaki işaretçi, odak ve klavyeyi
    // engeller.
    useEffect(() => {
        if (!locked) return;
        const root = document.getElementById('root');
        if (!root) return;
        const hadInert = root.hasAttribute('inert');
        const previousBusy = root.getAttribute('aria-busy');
        root.setAttribute('inert', '');
        root.setAttribute('aria-busy', 'true');
        return () => {
            if (!hadInert) root.removeAttribute('inert');
            if (previousBusy === null) root.removeAttribute('aria-busy');
            else root.setAttribute('aria-busy', previousBusy);
            const focusTarget = focusBeforeLockRef.current;
            focusBeforeLockRef.current = null;
            if (focusTarget?.isConnected) focusTarget.focus();
        };
    }, [locked]);

    const finishFailure = (error: ApiError) => {
        clearStoredOperation();
        recoveryMarkerRef.current = null;
        adoptedOperationIDRef.current = '';
        lockedRef.current = false;
        setFailure(error);
        setOperation(null);
        setSubmitting(false);
        setRecoveringRequest(false);
        setDiscoveringActive(false);
        setRefreshingCatalog(false);
        setConnectionInterrupted(false);
    };

    const adoptOperation = (
        next: ComponentOperation,
        nextLabel: string,
        marker: OperationRecoveryMarker | null,
    ): boolean => {
        if (marker === null || !operationMatchesRecoveryMarker(next, marker)) return false;
        lockedRef.current = true;
        recoveryMarkerRef.current = marker;
        adoptedOperationIDRef.current = next.id;
        storeRecoveryMarker(marker);
        storeOperation(next.id, nextLabel);
        setLabel(nextLabel);
        setFailure(null);
        setConnectionInterrupted(false);
        setRecoveringRequest(false);
        setSubmitting(false);
        setOperation(next);
        return true;
    };

    const recoverActiveOperation = async (): Promise<'adopted' | 'retry' | 'auth'> => {
        let response: Response;
        try {
            response = await fetch(
                '/api/v1/service/operation?active=1',
                { cache: 'no-store' },
            );
        } catch {
            setConnectionInterrupted(true);
            return 'retry';
        }
        if (response.status === 401) {
            setConnectionInterrupted(true);
            return 'auth';
        }
        if (!response.ok) {
            setConnectionInterrupted(true);
            return 'retry';
        }

        let payload: unknown;
        try {
            payload = await response.json();
        } catch {
            setConnectionInterrupted(true);
            return 'retry';
        }
        const envelope = payload && typeof payload === 'object'
            ? payload as Record<string, unknown>
            : null;
        if (envelope?.operation === null) {
            setConnectionInterrupted(true);
            return 'retry';
        }
        const next = decodeOperation(payload);
        if (next === null || (next.status !== 'queued' && next.status !== 'running')) {
            setConnectionInterrupted(true);
            return 'retry';
        }
        const marker = recoveryMarkerForOperation(next);
        if (marker === null) {
            setConnectionInterrupted(true);
            return 'retry';
        }

        // Storage can be unavailable in a restricted browser. The live tab can
        // still remain safe by adopting in memory; a reload rediscovers the
        // server-wide active operation before enabling mutations.
        // Kısıtlı bir tarayıcıda depolama kullanılamayabilir. Canlı sekme işlemi
        // bellekte benimseyerek güvenli kalır; sayfa yenilendiğinde değişiklikler
        // açılmadan önce sunucu genelindeki etkin işlem yeniden bulunur.
        storeRecoveryMarker(marker);
        recoveryMarkerRef.current = marker;
        return adoptOperation(next, operationDisplayLabel(next), marker)
            ? 'adopted'
            : 'retry';
    };

    // A failed POST response is not proof that the POST failed. Keep the
    // panel locked while the authoritative operation endpoint is temporarily
    // unreachable, and attach to the operation once its identity is known.
    // Başarısız POST yanıtı POST'un başarısız olduğunu kanıtlamaz. Yetkili
    // işlem ucu geçici olarak erişilemezken paneli kilitli tut ve kimliği
    // öğrenilince işleme bağlan.
    const recoverCurrentOperation = async (
        request: InstallOperationRequest,
        mode: 'busy' | 'indeterminate' | 'marker',
        generation: number,
    ): Promise<boolean> => {
        const graceDeadline = Date.now() + RECOVERY_LOOKUP_GRACE_MS;
        const busyExactDeadline = Date.now() + BUSY_EXACT_LOOKUP_GRACE_MS;
        while (recoveryGenerationRef.current === generation) {
            const marker = recoveryMarkerRef.current;
            if (marker === null) {
                const activeResult = await recoverActiveOperation();
                if (activeResult === 'adopted') return true;
                if (activeResult === 'auth') return false;
                await waitForRetry();
                continue;
            }

            let response: Response;
            try {
                response = await fetch(
                    `/api/v1/service/operation?request_id=${encodeURIComponent(marker.request_id)}`,
                    { cache: 'no-store' },
                );
            } catch {
                setConnectionInterrupted(true);
                await waitForRetry();
                continue;
            }

            if (!response.ok) {
                // AuthGate owns 401 handling and preserves the durable marker.
                // AuthGate 401 işlemini yönetir ve kalıcı işaretçiyi korur.
                if (response.status === 401) {
                    setConnectionInterrupted(true);
                    return false;
                }
                if (response.status === 429 || response.status >= 500) {
                    setConnectionInterrupted(true);
                    await waitForRetry();
                    continue;
                }
                if (response.status === 404) {
                    if (mode === 'busy' && Date.now() < busyExactDeadline) {
                        setConnectionInterrupted(false);
                        await waitForRetry();
                        continue;
                    }
                    if (mode !== 'busy') {
                        if (Date.now() < graceDeadline) {
                            setConnectionInterrupted(false);
                            await waitForRetry();
                            continue;
                        }
                    }

                    let activeResponse: Response;
                    try {
                        activeResponse = await fetch(
                            '/api/v1/service/operation?active=1',
                            { cache: 'no-store' },
                        );
                    } catch {
                        setConnectionInterrupted(true);
                        await waitForRetry();
                        continue;
                    }
                    if (activeResponse.status === 401) {
                        setConnectionInterrupted(true);
                        return false;
                    }
                    if (activeResponse.status === 429 || activeResponse.status >= 500) {
                        setConnectionInterrupted(true);
                        await waitForRetry();
                        continue;
                    }
                    if (!activeResponse.ok) {
                        setConnectionInterrupted(true);
                        await waitForRetry();
                        continue;
                    }

                    let activePayload: unknown;
                    try {
                        activePayload = await activeResponse.json();
                    } catch {
                        setConnectionInterrupted(true);
                        await waitForRetry();
                        continue;
                    }
                    const activeEnvelope = activePayload && typeof activePayload === 'object'
                        ? activePayload as Record<string, unknown>
                        : null;
                    if (activeEnvelope?.operation === null) {
                        setConnectionInterrupted(true);
                        await waitForRetry();
                        continue;
                    }
                    const activeOperation = decodeOperation(activePayload);
                    if (
                        activeOperation === null
                        || (activeOperation.status !== 'queued' && activeOperation.status !== 'running')
                    ) {
                        setConnectionInterrupted(true);
                        await waitForRetry();
                        continue;
                    }
                    const activeMarker = recoveryMarkerForOperation(activeOperation);
                    if (activeMarker === null) {
                        setConnectionInterrupted(true);
                        await waitForRetry();
                        continue;
                    }
                    storeRecoveryMarker(activeMarker);
                    recoveryMarkerRef.current = activeMarker;
                    return adoptOperation(
                        activeOperation,
                        operationDisplayLabel(activeOperation),
                        activeMarker,
                    );
                }
                setConnectionInterrupted(true);
                await waitForRetry();
                continue;
            }

            let payload: unknown;
            try {
                payload = await response.json();
            } catch {
                setConnectionInterrupted(true);
                await waitForRetry();
                continue;
            }
            const next = decodeOperation(payload);
            if (
                next !== null
                && shouldAdoptOperationFromRecoveryMarker(
                    next,
                    marker,
                    responseReferenceTime(response),
                )
            ) {
                return adoptOperation(next, request.name, marker);
            }

            setConnectionInterrupted(false);
            if (Date.now() >= graceDeadline) {
                setConnectionInterrupted(true);
            }
            await waitForRetry();
        }
        return false;
    };

    // A POST can reach the server while its response is lost. The marker was
    // saved before that POST, so a reload/login resumes discovery instead of
    // offering a second Install click. Only a matching active or recent
    // operation is adopted; this browser marker can never start an operation.
    //
    // POST sunucuya ulaşıp yanıtı kaybolabilir. İşaretçi POST'tan önce
    // kaydedildiği için yenileme/giriş sonrası ikinci Kur tıklaması sunmak yerine
    // keşif sürer. Yalnız eşleşen etkin veya yakın tarihli işlem benimsenir;
    // tarayıcı işaretçisi hiçbir zaman işlem başlatamaz.
    useEffect(() => {
        const marker = recoveryMarkerRef.current;
        if (initialSession.operation !== null || marker === null) return;
        const request = requestFromRecoveryMarker(marker);
        const generation = ++recoveryGenerationRef.current;
        lockedRef.current = true;
        setFailure(null);
        setLabel(request.name);
        setSubmitting(true);
        setRecoveringRequest(true);
        void recoverCurrentOperation(request, 'marker', generation).finally(() => {
            if (recoveryGenerationRef.current !== generation) return;
            setRecoveringRequest(false);
            if (recoveryMarkerRef.current === null) setSubmitting(false);
        });
        // The initial durable session state owns exactly one recovery lookup.
        // İlk kalıcı oturum durumu tam olarak bir kurtarma sorgusuna sahiptir.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    useEffect(() => {
        let cancelled = false;
        let activeDiscoveryController: AbortController | null = null;
        let activeDiscoveryRetryTimer: number | undefined;

        const syncActiveOperation = async (retrying = false) => {
            if (
                (lockedRef.current && !retrying)
                || recoveryMarkerRef.current !== null
                || activeSyncInFlightRef.current
            ) {
                return;
            }
            if (!lockedRef.current && document.activeElement instanceof HTMLElement) {
                focusBeforeLockRef.current = document.activeElement;
            }
            activeSyncInFlightRef.current = true;
            lockedRef.current = true;
            setDiscoveringActive(true);
            let adopted = false;
            let verifiedNoActive = false;
            let shouldRetry = false;
            const controller = new AbortController();
            activeDiscoveryController = controller;
            const timeout = window.setTimeout(
                () => controller.abort(),
                ACTIVE_DISCOVERY_TIMEOUT_MS,
            );
            try {
                let response: Response;
                try {
                    response = await fetch(
                        '/api/v1/service/operation?active=1',
                        { cache: 'no-store', signal: controller.signal },
                    );
                } catch {
                    if (!cancelled) {
                        setFailure({ message: t('services.operation.activeCheckFailed') });
                        setConnectionInterrupted(true);
                        shouldRetry = true;
                    }
                    return;
                }
                if (cancelled) return;
                if (response.status === 401) {
                    setConnectionInterrupted(true);
                    return;
                }
                if (!response.ok) {
                    setFailure({ message: t('services.operation.activeCheckFailed') });
                    setConnectionInterrupted(true);
                    shouldRetry = true;
                    return;
                }

                let payload: unknown;
                try {
                    payload = await response.json();
                } catch {
                    if (!cancelled) {
                        setFailure({ message: t('services.operation.invalidResponse') });
                        setConnectionInterrupted(true);
                        shouldRetry = true;
                    }
                    return;
                }
                const envelope = payload && typeof payload === 'object'
                    ? payload as Record<string, unknown>
                    : null;
                if (envelope?.operation === null) {
                    verifiedNoActive = true;
                    return;
                }

                const next = decodeOperation(payload);
                if (
                    next === null
                    || (next.status !== 'queued' && next.status !== 'running')
                ) {
                    if (!cancelled) {
                        setFailure({ message: t('services.operation.invalidResponse') });
                        setConnectionInterrupted(true);
                        shouldRetry = true;
                    }
                    return;
                }
                const marker = recoveryMarkerForOperation(next);
                if (marker === null) {
                    if (!cancelled) {
                        setFailure({ message: t('services.operation.recoveryStorageUnavailable') });
                        setConnectionInterrupted(true);
                        shouldRetry = true;
                    }
                    return;
                }
                if (cancelled) return;
                storeRecoveryMarker(marker);
                recoveryMarkerRef.current = marker;
                adopted = adoptOperation(next, operationDisplayLabel(next), marker);
                if (!adopted) {
                    setConnectionInterrupted(true);
                    shouldRetry = true;
                }
            } finally {
                window.clearTimeout(timeout);
                if (activeDiscoveryController === controller) activeDiscoveryController = null;
                activeSyncInFlightRef.current = false;
                if (adopted) {
                    if (!cancelled) setDiscoveringActive(false);
                } else if (verifiedNoActive) {
                    lockedRef.current = false;
                    if (!cancelled) {
                        setDiscoveringActive(false);
                        setConnectionInterrupted(false);
                        setFailure(null);
                    }
                } else {
                    // No valid "operation: null" or adoptable operation means
                    // absence was not proved. Keep the page inert and retry.
                    // Geçerli "operation: null" veya devralınabilir işlem yoksa
                    // yokluk kanıtlanmamıştır. Sayfayı etkisiz tut ve yeniden dene.
                    lockedRef.current = true;
                    if (!cancelled) {
                        setDiscoveringActive(true);
                        if (shouldRetry) {
                            activeDiscoveryRetryTimer = window.setTimeout(
                                () => void syncActiveOperation(true),
                                RETRY_DELAY_MS,
                            );
                        }
                    }
                }
            }
        };

        const onFocus = () => {
            void syncActiveOperation();
        };
        const onVisibilityChange = () => {
            if (document.visibilityState === 'visible') void syncActiveOperation();
        };

        void syncActiveOperation(true);
        window.addEventListener('focus', onFocus);
        document.addEventListener('visibilitychange', onVisibilityChange);
        return () => {
            cancelled = true;
            activeDiscoveryController?.abort();
            if (activeDiscoveryRetryTimer !== undefined) {
                window.clearTimeout(activeDiscoveryRetryTimer);
            }
            window.removeEventListener('focus', onFocus);
            document.removeEventListener('visibilitychange', onVisibilityChange);
        };
        // Mount, focus and visibility share one server-authoritative active lock.
        // Açılış, odaklanma ve görünürlük aynı sunucu kaynaklı etkin kilidi paylaşır.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    const startInstall = async (request: InstallOperationRequest): Promise<boolean> => {
        if (lockedRef.current) {
            showToast('info', t(
                activeSyncInFlightRef.current
                    ? 'services.operation.activeCheckInProgress'
                    : 'services.operation.actionLocked',
            ));
            return false;
        }
        const marker = createOperationRecoveryMarker(request);
        if (marker === null || !storeRecoveryMarker(marker)) {
            setFailure({ message: t('services.operation.recoveryStorageUnavailable') });
            return false;
        }
        recoveryMarkerRef.current = marker;
        const recoveryGeneration = ++recoveryGenerationRef.current;
        if (document.activeElement instanceof HTMLElement) {
            focusBeforeLockRef.current = document.activeElement;
        }
        lockedRef.current = true;
        setFailure(null);
        setLabel(request.name);
        setSubmitting(true);
        setRecoveringRequest(false);
        setConnectionInterrupted(false);

        const endpoint = marker.operation_kind === 'runtime_install'
            ? '/api/v1/runtimes/node'
            : marker.operation_kind === 'mail_profile_install'
                ? '/api/v1/service/profile/install'
                : '/api/v1/service/install';
        const body = marker.operation_kind === 'runtime_install'
            ? { version: request.version, request_id: marker.request_id }
            : marker.operation_kind === 'mail_profile_install'
                ? { profile_id: request.serviceId, request_id: marker.request_id, confirmed: true }
                : {
                      service_id: request.serviceId,
                      ...(request.package ? { package: request.package } : {}),
                      request_id: marker.request_id,
                  };

        try {
            const response = await fetch(endpoint, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(body),
            });
            if (!response.ok) {
                if (response.status === 401) {
                    setConnectionInterrupted(true);
                    return false;
                }
                if (response.status === 409) {
                    const error = await readApiError(response);
                    if (error.code === ERROR_CODE_OPERATION_BUSY) {
                        return await recoverCurrentOperation(
                            request,
                            'busy',
                            recoveryGeneration,
                        );
                    }
                    finishFailure({
                        ...error,
                        message: error.message || t('services.operation.startFailed'),
                    });
                    return false;
                }
                if (response.status === 429 || response.status >= 500) {
                    setConnectionInterrupted(true);
                    return await recoverCurrentOperation(request, 'indeterminate', recoveryGeneration);
                }
                const error = await readApiError(response);
                finishFailure({
                    ...error,
                    message: error.message || t('services.operation.startFailed'),
                });
                return false;
            }

            let payload: unknown;
            try {
                payload = await response.json();
            } catch {
                return await recoverCurrentOperation(request, 'indeterminate', recoveryGeneration);
            }
            const next = decodeOperation(payload);
            if (!next) {
                return await recoverCurrentOperation(request, 'indeterminate', recoveryGeneration);
            }
            if (
                shouldAdoptOperationFromRecoveryMarker(
                    next,
                    marker,
                    responseReferenceTime(response),
                )
                && adoptOperation(next, request.name, marker)
            ) {
                return true;
            }
            return await recoverCurrentOperation(
                request,
                'indeterminate',
                recoveryGeneration,
            );
        } catch {
            // The POST may have reached the server even when its response was lost.
            // Yanıt kaybolsa bile POST sunucuya ulaşmış olabilir.
            setConnectionInterrupted(true);
            return await recoverCurrentOperation(request, 'indeterminate', recoveryGeneration);
        } finally {
            if (
                recoveryGenerationRef.current === recoveryGeneration
                && recoveryMarkerRef.current === null
            ) {
                setSubmitting(false);
            }
        }
    };

    useEffect(() => {
        if (!operation?.id) return;

        let cancelled = false;
        let timer: number | undefined;
        const schedule = (fn: () => void, delay: number) => {
            if (!cancelled) timer = window.setTimeout(fn, delay);
        };

        // A terminal failure is not safe to publish until a fresh managed-service
        // snapshot confirms the machine's real state. Preserve the operation error
        // across retries and keep the global lock and recovery marker intact.
        // Terminal hata, yeni bir yönetilen servis snapshot'ı makinenin gerçek
        // durumunu doğrulamadan yayınlanamaz. Yeniden denemelerde işlem hatasını,
        // genel kilidi ve kurtarma işaretçisini koru.
        const refreshFailedSnapshot = async (
            terminalFailure: ApiError,
            terminalOperation: ComponentOperation,
        ) => {
            if (cancelled) return;
            setRefreshingCatalog(true);

            let scanResponse: Response;
            try {
                scanResponse = await fetch('/api/v1/managed-services/scan', {
                    method: 'POST',
                    cache: 'no-store',
                });
            } catch {
                if (!cancelled) {
                    setConnectionInterrupted(true);
                    schedule(
                        () => void refreshFailedSnapshot(terminalFailure, terminalOperation),
                        RETRY_DELAY_MS,
                    );
                }
                return;
            }

            // AuthGate owns 401 handling and the durable marker must survive login.
            // 401 işlemini AuthGate yönetir; kalıcı işaretçi giriş boyunca korunmalıdır.
            if (scanResponse.status === 401) return;
            if (!scanResponse.ok) {
                setConnectionInterrupted(true);
                schedule(
                    () => void refreshFailedSnapshot(terminalFailure, terminalOperation),
                    RETRY_DELAY_MS,
                );
                return;
            }

            let snapshot: unknown;
            try {
                snapshot = await scanResponse.json();
            } catch {
                setConnectionInterrupted(true);
                schedule(
                    () => void refreshFailedSnapshot(terminalFailure, terminalOperation),
                    RETRY_DELAY_MS,
                );
                return;
            }
            const freshSnapshot = decodeManagedServicesSnapshot(snapshot);
            if (
                freshSnapshot === null
                || !snapshotConfirmsTerminalOperation(freshSnapshot, terminalOperation)
            ) {
                setConnectionInterrupted(true);
                schedule(
                    () => void refreshFailedSnapshot(terminalFailure, terminalOperation),
                    RETRY_DELAY_MS,
                );
                return;
            }

            if (cancelled) return;
            setCatalogSnapshot(freshSnapshot);
            finishFailure(terminalFailure);
        };

        let poll: () => Promise<void>;
        const reconcileUnverifiableOperation = async () => {
            if (cancelled) return;
            const marker = recoveryMarkerRef.current;
            const request = marker !== null
                ? requestFromRecoveryMarker(marker)
                : {
                    serviceId: operation.service_id || 'service',
                    name: label || operationDisplayLabel(operation),
                };
            const generation = ++recoveryGenerationRef.current;
            setRecoveringRequest(true);
            setConnectionInterrupted(true);
            const recovered = await recoverCurrentOperation(request, 'marker', generation);
            if (
                !cancelled
                && recovered
                && adoptedOperationIDRef.current === operation.id
            ) {
                schedule(poll, POLL_DELAY_MS);
            }
        };

        poll = async () => {
            try {
                const response = await fetch(
                    `/api/v1/service/operation?id=${encodeURIComponent(operation.id)}`,
                    { cache: 'no-store' },
                );
                if (!response.ok) {
                    // AuthGate will require a fresh login. Do not erase the
                    // operation id: it is the reattachment token after login.
                    // AuthGate yeniden giriş ister. Giriş sonrası yeniden bağlanma
                    // belirteci olan işlem kimliğini silme.
                    if (response.status === 401) {
                        return;
                    }
                    await reconcileUnverifiableOperation();
                    return;
                }

                let payload: unknown;
                try {
                    payload = await response.json();
                } catch {
                    await reconcileUnverifiableOperation();
                    return;
                }
                const next = decodeOperation(payload);
                if (!next) {
                    await reconcileUnverifiableOperation();
                    return;
                }
                if (cancelled) return;

                let marker = recoveryMarkerRef.current;
                if (next.id !== operation.id) {
                    await reconcileUnverifiableOperation();
                    return;
                }
                if (marker === null) {
                    marker = recoveryMarkerForOperation(next);
                    if (marker === null) {
                        await reconcileUnverifiableOperation();
                        return;
                    }
                    storeRecoveryMarker(marker);
                    recoveryMarkerRef.current = marker;
                }
                if (!operationMatchesRecoveryMarker(next, marker)) {
                    await reconcileUnverifiableOperation();
                    return;
                }

                setConnectionInterrupted(false);
                setOperation(next);

                if (next.status === 'failed') {
                    await refreshFailedSnapshot(
                        operationError(next.error, t('services.operation.failed')),
                        next,
                    );
                    return;
                }

                if (next.status !== 'succeeded') {
                    schedule(poll, POLL_DELAY_MS);
                    return;
                }

                // A terminal success is not a UI success yet. Keep the global
                // lock until a fresh server scan has been received and stored;
                // every Components consumer then renders the same snapshot.
                // Terminal başarı henüz arayüz başarısı değildir. Taze sunucu
                // taraması alınıp saklanana dek genel kilidi tut; ardından tüm
                // Bileşenler tüketicileri aynı snapshot'ı çizer.
                setRefreshingCatalog(true);
                let scanResponse: Response;
                try {
                    scanResponse = await fetch('/api/v1/managed-services/scan', {
                        method: 'POST',
                        cache: 'no-store',
                    });
                } catch {
                    setConnectionInterrupted(true);
                    schedule(poll, RETRY_DELAY_MS);
                    return;
                }
                if (!scanResponse.ok) {
                    if (scanResponse.status === 401) {
                        return;
                    }
                    setConnectionInterrupted(true);
                    schedule(poll, RETRY_DELAY_MS);
                    return;
                }

                let snapshot: unknown;
                try {
                    snapshot = await scanResponse.json();
                } catch {
                    setConnectionInterrupted(true);
                    schedule(poll, RETRY_DELAY_MS);
                    return;
                }
                const freshSnapshot = decodeManagedServicesSnapshot(snapshot);
                if (
                    freshSnapshot === null
                    || !snapshotConfirmsTerminalOperation(freshSnapshot, next)
                ) {
                    setConnectionInterrupted(true);
                    schedule(poll, RETRY_DELAY_MS);
                    return;
                }
                const verifiedProfileResult = next.kind === 'mail_profile_install'
                    ? decodeVerifiedMailProfileResult(freshSnapshot, next)
                    : null;

                setCatalogSnapshot(freshSnapshot);
                clearStoredOperation();
                recoveryMarkerRef.current = null;
                adoptedOperationIDRef.current = '';
                lockedRef.current = false;
                setOperation(null);
                setRefreshingCatalog(false);
                setConnectionInterrupted(false);
                setFailure(null);
                const installedName = label || next.service_id || t('services.install');
                if (verifiedProfileResult?.fallbackOnly) {
                    showToast('warning', t('services.mailProfiles.fallbackWarning', {
                        name: installedName,
                    }));
                } else {
                    showToast('success', t('services.installed', { name: installedName }));
                }
            } catch {
                // Network errors and 5xx/429 responses are transient. Never
                // unlock here: the server may still be changing packages.
                // Ağ hataları ile 5xx/429 yanıtları geçicidir. Sunucu hâlâ
                // paket değiştiriyor olabileceğinden burada kilidi asla açma.
                if (!cancelled) {
                    setConnectionInterrupted(true);
                    schedule(poll, RETRY_DELAY_MS);
                }
            }
        };

        poll();
        return () => {
            cancelled = true;
            if (timer !== undefined) window.clearTimeout(timer);
        };
        // The operation id owns one polling loop. Phase/status updates should
        // not tear it down and open a second loop.
        // İşlem kimliği tek yoklama döngüsünün sahibidir. Aşama/durum
        // güncellemeleri onu kapatıp ikinci döngü açmamalıdır.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [operation?.id]);

    return (
        <ComponentOperationContext.Provider
            value={{
                operation,
                locked,
                failure,
                catalogSnapshot,
                startInstall,
                clearFailure: () => setFailure(null),
            }}
        >
            {children}
            {locked && createPortal(
                <OperationOverlay
                    operation={operation}
                    label={label}
                    discovering={discoveringActive}
                    submitting={submitting}
                    recovering={recoveringRequest}
                    refreshing={refreshingCatalog}
                    interrupted={connectionInterrupted}
                />,
                document.body,
            )}
            {failure && !locked && createPortal(
                <div
                    role="alert"
                    className="fixed inset-x-4 top-4 z-[90] mx-auto max-w-2xl rounded-xl bg-surface shadow-2xl"
                >
                    <div className="relative">
                        <ErrorBanner error={failure} className="pr-12" />
                        <button
                            type="button"
                            onClick={() => setFailure(null)}
                            aria-label={t('services.operation.dismiss')}
                            title={t('services.operation.dismiss')}
                            className="absolute right-2 top-2 rounded-md p-1.5 text-danger transition-colors hover:bg-danger/10"
                        >
                            <X className="h-4 w-4" />
                        </button>
                    </div>
                </div>,
                document.body,
            )}
        </ComponentOperationContext.Provider>
    );
}

function OperationOverlay({
    operation,
    label,
    discovering,
    submitting,
    recovering,
    refreshing,
    interrupted,
}: {
    operation: ComponentOperation | null;
    label: string;
    discovering: boolean;
    submitting: boolean;
    recovering: boolean;
    refreshing: boolean;
    interrupted: boolean;
}) {
    const { t } = useI18n();
    const dialogRef = useRef<HTMLDivElement>(null);

    useEffect(() => {
        dialogRef.current?.focus();
    }, []);

    let statusText = interrupted
        ? t('services.operation.reconnecting')
        : discovering
            ? t('services.operation.checkingActive')
            : t('services.operation.starting');
    if (!interrupted && !discovering && recovering) {
        statusText = t('services.operation.recoveringRequest');
    } else if (!interrupted && !discovering && refreshing) {
        statusText = t('services.operation.refreshing');
    } else if (!interrupted && !discovering && !submitting && operation) {
        const normalizedPhase = operation.phase.trim().toLowerCase().replace(/[^a-z0-9]+/g, '_');
        const phaseKey = `services.operation.phase.${normalizedPhase}` as TranslationKey;
        const translated = t(phaseKey);
        statusText = translated === phaseKey
            ? operation.phase || t('services.operation.running')
            : translated;
    }

    return (
        <div
            ref={dialogRef}
            role="dialog"
            aria-modal="true"
            aria-labelledby="component-operation-title"
            tabIndex={-1}
            className="fixed inset-0 z-[100] flex items-center justify-center bg-slate-950/75 p-4 backdrop-blur-sm outline-none"
        >
            <div className="w-full max-w-lg rounded-2xl border border-border-strong bg-surface p-6 text-center shadow-2xl">
                <span className={`mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-2xl ${
                    interrupted ? 'bg-warning/10 text-warning' : 'bg-primary/10 text-primary'
                }`}>
                    {interrupted
                        ? <WifiOff className="h-7 w-7" />
                        : <LoaderCircle className="h-7 w-7 animate-spin" />}
                </span>
                <h2 id="component-operation-title" className="text-xl font-semibold text-fg">
                    {discovering
                        ? t('services.operation.checkingTitle')
                        : t('services.operation.title', { name: label || t('services.install') })}
                </h2>
                <p role="status" aria-live="polite" className="mt-2 text-sm font-medium text-fg-muted">
                    {statusText}
                </p>
                <p className="mt-4 rounded-lg border border-border bg-surface-2 px-4 py-3 text-xs leading-5 text-fg-subtle">
                    {t(discovering
                        ? 'services.operation.activeCheckHint'
                        : 'services.operation.backgroundHint')}
                </p>
                {operation?.id && (
                    <p className="mt-3 font-mono text-[11px] text-fg-subtle">
                        {t('services.operation.id', { id: operation.id })}
                    </p>
                )}
            </div>
        </div>
    );
}

export function useComponentOperation(): ComponentOperationContextValue {
    const value = useContext(ComponentOperationContext);
    if (!value) {
        throw new Error('useComponentOperation must be used within ComponentOperationProvider');
    }
    return value;
}
