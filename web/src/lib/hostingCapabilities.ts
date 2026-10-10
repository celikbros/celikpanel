import { useRemote, type Remote, type RemoteHandle } from './remote';

// What this server can host right now: GET /api/v1/hosting/capabilities.
//
// This is the ONE reader of that address. Before 9 Oct 2026 eight screens read
// it on their own and each gave a failed or unfinished read its own meaning:
// "no DNS engine", "PHP is installed", "no database engine", an empty version
// list. An owner opening "Add domain" on a server that has DNS was told to
// choose a DNS engine for as long as the read took.
//
// Every reader now gets the same three states from the same request. Nothing
// about the server is shown as missing until the server has said so.
//
// Bu sunucunun şu anda neyi barındırabildiği. Bu adresin TEK okuyucusu budur.
// 9 Eki 2026'dan önce sekiz ekran onu ayrı ayrı okuyor ve bitmemiş ya da
// başarısız okumaya her biri kendi anlamını veriyordu. Artık her okuyan aynı
// istekten aynı üç durumu alır; sunucu söylemeden hiçbir şey eksik gösterilmez.
export interface HostingCapabilities {
    /** "nginx", or "" when no supported web server is installed. */
    web_server: string;
    /** Installed PHP-FPM versions, newest first. */
    php_versions: string[];
    /** The proven active engine ("bind", "pdns"), or "". */
    dns_server: string;
    /** The nameserver pair and operating mode are saved. */
    dns_identity_ready: boolean;
    dns_management_mode: 'local' | 'external' | 'existing' | '';
    dns_management_ready: boolean;
    mail_server: boolean;
    database_servers: string[];
    db_tools: string[];
}

export const HOSTING_CAPABILITIES_URL = '/api/v1/hosting/capabilities';

function strings(value: unknown): string[] {
    if (!Array.isArray(value) || value.some((item) => typeof item !== 'string')) throw new Error('list');
    return value as string[];
}

// The answer is the contract or it is unknown. A missing or mistyped field is
// not read as "nothing installed": that is how an unreadable answer once became
// "DNS is missing" on screen.
// Yanıt ya sözleşmedir ya da bilinmeyendir. Eksik ya da yanlış türde bir alan
// "hiçbir şey kurulu değil" diye okunmaz.
export function decodeHostingCapabilities(raw: unknown): HostingCapabilities {
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) throw new Error('shape');
    const body = raw as Record<string, unknown>;
    if (
        typeof body.web_server !== 'string' ||
        typeof body.dns_server !== 'string' ||
        typeof body.dns_identity_ready !== 'boolean' ||
        typeof body.mail_server !== 'boolean'
    ) {
        throw new Error('field');
    }
    const mode = body.dns_management_mode;
    return {
        web_server: body.web_server,
        php_versions: strings(body.php_versions),
        dns_server: body.dns_server,
        dns_identity_ready: body.dns_identity_ready,
        dns_management_mode: mode === 'local' || mode === 'external' || mode === 'existing' ? mode : '',
        dns_management_ready: body.dns_management_ready === true,
        mail_server: body.mail_server,
        database_servers: strings(body.database_servers),
        db_tools: strings(body.db_tools),
    };
}

// A dialog or panel that mounts within this time of the page's own read uses
// the page's answer and sends nothing. Later than that it still shows the
// page's answer at once and refreshes it behind the screen.
// Sayfanın okumasından bu süre içinde açılan pencere ya da bölüm sayfanın
// yanıtını kullanır ve istek göndermez.
const FRESH_FOR_MS = 30_000;

// `enabled: false` is for a signed-in team member: the server-wide inventory
// is not theirs to read, so nothing is requested and the caller must not draw
// from the handle.
// `enabled: false` ekip üyesi içindir: sunucu geneli envanter ona ait değildir.
export function useHostingCapabilities(options: { enabled?: boolean } = {}): RemoteHandle<HostingCapabilities> {
    return useRemote(
        options.enabled === false ? null : HOSTING_CAPABILITIES_URL,
        decodeHostingCapabilities,
        { freshFor: FRESH_FOR_MS },
    );
}

// --- DNS readiness for adding a domain --------------------------------------
//
// Explicit external or remote DNS permits websites without a local publisher.
// A DNS-only domain still requires a proven local authoritative service.
// Açık harici ya da uzak DNS, yerel yayıncı olmadan web sitesine izin verir.
// Yalnız-DNS alan adı ise kanıtlanmış yerel bir yetkili hizmet ister.
export type DomainPurpose = 'website' | 'dnsonly';

/** Which half is missing: no active engine, or an engine without its identity. */
export type DNSBlocker = 'engine' | 'identity';

export function localDNSReady(caps: HostingCapabilities): boolean {
    return caps.dns_server !== '' && caps.dns_identity_ready === true;
}

export function hostingDNSReady(caps: HostingCapabilities): boolean {
    const delegatedElsewhere =
        (caps.dns_management_mode === 'external' || caps.dns_management_mode === 'existing') &&
        caps.dns_management_ready === true;
    return localDNSReady(caps) || delegatedElsewhere;
}

// null when a domain of this purpose can be added. The blocker is computed
// from a known answer only; see gateOn in ./remote.
// Bu amaçla alan adı eklenebiliyorsa null. Engel yalnız bilinen yanıttan
// hesaplanır.
export function dnsBlocker(caps: HostingCapabilities, purpose: DomainPurpose): DNSBlocker | null {
    const ready = purpose === 'dnsonly' ? localDNSReady(caps) : hostingDNSReady(caps);
    if (ready) return null;
    return caps.dns_server === '' ? 'engine' : 'identity';
}

export type CapabilitiesRemote = Remote<HostingCapabilities>;
