import { useCallback, useEffect, useState } from 'react';
import { Copy, Globe, RefreshCw, ShieldCheck, AlertTriangle, Check } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { StatusDot } from './ui';
import { HelpButton } from './HelpDrawer';

// The screen that answers "what do I do at my registrar?" — and then checks.
//
// Before this, the panel created a domain, served a zone for it, and said
// nothing about the one step that makes any of it real: pointing the domain at
// this server. The operator found out the hard way (25 Jul) — a domain added,
// SSL nowhere to be found, nameservers still parked at the registrar and an A
// record aimed at a different machine entirely. Every value below is one the
// panel already knew and never showed.
//
// It deliberately offers TWO routes rather than one, because they are honestly
// different products: full delegation gives the panel the zone (and with it
// mail authentication and automatic records), while a single A record is
// enough for a website and a certificate and leaves DNS where it is.
//
// "Kayıtçımda ne yapacağım?" sorusunu cevaplayan — ve sonra kontrol eden ekran.
//
// Bundan önce panel bir alan adı oluşturuyor, ona zone sunuyor ve hepsini
// gerçek kılan tek adım hakkında hiçbir şey söylemiyordu: alan adını bu
// sunucuya yöneltmek. Operatör bunu zor yoldan öğrendi (25 Tem) — alan adı
// eklendi, SSL hiçbir yerde bulunamadı, nameserver'lar hâlâ kayıtçıda park
// etmiş ve A kaydı bambaşka bir makineyi gösteriyordu. Aşağıdaki her değer,
// panelin zaten bildiği ve hiç göstermediği bir değerdi.
//
// Bilerek TEK yol değil İKİ yol sunar, çünkü ikisi dürüstçe farklı ürünlerdir:
// tam devir zone'u panele verir (yanında posta kimlik doğrulaması ve otomatik
// kayıtlarla), tek bir A kaydı ise bir web sitesi ve sertifika için yeterlidir
// ve DNS'i olduğu yerde bırakır.
interface Connection {
    domain: string;
    server_ip: string;
    server_ipv6?: string;
    nameservers: string[];
    live_nameservers: string[];
    live_ips: string[];
    status: 'delegated' | 'delegated_mismatch' | 'a_record' | 'elsewhere' | 'unresolved';
    ssl_ready: boolean;
    glue_needed: boolean;
    nameservers_usable: boolean;
    nameserver_facts?: { host: string; ips: string[]; points_here: boolean }[];
    checked_at: string;
}

function CopyField({ label, value }: { label: string; value: string }) {
    const { t } = useI18n();
    const [copied, setCopied] = useState(false);
    if (!value) return null;
    return (
        <div className="flex items-center gap-2 rounded-lg border border-border bg-surface-2/60 px-3 py-2">
            <span className="w-32 shrink-0 text-xs text-fg-subtle">{label}</span>
            <code className="min-w-0 flex-1 truncate font-mono text-sm text-fg">{value}</code>
            <button
                onClick={() => {
                    navigator.clipboard?.writeText(value);
                    setCopied(true);
                    setTimeout(() => setCopied(false), 1500);
                    showToast('success', t('conn.copied'));
                }}
                title={t('conn.copy')}
                className="shrink-0 rounded p-1 text-fg-muted transition-colors hover:bg-surface hover:text-fg"
            >
                {copied ? <Check className="h-3.5 w-3.5 text-success" /> : <Copy className="h-3.5 w-3.5" />}
            </button>
        </div>
    );
}

export function DomainConnection({ domainId, domainName }: { domainId: number; domainName: string }) {
    const { t } = useI18n();
    const [c, setC] = useState<Connection | null>(null);
    const [busy, setBusy] = useState(true);

    const load = useCallback(async () => {
        setBusy(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/connection`);
            if (res.ok) setC(await res.json());
        } catch {
            /* the card simply stays quiet if the check cannot run */
        } finally {
            setBusy(false);
        }
    }, [domainId]);

    useEffect(() => {
        load();
    }, [load]);

    if (!c && busy) {
        return <div className="rounded-xl border border-border bg-surface p-5 text-sm text-fg-muted">{t('common.loading')}</div>;
    }
    if (!c) return null;

    const ok = c.status === 'delegated' || c.status === 'a_record';
    const tone = ok ? 'border-success/40 bg-success/5' : 'border-warning/40 bg-warning/5';

    return (
        <section className={`rounded-xl border ${tone} p-5`}>
            <div className="mb-3 flex flex-wrap items-center gap-2">
                <span className="flex h-9 w-9 items-center justify-center rounded-lg bg-primary/10 text-primary">
                    <Globe className="h-4.5 w-4.5" />
                </span>
                <div className="min-w-0">
                    <h3 className="text-sm font-semibold text-fg">{t('conn.title')}</h3>
                    <p className="flex items-center gap-1.5 text-xs">
                        <StatusDot ok={ok} />
                        <span className={ok ? 'text-success' : 'text-warning'}>{t(`conn.status.${c.status}` as Parameters<typeof t>[0])}</span>
                    </p>
                </div>
                <div className="ml-auto flex items-center gap-2">
                    <HelpButton serviceId="domain-connection" name={domainName} />
                    <button
                        onClick={load}
                        disabled={busy}
                        className="inline-flex items-center gap-1.5 rounded-lg border border-border-strong bg-surface px-2.5 py-1.5 text-xs font-medium text-fg transition-colors hover:bg-surface-2 disabled:opacity-50"
                    >
                        <RefreshCw className={`h-3.5 w-3.5 ${busy ? 'animate-spin' : ''}`} />
                        {t('conn.recheck')}
                    </button>
                </div>
            </div>

            {/* What the world sees right now — the fact, before any advice.
                Dünyanın şu an gördüğü — tavsiyeden önce olgu. */}
            <div className="mb-4 grid gap-2 sm:grid-cols-2">
                <div className="rounded-lg border border-border bg-surface p-3">
                    <p className="mb-1 text-xs text-fg-subtle">{t('conn.liveNs')}</p>
                    <p className="font-mono text-xs text-fg">
                        {c.live_nameservers.length ? c.live_nameservers.join(', ') : t('conn.none')}
                    </p>
                </div>
                <div className="rounded-lg border border-border bg-surface p-3">
                    <p className="mb-1 text-xs text-fg-subtle">{t('conn.liveIp')}</p>
                    <p className="font-mono text-xs text-fg">{c.live_ips.length ? c.live_ips.join(', ') : t('conn.none')}</p>
                </div>
            </div>

            {ok ? (
                <p className="rounded-lg bg-success/10 p-3 text-sm text-fg">
                    {c.status === 'delegated' ? t('conn.okDelegated') : t('conn.okARecord')}
                </p>
            ) : (
                <>
                    <p className="mb-3 text-sm text-fg-muted">{t('conn.intro')}</p>

                    {/* Route A is offered ONLY when this server's nameserver
                        names actually answer for this server. Otherwise the
                        instruction would break the domain — and the panel would
                        be what said to do it. / A yolu YALNIZ bu sunucunun ad
                        sunucusu adları gerçekten bu sunucu adına cevap
                        verdiğinde sunulur. Aksi hâlde talimat alan adını
                        bozardı — ve bunu söyleyen panel olurdu. */}
                    {!c.nameservers_usable && (
                        <div className="mb-3 rounded-xl border border-danger/40 bg-danger/5 p-4">
                            <h4 className="mb-1 text-sm font-semibold text-fg">{t('conn.nsBroken.title')}</h4>
                            <p className="text-xs leading-relaxed text-fg-muted">{t('conn.nsBroken.desc')}</p>
                            {c.nameserver_facts?.length ? (
                                <ul className="mt-2 space-y-1">
                                    {c.nameserver_facts.map((f) => (
                                        <li key={f.host} className="font-mono text-xs text-fg-muted">
                                            {f.host} → {f.ips.length ? f.ips.join(', ') : t('conn.none')}
                                            {!f.points_here && <span className="ml-1 text-danger">✕</span>}
                                        </li>
                                    ))}
                                </ul>
                            ) : null}
                        </div>
                    )}

                    {/* Route A — full delegation. / A yolu — tam devir. */}
                    <div className={`mb-3 rounded-xl border border-border bg-surface p-4 ${!c.nameservers_usable ? 'opacity-50' : ''}`}>
                        <h4 className="mb-1 text-sm font-semibold text-fg">{t('conn.routeA.title')}</h4>
                        <p className="mb-3 text-xs leading-relaxed text-fg-muted">{t('conn.routeA.desc')}</p>
                        {/* Glue is only this domain's business when the
                            nameserver names live under it. With the server's
                            shared pair the glue was registered once, on the
                            panel's own domain. / Glue, ad sunucusu adları bu
                            alan adının altındaysa onun işidir. Sunucunun ortak
                            çiftinde glue bir kez, panelin kendi alan adında
                            kaydedilmiştir. */}
                        {c.glue_needed ? (
                            <>
                                <p className="mb-2 text-xs font-medium text-fg-subtle">{t('conn.routeA.step1')}</p>
                                <div className="mb-3 space-y-1.5">
                                    {c.nameservers.map((ns) => (
                                        <CopyField key={ns} label={ns} value={c.server_ip} />
                                    ))}
                                </div>
                            </>
                        ) : (
                            <p className="mb-3 rounded-lg bg-surface-2/60 p-2.5 text-xs leading-relaxed text-fg-muted">
                                {t('conn.routeA.sharedNs')}
                            </p>
                        )}
                        <p className="mb-2 text-xs font-medium text-fg-subtle">{t('conn.routeA.step2')}</p>
                        <div className="space-y-1.5">
                            {c.nameservers.map((ns, i) => (
                                <CopyField key={ns} label={t('conn.nameserverN', { n: String(i + 1) })} value={ns} />
                            ))}
                        </div>
                    </div>

                    {/* Route B — just point the address. / B yolu — yalnız adresi yönelt. */}
                    <div className="rounded-xl border border-border bg-surface p-4">
                        <h4 className="mb-1 text-sm font-semibold text-fg">{t('conn.routeB.title')}</h4>
                        <p className="mb-3 text-xs leading-relaxed text-fg-muted">{t('conn.routeB.desc')}</p>
                        <div className="space-y-1.5">
                            <CopyField label={`A    @`} value={c.server_ip} />
                            <CopyField label={`A    www`} value={c.server_ip} />
                            {c.server_ipv6 && <CopyField label={`AAAA @`} value={c.server_ipv6} />}
                        </div>
                    </div>
                </>
            )}

            {/* SSL is the most common reason someone lands here, so say plainly
                whether it can work yet. / İnsanların buraya en sık geliş sebebi
                SSL'dir; bu yüzden şimdilik çalışıp çalışamayacağını düz söyle. */}
            <p className="mt-4 flex items-start gap-2 text-xs leading-relaxed text-fg-muted">
                {c.ssl_ready ? (
                    <ShieldCheck className="mt-0.5 h-4 w-4 shrink-0 text-success" />
                ) : (
                    <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
                )}
                <span>{c.ssl_ready ? t('conn.sslReady') : t('conn.sslBlocked')}</span>
            </p>
        </section>
    );
}
