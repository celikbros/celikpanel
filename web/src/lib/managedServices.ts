import { decodeManagedServicesSnapshot, type ManagedServicesSnapshot } from '../components/ComponentOperation';
import { mapRemote, useRemote, type Remote, type RemoteHandle } from './remote';

// The stored component records, for a screen that only shows them or takes a
// component's own facts from them: GET /api/v1/managed-services. One address,
// one decoder (the fail-closed one the operation tracker exports, plus the
// shape of the one field read here that it does not look at), one request
// shared by everything on screen. It reads the stored scan and never probes
// the host. A payload the decoder refuses is unknown; it is not "nothing
// installed", not "not checked yet" and not "this component has no
// configuration files": before 9 Oct 2026 the PostgreSQL and MariaDB pages
// said "postgresql.conf not found" on an installed server for as long as the
// read took, and for good when it failed.
//
// Every reader of this address uses `decodeManagedServices` (see
// lib/remote.ts: the first reader's decoder is the address's decoder). The
// answer is kept as the server sent it, so a reader takes what it needs with
// `mapRemote`.
//
// Kayıtlı bileşen kayıtları; onları yalnız gösteren ya da bir bileşenin kendi
// olgularını onlardan alan ekran için. Tek adres, tek çözücü (işlem
// izleyicinin dışa açtığı güvenli taraf çözücüsü ve onun bakmadığı, burada
// okunan tek alanın biçimi), ekrandaki her şeyin paylaştığı tek istek. Kayıtlı
// taramayı okur, makineyi asla yoklamaz. Çözücünün reddettiği yük
// bilinmeyendir; "hiçbir şey kurulu değil", "henüz bakılmadı" ya da "bu
// bileşenin yapılandırma dosyası yok" değildir.
export const MANAGED_SERVICES_URL = '/api/v1/managed-services';

// `config_files` is the list a reader here walks. The server writes a list or
// `null` (a Go list nobody appended to); anything else is not the contract.
// `config_files`, buradaki okuyucunun gezdiği listedir. Sunucu liste ya da
// `null` yazar; başka her şey sözleşme değildir.
function configFilesReadable(service: Record<string, unknown>): boolean {
    const files = service.config_files;
    if (files === undefined || files === null) return true;
    return Array.isArray(files) && files.every((file) => (
        !!file && typeof file === 'object' && typeof (file as { path?: unknown }).path === 'string'
    ));
}

export function decodeManagedServices(raw: unknown): ManagedServicesSnapshot {
    const snapshot = decodeManagedServicesSnapshot(raw);
    if (!snapshot || !snapshot.services.every(configFilesReadable)) throw new Error('shape');
    return snapshot;
}

// A panel that mounts within this time of the page's own read uses the page's
// answer and sends nothing: a component's page draws its sections once its
// header knows the component, a moment after the page itself asked. Without
// this that late section asked a second time, and a second answer that failed
// took away what the first had just shown.
// Sayfanın okumasından bu süre içinde açılan bölüm sayfanın yanıtını kullanır
// ve istek göndermez. Bu olmadan geç açılan bölüm ikinci kez soruyordu ve
// başarısız ikinci yanıt, ilkinin az önce gösterdiğini geri alıyordu.
const FRESH_FOR_MS = 30_000;

export function useManagedServices(): RemoteHandle<ManagedServicesSnapshot> {
    return useRemote(MANAGED_SERVICES_URL, decodeManagedServices, { freshFor: FRESH_FOR_MS });
}

// The configuration files the scan found for one component. An empty list is a
// known answer: the scan was read and names none for it. It is the same read
// as `useManagedServices`, so a page that shows both causes one request.
// Taramanın bir bileşen için bulduğu yapılandırma dosyaları. Boş liste bilinen
// bir yanıttır. `useManagedServices` ile aynı okumadır; ikisini gösteren sayfa
// tek istek yapar.
export function useComponentConfigFiles(serviceId: string): { files: Remote<string[]>; retry: () => void; reading: boolean } {
    const { remote, retry, reading } = useManagedServices();
    const files = mapRemote(remote, (snapshot) => {
        const record = snapshot.services.find((service) => service.id === serviceId);
        return ((record?.config_files as { path: string }[] | null | undefined) ?? [])
            .map((file) => file.path)
            .filter((path) => path !== '');
    });
    return { files, retry: () => void retry(), reading };
}

export type { ManagedServicesSnapshot };
