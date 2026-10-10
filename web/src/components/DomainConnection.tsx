import { useState } from 'react';
import { Copy, Globe, RefreshCw, ShieldCheck, AlertTriangle, Check } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { Checking, CouldNotCheck, RemoteGate, StatusDot } from './ui';
import { decodeList, useRemote } from '../lib/remote';
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
    dns_management_mode?: 'local' | 'external' | 'existing';
    required_records?: { name: string; type: string; content: string; ttl: number; prio?: number }[];
    domain: string;
    server_ip: string;
    server_ipv6?: string;
    nameservers: string[];
    live_nameservers: string[];
    live_ips: string[];
    status: 'delegated' | 'delegated_mismatch' | 'a_record' | 'elsewhere' | 'unresolved' | 'unknown';
    ssl_ready: boolean;
    propagation_pending?: boolean;
    resolver_observations?: {
        resolver: string;
        nameservers: string[];
        ips: string[];
        status: string;
        ssl_ready: boolean;
    }[];
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

const connectionStatuses = ['delegated', 'delegated_mismatch', 'a_record', 'elsewhere', 'unresolved', 'unknown'] as const;

// The answer as this card uses it. Every list is a list here: the server
// writes a list it never filled as `null` (all of them when `status` is
// `unknown`), and reading `.length` of that took the whole domain page down.
// A status this card has no words for is not guessed at; the answer is then
// unknown.
// Bu kartın kullandığı biçimiyle yanıt. Her liste burada listedir: sunucu hiç
// doldurmadığı listeyi `null` yazar (`status` `unknown` iken hepsini) ve onun
// `.length`ini okumak bütün alan adı sayfasını düşürüyordu. Kartın sözü
// olmayan bir durum tahmin edilmez; yanıt o zaman bilinmeyendir.
function decodeConnection(raw: unknown): Connection {
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) throw new Error('shape');
    const body = raw as Connection;
    if (!connectionStatuses.includes(body.status) || typeof body.server_ip !== 'string') throw new Error('field');
    return {
        ...body,
        nameservers: decodeList<string>(body.nameservers ?? null),
        live_nameservers: decodeList<string>(body.live_nameservers ?? null),
        live_ips: decodeList<string>(body.live_ips ?? null),
        resolver_observations: decodeList<NonNullable<Connection['resolver_observations']>[number]>(body.resolver_observations ?? null)
            .map((observation) => ({
                ...observation,
                nameservers: decodeList<string>(observation.nameservers ?? null),
                ips: decodeList<string>(observation.ips ?? null),
            })),
        nameserver_facts: decodeList<NonNullable<Connection['nameserver_facts']>[number]>(body.nameserver_facts ?? null)
            .map((fact) => ({ ...fact, ips: decodeList<string>(fact.ips ?? null) })),
    };
}

export function DomainConnection({ domainId, domainName }: { domainId: number; domainName: string }) {
    const { t } = useI18n();
    const connection = useRemote(`/api/v1/domains/${domainId}/connection`, decodeConnection, { init: { cache: 'no-store' } });
    const recheck = () => void connection.retry();

    // Two different things can be unknown here, and neither is "this domain
    // does not point here":
    //   - the card's own read failed: the frame stays, with the read again;
    //   - the read succeeded and the server says it could not ask the public
    //     resolvers (`status: unknown`): the card says exactly that.
    // Burada iki ayrı şey bilinmeyebilir ve hiçbiri "bu alan adı burayı
    // göstermiyor" değildir: kartın kendi okuması başarısız olabilir ya da
    // okuma başarılıdır ve sunucu genel çözümleyicilere soramadığını söyler.
    // The card keeps one least height in every state: the height it has for a
    // connected domain. The rest of the Overview then does not move when the
    // answer arrives, in either direction and in either language.
    // Kart her durumda tek bir en az yüksekliği korur: bağlı bir alan adı için
    // sahip olduğu yükseklik. Yanıt geldiğinde Genel Bakış'ın geri kalanı hiçbir
    // yönde ve hiçbir dilde yerinden oynamaz.
    const height = 'min-h-[27.25rem] sm:min-h-[15.5rem]';
    const frame = `${height} rounded-xl border border-border bg-surface p-5`;
    if (connection.remote.state === 'loading') {
        return <section className={frame}><Checking label={t('conn.checking')} /></section>;
    }
    if (connection.remote.state === 'unknown' && !connection.remote.previous) {
        return (
            <section className={frame}>
                <CouldNotCheck text={t('conn.readFailed')} onRetry={recheck} busy={connection.reading} />
            </section>
        );
    }

    return (
        <RemoteGate
            remote={connection.remote}
            checking={t('conn.checking')}
            failed={t('conn.readFailed')}
            onRetry={recheck}
            busy={connection.reading}
        >
            {({ value: c }) => {
    const externalDNS = c.dns_management_mode === 'external';
    const notChecked = c.status === 'unknown';
    const connected = c.status === 'delegated' || c.status === 'a_record';
    const stable = connected && !c.propagation_pending;
    const tone = stable
        ? 'border-success/40 bg-success/5'
        : notChecked ? 'border-border bg-surface' : 'border-warning-mark/60 bg-warning-mark/10';
    // What the public resolvers answered, in the face for literal values; or,
    // in words, that there is no answer or that they could not be asked.
    // Genel çözümleyicilerin yanıtı, harfi değerlerin yazı yüzüyle; ya da
    // sözle, yanıt olmadığı ya da onlara sorulamadığı.
    const observed = (values: string[]) => (values.length
        ? <p className="break-words font-mono text-xs text-fg">{values.join(', ')}</p>
        : <p className="text-xs text-fg-muted">{t(notChecked ? 'conn.notChecked' : 'conn.none')}</p>);

    return (
        <section className={`${height} rounded-xl border ${tone} p-5`}>
            <div className="mb-3 flex flex-wrap items-center gap-2">
                <span className="flex h-9 w-9 items-center justify-center rounded-lg bg-primary/10 text-primary">
                    <Globe className="h-4.5 w-4.5" />
                </span>
                <div className="min-w-0">
                    <h3 className="text-sm font-semibold text-fg">{t('conn.title')}</h3>
                    <p className="flex items-center gap-1.5 text-xs">
                        <StatusDot ok={stable} />
                        <span className={stable ? 'text-success' : notChecked ? 'text-fg-muted' : 'text-warning'}>
                            {c.propagation_pending
                                ? t('conn.status.propagating')
                                : t(`conn.status.${c.status}` as Parameters<typeof t>[0])}
                        </span>
                    </p>
                </div>
                <div className="ml-auto flex items-center gap-2">
                    <HelpButton serviceId="domain-connection" name={domainName} />
                    <button
                        onClick={recheck}
                        disabled={connection.reading}
                        className="inline-flex items-center gap-1.5 rounded-lg border border-border-strong bg-surface px-2.5 py-1.5 text-xs font-medium text-fg transition-colors hover:bg-surface-2 disabled:opacity-50"
                    >
                        <RefreshCw className={`h-3.5 w-3.5 ${connection.reading ? 'animate-spin' : ''}`} />
                        {t('conn.recheck')}
                    </button>
                </div>
            </div>

            {/* What the world sees right now — the fact, before any advice.
                Dünyanın şu an gördüğü — tavsiyeden önce olgu. */}
            <div className="mb-4 grid gap-2 sm:grid-cols-2">
                <div className="rounded-lg border border-border bg-surface p-3">
                    <p className="mb-1 text-xs text-fg-subtle">{t('conn.liveNs')}</p>
                    {observed(c.live_nameservers)}
                </div>
                <div className="rounded-lg border border-border bg-surface p-3">
                    <p className="mb-1 text-xs text-fg-subtle">{t('conn.liveIp')}</p>
                    {observed(c.live_ips)}
                </div>
            </div>

            {c.propagation_pending && c.resolver_observations?.length ? (
                <div className="mb-4 rounded-lg border border-warning-mark/60 bg-warning-mark/20 p-3">
                    <p className="text-sm font-medium text-fg">{t('conn.propagation.title')}</p>
                    <p className="mt-1 text-xs leading-relaxed text-fg-muted">{t('conn.propagation.desc')}</p>
                    <ul className="mt-2 space-y-1">
                        {c.resolver_observations.map((observation) => (
                            <li key={observation.resolver} className="text-xs text-fg-muted">
                                <span className="font-medium text-fg">{observation.resolver}</span>
                                {' — NS '}
                                <span className="font-mono">
                                    {observation.nameservers.length ? observation.nameservers.join(', ') : t('conn.none')}
                                </span>
                                {' · IP '}
                                <span className="font-mono">
                                    {observation.ips.length ? observation.ips.join(', ') : t('conn.none')}
                                </span>
                            </li>
                        ))}
                    </ul>
                </div>
            ) : null}

            {connected ? (
                <p className="rounded-lg bg-success/10 p-3 text-sm text-fg">
                    {c.status === 'delegated' ? t('conn.okDelegated') : t('conn.okARecord')}
                </p>
            ) : (
                <>
                    {/* Not checked is not "does not point here": say what is
                        not known, then offer the values for the case that the
                        domain is not connected yet.
                        Kontrol edilemedi, "burayı göstermiyor" değildir: neyin
                        bilinmediğini söyle, sonra alan adı henüz bağlı değilse
                        diye değerleri sun. */}
                    <p className="mb-3 max-w-[75ch] text-sm text-fg-muted">{t(notChecked ? 'conn.unknownHelp' : 'conn.intro')}</p>

                    {/* Route A is offered ONLY when this server's nameserver
                        names actually answer for this server. Otherwise the
                        instruction would break the domain — and the panel would
                        be what said to do it. / A yolu YALNIZ bu sunucunun ad
                        sunucusu adları gerçekten bu sunucu adına cevap
                        verdiğinde sunulur. Aksi hâlde talimat alan adını
                        bozardı — ve bunu söyleyen panel olurdu. */}
                    {!externalDNS && !c.nameservers_usable && notChecked && (
                        // The names could not be verified either. They are
                        // not called broken, and delegating to them is not
                        // offered until they are known to answer.
                        // Adlar da doğrulanamadı. Bozuk denmez; yanıt
                        // verdikleri bilinene dek onlara devir de sunulmaz.
                        <p className="mb-3 max-w-[75ch] rounded-lg bg-surface-2/60 p-2.5 text-xs leading-relaxed text-fg-muted">
                            {t('conn.routeAUnknown')}
                        </p>
                    )}
                    {!externalDNS && !c.nameservers_usable && !notChecked && (
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
                    {!externalDNS && (c.nameservers_usable || !notChecked) && <div className={`mb-3 rounded-xl border border-border bg-surface p-4 ${!c.nameservers_usable ? 'opacity-50' : ''}`}>
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
                    </div>}

                    {/* Route B — just point the address. / B yolu — yalnız adresi yönelt. */}
                    <div className="rounded-xl border border-border bg-surface p-4">
                        <h4 className="mb-1 text-sm font-semibold text-fg">{t(externalDNS ? 'dns.externalTitle' : 'conn.routeB.title')}</h4>
                        <p className="mb-3 text-xs leading-relaxed text-fg-muted">{t(externalDNS ? 'dns.externalHelp' : 'conn.routeB.desc')}</p>
                        <div className="space-y-1.5">
                            {externalDNS && c.required_records ? c.required_records.map((record, index) => <CopyField key={`${record.type}:${record.name}:${index}`} label={`${record.type} ${record.name}`} value={`${record.prio ? record.prio + ' ' : ''}${record.content}`} />) : <>
                                <CopyField label={`A    @`} value={c.server_ip} />
                                <CopyField label={`A    www`} value={c.server_ip} />
                                {c.server_ipv6 && <CopyField label={`AAAA @`} value={c.server_ipv6} />}
                            </>}
                        </div>
                    </div>
                </>
            )}

            {/* SSL is the most common reason someone lands here, so say plainly
                whether it can work yet - or that this is not known.
                / İnsanların buraya en sık geliş sebebi SSL'dir; bu yüzden
                şimdilik çalışıp çalışamayacağını, ya da bunun bilinmediğini
                düz söyle. */}
            <p className="mt-4 flex items-start gap-2 text-xs leading-relaxed text-fg-muted">
                {notChecked ? (
                    <Globe className="mt-0.5 h-4 w-4 shrink-0 text-fg-subtle" />
                ) : c.ssl_ready && !c.propagation_pending ? (
                    <ShieldCheck className="mt-0.5 h-4 w-4 shrink-0 text-success" />
                ) : (
                    <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
                )}
                <span>
                    {notChecked
                        ? t('conn.sslUnknown')
                        : c.ssl_ready
                            ? c.propagation_pending
                                ? t('conn.sslPropagating')
                                : t('conn.sslReady')
                            : t('conn.sslBlocked')}
                </span>
            </p>
        </section>
    );
            }}
        </RemoteGate>
    );
}
