import { useEffect, useState } from 'react';
import { useNavigate } from '../router';
import {
    Cpu, MemoryStick, HardDrive, Server, Globe, Database, Activity, Bell,
    Shield, ShieldOff, Users, Mail, Rocket, Check, ArrowRight,
    DownloadCloud, UserPlus, Plus, Lock, Layers, ScanSearch,
} from 'lucide-react';
import { api, type SystemStats } from '../lib/api';
import { useI18n } from '../i18n';
import { useAuth } from '../auth/AuthContext';
import type { TranslationKey } from '../i18n/en';
import { UsageBar, Card } from './ui';
import { PageHeader } from './PageHeader';
import { showToast } from './Toast';
import { FirewallNoSSHAcknowledgement, readFirewallSSHReason } from './FirewallSSHNotice';
import { apiErrorText, readApiError } from '../lib/apiError';
import { summarizeDashboardMailTruth } from '../lib/dashboardMailTruth';
import { publishComponentCensus } from '../lib/componentCensus';
import {
    decodeManagedMailProfiles,
    type ManagedMailProfile,
} from './ComponentOperation';

// Admin dashboard (Claude Design'dan uyarlandı): one glance answers "is my
// server healthy?" and "does anything need me?". Every number is real — the
// health strip polls system-stats, the rest reads the same endpoints the
// dedicated pages use, plus /api/v1/dashboard for the few aggregates.
// The setup journey renders from live state until all steps are done: a
// fresh server shows a guided path instead of empty widgets.
//
// Yönetici panosu: tek bakış "sunucum sağlıklı mı?" ve "bana ihtiyaç duyan
// bir şey var mı?" sorularını yanıtlar. Her sayı gerçek — sağlık şeridi
// system-stats'ı yoklar, kalanı özel sayfaların kullandığı uçları okur,
// birkaç toplam için /api/v1/dashboard eklenir. Kurulum yolculuğu tüm
// adımlar bitene dek canlı durumdan çizilir: taze sunucu boş widget yerine
// yol gösteren bir liste görür.

interface SvcLite {
    id: string;
    name: string;
    status: string;
    /** null = never observed on this host, which is not the same as absent. */
    /** null = bu makinede hiç gözlenmedi; bu, yok demek değildir. */
    is_installed: boolean | null;
    kind?: 'service' | 'runtime' | 'tool';
}
interface FwState {
    enabled: boolean;
    tcp_ports?: number[];
    udp_ports?: number[];
    persistence_state?: 'disabled' | 'missing' | 'ready' | 'stale' | 'invalid' | 'unverified';
    ssh_discovery_reason?: string;
}
interface HostMutationReadiness {
    ready: boolean;
    code?: 'HOST_MUTATION_BUSY' | 'HOST_MUTATION_UNAVAILABLE';
    reason?: 'panel_operation_active' | 'agent_mutation_active' | 'host_lock_busy' | 'package_manager_active' | 'state_unverified';
}
interface DomainLite {
    id: number;
    domain_name: string;
    ssl_enabled: boolean;
    project_type?: string;
    php_version?: string;
    created_at: string;
}
interface AuditLite {
    id: number;
    username: string;
    action: string;
    ip_address?: string;
    resource_type?: string;
    resource_id?: number;
    created_at: string;
}
interface AuditGroup extends AuditLite {
    count: number;
}
interface Extras {
    databases: number;
    mail_accounts: number;
    expiring_certs: { domain_name: string; days_left: number }[];
}

type Translate = ReturnType<typeof useI18n>['t'];

function groupAuditEntries(entries: AuditLite[]): AuditGroup[] {
    const groups = new Map<string, AuditGroup>();
    for (const entry of entries) {
        const key = [
            entry.username,
            entry.action,
            entry.ip_address ?? '',
            entry.resource_type ?? '',
            entry.resource_id ?? '',
        ].join('\u0000');
        const current = groups.get(key);
        if (current) {
            current.count += 1;
        } else {
            groups.set(key, { ...entry, count: 1 });
        }
    }
    return [...groups.values()];
}

function auditActionText(action: string, t: Translate): string {
    const event = action.split(' — ', 1)[0];
    if (event === 'auth.login' || event === 'auth.login.2fa') {
        return t('dashboard.audit.login');
    }
    const profileEvent = event.match(/^mail\.profile\.install(?:\.recovered)?(\.failed)?:([^\s]+)$/);
    if (profileEvent) {
        const profileKey = ({
            'core-mail': 'dashboard.audit.profile.coreMail',
            webmail: 'dashboard.audit.profile.webmail',
            'protected-mail': 'dashboard.audit.profile.protectedMail',
        } as const)[profileEvent[2] as 'core-mail' | 'webmail' | 'protected-mail'];
        const profile = profileKey ? t(profileKey) : profileEvent[2];
        return t(
            profileEvent[1] ? 'dashboard.audit.mailProfileFailed' : 'dashboard.audit.mailProfileInstalled',
            { profile },
        );
    }
    return action;
}

function decodeDashboardServices(value: unknown): {
    services: SvcLite[];
    profiles: ManagedMailProfile[];
    scannedAt: string | null;
    dnsIdentityReady: boolean;
} | null {
    if (!value || typeof value !== 'object') return null;
    const payload = value as Record<string, unknown>;
    if (!Array.isArray(payload.services)) return null;

    const serviceIDs = new Set<string>();
    const services: SvcLite[] = [];
    for (const candidate of payload.services) {
        if (!candidate || typeof candidate !== 'object') return null;
        const service = candidate as Record<string, unknown>;
        if (
            typeof service.id !== 'string'
            || service.id.trim() !== service.id
            || service.id === ''
            || serviceIDs.has(service.id)
            || typeof service.name !== 'string'
            || typeof service.status !== 'string'
            || (service.is_installed !== null && typeof service.is_installed !== 'boolean')
            || (
                service.kind !== undefined
                && service.kind !== 'service'
                && service.kind !== 'runtime'
                && service.kind !== 'tool'
            )
        ) return null;
        serviceIDs.add(service.id);
        services.push(service as unknown as SvcLite);
    }

    const profiles = decodeManagedMailProfiles(payload.profiles, serviceIDs);
    const scannedAt = payload.scanned_at;
    if (
        profiles === null
        || typeof payload.dns_identity_ready !== 'boolean'
        || (
            scannedAt !== null
            && (typeof scannedAt !== 'string' || !Number.isFinite(Date.parse(scannedAt)))
        )
    ) return null;
    return {
        services,
        profiles,
        scannedAt: scannedAt as string | null,
        dnsIdentityReady: payload.dns_identity_ready,
    };
}

function freshScanTimestamp(value: string | null, now = Date.now()): boolean {
    if (!value) return false;
    const scannedAt = Date.parse(value);
    if (!Number.isFinite(scannedAt) || scannedAt > now) return false;
    return now - scannedAt <= 5 * 60 * 1000;
}

function serviceStatusRunning(status: string): boolean {
    const normalized = status.trim().toLowerCase();
    return normalized === 'running' || normalized.startsWith('active');
}

function decodeHostMutationReadiness(value: unknown): HostMutationReadiness | null {
    if (!value || typeof value !== 'object') return null;
    const payload = value as Record<string, unknown>;
    if (typeof payload.ready !== 'boolean') return null;
    if (payload.ready) {
        return payload.code === undefined && payload.reason === undefined ? { ready: true } : null;
    }
    if (payload.code !== 'HOST_MUTATION_BUSY' && payload.code !== 'HOST_MUTATION_UNAVAILABLE') return null;
    if (
        payload.reason !== 'panel_operation_active'
        && payload.reason !== 'agent_mutation_active'
        && payload.reason !== 'host_lock_busy'
        && payload.reason !== 'package_manager_active'
        && payload.reason !== 'state_unverified'
    ) return null;
    return {
        ready: false,
        code: payload.code,
        reason: payload.reason,
    };
}

async function fetchHostMutationReadiness(): Promise<HostMutationReadiness> {
    try {
        const response = await fetch('/api/v1/host-mutation-readiness', {
            method: 'GET',
            cache: 'no-store',
        });
        if (!response.ok) {
            return { ready: false, code: 'HOST_MUTATION_UNAVAILABLE', reason: 'state_unverified' };
        }
        return decodeHostMutationReadiness(await response.json())
            ?? { ready: false, code: 'HOST_MUTATION_UNAVAILABLE', reason: 'state_unverified' };
    } catch {
        return { ready: false, code: 'HOST_MUTATION_UNAVAILABLE', reason: 'state_unverified' };
    }
}

export function Dashboard() {
    const { role } = useAuth();
    if (role === 'admin') return <AdminDashboard />;
    if (role === 'additional_user') return <AdditionalUserDashboard />;
    return <CustomerDashboard />;
}

function AdminDashboard() {
    const { t } = useI18n();
    const navigate = useNavigate();
    const [stats, setStats] = useState<SystemStats | null>(null);
    const [services, setServices] = useState<SvcLite[]>([]);
    const [mailProfiles, setMailProfiles] = useState<ManagedMailProfile[] | null>(null);
    const [fw, setFw] = useState<FwState | null>(null);
    const [domains, setDomains] = useState<DomainLite[]>([]);
    const [audit, setAudit] = useState<AuditLite[]>([]);
    const [usersCount, setUsersCount] = useState(0);
    const [dnsServer, setDnsServer] = useState('');
    const [dnsIdentityReady, setDNSIdentityReady] = useState(false);
    const [serviceScannedAt, setServiceScannedAt] = useState<string | null>(null);
    const [freshnessNow, setFreshnessNow] = useState(() => Date.now());
    // capabilities.mail_server is a BOOL in the API (dns_server is a string) —
    // treating it like a string silently marks the step done when it is false.
    // capabilities.mail_server API'de BOOL'dur (dns_server metindir) — metin
    // gibi ele almak false iken adımı sessizce 'tamamlandı' işaretler.
    // A real CA cert on the panel (self_signed === false) counts as "got an
    // SSL certificate" — the operator did obtain one, even if no site has one.
    // Panelde gerçek CA sertifikası (self_signed === false) "SSL aldın"
    // sayılır — operatör gerçekten bir sertifika aldı, hiçbir sitede olmasa da.
    const [panelSecured, setPanelSecured] = useState(false);
    const [extras, setExtras] = useState<Extras | null>(null);
    const [fwBusy, setFwBusy] = useState(false);
    const [firewallConfirmationOpen, setFirewallConfirmationOpen] = useState(false);
    const [noSSHAcknowledged, setNoSSHAcknowledged] = useState(false);
    const [hostMutationReadiness, setHostMutationReadiness] = useState<HostMutationReadiness | null>(null);
    const [componentScanBusy, setComponentScanBusy] = useState(false);

    useEffect(() => {
        const loadStats = () => api.getSystemStats().then(setStats).catch(() => {});
        loadStats();
        const timer = setInterval(loadStats, 5000);

        fetch('/api/v1/managed-services')
            .then((r) => (r.ok ? r.json() : null))
            .then((value: unknown) => {
                const snapshot = decodeDashboardServices(value);
                if (!snapshot) return;
                publishComponentCensus(snapshot.services);
                setServices(snapshot.services);
                setMailProfiles(snapshot.profiles);
                setServiceScannedAt(snapshot.scannedAt);
                setDNSIdentityReady(snapshot.dnsIdentityReady);
            })
            .catch(() => {});
        fetch('/api/v1/firewall').then((r) => (r.ok ? r.json() : null)).then(setFw).catch(() => {});
        fetch('/api/v1/domains').then((r) => (r.ok ? r.json() : [])).then((d) => setDomains(d || [])).catch(() => {});
        fetch('/api/v1/audit-logs?limit=28').then((r) => (r.ok ? r.json() : null)).then((d) => setAudit(d?.entries || [])).catch(() => {});
        fetch('/api/v1/users').then((r) => (r.ok ? r.json() : null)).then((d) => setUsersCount((d?.users || []).length)).catch(() => {});
        fetch('/api/v1/hosting/capabilities')
            .then((r) => (r.ok ? r.json() : null))
            .then((c) => { setDnsServer(c?.dns_server ?? ''); })
            .catch(() => {});
        fetch('/api/v1/dashboard').then((r) => (r.ok ? r.json() : null)).then(setExtras).catch(() => {});
        fetch('/api/v1/panel/certificate').then((r) => (r.ok ? r.json() : null)).then((c) => setPanelSecured(c ? c.self_signed === false : false)).catch(() => {});

        return () => clearInterval(timer);
    }, []);

    useEffect(() => {
        let mounted = true;
        const refresh = async () => {
            const readiness = await fetchHostMutationReadiness();
            if (mounted) setHostMutationReadiness(readiness);
        };
        void refresh();
        const timer = window.setInterval(() => void refresh(), 5000);
        return () => {
            mounted = false;
            window.clearInterval(timer);
        };
    }, []);

    useEffect(() => {
        const timer = window.setInterval(() => setFreshnessNow(Date.now()), 30_000);
        return () => window.clearInterval(timer);
    }, []);

    const serviceScanFresh = freshScanTimestamp(serviceScannedAt, freshnessNow);

    // The census this panel has actually taken. `is_installed: null` is a row
    // nobody has looked at on this host, so it belongs to NEITHER side of a
    // count: it is not known installed, and it is not known absent. A host
    // where no row has been observed therefore has no number to show — "0
    // installed" there would report an inventory nobody took (R-040).
    // Bu panelin gerçekten yaptığı sayım. `is_installed: null`, bu makinede
    // kimsenin bakmadığı bir satırdır; sayımın HİÇBİR yakasına ait değildir.
    // Hiçbir satırı gözlenmemiş bir makinenin gösterilecek sayısı yoktur.
    const uncheckedServices = services.filter((s) => s.is_installed === null);
    const hostNeverChecked = services.length > 0 && uncheckedServices.length === services.length;
    const componentCensusComplete = uncheckedServices.length === 0;

    // The same check the Components page offers, run from here instead of
    // pointing at another page — the firewall lesson (Jul 17): an action this
    // central acts in place. The response goes through the same fail-closed
    // decoder as the initial load, so a payload this panel cannot read never
    // becomes a number on this screen.
    // Bileşenler sayfasının sunduğu kontrolün aynısı, başka sayfayı işaret
    // etmek yerine burada koşar. Yanıt, ilk yüklemeyle aynı fail-closed
    // çözücüden geçer; okunamayan bir yük asla bu ekranda sayıya dönüşmez.
    const scanComponents = async () => {
        if (componentScanBusy) return;
        setComponentScanBusy(true);
        try {
            const response = await fetch('/api/v1/managed-services/scan', {
                method: 'POST',
                cache: 'no-store',
            });
            if (!response.ok) {
                showToast('error', apiErrorText(await readApiError(response), t, 'services.scanFailed'));
                return;
            }
            const snapshot = decodeDashboardServices(await response.json());
            if (!snapshot) {
                showToast('error', t('services.scanFailed'));
                return;
            }
            // The check the operator ran here answers for the whole host,
            // so the sidebar badge takes it from here — no reload, no second
            // request, no poll.
            // Operatörün burada çalıştırdığı kontrol bütün makine adına cevap
            // verir; kenar çubuğu rozeti onu buradan alır.
            publishComponentCensus(snapshot.services);
            setServices(snapshot.services);
            setMailProfiles(snapshot.profiles);
            setServiceScannedAt(snapshot.scannedAt);
            setDNSIdentityReady(snapshot.dnsIdentityReady);
            // Freshness is measured against a clock that ticks every 30s. A
            // scan timestamp newer than that clock reads as "in the future"
            // and would show Unknown for half a minute after a successful
            // check, so move the clock with the answer.
            // Tazelik 30 saniyede bir ilerleyen bir saate göre ölçülür; taze
            // tarama damgası o saatten yeni olduğu için saati birlikte ilerlet.
            setFreshnessNow(Date.now());
        } catch {
            showToast('error', t('services.scanFailed'));
        } finally {
            setComponentScanBusy(false);
        }
    };

    const serviceRunning = (id: string) => {
        if (!serviceScanFresh) return false;
        const svc = services.find((s) => s.id === id);
        return Boolean(
            svc?.is_installed
            && svc.kind === 'service'
            && serviceStatusRunning(svc.status),
        );
    };

    // Known installed, explicitly: `s.is_installed` truthy already excluded
    // the unchecked rows, and saying so keeps the next reader from "fixing"
    // it into a null-tolerant test.
    // Bilinen kurulular: `null` satırlar bu kümenin dışındadır.
    const installed = services.filter((s) => s.is_installed === true);
    // A `tool` can never be "stopped" — it has no daemon of ours, so counting
    // phpMyAdmin as a dead service was a false alarm the operator could not act
    // on. Status "installed" is the same truth for a unit-less runtime (node:
    // executed only by per-site apps). php-fpm still counts: it has real units
    // and a dead one breaks every PHP site, so it must reach this list
    // (D-010/B3b).
    // `tool` asla "durmuş" olamaz — bize ait daemon'ı yok; phpMyAdmin'i ölü
    // servis saymak operatörün eyleme dökemeyeceği yanlış alarmdı. "installed"
    // durumu, unit'siz runtime için aynı gerçektir (node: onu yalnız site
    // başına uygulamalar çalıştırır). php-fpm sayılmaya devam eder: gerçek
    // unit'leri var ve ölüsü her PHP sitesini kırar, bu listeye ulaşmalıdır
    // (D-010/B3b).
    // This card is specifically systemd service truth. Tools (Roundcube) and
    // unit-less runtimes are installed components, never fabricated as
    // "running services" merely to make the ratio green.
    const systemServices = serviceScanFresh
        ? installed.filter((s) => s.kind === 'service')
        : [];
    // What the sidebar badge counts and this ratio does not: installed
    // components that have no systemd unit of their own — tools, and runtimes
    // executed only by per-site apps. They are why the two numbers differ.
    // Kenar çubuğu rozetinin sayıp bu oranın saymadığı şey: kendi systemd
    // unit'i olmayan kurulu bileşenler — araçlar ve yalnız site başına
    // uygulamaların çalıştırdığı runtime'lar. İki sayının farkı buradan gelir.
    const unitlessInstalled = serviceScanFresh
        ? installed.filter((s) => s.kind !== 'service')
        : [];
    const running = systemServices.filter((s) => serviceStatusRunning(s.status));
    const stoppedSvcs = systemServices.filter((s) => !serviceStatusRunning(s.status));

    // Turn the firewall on right where the operator reads about it. Field
    // finding (Jul 17): the journey said "turn on the firewall" but its button
    // navigated away, and the operator never found the switch — an action this
    // important acts in place, it does not give directions.
    // Güvenlik duvarını, operatörün onu okuduğu yerde aç. Saha bulgusu
    // (17 Tem): yolculuk "firewall'u aç" diyordu ama düğmesi başka sayfaya
    // götürüyordu ve operatör anahtarı hiç bulamadı — bu önemde bir eylem
    // yerinde yapılır, adres tarif etmez.
    const refreshHostMutationReadiness = async () => {
        const readiness = await fetchHostMutationReadiness();
        setHostMutationReadiness(readiness);
        return readiness.ready;
    };

    const firewallSSHReason = readFirewallSSHReason(fw?.ssh_discovery_reason);
    const firewallNeedsNoSSHConsent = firewallSSHReason === 'no_ssh_service';

    const requestTurnOnFirewall = () => {
        setNoSSHAcknowledged(false);
        setFirewallConfirmationOpen(true);
        void refreshHostMutationReadiness();
    };

    const turnOnFirewall = async () => {
        if (hostMutationReadiness?.ready !== true || fwBusy) return;
        if (firewallNeedsNoSSHConsent && !noSSHAcknowledged) return;
        setFwBusy(true);
        try {
            const r = await fetch('/api/v1/firewall', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                // Its own field, and only on a request that turns the firewall
                // on. / Kendi alani, ve yalniz guvenlik duvarini acan bir
                // istekte.
                body: JSON.stringify(
                    noSSHAcknowledged
                        ? { enabled: true, no_ssh_acknowledged: true }
                        : { enabled: true },
                ),
            });
            if (!r.ok) {
                // The server may have changed since the status read; re-read it
                // so the dialog can offer the same way forward.
                fetch('/api/v1/firewall')
                    .then((response) => (response.ok ? response.json() : null))
                    .then(setFw)
                    .catch(() => {});
                showToast('error', apiErrorText(await readApiError(r), t, 'firewall.changeFailed'));
                return;
            }
            const value: unknown = await r.json();
            if (!value || typeof value !== 'object' || typeof (value as Record<string, unknown>).enabled !== 'boolean') {
                showToast('error', apiErrorText({ message: '' }, t, 'firewall.changeFailed'));
                return;
            }
            setFw(value as FwState);
            setFirewallConfirmationOpen(false);
            showToast('success', t('firewall.onDone'));
        } catch {
            showToast('error', apiErrorText({ message: '' }, t, 'firewall.changeFailed'));
        } finally {
            setFwBusy(false);
            void refreshHostMutationReadiness();
        }
    };

    // Attention items: real, actionable problems only. / İlgi kalemleri:
    // yalnız gerçek ve eyleme dönüştürülebilir sorunlar.
    const attention: { key: string; icon: typeof Cpu; text: string; action: string; to: string; danger?: boolean; onAct?: () => void }[] = [];
    for (const c of extras?.expiring_certs || []) {
        attention.push({
            key: `cert-${c.domain_name}`,
            icon: Lock,
            text: c.days_left <= 0
                ? t('dashboard.certExpired', { domain: c.domain_name })
                : t('dashboard.certExpires', { domain: c.domain_name, days: c.days_left }),
            action: t('dashboard.renew'),
            to: `/domains/${encodeURIComponent(c.domain_name)}`,
            danger: c.days_left <= 7,
        });
    }
    for (const s of stoppedSvcs) {
        attention.push({
            key: `svc-${s.id}`,
            icon: Activity,
            text: t('dashboard.svcStoppedItem', { name: s.name }),
            action: t('dashboard.goServices'),
            to: '/services',
        });
    }
    if (fw && !fw.enabled) {
        attention.push({
            key: 'fw-off',
            icon: ShieldOff,
            text: t('dashboard.fwOffItem'),
            action: t('firewall.turnOn'),
            to: '/services',
            danger: true,
            onAct: requestTurnOnFirewall,
        });
    } else if (fw?.enabled && fw.persistence_state !== 'ready') {
        attention.push({
            key: 'fw-persistence',
            icon: Shield,
            text: t('dashboard.fwPersistenceItem'),
            action: t('dashboard.saveFirewall'),
            to: '/services',
        });
    }
    // Security posture suggestions — surfaced only when they actually apply,
    // so they guide rather than nag: antivirus once there is content to scan,
    // spam filtering once mail is running.
    // Güvenlik duruşu önerileri — yalnız gerçekten geçerliyken çıkar, böylece
    // dırdır değil yol gösterir: taranacak içerik varken antivirüs, posta
    // çalışırken spam filtresi.
    const hasClamAV = serviceRunning('clamav');
    const verifiedMailProfiles = serviceScanFresh
        ? (mailProfiles?.filter((profile) => (
            profile.status === 'complete'
            && profile.verified
            && !profile.warning
        )) ?? [])
        : [];
    const mailProfileVerified = verifiedMailProfiles.length > 0;
    // This suppresses only the "install a spam filter" suggestion. It does
    // not claim filter wiring or a verified protected profile; that separate
    // truth stays visible in the mail-stack summary.
    const hasSpam = serviceRunning('spamassassin') || serviceRunning('rspamd');
    // "Content to scan" means a hosted site — a DNS-only domain serves
    // records, not files, so it must not trigger the antivirus nag.
    // "Taranacak içerik" barındırılan site demektir — yalnız-DNS domain dosya
    // değil kayıt sunar; antivirüs dırdırını tetiklememeli.
    const hostsContent = domains.some((d) => d.project_type !== 'dnsonly');
    if (serviceScanFresh && hostsContent && !hasClamAV) {
        attention.push({
            key: 'no-av',
            icon: Shield,
            text: t('dashboard.avItem'),
            action: t('dashboard.installService'),
            to: '/services',
        });
    }
    if (mailProfileVerified && !hasSpam) {
        attention.push({
            key: 'no-spam',
            icon: Mail,
            text: t('dashboard.spamItem'),
            action: t('dashboard.installService'),
            to: '/services',
        });
    }

    // Setup journey — live completion; the card disappears when all done.
    // Kurulum yolculuğu — canlı tamamlanma; hepsi bitince kart kaybolur.
    const steps: { key: TranslationKey; hint?: TranslationKey; done: boolean; to: string; cta?: TranslationKey; onAct?: () => void }[] = [
        // Every CTA says what it actually does — "Go to services" on a button
        // that opens the Domains page was a lie the operator caught (Jul 17).
        // Her düğme gerçekten yaptığını söyler — Domains sayfasını açan
        // düğmede "Go to services" yazması operatörün yakaladığı bir yalandı.
        { key: 'dashboard.step.panel', done: true, to: '/' },
        {
            key: 'dashboard.step.serviceScan',
            hint: 'dashboard.step.serviceScanHint',
            // Fresh is not the same as complete. Every step below this one
            // reads its answer off the component census, so a census with
            // rows nobody has looked at leaves this step open instead of
            // letting the journey suggest an install against an unknown.
            // Taze olmak, tamamlanmış olmak değildir. Aşağıdaki her adım
            // yanıtını bileşen sayımından okur; bakılmamış satır varsa bu
            // adım açık kalır, yolculuk bilinmeyene karşı kurulum önermez.
            done: serviceScanFresh && componentCensusComplete,
            to: '/services',
            cta: 'dashboard.rescanComponents',
        },
        // "Done" means WORKING, not merely present. A DNS server that is
        // installed but not running serves no zone, so ticking that step was a
        // lie — Hostinger's Arch image ships a disabled named.service and the
        // journey happily said "DNS installed: Done" while the Components page
        // showed 0/0 (Jul 16). The same honesty applies to mail: an installed
        // Postfix that is dead delivers nothing.
        // "Tamamlandı" ÇALIŞIYOR demektir, yalnız var demek değil. Kurulu ama
        // koşmayan bir DNS sunucusu hiçbir zone sunmaz; o adımı işaretlemek
        // yalandı — Hostinger'ın Arch imajı devre dışı bir named.service ile
        // geliyor ve yolculuk keyifle "DNS kuruldu: Tamam" diyordu, Bileşenler
        // sayfası 0/0 gösterirken (16 Tem). Aynı dürüstlük posta için de:
        // kurulu ama ölü bir Postfix hiçbir şey teslim etmez.
        { key: 'dashboard.step.dns', hint: 'dashboard.step.dnsHint', done: serviceScanFresh && dnsServer !== '' && serviceRunning(dnsServer), to: '/services' },
        { key: 'dashboard.step.dnsIdentity', hint: 'dashboard.step.dnsIdentityHint', done: dnsIdentityReady, to: '/settings?section=dns', cta: 'dashboard.configureDNSIdentity' },
        { key: 'dashboard.step.domain', done: domains.length > 0, to: '/domains', cta: 'dashboard.addDomain' },
        { key: 'dashboard.step.ssl', hint: 'dashboard.step.sslHint', done: panelSecured || domains.some((d) => d.ssl_enabled), to: '/settings', cta: 'dashboard.goSettings' },
        // The firewall step acts in place: the engine ships with install.sh,
        // so "turn on" is one honest click, not a scavenger hunt.
        // Firewall adımı yerinde eyler: motor install.sh ile gelir, "aç" tek
        // dürüst tıktır, define avı değil.
        {
            key: 'dashboard.step.firewall',
            hint: 'dashboard.step.firewallHint',
            done: fw?.enabled === true && fw.persistence_state === 'ready',
            to: '/services',
            cta: fw?.enabled ? 'dashboard.saveFirewall' : 'firewall.turnOn',
            onAct: fw?.enabled ? undefined : requestTurnOnFirewall,
        },
        { key: 'dashboard.step.mail', done: mailProfileVerified, to: '/services' },
    ];
    const doneCount = steps.filter((s) => s.done).length;
    const journeyOpen = doneCount < steps.length;
    const nextIdx = steps.findIndex((s) => !s.done);
    const hasContent = installed.length > 0 || domains.length > 0;

    const recentDomains = [...domains]
        .sort((a, b) => (a.created_at < b.created_at ? 1 : -1))
        .slice(0, 4);
    const recentActivity = groupAuditEntries(audit).slice(0, 7);


    return (
        <div className="p-6 md:p-8">
            <PageHeader
                title={t('dashboard.title')}
                subtitle={stats ? `${stats.hostname} · ${t('dashboard.uptimeFor', { time: fmtUptime(stats.uptime_seconds, t) })}` : undefined}
            />

            {/* Health strip: only living numbers. Firewall state earned no
                card (operator call, Jul 17): OFF is an alert with a button,
                ON is a config fact — the Services page owns the detail.
                Sağlık şeridi: yalnız yaşayan sayılar. Firewall durumu kart hak
                etmedi (operatör kararı, 17 Tem): KAPALI düğmesiyle bir uyarıdır,
                AÇIK bir yapılandırma gerçeğidir — ayrıntının sahibi Services. */}
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
                <GaugeCard
                    icon={Cpu}
                    label={t('dashboard.cpuUsage')}
                    percent={stats?.cpu_percent ?? 0}
                    value={stats ? t('dashboard.percentValue', { n: Math.round(stats.cpu_percent) }) : '—'}
                    hint={stats ? `${t('dashboard.cores', { n: stats.cpu_cores })} · ${t('dashboard.loadValue', { n: stats.load_avg[0]?.toFixed(2) ?? '—' })}` : ''}
                />
                <GaugeCard
                    icon={MemoryStick}
                    label={t('dashboard.memoryUsage')}
                    percent={pct(stats?.mem_used_bytes, stats?.mem_total_bytes)}
                    value={stats ? t('dashboard.percentValue', { n: Math.round(pct(stats.mem_used_bytes, stats.mem_total_bytes)) }) : '—'}
                    hint={stats ? `${fmtBytes(stats.mem_used_bytes)} / ${fmtBytes(stats.mem_total_bytes)}` : ''}
                />
                <GaugeCard
                    icon={HardDrive}
                    label={t('dashboard.diskUsage')}
                    percent={pct(stats?.disk_used_bytes, stats?.disk_total_bytes)}
                    value={stats ? t('dashboard.percentValue', { n: Math.round(pct(stats.disk_used_bytes, stats.disk_total_bytes)) }) : '—'}
                    hint={stats ? `${fmtBytes(stats.disk_used_bytes)} / ${fmtBytes(stats.disk_total_bytes)}` : ''}
                />
                {/* A host nobody has looked at keeps its card in the strip but
                    shows no number: "0 / 0" here was the same false claim the
                    Components page used to make one screen over. It says what
                    is true — nothing has been checked — and carries the check
                    itself, because pointing at another page is how the
                    firewall switch went unfound (Jul 17). Neutral, not
                    warning-coloured: a server that has simply not been looked
                    at yet is the normal first state, not a fault.
                    Kimsenin bakmadığı bir makine şeritteki yerini korur ama
                    sayı göstermez: buradaki "0 / 0", Bileşenler sayfasının bir
                    ekran ötede yaptığı yanlış iddianın aynısıydı. Doğru olanı
                    söyler ve kontrolün kendisini taşır. Nötr, uyarı renginde
                    değil — henüz bakılmamış makine olağan ilk durumdur. */}
                {hostNeverChecked ? (
                    <section role="status" className="rounded-xl border border-border bg-surface p-5 text-left shadow-card">
                        <div className="flex items-center justify-between">
                            <span className="text-sm font-medium text-fg-muted">{t('dashboard.systemServices')}</span>
                            <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary/10 text-primary">
                                <Activity className="h-4 w-4" />
                            </span>
                        </div>
                        <p className="mt-2 text-lg font-semibold text-fg">{t('services.notChecked')}</p>
                        <p className="mt-1 text-xs text-fg-muted">{t('dashboard.componentsNotCheckedHint')}</p>
                        <button
                            type="button"
                            onClick={scanComponents}
                            disabled={componentScanBusy}
                            className="mt-3 inline-flex items-center gap-1.5 rounded-lg bg-primary px-3 py-1.5 text-xs font-semibold text-primary-fg transition-colors hover:bg-primary/90 disabled:opacity-50"
                        >
                            <ScanSearch className="h-3.5 w-3.5" />
                            {componentScanBusy ? t('services.scanning') : t('services.scanNow')}
                        </button>
                    </section>
                ) : (
                <button
                    onClick={() => navigate('/services')}
                    className="rounded-xl border border-border bg-surface p-5 text-left shadow-card transition-colors hover:bg-surface-2/60"
                >
                    <div className="flex items-center justify-between">
                        <span className="text-sm font-medium text-fg-muted">{t('dashboard.systemServices')}</span>
                        <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary/10 text-primary">
                            <Activity className="h-4 w-4" />
                        </span>
                    </div>
                    <p className="mt-2 text-3xl font-bold tracking-tight text-fg tabular-nums">
                        {!serviceScanFresh
                            ? t('dashboard.statusUnknown')
                            : `${running.length} / ${systemServices.length}`}
                    </p>
                    {!serviceScanFresh ? (
                        <p className="mt-2 text-xs text-fg-subtle">{t('dashboard.systemServicesScanStale')}</p>
                    ) : stoppedSvcs.length > 0 ? (
                        <p className="mt-2 text-xs font-medium text-warning">{t('dashboard.svcStopped', { n: stoppedSvcs.length })}</p>
                    ) : (
                        <p className="mt-2 text-xs text-fg-subtle">
                            {/* "0 / 0 · No services yet" beside a sidebar
                                reading "Components 1" is two true statements
                                that look like a contradiction: this card counts
                                systemd services, and the one installed
                                component (nftables) is a tool with no unit to
                                run. Both numbers stay; the sentence between
                                them is what was missing.
                                Kenar çubuğu "Bileşenler 1" derken buradaki
                                "0 / 0 · Henüz servis yok", çelişki gibi duran
                                iki doğrudur: bu kart systemd servislerini
                                sayar, kurulu tek bileşen (nftables) ise
                                çalıştıracak unit'i olmayan bir araçtır. İki
                                sayı da kalır; eksik olan aradaki cümleydi. */}
                            {systemServices.length > 0
                                ? t('dashboard.svcRunningHint')
                                : unitlessInstalled.length === 0
                                    ? t('dashboard.svcNone')
                                    : unitlessInstalled.length === 1
                                        ? t('dashboard.svcNoneUnitlessOne')
                                        : t('dashboard.svcNoneUnitless', { n: unitlessInstalled.length })}
                        </p>
                    )}
                    {/* Partly checked is not unchecked. The ratio above counts
                        only rows this panel has actually observed, and the
                        rows nobody has looked at are named here rather than
                        being folded into either side of it. fg-muted, not
                        fg-subtle: this is information the operator acts on.
                        Kısmen bakılmış, bakılmamış değildir. Yukarıdaki oran
                        yalnız gerçekten gözlenmiş satırları sayar; bakılmamış
                        satırlar oranın bir yakasına katılmak yerine burada
                        adıyla anılır. */}
                    {uncheckedServices.length > 0 && (
                        <p className="mt-1 text-xs text-fg-muted">
                            {uncheckedServices.length === 1
                                ? t('dashboard.componentsUncheckedOne')
                                : t('dashboard.componentsUnchecked', { n: uncheckedServices.length })}
                        </p>
                    )}
                </button>
                )}
            </div>

            {mailProfiles && (
                <MailStackSummary
                    profiles={mailProfiles}
                    scanFresh={serviceScanFresh}
                    hostNeverChecked={hostNeverChecked}
                    checking={componentScanBusy}
                    onCheck={scanComponents}
                    onOpen={() => navigate('/services#mail-stacks')}
                />
            )}

            {/* Needs attention BEFORE the journey: an active problem outranks
                guidance. Operator feedback (Jul 17): the alert list lived below
                the fold while the top of the page stayed calm.
                Needs attention yolculuktan ÖNCE: aktif sorun, rehberlikten
                önce gelir. Operatör geri bildirimi (17 Tem): uyarı listesi
                sayfanın altında kalırken üst taraf sakin görünüyordu. */}
            {attention.length > 0 && (
                <section className="mt-6">
                    <SectionTitle
                        icon={Bell}
                        tint="bg-amber-500/10 text-amber-600 dark:text-amber-400"
                        title={t('dashboard.attention')}
                        right={
                            attention.length > 0 ? (
                                <span className="rounded-full bg-warning/15 px-2.5 py-1 text-xs font-semibold text-warning">
                                    {attention.length === 1
                                        ? t('dashboard.warnCountOne')
                                        : t('dashboard.warnCount', { n: attention.length })}
                                </span>
                            ) : undefined
                        }
                    />
                    <div className="overflow-hidden rounded-xl border border-border bg-surface shadow-card">
                        <ul>
                            {attention.map((a) => (
                                    <li key={a.key} className="flex flex-wrap items-center gap-3 border-b border-border px-4 py-3 last:border-0">
                                        <a.icon className={`h-4 w-4 shrink-0 ${a.danger ? 'text-danger' : 'text-warning'}`} />
                                        <span className="min-w-0 flex-1 text-sm text-fg">{a.text}</span>
                                        {/* An item with a direct action gets a REAL button — a quiet
                                            text link is how the operator missed the firewall switch.
                                            Doğrudan eylemi olan kalem GERÇEK düğme alır — operatörün
                                            firewall anahtarını kaçırmasının sebebi sessiz metin bağıydı. */}
                                        {a.onAct ? (
                                            <button
                                                onClick={a.onAct}
                                                disabled={fwBusy}
                                                className="rounded-lg bg-primary px-3 py-1.5 text-xs font-semibold text-primary-fg transition-colors hover:bg-primary/90 disabled:opacity-50"
                                            >
                                                {a.action}
                                            </button>
                                        ) : (
                                            <button
                                                onClick={() => navigate(a.to)}
                                                className="inline-flex items-center gap-1 text-sm font-medium text-primary hover:underline"
                                            >
                                                {a.action} <ArrowRight className="h-3.5 w-3.5" />
                                            </button>
                                        )}
                                    </li>
                            ))}
                        </ul>
                    </div>
                </section>
            )}

            {/* Setup journey / Kurulum yolculuğu */}
            {journeyOpen && (
                <section className="mt-6">
                    <SectionTitle
                        icon={Rocket}
                        tint="bg-blue-500/10 text-blue-600 dark:text-blue-400"
                        title={t('dashboard.journey')}
                        right={
                            <span className="rounded-full bg-surface-2 px-2.5 py-1 text-xs font-medium text-fg-muted">
                                {t('dashboard.journeyProgress', { done: doneCount, total: steps.length })}
                            </span>
                        }
                    />
                    <div className="overflow-hidden rounded-xl border border-border bg-surface shadow-card">
                        <ul>
                            {steps.map((s, i) => (
                                <li
                                    key={s.key}
                                    className={`flex flex-wrap items-center gap-3 border-b border-border px-4 py-3.5 last:border-0 ${
                                        i === nextIdx ? 'bg-primary/5' : ''
                                    }`}
                                >
                                    {s.done ? (
                                        <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-success/15 text-success">
                                            <Check className="h-4 w-4" />
                                        </span>
                                    ) : (
                                        <span
                                            className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-full text-sm font-semibold ${
                                                i === nextIdx ? 'bg-primary text-primary-fg' : 'bg-surface-2 text-fg-subtle'
                                            }`}
                                        >
                                            {i + 1}
                                        </span>
                                    )}
                                    {/* `flex-1` alone gives this block a base
                                        width of zero, so the row never wrapped:
                                        the text was squeezed to whatever the
                                        button left over instead. On the one
                                        step that carries a hint AND a button
                                        that collapsed at 390px into a ~60px
                                        ribbon of one or two words per line. A
                                        real basis makes the row wrap the way it
                                        was always meant to — the button drops
                                        to its own line and the text keeps a
                                        readable measure — and it is the shared
                                        row that changes, so every step lays out
                                        by the same rule.
                                        Tek başına `flex-1` bu bloğa sıfır
                                        genişlik tabanı verir; satır bu yüzden
                                        hiç alt satıra taşmaz, metin düğmeden
                                        artana sıkışırdı. Hem ipucu hem düğme
                                        taşıyan tek adımda bu, 390px'te satır
                                        başına bir-iki sözcüklük ~60px'lik bir
                                        şeride dönüşüyordu. Gerçek bir taban,
                                        satırı en baştan tasarlandığı gibi
                                        sardırır ve değişen paylaşılan satırdır;
                                        her adım aynı kurala göre dizilir.

                                        11rem is chosen, not rounded to: at
                                        390px it leaves a wide CTA no room and
                                        the button wraps onto its own line,
                                        while the short statuses on the other
                                        steps — Done, Next, and the longer
                                        Turkish "Tamamlandı" — still fit beside
                                        the title and do not cost six rows an
                                        extra line each.
                                        11rem seçilmiştir, yuvarlanmış değil:
                                        390px'te geniş bir CTA'ya yer bırakmaz
                                        ve düğme kendi satırına geçer; öteki
                                        adımların kısa durumları — Done, Next
                                        ve daha uzun olan "Tamamlandı" — yine
                                        başlığın yanında kalır ve altı satıra
                                        birer satır fazladan mal olmaz. */}
                                    <div className="min-w-0 grow basis-44">
                                        <div className={`text-base font-medium ${s.done || i === nextIdx ? 'text-fg' : 'text-fg-muted'}`}>
                                            {t(s.key)}
                                        </div>
                                        {i === nextIdx && s.hint && (
                                            <div className="mt-0.5 text-xs text-fg-subtle">{t(s.hint)}</div>
                                        )}
                                    </div>
                                    {s.done ? (
                                        <span className="ml-auto shrink-0 text-sm text-fg-subtle">{t('dashboard.stepDone')}</span>
                                    ) : i === nextIdx ? (
                                        <button
                                            onClick={() => (s.onAct ? s.onAct() : navigate(s.to))}
                                            disabled={s.onAct ? fwBusy : false}
                                            className="ml-auto shrink-0 rounded-lg bg-primary px-3 py-1.5 text-xs font-semibold text-primary-fg transition-colors hover:bg-primary/90 disabled:opacity-50"
                                        >
                                            {t(s.cta ?? 'dashboard.goServices')}
                                        </button>
                                    ) : (
                                        <span className="ml-auto shrink-0 text-sm text-fg-subtle">{i === nextIdx + 1 ? t('dashboard.stepNext') : ''}</span>
                                    )}
                                </li>
                            ))}
                        </ul>
                    </div>
                </section>
            )}

            {/* Hosting + activity / Barındırma + etkinlik */}
            {hasContent && (
                <div className="mt-6 grid grid-cols-1 gap-6 xl:grid-cols-2">
                    <section>
                        <SectionTitle icon={Globe} tint="bg-teal-500/10 text-teal-600 dark:text-teal-400" title={t('dashboard.hosting')} />
                        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
                            <CountCard icon={Globe} n={domains.length} label={t('dashboard.domains')} to="/domains" />
                            <CountCard icon={Database} n={extras?.databases ?? 0} label={t('dashboard.databases')} to="/databases" />
                            <CountCard icon={Users} n={usersCount} label={t('nav.users')} to="/users" />
                            <CountCard icon={Mail} n={extras?.mail_accounts ?? 0} label={t('dashboard.mailAccounts')} to="/domains" />
                        </div>
                        {recentDomains.length > 0 && (
                            <>
                                <h3 className="mb-2 mt-4 text-sm font-semibold text-fg-muted">{t('dashboard.recentDomains')}</h3>
                                <ul className="overflow-hidden rounded-xl border border-border bg-surface shadow-card">
                                    {recentDomains.map((d) => (
                                        <li key={d.id} className="border-b border-border last:border-0">
                                            <button
                                                onClick={() => navigate(`/domains/${encodeURIComponent(d.domain_name)}`)}
                                                className="flex w-full flex-wrap items-center gap-2.5 px-4 py-3 text-left transition-colors hover:bg-surface-2/60"
                                            >
                                                {d.ssl_enabled ? (
                                                    <Lock className="h-4 w-4 shrink-0 text-success" />
                                                ) : (
                                                    <Globe className="h-4 w-4 shrink-0 text-fg-subtle" />
                                                )}
                                                <span className="text-base font-medium text-fg">{d.domain_name}</span>
                                                <span className="rounded-md bg-surface-2 px-1.5 py-0.5 text-xs font-medium text-fg-muted">
                                                    {(d.project_type || 'php') === 'php' && d.php_version
                                                        ? `PHP ${d.php_version}`
                                                        : d.project_type || 'php'}
                                                </span>
                                                {/* SSL warns only where a site exists to secure — a
                                                    DNS-only domain has nothing to certify.
                                                    SSL uyarısı ancak güvence altına alınacak site varsa —
                                                    yalnız-DNS domain'in sertifikalanacak şeyi yok. */}
                                                {d.project_type !== 'dnsonly' && !d.ssl_enabled && (
                                                    <span className="text-xs font-medium text-warning">{t('dashboard.noSsl')}</span>
                                                )}
                                                <span className="ml-auto text-xs text-fg-subtle">{fmtRelative(d.created_at, t)}</span>
                                            </button>
                                        </li>
                                    ))}
                                </ul>
                            </>
                        )}
                    </section>

                    <section>
                        <SectionTitle icon={Activity} tint="bg-violet-500/10 text-violet-600 dark:text-violet-400" title={t('dashboard.activity')} />
                        {recentActivity.length === 0 ? (
                            <Card><p className="p-4 text-sm text-fg-subtle">—</p></Card>
                        ) : (
                            <ul className="overflow-hidden rounded-xl border border-border bg-surface shadow-card">
                                {recentActivity.map((e) => (
                                    <li key={e.id} className="flex flex-wrap items-center gap-2.5 border-b border-border px-4 py-2.5 last:border-0">
                                        <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-surface-2 text-xs font-semibold uppercase text-fg-muted">
                                            {e.username.slice(0, 1) || '?'}
                                        </span>
                                        <span className="min-w-0 flex-1 text-sm text-fg">
                                            <span className="font-semibold">{e.username}</span>{' '}
                                            <span className="text-fg-muted">{auditActionText(e.action, t)}</span>
                                            {e.count > 1 && (
                                                <span className="ml-2 rounded-full bg-surface-2 px-2 py-0.5 text-xs font-semibold text-fg-muted">
                                                    {t('dashboard.audit.repeated', { n: e.count })}
                                                </span>
                                            )}
                                        </span>
                                        {e.ip_address && <span className="font-mono text-xs text-fg-subtle">{e.ip_address}</span>}
                                        <span className="text-xs text-fg-subtle">{fmtRelative(e.created_at, t)}</span>
                                    </li>
                                ))}
                            </ul>
                        )}
                    </section>
                </div>
            )}

            {/* Quick actions / Hızlı eylemler */}
            <section className="mt-6">
                <SectionTitle icon={Plus} tint="bg-primary/10 text-primary" title={t('dashboard.quickActions')} />
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
                    <QuickAction icon={Globe} labelKey="dashboard.addDomain" to="/domains" />
                    <QuickAction icon={Server} labelKey="dashboard.installService" to="/services" />
                    <QuickAction icon={UserPlus} labelKey="dashboard.addUser" to="/users" />
                    <QuickAction icon={DownloadCloud} labelKey="nav.import" to="/import" />
                </div>
            </section>

            {/* Server identity: the facts an operator pastes into tickets and
                DNS records — usage numbers stay in the gauges above.
                Sunucu kimliği: operatörün destek kaydına ve DNS kayıtlarına
                yapıştırdığı bilgiler — kullanım sayıları üstteki göstergelerde. */}
            <div className="mt-6">
                <Card title={t('dashboard.serverInfo')} icon={Server}>
                    <dl className="divide-y divide-border text-sm">
                        <InfoRow label={t('dashboard.hostname')} value={stats?.hostname || '—'} />
                        <InfoRow label={t('dashboard.ipv4')} value={stats?.ipv4 || '—'} />
                        <InfoRow label={t('dashboard.os')} value={stats?.os || '—'} />
                        <InfoRow label={t('dashboard.kernel')} value={stats?.kernel || '—'} />
                        <InfoRow label={t('dashboard.arch')} value={stats?.arch || '—'} />
                        <InfoRow label={t('dashboard.uptime')} value={stats ? fmtUptime(stats.uptime_seconds, t) : '—'} />
                    </dl>
                </Card>
            </div>
            {firewallConfirmationOpen && (
                <DashboardFirewallConfirmationDialog
                    readiness={hostMutationReadiness}
                    busy={fwBusy}
                    noSSHService={firewallNeedsNoSSHConsent}
                    noSSHAcknowledged={noSSHAcknowledged}
                    onAcknowledgeNoSSH={setNoSSHAcknowledged}
                    onCancel={() => {
                        if (!fwBusy) {
                            setNoSSHAcknowledged(false);
                            setFirewallConfirmationOpen(false);
                        }
                    }}
                    onConfirm={() => void turnOnFirewall()}
                />
            )}
        </div>
    );
}

function DashboardFirewallConfirmationDialog({
    readiness,
    busy,
    noSSHService,
    noSSHAcknowledged,
    onAcknowledgeNoSSH,
    onCancel,
    onConfirm,
}: {
    readiness: HostMutationReadiness | null;
    busy: boolean;
    noSSHService: boolean;
    noSSHAcknowledged: boolean;
    onAcknowledgeNoSSH: (value: boolean) => void;
    onCancel: () => void;
    onConfirm: () => void;
}) {
    const { t } = useI18n();
    const readinessMessage = readiness === null
        ? t('services.mutationReadiness.checking')
        : readiness.ready
            ? ''
            : t(
                `services.mutationReadiness.${readiness.reason ?? 'state_unverified'}` as Parameters<typeof t>[0],
            );

    useEffect(() => {
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.key === 'Escape' && !busy) onCancel();
        };
        document.addEventListener('keydown', onKeyDown);
        return () => document.removeEventListener('keydown', onKeyDown);
    }, [busy, onCancel]);

    return (
        <div
            className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
            onMouseDown={(event) => {
                if (event.currentTarget === event.target && !busy) onCancel();
            }}
        >
            <div
                role="dialog"
                aria-modal="true"
                aria-labelledby="dashboard-firewall-confirm-title"
                aria-describedby="dashboard-firewall-confirm-description"
                aria-busy={busy}
                className="w-full max-w-md rounded-2xl border border-border bg-surface p-6 shadow-xl"
            >
                <div className="mb-4 flex items-start gap-3">
                    <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
                        <Shield className="h-5 w-5" />
                    </span>
                    <div className="min-w-0">
                        <h3 id="dashboard-firewall-confirm-title" className="text-lg font-semibold text-fg">
                            {t('firewall.confirm.enable.title')}
                        </h3>
                        <p id="dashboard-firewall-confirm-description" className="mt-1 text-sm leading-5 text-fg-muted">
                            {t('firewall.confirm.enable.description')}
                        </p>
                    </div>
                </div>
                {readiness?.ready !== true && (
                    <p role="status" className="mb-4 rounded-lg border border-warning/40 bg-warning/10 px-3 py-2 text-sm text-fg">
                        <span className="font-semibold">{t('services.mutationReadiness.title')}</span>{' '}
                        {readinessMessage}
                    </p>
                )}
                {noSSHService && (
                    <FirewallNoSSHAcknowledgement
                        id="dashboard-firewall-no-ssh"
                        checked={noSSHAcknowledged}
                        disabled={busy}
                        onChange={onAcknowledgeNoSSH}
                    />
                )}
                <div className="flex justify-end gap-2">
                    <button
                        type="button"
                        autoFocus
                        disabled={busy}
                        onClick={onCancel}
                        className="rounded-lg border border-border bg-surface px-4 py-2 text-sm font-semibold text-fg hover:bg-surface-2 disabled:opacity-50"
                    >
                        {t('common.cancel')}
                    </button>
                    <button
                        type="button"
                        disabled={busy || readiness?.ready !== true || (noSSHService && !noSSHAcknowledged)}
                        onClick={onConfirm}
                        className="rounded-lg bg-primary px-4 py-2 text-sm font-semibold text-primary-fg hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-50"
                    >
                        {noSSHService
                            ? t('firewall.ssh.no_ssh_service.confirm')
                            : t('firewall.confirm.enable.button')}
                    </button>
                </div>
            </div>
        </div>
    );
}

// A host nobody has checked and a host checked six minutes ago are not the
// same host. This card used to fold both into "Status needs refresh" and told
// the operator the scan was older than five minutes — on a machine where no
// scan has ever run. That is the unknown-versus-absent fold R-040 removed on
// the components screen and on the system-services card beside this one,
// surviving one card over. Unknown comes first here, in the same voice and
// with the same check, so the two cards on this row agree.
//
// Kimsenin bakmadığı makine ile altı dakika önce bakılmış makine aynı makine
// değildir. Bu kart ikisini de "Durum yenilenmeli"ye katlıyor ve hiç tarama
// koşmamış bir makinede operatöre taramanın beş dakikadan eski olduğunu
// söylüyordu. R-040'ın bileşenler ekranında ve yanındaki sistem servisleri
// kartında kaldırdığı bilinmeyen/yok katlaması bir kart öteye sağ kalmıştı.
function MailStackSummary({ profiles, scanFresh, hostNeverChecked, checking, onCheck, onOpen }: {
    profiles: ManagedMailProfile[];
    scanFresh: boolean;
    hostNeverChecked: boolean;
    checking: boolean;
    onCheck: () => void;
    onOpen: () => void;
}) {
    const { t } = useI18n();
    const { complete, problem, partial, needsAttention, availableOnly } =
        summarizeDashboardMailTruth(profiles, scanFresh);

    const statusKey: TranslationKey = hostNeverChecked
        ? 'services.notChecked'
        : !scanFresh
        ? 'dashboard.mailStacks.status.stale'
        : partial
            ? 'dashboard.mailStacks.status.partial'
        : needsAttention
            ? 'dashboard.mailStacks.status.attention'
            : availableOnly
                ? 'dashboard.mailStacks.status.available'
                : 'dashboard.mailStacks.status.ready';
    const detail = hostNeverChecked
        ? t('dashboard.mailStacks.notChecked')
        : !scanFresh
        ? t('dashboard.mailStacks.scanStale')
        : problem?.latest_attempt_error || problem?.blocked_reason || problem?.warning
            ? t('dashboard.mailStacks.reason', {
                name: problem?.name ?? t('dashboard.mailStacks.title'),
                reason: problem?.latest_attempt_error ?? problem?.blocked_reason ?? problem?.warning ?? '',
            })
            : problem?.latest_attempt_status === 'failed'
                ? t('dashboard.mailStacks.reconciliationFailed', { name: problem.name })
                : problem?.latest_attempt_status === 'in_progress'
                    ? t('dashboard.mailStacks.reconciliationInProgress', { name: problem.name })
                    : problem?.latest_attempt_status === 'succeeded' && !problem.verified
                        ? t('dashboard.mailStacks.reconciliationUnverified', { name: problem.name })
                        : partial
                            ? t('dashboard.mailStacks.partialHint')
                            : problem
                                ? t('dashboard.mailStacks.attentionHint')
                                : complete
                                    ? t('dashboard.mailStacks.completeHint')
                                    : t('dashboard.mailStacks.availableHint');
    // Neutral for the unknown: a machine nobody has looked at yet is the
    // normal first state, not a fault, and semantic colour is reserved for
    // something actually being wrong.
    // Bilinmeyen için nötr: henüz bakılmamış makine olağan ilk durumdur.
    const statusClass = !hostNeverChecked && (needsAttention || partial)
        ? 'bg-warning/15 text-warning'
        : hostNeverChecked || !scanFresh || availableOnly
            ? 'bg-surface-2 text-fg-muted'
            : 'bg-success/15 text-success';

    return (
        <section className='mt-6' aria-labelledby='dashboard-mail-stacks-heading'>
            <div className='rounded-xl border border-border bg-surface p-4 shadow-card sm:p-5'>
                <div className='flex flex-col gap-4 lg:flex-row lg:items-center'>
                    <span className='flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-amber-500/10 text-amber-600 dark:text-amber-400'>
                        <Layers className='h-5 w-5' />
                    </span>
                    <div className='min-w-0 flex-1'>
                        <div className='flex flex-wrap items-center gap-2'>
                            <h2 id='dashboard-mail-stacks-heading' className='font-semibold text-fg'>{t('dashboard.mailStacks.title')}</h2>
                            <span className={'rounded-full px-2.5 py-1 text-xs font-semibold ' + statusClass}>{t(statusKey)}</span>
                        </div>
                        <p className='mt-1 text-sm text-fg-muted'>{detail}</p>
                    </div>
                    {/* The same check the system-services card beside it
                        offers, run here rather than pointing at another page —
                        the firewall lesson (Jul 17): an action this central
                        acts in place.
                        Yanındaki sistem servisleri kartının sunduğu kontrolün
                        aynısı, başka sayfayı işaret etmek yerine burada koşar. */}
                    {hostNeverChecked ? (
                        <button
                            type='button'
                            onClick={onCheck}
                            disabled={checking}
                            className='inline-flex shrink-0 items-center justify-center gap-1.5 rounded-lg bg-primary px-3.5 py-2 text-sm font-semibold text-primary-fg transition-colors hover:bg-primary/90 disabled:opacity-50'
                        >
                            <ScanSearch className='h-4 w-4' />
                            {checking ? t('services.scanning') : t('services.scanNow')}
                        </button>
                    ) : (
                        <button type='button' onClick={onOpen} className='inline-flex shrink-0 items-center justify-center gap-1.5 rounded-lg bg-primary px-3.5 py-2 text-sm font-semibold text-primary-fg hover:bg-primary/90'>
                            {t(scanFresh ? 'dashboard.mailStacks.open' : 'dashboard.mailStacks.rescan')} <ArrowRight className='h-4 w-4' />
                        </button>
                    )}
                </div>
            </div>
        </section>
    );
}

// Dashboard mail-stack summary remains read-only; installation stays in Components.
// Additional users get no server telemetry or account-level actions here.
function AdditionalUserDashboard() {
    const { t } = useI18n();
    const { user } = useAuth();
    const navigate = useNavigate();
    const [domains, setDomains] = useState<DomainLite[]>([]);
    const [loading, setLoading] = useState(true);

    useEffect(() => {
        const controller = new AbortController();

        fetch('/api/v1/domains', { signal: controller.signal })
            .then((response) => {
                if (!response.ok) throw new Error('failed to load granted domains');
                return response.json();
            })
            .then((value: unknown) => {
                // The server filters this collection to the signed-in user's
                // grants. An unexpected payload still fails closed here.
                setDomains(Array.isArray(value) ? value : []);
            })
            .catch(() => {
                if (!controller.signal.aborted) setDomains([]);
            })
            .finally(() => {
                if (!controller.signal.aborted) setLoading(false);
            });

        return () => controller.abort();
    }, []);

    return (
        <div className={'p-6 md:p-8'}>
            <PageHeader
                title={t('dashboard.welcome', { name: user.username })}
                subtitle={t('nav.domains')}
            />

            <section className={'max-w-4xl rounded-xl border border-border bg-surface p-6 shadow-card'}>
                <div className={'flex items-center justify-between gap-4'}>
                    <div className={'flex items-center gap-3'}>
                        <span className={'rounded-lg bg-primary/10 p-2 text-primary'}>
                            <Globe className={'h-5 w-5'} />
                        </span>
                        <h2 className={'text-lg font-semibold text-fg'}>{t('dashboard.domains')}</h2>
                    </div>
                    <span
                        className={'rounded-full bg-surface-muted px-3 py-1 text-sm font-semibold text-fg-muted'}
                        aria-label={t('dashboard.domains')}
                    >
                        {loading ? '—' : domains.length}
                    </span>
                </div>

                {loading ? (
                    <p className={'mt-6 text-sm text-fg-muted'} aria-live={'polite'}>
                        {t('common.loading')}
                    </p>
                ) : domains.length === 0 ? (
                    <div className={'mt-6 rounded-lg border border-dashed border-border p-6 text-center'}>
                        <p className={'font-semibold text-fg'}>{t('domains.empty')}</p>
                        <button
                            type={'button'}
                            onClick={() => navigate('/domains')}
                            className={'mt-4 rounded-lg border border-border px-4 py-2 text-sm font-semibold text-fg hover:bg-surface-muted'}
                        >
                            {t('nav.domains')}
                        </button>
                    </div>
                ) : (
                    <div className={'mt-6 grid grid-cols-1 gap-3 sm:grid-cols-2'}>
                        {domains.map((domain) => (
                            <button
                                key={domain.id}
                                type={'button'}
                                onClick={() => navigate(`/domains/${encodeURIComponent(domain.domain_name)}`)}
                                className={'flex items-center justify-between gap-4 rounded-lg border border-border p-4 text-left hover:border-primary/40 hover:bg-surface-muted'}
                            >
                                <span className={'min-w-0 truncate font-semibold text-fg'}>{domain.domain_name}</span>
                                <span className={'shrink-0 text-sm font-semibold text-primary'}>{t('domains.action.manage')}</span>
                            </button>
                        ))}
                    </div>
                )}
            </section>
        </div>
    );
}

// Non-admin account view: server gauges + facts + quick actions.
function CustomerDashboard() {
    const { t } = useI18n();
    const [stats, setStats] = useState<SystemStats | null>(null);

    useEffect(() => {
        const load = () => api.getSystemStats().then(setStats).catch(() => {});
        load();
        const timer = setInterval(load, 5000);
        return () => clearInterval(timer);
    }, []);

    return (
        <div className="p-6 md:p-8">
            <PageHeader title={t('dashboard.title')} subtitle={stats?.hostname} />

            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
                <GaugeCard
                    icon={Cpu}
                    label={t('dashboard.cpuUsage')}
                    percent={stats?.cpu_percent ?? 0}
                    value={stats ? t('dashboard.percentValue', { n: Math.round(stats.cpu_percent) }) : '—'}
                    hint={stats ? t('dashboard.cores', { n: stats.cpu_cores }) : ''}
                />
                <GaugeCard
                    icon={MemoryStick}
                    label={t('dashboard.memoryUsage')}
                    percent={pct(stats?.mem_used_bytes, stats?.mem_total_bytes)}
                    value={stats ? t('dashboard.percentValue', { n: Math.round(pct(stats.mem_used_bytes, stats.mem_total_bytes)) }) : '—'}
                    hint={stats ? t('dashboard.usedOfTotal', { used: fmtBytes(stats.mem_used_bytes), total: fmtBytes(stats.mem_total_bytes) }) : ''}
                />
                <GaugeCard
                    icon={HardDrive}
                    label={t('dashboard.diskUsage')}
                    percent={pct(stats?.disk_used_bytes, stats?.disk_total_bytes)}
                    value={stats ? t('dashboard.percentValue', { n: Math.round(pct(stats.disk_used_bytes, stats.disk_total_bytes)) }) : '—'}
                    hint={stats ? t('dashboard.usedOfTotal', { used: fmtBytes(stats.disk_used_bytes), total: fmtBytes(stats.disk_total_bytes) }) : ''}
                />
            </div>

            <div className="mt-4">
                <Card title={t('dashboard.serverInfo')} icon={Server}>
                    <dl className="divide-y divide-border text-sm">
                        <InfoRow label={t('dashboard.hostname')} value={stats?.hostname ?? '—'} />
                        <InfoRow label={t('dashboard.os')} value={stats?.os ?? '—'} />
                        <InfoRow label={t('dashboard.uptime')} value={stats ? fmtUptime(stats.uptime_seconds, t) : '—'} />
                    </dl>
                </Card>
            </div>

            <section className="mt-6">
                <SectionTitle icon={Plus} tint="bg-primary/10 text-primary" title={t('dashboard.quickActions')} />
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                    <QuickAction icon={Globe} labelKey="dashboard.addDomain" to="/domains" />
                    <QuickAction icon={Database} labelKey="dashboard.viewDatabases" to="/databases" />
                </div>
            </section>
        </div>
    );
}

function SectionTitle({
    icon: Icon,
    tint,
    title,
    right,
}: {
    icon: typeof Cpu;
    tint: string;
    title: string;
    right?: React.ReactNode;
}) {
    return (
        <div className="mb-3 flex items-center gap-3">
            <span className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-lg ${tint}`}>
                <Icon className="h-5 w-5" />
            </span>
            <h2 className="text-lg font-semibold text-fg">{title}</h2>
            {right && <span className="ml-auto">{right}</span>}
        </div>
    );
}

function GaugeCard({
    icon: Icon,
    label,
    percent,
    value,
    hint,
}: {
    icon: typeof Cpu;
    label: string;
    percent: number;
    value: string;
    hint: string;
}) {
    return (
        <div className="rounded-xl border border-border bg-surface p-5 shadow-card">
            <div className="flex items-center justify-between">
                <span className="text-sm font-medium text-fg-muted">{label}</span>
                <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary/10 text-primary">
                    <Icon className="h-4 w-4" />
                </span>
            </div>
            <p className="mt-2 text-3xl font-bold tracking-tight text-fg">{value}</p>
            <div className="mt-3">
                <UsageBar percent={percent} />
            </div>
            {hint && <p className="mt-2 text-xs text-fg-subtle">{hint}</p>}
        </div>
    );
}

function CountCard({ icon: Icon, n, label, to }: { icon: typeof Cpu; n: number; label: string; to: string }) {
    const navigate = useNavigate();
    return (
        <button
            onClick={() => navigate(to)}
            className="rounded-xl border border-border bg-surface p-4 text-left shadow-card transition-colors hover:border-primary/40 hover:bg-surface-2/60"
        >
            <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary/10 text-primary">
                <Icon className="h-4 w-4" />
            </span>
            <p className="mt-2 text-2xl font-bold tracking-tight text-fg">{n}</p>
            <p className="text-xs text-fg-muted">{label}</p>
        </button>
    );
}

function InfoRow({ label, value }: { label: string; value: string }) {
    return (
        <div className="flex items-center justify-between px-4 py-2.5">
            <dt className="text-fg-muted">{label}</dt>
            <dd className="max-w-[60%] truncate text-right font-medium text-fg">{value}</dd>
        </div>
    );
}

function QuickAction({ icon: Icon, labelKey, to }: { icon: typeof Server; labelKey: TranslationKey; to: string }) {
    const navigate = useNavigate();
    const { t } = useI18n();
    return (
        <button
            onClick={() => navigate(to)}
            className="group flex items-center gap-3 rounded-xl border border-border bg-surface p-4 text-left shadow-card transition-colors hover:border-primary/40 hover:bg-surface-2"
        >
            <span className="flex h-9 w-9 items-center justify-center rounded-lg bg-surface-2 text-fg-muted transition-colors group-hover:bg-primary group-hover:text-primary-fg">
                <Icon className="h-5 w-5" />
            </span>
            <span className="flex-1 text-base font-medium text-fg">{t(labelKey)}</span>
        </button>
    );
}

function pct(used?: number, total?: number): number {
    if (!used || !total) return 0;
    return (used / total) * 100;
}

function fmtBytes(bytes: number): string {
    if (bytes >= 1024 ** 3) return `${(bytes / 1024 ** 3).toFixed(1)} GB`;
    if (bytes >= 1024 ** 2) return `${(bytes / 1024 ** 2).toFixed(0)} MB`;
    if (bytes >= 1024) return `${(bytes / 1024).toFixed(0)} KB`;
    return `${bytes} B`;
}

function fmtUptime(seconds: number, t: (k: TranslationKey, v?: Record<string, string | number>) => string): string {
    const d = Math.floor(seconds / 86400);
    const h = Math.floor((seconds % 86400) / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    if (d > 0) return `${d}${t('common.days')} ${h}${t('common.hours')}`;
    if (h > 0) return `${h}${t('common.hours')} ${m}${t('common.minutes')}`;
    return `${m}${t('common.minutes')}`;
}

// SQLite TEXT timestamps arrive as "YYYY-MM-DD HH:MM:SS" (UTC) or RFC3339.
// SQLite TEXT zaman damgaları "YYYY-AA-GG SS:DD:SS" (UTC) ya da RFC3339 gelir.
function fmtRelative(ts: string, t: (k: TranslationKey, v?: Record<string, string | number>) => string): string {
    const iso = ts.includes('T') ? ts : ts.replace(' ', 'T') + 'Z';
    const then = new Date(iso).getTime();
    if (Number.isNaN(then)) return ts;
    const mins = Math.floor((Date.now() - then) / 60000);
    if (mins < 1) return t('time.justNow');
    if (mins < 60) return t('time.minAgo', { n: mins });
    const hours = Math.floor(mins / 60);
    if (hours < 24) return t('time.hoursAgo', { n: hours });
    return t('time.daysAgo', { n: Math.floor(hours / 24) });
}
