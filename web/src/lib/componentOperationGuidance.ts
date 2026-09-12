import type { TranslationKey } from '../i18n/en';

interface OperationObservation {
    status: string;
    phase: string;
}

export interface ComponentOperationGuidance {
    statusKey?: TranslationKey;
    hintKey: TranslationKey;
    failed: boolean;
}

// A lost connection is not evidence that installation is still running. A
// recorded failure must stay visible while the separate state scan retries.
// Baglanti kaybi kurulumun surdugunu kanitlamaz. Kaydedilmis hata, ayri durum
// taramasi yeniden denenirken gorunur kalmalidir.
export function componentOperationGuidance(
    operation: OperationObservation | null,
    interrupted: boolean,
    recovering: boolean,
    refreshing: boolean,
): ComponentOperationGuidance {
    if (operation?.status === 'failed') {
        return {
            statusKey: 'services.operation.failedChecking',
            hintKey: interrupted
                ? 'services.operation.failedConnectionHint'
                : 'services.operation.failedCheckingHint',
            failed: true,
        };
    }
    if (interrupted || recovering) {
        return {
            statusKey: interrupted
                ? 'services.operation.reconnecting'
                : 'services.operation.recoveringRequest',
            hintKey: 'services.operation.uncertainHint',
            failed: false,
        };
    }
    if (refreshing || operation?.status === 'succeeded') {
        return {
            statusKey: 'services.operation.refreshing',
            hintKey: 'services.operation.verifyingHint',
            failed: false,
        };
    }
    return { hintKey: 'services.operation.backgroundHint', failed: false };
}

const phaseKeys: Record<string, TranslationKey> = {
    queued: 'services.operation.phase.queued',
    running: 'services.operation.phase.running',
    preflight: 'services.operation.phase.preparing',
    preparing: 'services.operation.phase.preparing',
    downloading: 'services.operation.phase.downloading',
    installing: 'services.operation.phase.installing',
    configuring: 'services.operation.phase.configuring',
    starting: 'services.operation.phase.starting',
    verifying: 'services.operation.phase.verifying',
    scanning: 'services.operation.phase.scanning',
    syncing: 'services.operation.phase.syncing',
    syncing_dns: 'services.operation.phase.syncing_dns',
    finalizing: 'services.operation.phase.finalizing',
    'mail-stack': 'services.operation.phase.configuring',
    submission: 'services.operation.phase.configuring',
};

export function componentOperationPhaseKey(phase: string): TranslationKey {
    // Mail profiles prefix a known phase with their profile and component IDs.
    // Posta profilleri bilinen asamanin onune profil ve bilesen kimliklerini ekler.
    const profile = /^profile\/(core-mail|webmail|protected-mail)\/(?:[a-z0-9-]+\/)?([a-z_\-]+)$/.exec(phase);
    const key = profile ? profile[2] : phase;
    return Object.prototype.hasOwnProperty.call(phaseKeys, key) ? phaseKeys[key] : 'services.operation.running';
}
