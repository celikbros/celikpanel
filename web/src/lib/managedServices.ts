import { decodeManagedServicesSnapshot, type ManagedServicesSnapshot } from '../components/ComponentOperation';
import { useRemote, type RemoteHandle } from './remote';

// The stored component records, for a screen that only shows them:
// GET /api/v1/managed-services. One address, one decoder (the fail-closed one
// the operation tracker exports), one request shared by everything on screen.
// It reads the stored scan and never probes the host. A payload the decoder
// refuses is unknown; it is not "nothing installed" and not "not checked yet".
//
// Kayıtlı bileşen kayıtları, onları yalnız gösteren ekran için. Tek adres, tek
// çözücü (işlem izleyicinin dışa açtığı güvenli taraf çözücüsü), ekrandaki her
// şeyin paylaştığı tek istek. Kayıtlı taramayı okur, makineyi asla yoklamaz.
// Çözücünün reddettiği yük bilinmeyendir; "hiçbir şey kurulu değil" ya da
// "henüz bakılmadı" değildir.
export const MANAGED_SERVICES_URL = '/api/v1/managed-services';

export function decodeManagedServices(raw: unknown): ManagedServicesSnapshot {
    const snapshot = decodeManagedServicesSnapshot(raw);
    if (!snapshot) throw new Error('shape');
    return snapshot;
}

export function useManagedServices(): RemoteHandle<ManagedServicesSnapshot> {
    return useRemote(MANAGED_SERVICES_URL, decodeManagedServices);
}

export type { ManagedServicesSnapshot };
