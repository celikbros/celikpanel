export type HostMutationReason =
    | 'panel_operation_active'
    | 'agent_mutation_active'
    | 'host_lock_busy'
    | 'package_manager_active'
    | 'state_unverified';

export type HostMutationReadiness =
    | { ready: true }
    | {
        ready: false;
        code: 'HOST_MUTATION_BUSY' | 'HOST_MUTATION_UNAVAILABLE';
        reason: HostMutationReason;
    };

export interface HostMutationReadinessRuntime {
    fetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response>;
    setTimeout(callback: () => void, delay: number): ReturnType<typeof globalThis.setTimeout>;
    clearTimeout(handle: ReturnType<typeof globalThis.setTimeout>): void;
}

export const HOST_MUTATION_READINESS_TIMEOUT_MS = 8000;

const readinessReasons: HostMutationReason[] = [
    'panel_operation_active',
    'agent_mutation_active',
    'host_lock_busy',
    'package_manager_active',
    'state_unverified',
];

export function unverifiedHostMutationReadiness(): HostMutationReadiness {
    return {
        ready: false,
        code: 'HOST_MUTATION_UNAVAILABLE',
        reason: 'state_unverified',
    };
}

export function decodeHostMutationReadiness(payload: unknown): HostMutationReadiness | null {
    if (!payload || typeof payload !== 'object') return null;
    const value = payload as Record<string, unknown>;
    if (value.ready === true) {
        return value.code === undefined && value.reason === undefined ? { ready: true } : null;
    }
    if (value.ready !== false
        || (value.code !== 'HOST_MUTATION_BUSY' && value.code !== 'HOST_MUTATION_UNAVAILABLE')
        || !readinessReasons.includes(value.reason as HostMutationReason)) return null;
    return {
        ready: false,
        code: value.code,
        reason: value.reason as HostMutationReason,
    };
}

export async function fetchHostMutationReadiness(
    runtime?: HostMutationReadinessRuntime,
    externalSignal?: AbortSignal,
): Promise<HostMutationReadiness> {
    const request = runtime?.fetch ?? globalThis.fetch;
    const schedule = runtime?.setTimeout ?? globalThis.setTimeout;
    const cancel = runtime?.clearTimeout ?? globalThis.clearTimeout;
    const controller = new AbortController();
    let resolveUnavailable: (value: HostMutationReadiness) => void = () => undefined;
    const unavailable = new Promise<HostMutationReadiness>((resolve) => {
        resolveUnavailable = resolve;
    });
    const abortAndResolveUnavailable = () => {
        controller.abort();
        resolveUnavailable(unverifiedHostMutationReadiness());
    };
    if (externalSignal?.aborted) abortAndResolveUnavailable();
    else externalSignal?.addEventListener('abort', abortAndResolveUnavailable, { once: true });
    const timeout = schedule(
        abortAndResolveUnavailable,
        HOST_MUTATION_READINESS_TIMEOUT_MS,
    );
    try {
        const requestAndDecode = (async (): Promise<HostMutationReadiness> => {
            try {
                const response = await request('/api/v1/host-mutation-readiness', {
                    method: 'GET',
                    cache: 'no-store',
                    credentials: 'same-origin',
                    signal: controller.signal,
                });
                if (!response.ok) return unverifiedHostMutationReadiness();
                return decodeHostMutationReadiness(await response.json()) ?? unverifiedHostMutationReadiness();
            } catch {
                return unverifiedHostMutationReadiness();
            }
        })();
        // Some fetch shims and partially-read response bodies do not reject when
        // their signal is aborted. Racing the complete request+decode path keeps
        // the admission deadline authoritative for every caller.
        return await Promise.race([requestAndDecode, unavailable]);
    } finally {
        cancel(timeout);
        externalSignal?.removeEventListener('abort', abortAndResolveUnavailable);
    }
}

export async function runHostMutationAdmission<T>(
    readReadiness: () => Promise<HostMutationReadiness>,
    onReady: () => Promise<T>,
): Promise<HostMutationReadiness> {
    const readiness = await readReadiness();
    if (readiness.ready) await onReady();
    return readiness;
}
