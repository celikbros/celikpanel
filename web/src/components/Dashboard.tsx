import { useEffect, useState } from 'react';
import { useNavigate } from '../router';
import {
    Cpu, MemoryStick, HardDrive, Server, Globe, Database, Activity, Bell,
    Shield, ShieldOff, Users, Mail, Rocket, Check, ArrowRight,
    DownloadCloud, UserPlus, Plus, Lock,
} from 'lucide-react';
import { api, type SystemStats } from '../lib/api';
import { useI18n } from '../i18n';
import { useAuth } from '../auth/AuthContext';
import type { TranslationKey } from '../i18n/en';
import { PageHeader, UsageBar, StatusDot, Card } from './ui';
import { showToast } from './Toast';

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
    is_installed: boolean;
    kind?: 'service' | 'runtime' | 'tool';
}
interface FwState {
    enabled: boolean;
    tcp_ports?: number[];
    udp_ports?: number[];
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
    created_at: string;
}
interface Extras {
    databases: number;
    mail_accounts: number;
    expiring_certs: { domain_name: string; days_left: number }[];
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
    const [fw, setFw] = useState<FwState | null>(null);
    const [domains, setDomains] = useState<DomainLite[]>([]);
    const [audit, setAudit] = useState<AuditLite[]>([]);
    const [usersCount, setUsersCount] = useState(0);
    const [dnsServer, setDnsServer] = useState('');
    // capabilities.mail_server is a BOOL in the API (dns_server is a string) —
    // treating it like a string silently marks the step done when it is false.
    // capabilities.mail_server API'de BOOL'dur (dns_server metindir) — metin
    // gibi ele almak false iken adımı sessizce 'tamamlandı' işaretler.
    const [mailInstalled, setMailInstalled] = useState(false);
    // A real CA cert on the panel (self_signed === false) counts as "got an
    // SSL certificate" — the operator did obtain one, even if no site has one.
    // Panelde gerçek CA sertifikası (self_signed === false) "SSL aldın"
    // sayılır — operatör gerçekten bir sertifika aldı, hiçbir sitede olmasa da.
    const [panelSecured, setPanelSecured] = useState(false);
    const [extras, setExtras] = useState<Extras | null>(null);

    useEffect(() => {
        const loadStats = () => api.getSystemStats().then(setStats).catch(() => {});
        loadStats();
        const timer = setInterval(loadStats, 5000);

        fetch('/api/v1/managed-services').then((r) => r.json()).then((d) => setServices(d?.services || [])).catch(() => {});
        fetch('/api/v1/firewall').then((r) => (r.ok ? r.json() : null)).then(setFw).catch(() => {});
        fetch('/api/v1/domains').then((r) => (r.ok ? r.json() : [])).then((d) => setDomains(d || [])).catch(() => {});
        fetch('/api/v1/audit-logs?limit=7').then((r) => (r.ok ? r.json() : null)).then((d) => setAudit(d?.entries || [])).catch(() => {});
        fetch('/api/v1/users').then((r) => (r.ok ? r.json() : null)).then((d) => setUsersCount((d?.users || []).length)).catch(() => {});
        fetch('/api/v1/hosting/capabilities')
            .then((r) => (r.ok ? r.json() : null))
            .then((c) => { setDnsServer(c?.dns_server ?? ''); setMailInstalled(Boolean(c?.mail_server)); })
            .catch(() => {});
        fetch('/api/v1/dashboard').then((r) => (r.ok ? r.json() : null)).then(setExtras).catch(() => {});
        fetch('/api/v1/panel/certificate').then((r) => (r.ok ? r.json() : null)).then((c) => setPanelSecured(c ? c.self_signed === false : false)).catch(() => {});

        return () => clearInterval(timer);
    }, []);

    const installed = services.filter((s) => s.is_installed);
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
    const cannotStop = (s: SvcLite) => s.kind === 'tool' || s.status === 'installed';
    const running = installed.filter((s) => cannotStop(s) || s.status?.toLowerCase().includes('running'));
    const stoppedSvcs = installed.filter((s) => !cannotStop(s) && !s.status?.toLowerCase().includes('running'));

    // Turn the firewall on right where the operator reads about it. Field
    // finding (Jul 17): the journey said "turn on the firewall" but its button
    // navigated away, and the operator never found the switch — an action this
    // important acts in place, it does not give directions.
    // Güvenlik duvarını, operatörün onu okuduğu yerde aç. Saha bulgusu
    // (17 Tem): yolculuk "firewall'u aç" diyordu ama düğmesi başka sayfaya
    // götürüyordu ve operatör anahtarı hiç bulamadı — bu önemde bir eylem
    // yerinde yapılır, adres tarif etmez.
    const [fwBusy, setFwBusy] = useState(false);
    const turnOnFirewall = async () => {
        setFwBusy(true);
        try {
            const r = await fetch('/api/v1/firewall', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ enabled: true }),
            });
            const d = await r.json();
            if (!r.ok || d.error) throw new Error(d.error);
            setFw(d);
            showToast('success', t('firewall.onDone'));
        } catch (e) {
            showToast('error', e instanceof Error && e.message ? e.message : t('common.error'));
        } finally {
            setFwBusy(false);
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
            onAct: turnOnFirewall,
        });
    }
    // Security posture suggestions — surfaced only when they actually apply,
    // so they guide rather than nag: antivirus once there is content to scan,
    // spam filtering once mail is running.
    // Güvenlik duruşu önerileri — yalnız gerçekten geçerliyken çıkar, böylece
    // dırdır değil yol gösterir: taranacak içerik varken antivirüs, posta
    // çalışırken spam filtresi.
    const hasClamAV = services.some((s) => s.id === 'clamav' && s.is_installed);
    const hasSpam = services.some((s) => s.id === 'spamassassin' && s.is_installed);
    // "Content to scan" means a hosted site — a DNS-only domain serves
    // records, not files, so it must not trigger the antivirus nag.
    // "Taranacak içerik" barındırılan site demektir — yalnız-DNS domain dosya
    // değil kayıt sunar; antivirüs dırdırını tetiklememeli.
    const hostsContent = domains.some((d) => d.project_type !== 'dnsonly');
    if (hostsContent && !hasClamAV) {
        attention.push({
            key: 'no-av',
            icon: Shield,
            text: t('dashboard.avItem'),
            action: t('dashboard.installService'),
            to: '/services',
        });
    }
    if (mailInstalled && !hasSpam) {
        attention.push({
            key: 'no-spam',
            icon: Mail,
            text: t('dashboard.spamItem'),
            action: t('dashboard.installService'),
            to: '/services',
        });
    }

    // serviceRunning: is this catalogue service actually up right now? The
    // journey asks it because "installed" and "working" are different facts,
    // and only the second one earns a tick.
    // serviceRunning: bu katalog servisi şu an gerçekten ayakta mı? Yolculuk
    // bunu sorar çünkü "kurulu" ile "çalışıyor" farklı gerçeklerdir ve tiki
    // yalnız ikincisi hak eder.
    const serviceRunning = (id: string) => {
        const svc = services.find((s) => s.id === id);
        if (!svc || !svc.is_installed) return false;
        // A tool or a unit-less runtime has no daemon of ours: "installed" IS
        // its working state (D-010) — never demand a running dot from it.
        // Bir tool'un ya da unit'siz runtime'ın bize ait daemon'ı yoktur:
        // "kurulu" onun çalışma hâlidir (D-010) — ondan koşan nokta beklenmez.
        if (svc.kind === 'tool' || svc.status === 'installed') return true;
        return svc.status?.toLowerCase().includes('running') === true;
    };

    // Setup journey — live completion; the card disappears when all done.
    // Kurulum yolculuğu — canlı tamamlanma; hepsi bitince kart kaybolur.
    const steps: { key: TranslationKey; hint?: TranslationKey; done: boolean; to: string; cta?: TranslationKey; onAct?: () => void }[] = [
        // Every CTA says what it actually does — "Go to services" on a button
        // that opens the Domains page was a lie the operator caught (Jul 17).
        // Her düğme gerçekten yaptığını söyler — Domains sayfasını açan
        // düğmede "Go to services" yazması operatörün yakaladığı bir yalandı.
        { key: 'dashboard.step.panel', done: true, to: '/' },
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
        { key: 'dashboard.step.dns', hint: 'dashboard.step.dnsHint', done: dnsServer !== '' && serviceRunning(dnsServer), to: '/services' },
        { key: 'dashboard.step.domain', done: domains.length > 0, to: '/domains', cta: 'dashboard.addDomain' },
        { key: 'dashboard.step.ssl', hint: 'dashboard.step.sslHint', done: panelSecured || domains.some((d) => d.ssl_enabled), to: '/settings', cta: 'dashboard.goSettings' },
        // The firewall step acts in place: the engine ships with install.sh,
        // so "turn on" is one honest click, not a scavenger hunt.
        // Firewall adımı yerinde eyler: motor install.sh ile gelir, "aç" tek
        // dürüst tıktır, define avı değil.
        { key: 'dashboard.step.firewall', hint: 'dashboard.step.firewallHint', done: fw?.enabled === true, to: '/services', cta: 'firewall.turnOn', onAct: turnOnFirewall },
        { key: 'dashboard.step.mail', done: mailInstalled && serviceRunning('postfix'), to: '/services' },
    ];
    const doneCount = steps.filter((s) => s.done).length;
    const journeyOpen = doneCount < steps.length;
    const nextIdx = steps.findIndex((s) => !s.done);
    const hasContent = installed.length > 0 || domains.length > 0;

    const recentDomains = [...domains]
        .sort((a, b) => (a.created_at < b.created_at ? 1 : -1))
        .slice(0, 4);


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
                    value={stats ? `%${Math.round(stats.cpu_percent)}` : '—'}
                    hint={stats ? `${t('dashboard.cores', { n: stats.cpu_cores })} · ${stats.load_avg[0]?.toFixed(2) ?? ''}` : ''}
                />
                <GaugeCard
                    icon={MemoryStick}
                    label={t('dashboard.memoryUsage')}
                    percent={pct(stats?.mem_used_bytes, stats?.mem_total_bytes)}
                    value={stats ? `%${Math.round(pct(stats.mem_used_bytes, stats.mem_total_bytes))}` : '—'}
                    hint={stats ? `${fmtBytes(stats.mem_used_bytes)} / ${fmtBytes(stats.mem_total_bytes)}` : ''}
                />
                <GaugeCard
                    icon={HardDrive}
                    label={t('dashboard.diskUsage')}
                    percent={pct(stats?.disk_used_bytes, stats?.disk_total_bytes)}
                    value={stats ? `%${Math.round(pct(stats.disk_used_bytes, stats.disk_total_bytes))}` : '—'}
                    hint={stats ? `${fmtBytes(stats.disk_used_bytes)} / ${fmtBytes(stats.disk_total_bytes)}` : ''}
                />
                <button
                    onClick={() => navigate('/services')}
                    className="rounded-xl border border-border bg-surface p-5 text-left shadow-card transition-colors hover:bg-surface-2/60"
                >
                    <div className="flex items-center justify-between">
                        <span className="text-sm font-medium text-fg-muted">{t('dashboard.services')}</span>
                        <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary/10 text-primary">
                            <Activity className="h-4 w-4" />
                        </span>
                    </div>
                    <p className="mt-2 text-3xl font-bold tracking-tight text-fg">
                        {installed.length > 0 ? `${running.length} / ${installed.length}` : '0 / 0'}
                    </p>
                    {stoppedSvcs.length > 0 ? (
                        <p className="mt-2 text-xs font-medium text-warning">{t('dashboard.svcStopped', { n: stoppedSvcs.length })}</p>
                    ) : (
                        <p className="mt-2 text-xs text-fg-subtle">
                            {installed.length > 0 ? t('dashboard.svcRunningHint') : t('dashboard.svcNone')}
                        </p>
                    )}
                    {installed.length === 0 && <p className="mt-1 text-xs text-fg-subtle">{t('dashboard.svcReady')}</p>}
                </button>
            </div>

            {/* Needs attention BEFORE the journey: an active problem outranks
                guidance. Operator feedback (Jul 17): the alert list lived below
                the fold while the top of the page stayed calm.
                Needs attention yolculuktan ÖNCE: aktif sorun, rehberlikten
                önce gelir. Operatör geri bildirimi (17 Tem): uyarı listesi
                sayfanın altında kalırken üst taraf sakin görünüyordu. */}
            {hasContent && (
                <section className="mt-6">
                    <SectionTitle
                        icon={Bell}
                        tint="bg-amber-500/10 text-amber-600 dark:text-amber-400"
                        title={t('dashboard.attention')}
                        right={
                            attention.length > 0 ? (
                                <span className="rounded-full bg-warning/15 px-2.5 py-1 text-xs font-semibold text-warning">
                                    {t('dashboard.warnCount', { n: attention.length })}
                                </span>
                            ) : undefined
                        }
                    />
                    <div className="overflow-hidden rounded-xl border border-border bg-surface shadow-card">
                        {attention.length === 0 ? (
                            <div className="flex items-center gap-2.5 px-4 py-3.5 text-sm text-fg-muted">
                                <StatusDot ok /> {t('dashboard.allGood')}
                            </div>
                        ) : (
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
                        )}
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
                                    <div className="min-w-0 flex-1">
                                        <div className={`text-base font-medium ${s.done || i === nextIdx ? 'text-fg' : 'text-fg-muted'}`}>
                                            {t(s.key)}
                                        </div>
                                        {i === nextIdx && s.hint && (
                                            <div className="text-xs text-fg-subtle">{t(s.hint)}</div>
                                        )}
                                    </div>
                                    {s.done ? (
                                        <span className="text-sm text-fg-subtle">{t('dashboard.stepDone')}</span>
                                    ) : i === nextIdx ? (
                                        <button
                                            onClick={() => (s.onAct ? s.onAct() : navigate(s.to))}
                                            disabled={s.onAct ? fwBusy : false}
                                            className="rounded-lg bg-primary px-3 py-1.5 text-xs font-semibold text-primary-fg transition-colors hover:bg-primary/90 disabled:opacity-50"
                                        >
                                            {t(s.cta ?? 'dashboard.goServices')}
                                        </button>
                                    ) : (
                                        <span className="text-sm text-fg-subtle">{i === nextIdx + 1 ? t('dashboard.stepNext') : ''}</span>
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
                        {audit.length === 0 ? (
                            <Card><p className="p-4 text-sm text-fg-subtle">—</p></Card>
                        ) : (
                            <ul className="overflow-hidden rounded-xl border border-border bg-surface shadow-card">
                                {audit.map((e) => (
                                    <li key={e.id} className="flex flex-wrap items-center gap-2.5 border-b border-border px-4 py-2.5 last:border-0">
                                        <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-surface-2 text-xs font-semibold uppercase text-fg-muted">
                                            {e.username.slice(0, 1) || '?'}
                                        </span>
                                        <span className="min-w-0 flex-1 text-sm text-fg">
                                            <span className="font-semibold">{e.username}</span>{' '}
                                            <span className="font-mono text-xs text-fg-muted">{e.action}</span>
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
        </div>
    );
}

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
                    value={stats ? `%${Math.round(stats.cpu_percent)}` : '—'}
                    hint={stats ? t('dashboard.cores', { n: stats.cpu_cores }) : ''}
                />
                <GaugeCard
                    icon={MemoryStick}
                    label={t('dashboard.memoryUsage')}
                    percent={pct(stats?.mem_used_bytes, stats?.mem_total_bytes)}
                    value={stats ? `%${Math.round(pct(stats.mem_used_bytes, stats.mem_total_bytes))}` : '—'}
                    hint={stats ? t('dashboard.usedOfTotal', { used: fmtBytes(stats.mem_used_bytes), total: fmtBytes(stats.mem_total_bytes) }) : ''}
                />
                <GaugeCard
                    icon={HardDrive}
                    label={t('dashboard.diskUsage')}
                    percent={pct(stats?.disk_used_bytes, stats?.disk_total_bytes)}
                    value={stats ? `%${Math.round(pct(stats.disk_used_bytes, stats.disk_total_bytes))}` : '—'}
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
