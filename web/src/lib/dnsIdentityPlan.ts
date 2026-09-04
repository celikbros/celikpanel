export type DNSIdentityRole = 'standalone' | 'paired';
export type DNSIdentityPairRole = 'primary' | 'secondary';

export interface SavedDNSIdentityPlan {
    configured: boolean;
    namesDerived: boolean;
    ns1: string;
    ns2: string;
    role: DNSIdentityRole;
    peer_ip: string;
    peer_ns: string;
    server_ip: string;
}

export interface DraftDNSIdentityPlan {
    ns1: string;
    ns2: string;
    role: DNSIdentityRole | '';
    peer_ip: string;
    peer_ns: string;
}

interface DNSIdentityEngineEvidence {
    active_engine: string | null;
    topology: string;
}

interface DNSEngineFlowEntryEvidence {
    id: string;
    installed: boolean;
    running: boolean;
    managed: boolean;
    status: string;
}

interface DNSEngineFlowEvidence extends DNSIdentityEngineEvidence {
    state: string;
    engine_epoch: number;
    engines: DNSEngineFlowEntryEvidence[];
}

export type DNSEngineSettingsFlow =
    | 'unavailable'
    | 'identityStaging'
    | 'legacyPowerDNSReconfigure'
    | 'active'
    | 'manualRecovery'
    | 'locked';

// The takeover's running shape, read from the same facts the panel's own
// `adoptableUnmanagedDNSEngine` reads it from (cmd/panel/dns_engine.go): BIND
// on disk and answering, no durable CelikPanel authority over any of it, and
// nothing else serving. This is a rented server that arrived with a DNS server
// somebody else installed, and it is the ordinary way they arrive - not a host
// whose ownership went wrong.
//
// Topology is the one fact read more widely here than the API reads it, and
// deliberately. The API needs a saved standalone identity because it is about
// to publish one; the screen is what the operator uses to save it, and a server
// that has never been configured reports `unconfigured`. Requiring `standalone`
// here would lock the operator out of the step that produces it. Nothing is
// taken on trust by doing so: while the identity is unconfigured
// `dnsEngineIdentityReviewLocked` keeps the review shut, and the preview - the
// only authority on whether this host can actually be taken over - still
// refuses everything it refused before, by name. `paired` is refused outright,
// as the API refuses it: pairing is a decision about two servers, and the
// takeover does not answer it.
function adoptableUnmanagedDNSEngine(snapshot: DNSEngineFlowEvidence): boolean {
    if (snapshot.state !== 'unmanaged' ||
        snapshot.active_engine !== null ||
        snapshot.engine_epoch !== 0 ||
        snapshot.topology === 'paired') {
        return false;
    }
    const bind = snapshot.engines.find((entry) => entry.id === 'bind');
    if (!bind || !bind.installed || !bind.running || bind.managed ||
        bind.status !== 'unmanaged') {
        return false;
    }
    return snapshot.engines.every((entry) => entry.id === 'bind' || !entry.running);
}

// Keep rendering decisions for the no-authority states in one fail-closed
// matrix. The legacy reconfigure exception is exact; an unmanaged service
// whose ownership is not proven must never fall into the fresh-install flow.
export function dnsEngineSettingsFlow(
    snapshot: DNSEngineFlowEvidence | null,
): DNSEngineSettingsFlow {
    if (snapshot === null) return 'unavailable';

    const legacyPowerDNS = snapshot.engines.find((entry) => entry.id === 'pdns');
    const legacyPowerDNSReconfigure = snapshot.state === 'unmanaged' &&
        snapshot.active_engine === null &&
        legacyPowerDNS?.status === 'unmanaged' &&
        legacyPowerDNS.installed && legacyPowerDNS.running && legacyPowerDNS.managed &&
        snapshot.engines.every((entry) => entry.id === 'pdns' || !entry.running);
    if (legacyPowerDNSReconfigure) return 'legacyPowerDNSReconfigure';

    if (snapshot.state === 'unconfigured' && snapshot.active_engine === null) {
        return 'identityStaging';
    }
    if (snapshot.state === 'ready' &&
        (snapshot.active_engine === 'pdns' || snapshot.active_engine === 'bind')) {
        return 'active';
    }
    if (snapshot.state === 'unmanaged' && snapshot.active_engine === null) {
        // Manual recovery is for a host whose DNS ownership could not be
        // proven, and it locks every action because there is nothing safe to
        // offer. It used to claim this whole state, which swallowed the one
        // shape inside it that has an answer: a running DNS server CelikPanel
        // did not install, which the product takes over with the operator's
        // explicit consent. That answer existed and was reachable only through
        // the API. It is the same route the stopped shape already takes - stage
        // the identity, then review and commit the adoption on the engine card.
        if (adoptableUnmanagedDNSEngine(snapshot)) return 'identityStaging';
        return 'manualRecovery';
    }
    return 'locked';
}

function cleanHostname(value: string): string {
    return value.trim().toLowerCase().replace(/\.$/, '');
}

function canonicalIPv4(value: string): string {
    const octets = value.trim().split('.');
    if (octets.length !== 4) return '';
    const parsed = octets.map((octet) => {
        if (!/^\d{1,3}$/.test(octet)) return -1;
        const number = Number(octet);
        return number >= 0 && number <= 255 ? number : -1;
    });
    return parsed.some((octet) => octet < 0) ? '' : parsed.join('.');
}

function isGlobalUnicastIPv4(value: string): boolean {
    const canonical = canonicalIPv4(value);
    if (canonical === '') return false;
    const [a, b] = canonical.split('.').map(Number);
    return canonical !== '0.0.0.0' &&
        canonical !== '255.255.255.255' &&
        a !== 127 &&
        !(a === 169 && b === 254) &&
        !(a >= 224 && a <= 239);
}

function isValidNameserver(value: string): boolean {
    const hostname = cleanHostname(value);
    if (hostname.length < 3 || hostname.length > 253 || !hostname.includes('.')) return false;
    return hostname.split('.').every((label) =>
        label.length > 0 &&
        label.length <= 63 &&
        /^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/.test(label),
    );
}

export function exactStagedIdentityIsCurrent(
    stagingOnly: boolean,
    saved: SavedDNSIdentityPlan | null,
    draft: DraftDNSIdentityPlan | null,
    needsClusterRetry: boolean,
    pairRole?: DNSIdentityPairRole,
): boolean {
    if (!stagingOnly || !saved || !draft || needsClusterRetry ||
        !saved.configured || saved.namesDerived || draft.role === '') {
        return false;
    }

    const ns1 = cleanHostname(draft.ns1);
    const ns2 = cleanHostname(draft.ns2);
    if (!isValidNameserver(ns1) || !isValidNameserver(ns2) || ns1 === ns2 ||
        ns1 !== saved.ns1 || ns2 !== saved.ns2 || draft.role !== saved.role) {
        return false;
    }

    const savedPeerIP = saved.peer_ip.trim();
    const savedPeerNS = cleanHostname(saved.peer_ns);
    if (draft.role === 'standalone') {
        return savedPeerIP === '' && savedPeerNS === '' && pairRole === undefined;
    }

    const serverIP = canonicalIPv4(saved.server_ip);
    const canonicalSavedPeerIP = canonicalIPv4(savedPeerIP);
    const expectedPairRole = savedPeerNS === ns2 ? 'primary' : savedPeerNS === ns1 ? 'secondary' : '';
    return isGlobalUnicastIPv4(serverIP) &&
        isGlobalUnicastIPv4(canonicalSavedPeerIP) &&
        savedPeerIP === canonicalSavedPeerIP &&
        canonicalSavedPeerIP !== serverIP &&
        saved.peer_ns === savedPeerNS &&
        pairRole === expectedPairRole &&
        draft.peer_ip.trim() === savedPeerIP &&
        cleanHostname(draft.peer_ns) === savedPeerNS;
}

export function dnsEngineIdentityReviewLocked(
    identityPlanCurrent: boolean,
    snapshot: DNSIdentityEngineEvidence | null,
): boolean {
    return !identityPlanCurrent || (
        snapshot?.active_engine === null && snapshot.topology === 'unconfigured'
    );
}
