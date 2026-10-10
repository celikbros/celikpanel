import { useMemo, useRef, useState } from 'react';
import { Globe, Plus, Trash2, ShieldCheck, Copy, AlertTriangle, RefreshCw } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { Button, Checking, CouldNotCheck, KnownEmpty, RemoteGate, ResultUnknown, inputClass } from './ui';
import { useHostingCapabilities } from '../lib/hostingCapabilities';
import { apiErrorText, readApiError } from '../lib/apiError';
import { countText, decodeListIn, lastKnown, mapRemote, useRemote, type Remote } from '../lib/remote';
import { useLostAnswer } from '../lib/lostAnswer';
import type { TranslationKey } from '../i18n/en';

interface DNSRecord {
    id: number;
    name: string;
    type: string;
    content: string;
    ttl: number;
    prio?: number;
    disabled: boolean;
}

interface DomainDNSManagerProps {
    domainId: number;
    domainName: string;
    readOnly?: boolean;
    isAdditionalUser?: boolean;
}

// What the server says about a domain's zone, its records and its signing.
// Each decoder refuses an answer that is not the contract; none fills in a
// default (lib/remote.ts).
// Sunucunun bir alan adının bölgesi, kayıtları ve imzası hakkında söylediği.
// Çözücüler sözleşme olmayan yanıtı reddeder; hiçbiri varsayılan doldurmaz.
interface Zone {
    /** The records are instructions for another provider; nothing here serves them. */
    external: boolean;
    /** The zone lives on another CelikPanel that this one may write to. */
    remote: boolean;
}

function decodeZone(raw: unknown): Zone {
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) throw new Error('shape');
    const body = raw as Record<string, unknown>;
    return {
        external: body.type === 'EXTERNAL' && body.management === 'external',
        remote: body.type === 'REMOTE' && body.management === 'existing',
    };
}

const decodeRecords = (raw: unknown) => decodeListIn<DNSRecord>(raw, 'records');

interface Signing {
    secured: boolean;
    ds: string[];
}

function decodeSigning(raw: unknown): Signing {
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) throw new Error('shape');
    const body = raw as Record<string, unknown>;
    if (typeof body.secured !== 'boolean') throw new Error('field');
    const ds = body.ds ?? [];
    if (!Array.isArray(ds) || ds.some((item) => typeof item !== 'string')) throw new Error('list');
    return { secured: body.secured, ds: ds as string[] };
}

const recordTypes = ['A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS', 'SRV'];

// Whether a list that was read again shows the record a form sent, judged only
// from the two lists (10 Oct 2026). The server writes the owner name in full
// and may rewrite a value (quotes, a trailing dot, case), so a row counts as
// the sent record only when it is NEW in the list, of the same type and owner,
// and its value is the sent one apart from those rewritings. No new row at
// all: the list does not show it. New rows of which none is clearly this one:
// not decided here; the person looks.
// Yeniden okunan listenin, formun gönderdiği kaydı gösterip göstermediği;
// yalnız iki listeden. Satır ancak listede YENİYSE, türü ve adı aynıysa ve
// değeri bu yeniden yazımlar dışında gönderilen değerse o kayıt sayılır.
const ownerName = (name: string, domainName: string) => {
    const owner = name.trim().toLowerCase().replace(/\.$/, '');
    const zone = domainName.toLowerCase();
    if (owner === '' || owner === '@') return zone;
    return owner === zone || owner.endsWith(`.${zone}`) ? owner : `${owner}.${zone}`;
};
const recordValue = (content: string) => content.trim().toLowerCase().replace(/^"|"$/g, '').replace(/\.$/, '').replace(/\s+/g, ' ');

function showsNewRecord(
    before: DNSRecord[],
    after: DNSRecord[],
    sent: { name: string; type: string; content: string },
    domainName: string,
): boolean | null {
    const had = new Set(before.map((rec) => rec.id));
    const added = after.filter((rec) => !had.has(rec.id));
    if (added.length === 0) return false;
    const owner = ownerName(sent.name, domainName);
    const value = recordValue(sent.content);
    return added.some((rec) => rec.type === sent.type && ownerName(rec.name, domainName) === owner && recordValue(rec.content) === value) ? true : null;
}

// Record-type pill colours. Categorical, readable in both themes; kept small
// and intentional rather than one hue per type.
// Kayıt-türü rozet renkleri. Kategorik, iki temada okunur; tür başına bir
// renk yerine küçük ve bilinçli tutuldu.
const typeColor: Record<string, string> = {
    A: 'bg-primary/10 text-primary',
    AAAA: 'bg-primary/10 text-primary',
    CNAME: 'bg-success/10 text-success',
    MX: 'bg-warning/15 text-warning',
    SRV: 'bg-warning/15 text-warning',
};

export function DomainDNSManager({
    domainId,
    domainName,
    readOnly = false,
    isAdditionalUser = false,
}: DomainDNSManagerProps) {
    const { t } = useI18n();
    const [showAddForm, setShowAddForm] = useState(false);
    const [newType, setNewType] = useState('A');
    const [newName, setNewName] = useState('@');
    const [newContent, setNewContent] = useState('');
    const [newTTL, setNewTTL] = useState(3600);
    const [newPrio, setNewPrio] = useState(0);
    const [publishing, setPublishing] = useState(false);
    const [mutatingRecord, setMutatingRecord] = useState(false);
    // What the add form holds now, for the one question asked after a lost
    // answer: is the form still the one that was sent?
    // Ekleme formunun şu an tuttuğu; yanıt yitince sorulan tek soru için.
    const typed = useRef('');
    typed.current = JSON.stringify([newType, newName, newContent, newTTL, newPrio]);

    // Whether anything actually serves this zone, from the one shared read of
    // the server's capabilities (lib/hostingCapabilities.ts). `dnsServer` is
    // '' when the server is KNOWN to have no active DNS engine (records are
    // saved but not published), "pdns"/"bind" when it has one, and null while
    // that is being checked or could not be checked - the "not served" banner
    // appears only once we know for sure. Team members deliberately cannot
    // inspect server-global service inventory, so nothing is read for them;
    // domain records, zone state and DNSSEC remain available through their
    // tenant-scoped endpoints below.
    // Bu zone'u fiilen bir şeyin sunup sunmadığı; sunucu yeteneklerinin tek
    // ortak okumasından. `dnsServer`, sunucuda etkin DNS motoru olmadığı
    // BİLİNİYORSA '' (kayıtlar kayıtlı ama yayınlanmıyor), varsa "pdns"/"bind",
    // kontrol edilirken ya da edilemediğinde null'dır — "yayınlanmıyor" bandı
    // ancak kesin bilince görünür. Ekip üyeleri için hiçbir şey okunmaz.
    const capabilities = useHostingCapabilities({ enabled: !isAdditionalUser });
    const dnsServer = !isAdditionalUser && capabilities.remote.state === 'known' ? capabilities.remote.value.dns_server : null;

    // The zone is addressed by its domain, so the server's 404 is an answer:
    // this domain has no zone. Every other refusal, a dropped connection and an
    // answer that is not the contract are "could not check", never "no zone".
    // The records are read only for a zone the server said exists.
    // Bölge alan adıyla adreslenir; sunucunun 404'ü bir yanıttır: bu alan
    // adının bölgesi yok. Diğer her ret, kopan bağlantı ve sözleşme olmayan
    // yanıt "kontrol edilemedi"dir, asla "bölge yok" değil. Kayıtlar yalnız
    // sunucunun var dediği bölge için okunur.
    const zone = useRemote(`/api/v1/domains/${domainId}/dns/zone`, decodeZone);
    const zoneRemote = useMemo<Remote<Zone | null>>(() => (
        zone.remote.state === 'unknown' && zone.remote.status === 404
            ? { state: 'known', value: null, observedAt: Date.now() }
            : zone.remote
    ), [zone.remote]);
    const zoneShown = lastKnown(zoneRemote);
    const zoneValue = zoneShown?.value ?? null;
    const records = useRemote(zoneValue ? `/api/v1/domains/${domainId}/dns/records` : null, decodeRecords);
    const externalDNS = zoneValue?.external === true;
    const remoteDNS = zoneValue?.remote === true;

    // A change whose answer was lost: nothing is sent again, the zone and its
    // records are read again, and nothing here changes anything until they are.
    // Yanıtı yiten değişiklik: hiçbir şey yeniden gönderilmez, bölge ve
    // kayıtları yeniden okunur; okunana dek buradan hiçbir şey değiştirilmez.
    const answer = useLostAnswer(async () => {
        const nextZone = await zone.retry();
        if (nextZone.state !== 'known') return nextZone.state === 'unknown' && nextZone.status === 404 ? [] : nextZone;
        return [nextZone, await records.retry()];
    });
    // The list a row is removed from, or added to, is the one the server last
    // sent and is not being read again.
    // Satırın silindiği ya da eklendiği liste, sunucunun son gönderdiği ve
    // yeniden okunmayan listedir.
    const recordsKnown = records.remote.state === 'known' && !records.reading;
    const canChange = !readOnly && !externalDNS && zoneRemote.state === 'known' && !answer.holding;

    const refused = async (res: Response, fallback: TranslationKey) => {
        showToast('error', apiErrorText(await readApiError(res), t, fallback));
    };

    const publishZone = async () => {
        if (!canChange) return;
        setPublishing(true);
        try {
            const res = await answer.send(`/api/v1/domains/${domainId}/dns/zone`, { method: 'POST' });
            if (!res) return;
            if (!res.ok) {
                await refused(res, 'dns.zonePublishFailed');
                return;
            }
            let created = false;
            try {
                created = ((await res.json()) as { created?: boolean }).created === true;
            } catch {
                created = false;
            }
            showToast('success', t(created ? 'dns.zoneCreated' : 'dns.zonePublished'));
            answer.settle();
            await zone.retry();
            await records.retry();
        } finally {
            setPublishing(false);
        }
    };

    const addRecord = async () => {
        if (!canChange || !recordsKnown || records.remote.state !== 'known') return;
        setMutatingRecord(true);
        try {
            // If the answer is lost, the records that are read again decide
            // what the form does: a new row that is this record closes and
            // clears it, so the same record is not one press from being sent
            // twice; a list without a new row leaves what was typed.
            // Yanıt yiterse formun ne yapacağına yeniden okunan kayıtlar karar
            // verir: bu kayıt olan yeni satır formu kapatır ve temizler; yeni
            // satırı olmayan liste yazılanı yerinde bırakır.
            const sent = { name: newName, type: newType, content: newContent, ttl: newTTL, prio: newPrio };
            const before = records.remote.value;
            const form = typed.current;
            const res = await answer.send(`/api/v1/domains/${domainId}/dns/records`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(sent),
            }, {
                shows: (read) => (read.length === 2 ? showsNewRecord(before, read[1].value as DNSRecord[], sent, domainName) : null),
                made: () => {
                    if (typed.current !== form) return;
                    setShowAddForm(false);
                    setNewContent('');
                },
            });
            if (!res) return;
            if (!res.ok) {
                await refused(res, 'dns.recordAddFailed');
                return;
            }
            showToast('success', t('dns.recordAdded'));
            answer.settle();
            setShowAddForm(false);
            setNewContent('');
            await records.retry();
        } finally {
            setMutatingRecord(false);
        }
    };

    const deleteRecord = async (id: number) => {
        if (!canChange || !recordsKnown) return;
        if (!confirm(t('dns.confirmDelete'))) return;
        setMutatingRecord(true);
        try {
            const res = await answer.send(`/api/v1/domains/${domainId}/dns/records?id=${id}`, { method: 'DELETE' });
            if (!res) return;
            if (!res.ok) {
                await refused(res, 'dns.recordDeleteFailed');
                return;
            }
            showToast('success', t('dns.recordDeleted'));
            answer.settle();
            await records.retry();
        } finally {
            setMutatingRecord(false);
        }
    };

    if (zoneRemote.state === 'loading') return <Checking label={t('dns.checking')} />;

    if (!zoneShown) {
        return (
            <CouldNotCheck
                text={t('dns.zoneUnknown', { name: domainName })}
                onRetry={() => void zone.retry()}
                busy={zone.reading}
            />
        );
    }

    if (zoneShown.value === null) {
        return (
            <div>
                <ResultUnknown answer={answer} className="mb-4" />
                <KnownEmpty
                    of={zoneShown}
                    icon={Globe}
                    title={t('dns.zoneMissing')}
                    hint={t('dns.zoneMissingHint', { name: domainName })}
                    action={
                        readOnly ? undefined : (
                            <Button variant="primary" icon={Plus} disabled={publishing || !canChange} onClick={publishZone}>
                                {publishing ? t('dns.publishing') : t('dns.enableZone')}
                            </Button>
                        )
                    }
                />
            </div>
        );
    }

    const needsPrio = newType === 'MX' || newType === 'SRV';
    const mayEdit = !readOnly && !externalDNS;

    return (
        <div>
            {/* The zone itself could not be read again: what is below is the
                earlier answer, and the page says so once, here.
                Bölgenin kendisi yeniden okunamadı: aşağıdaki önceki yanıttır;
                sayfa bunu bir kez, burada söyler. */}
            {zoneRemote.state === 'unknown' && (
                <CouldNotCheck
                    className="mb-4"
                    text={t('dns.zoneUnknown', { name: domainName })}
                    onRetry={() => void zone.retry()}
                    busy={zone.reading}
                />
            )}
            {remoteDNS && <p className="mb-5 rounded-lg border border-border bg-surface-2 p-4 text-sm">{t('dns.remoteHelp')}</p>}
            {externalDNS && <section className="mb-5 rounded-lg border border-border bg-surface-2 p-4"><h3 className="font-semibold">{t('dns.externalTitle')}</h3><p className="mt-2 max-w-3xl text-sm leading-6 text-fg-muted">{t('dns.externalHelp')}</p></section>}
            {/* Honesty first: the records below are real, editable panel data,
                but with no DNS server installed NOTHING serves them — say so
                loudly instead of letting 13 rows look live. DNSSEC signing
                needs the DNS server's tooling, so that card only exists when
                one is installed.
                Önce dürüstlük: aşağıdaki kayıtlar gerçek, düzenlenebilir panel
                verisidir ama DNS sunucusu kurulu değilken onları HİÇBİR ŞEY
                yayınlamaz — 13 satırı canlı gibi bırakmak yerine bunu açıkça
                söyle. DNSSEC imzalama DNS sunucusunun aracını ister; o kart
                yalnız biri kuruluyken var olur. */}
            {!externalDNS && !remoteDNS && dnsServer === '' && (
                <div className="mb-4 flex items-start gap-2 rounded-lg border border-warning-mark/50 bg-warning-mark/20 p-3 text-sm text-fg">
                    <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
                    <span>{t('dns.notServed')}</span>
                </div>
            )}
            {isAdditionalUser && !externalDNS && !remoteDNS && (
                <div className="mb-4 flex items-start gap-2 rounded-lg border border-info/30 bg-info/10 p-3 text-sm text-fg">
                    <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-info" />
                    <span>{t('dns.teamServerStatusUnavailable')}</span>
                </div>
            )}
            {/* Being checked: one quiet line. Could not be checked: that, and the
                read again. Neither says the zone is not served.
                Kontrol ediliyor: sakin tek satır. Kontrol edilemedi: bu ve
                okumanın yeniden sunulması. Hiçbiri zone yayınlanmıyor demez. */}
            {!externalDNS && !remoteDNS && !isAdditionalUser && capabilities.remote.state === 'loading' && (
                <Checking className="mb-4" label={t('dns.checkingServer')} />
            )}
            {!externalDNS && !remoteDNS && !isAdditionalUser && capabilities.remote.state === 'unknown' && (
                <CouldNotCheck
                    className="mb-4"
                    text={apiErrorText(capabilities.remote.reason, t, 'dns.statusUnavailable')}
                    onRetry={() => void capabilities.retry()}
                    busy={capabilities.reading}
                />
            )}
            {!externalDNS && !remoteDNS && (isAdditionalUser || (dnsServer !== null && dnsServer !== '')) && (
                <DNSSECSection domainId={domainId} domainName={domainName} readOnly={readOnly} />
            )}

            <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
                <span className="text-xs text-fg-subtle">
                    {t('common.itemsTotal', { n: countText(mapRemote(records.remote, (list) => list.length)) })}
                </span>
                <div className="flex flex-wrap items-center gap-2">
                    {readOnly || externalDNS ? (
                        <Button variant="secondary" icon={RefreshCw} disabled={records.reading} onClick={() => void records.retry()}>
                            {t('dns.refresh')}
                        </Button>
                    ) : (
                        <>
                            <Button
                                variant="secondary"
                                icon={RefreshCw}
                                disabled={publishing || !canChange || (!remoteDNS && dnsServer === '')}
                                onClick={publishZone}
                            >
                                {publishing ? t('dns.publishing') : t('dns.republish')}
                            </Button>
                            <Button
                                variant="primary"
                                icon={Plus}
                                disabled={!canChange || !recordsKnown}
                                onClick={() => setShowAddForm((s) => !s)}
                            >
                                {t('dns.addRecord')}
                            </Button>
                        </>
                    )}
                </div>
            </div>

            <ResultUnknown answer={answer} className="mb-4" />

            {!readOnly && !externalDNS && showAddForm && (
                <div className="mb-4 rounded-lg border border-border bg-surface-2/50 p-4">
                    <div className="grid grid-cols-1 gap-3 sm:grid-cols-12">
                        <label className="sm:col-span-2">
                            <span className="mb-1 block text-xs text-fg-muted">{t('dns.type')}</span>
                            <select value={newType} onChange={(e) => setNewType(e.target.value)} className={inputClass}>
                                {recordTypes.map((rt) => (
                                    <option key={rt} value={rt}>
                                        {rt}
                                    </option>
                                ))}
                            </select>
                        </label>
                        <label className="sm:col-span-3">
                            <span className="mb-1 block text-xs text-fg-muted">{t('dns.name')}</span>
                            <input value={newName} onChange={(e) => setNewName(e.target.value)} placeholder={t('dns.nameHint')} className={inputClass} />
                        </label>
                        <label className={needsPrio ? 'sm:col-span-4' : 'sm:col-span-5'}>
                            <span className="mb-1 block text-xs text-fg-muted">{t('dns.content')}</span>
                            <input value={newContent} onChange={(e) => setNewContent(e.target.value)} placeholder="192.168.1.1" className={inputClass} />
                        </label>
                        <label className="sm:col-span-2">
                            <span className="mb-1 block text-xs text-fg-muted">{t('dns.ttl')}</span>
                            <input type="number" value={newTTL} onChange={(e) => setNewTTL(parseInt(e.target.value))} className={inputClass} />
                        </label>
                        {needsPrio && (
                            <label className="sm:col-span-1">
                                <span className="mb-1 block text-xs text-fg-muted">{t('dns.priority')}</span>
                                <input type="number" value={newPrio} onChange={(e) => setNewPrio(parseInt(e.target.value))} className={inputClass} />
                            </label>
                        )}
                    </div>
                    <div className="mt-3 flex justify-end gap-2">
                        <Button variant="secondary" onClick={() => setShowAddForm(false)}>
                            {t('dns.cancel')}
                        </Button>
                        <Button variant="primary" icon={Plus} disabled={mutatingRecord || !canChange || !recordsKnown} onClick={addRecord}>
                            {t('dns.save')}
                        </Button>
                    </div>
                </div>
            )}

            <RemoteGate
                remote={records.remote}
                checking={t('dns.records.checking')}
                failed={t('dns.records.unknown')}
                onRetry={() => void records.retry()}
                busy={records.reading}
            >
                {(shown) => shown.value.length === 0 ? (
                    <KnownEmpty of={shown} icon={Globe} title={t('dns.noRecords')} />
                ) : (
                    <div className="overflow-x-auto rounded-lg border border-border">
                        <table className="w-full text-sm">
                            <thead>
                                <tr className="border-b border-border text-left text-xs font-semibold text-fg-muted">
                                    <th className="px-4 py-2.5">{t('dns.type')}</th>
                                    <th className="px-4 py-2.5">{t('dns.name')}</th>
                                    <th className="px-4 py-2.5">{t('dns.content')}</th>
                                    <th className="px-4 py-2.5">{t('dns.ttl')}</th>
                                    {mayEdit && <th className="row-actions px-4 py-2.5" />}
                                </tr>
                            </thead>
                            <tbody>
                                {shown.value.map((rec) => {
                                    const managedRecord = rec.type === 'SOA' || (rec.type === 'NS' && rec.name === domainName);
                                    return (
                                    <tr key={rec.id} className="border-b border-border last:border-0 hover:bg-surface-2/60">
                                        <td className="px-4 py-2.5">
                                            <span className={`rounded px-1.5 py-0.5 text-xs font-semibold ${typeColor[rec.type] ?? 'bg-surface-2 text-fg-muted'}`}>
                                                {rec.type}
                                            </span>
                                        </td>
                                        <td className="px-4 py-2.5 font-medium text-fg">{rec.name}</td>
                                        {/* A least width: without it a phone squeezes
                                            a long value to one character a line and
                                            the row grows to two screens. The table
                                            scrolls sideways instead; the row's action
                                            stays at its edge (row-actions).
                                            En az genişlik: yoksa telefon uzun değeri
                                            satır başına tek karaktere sıkıştırır. */}
                                        <td className="min-w-[13rem] break-all px-4 py-2.5 font-mono text-fg-muted">
                                            {rec.prio ? <span className="mr-1.5 text-warning">[{rec.prio}]</span> : null}
                                            {rec.content}
                                        </td>
                                        <td className="px-4 py-2.5 text-fg-muted">{rec.ttl}</td>
                                        {mayEdit && (
                                            <td className="row-actions px-4 py-2.5 text-right">
                                                <button
                                                    onClick={() => deleteRecord(rec.id)}
                                                    disabled={managedRecord || mutatingRecord || shown.stale || !recordsKnown || !canChange}
                                                    aria-label={t('dns.confirmDelete')}
                                                    title={managedRecord ? t('dns.managedRecord') : t('dns.confirmDelete')}
                                                    className="rounded-md p-1.5 text-fg-subtle transition-colors hover:bg-surface-2 hover:text-danger disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-transparent disabled:hover:text-fg-subtle"
                                                >
                                                    <Trash2 className="h-4 w-4" />
                                                </button>
                                            </td>
                                        )}
                                    </tr>
                                    );
                                })}
                            </tbody>
                        </table>
                    </div>
                )}
            </RemoteGate>
        </div>
    );
}


// A DS record is "KeyTag Algorithm DigestType Digest ; ( comment )". Registrars
// almost always want those four as separate form fields, so we split them.
// DS kaydı "KeyTag Algorithm DigestType Digest ; ( yorum )" biçimindedir. Kayıt
// operatörleri neredeyse her zaman bu dördünü ayrı alan olarak ister; ayırırız.
function parseDS(rec: string): { keyTag: string; algo: string; digestType: string; digest: string } {
    const main = rec.split(';')[0].trim();
    const parts = main.split(/\s+/);
    return {
        keyTag: parts[0] ?? '',
        algo: parts[1] ?? '',
        digestType: parts[2] ?? '',
        digest: parts.slice(3).join(''),
    };
}

// Human-readable names for the numeric codes, so the operator recognises the
// dropdown option at the registrar.
// Sayısal kodların insan-okur adları; operatör kayıt operatöründeki açılır
// seçeneği tanısın diye.
const algoLabel: Record<string, string> = {
    '8': 'RSA/SHA-256 (8)',
    '10': 'RSA/SHA-512 (10)',
    '13': 'ECDSA P-256/SHA-256 (13)',
    '14': 'ECDSA P-384/SHA-384 (14)',
    '15': 'Ed25519 (15)',
};
const digestLabel: Record<string, string> = {
    '1': 'SHA-1 (1)',
    '2': 'SHA-256 (2)',
    '4': 'SHA-384 (4)',
};

// One labelled, individually-copyable DS field.
// Etiketli, tek tek kopyalanabilir bir DS alanı.
function DSField({ label, value, note, mono }: { label: string; value: string; note?: string; mono?: boolean }) {
    const { t } = useI18n();
    return (
        <div className="min-w-0">
            <div className="mb-0.5 text-xs font-medium text-fg-subtle">{label}</div>
            <div className="flex items-center gap-1.5">
                <span className={`min-w-0 flex-1 truncate rounded bg-surface px-2 py-1 text-sm text-fg ${mono ? 'font-mono text-xs' : ''}`} title={value}>
                    {value}
                </span>
                <button
                    onClick={() => navigator.clipboard.writeText(value).then(() => showToast('success', t('vpn.copied')))}
                    title={t('vpn.copy')}
                    className="shrink-0 rounded-md p-1 text-fg-muted hover:bg-surface-2 hover:text-fg"
                >
                    <Copy className="h-3.5 w-3.5" />
                </button>
            </div>
            {note && <div className="mt-0.5 text-xs text-fg-subtle">{note}</div>}
        </div>
    );
}

// DNSSEC: sign the zone in one click and hand the operator the DS records to
// enter at the registrar. Without that DS, validators treat the zone (and
// its DANE/TLSA records) as insecure — so both live together here.
//
// Whether the zone is signed is read from the server. While it is read the
// card is already in its place with one quiet line; "sign the zone" exists
// only for an answer that says the zone is not signed. A read that failed is
// its own notice in the card's place, and says the zone is not thereby
// unsigned.
// DNSSEC: zone'u tek tıkla imzala ve operatöre registrar'a girilecek DS
// kayıtlarını ver. O DS olmadan doğrulayıcılar zone'u (ve DANE/TLSA
// kayıtlarını) güvensiz sayar — o yüzden ikisi burada birlikte yaşar.
// Bölgenin imzalı olup olmadığı sunucudan okunur. Okunurken kart yerindedir
// ve sakin tek satır gösterir; "bölgeyi imzala" yalnız bölgenin imzalı
// olmadığını söyleyen yanıt için vardır.
function DNSSECSection({ domainId, readOnly = false }: { domainId: number; domainName: string; readOnly?: boolean }) {
    const { t } = useI18n();
    const signing = useRemote(`/api/v1/domains/${domainId}/dnssec`, decodeSigning);
    const answer = useLostAnswer(() => signing.retry());
    const [busy, setBusy] = useState(false);

    const sign = async () => {
        if (readOnly || answer.holding || signing.remote.state !== 'known') return;
        setBusy(true);
        try {
            const res = await answer.send(`/api/v1/domains/${domainId}/dnssec`, { method: 'POST' });
            if (!res) return;
            if (!res.ok) {
                showToast('error', apiErrorText(await readApiError(res), t));
                return;
            }
            showToast('success', t('dnssec.signed'));
            answer.settle();
            await signing.retry();
        } finally {
            setBusy(false);
        }
    };

    const shown = lastKnown(signing.remote);
    if (signing.remote.state === 'unknown' && !shown) {
        return (
            <CouldNotCheck
                className="mb-5"
                text={t('dnssec.unknown')}
                onRetry={() => void signing.retry()}
                busy={signing.reading}
            />
        );
    }
    const secured = shown?.value.secured === true;

    return (
        <>
            {signing.remote.state === 'unknown' && (
                <CouldNotCheck
                    className="mb-4"
                    text={t('dnssec.unknown')}
                    onRetry={() => void signing.retry()}
                    busy={signing.reading}
                />
            )}
            <section className="mb-5 rounded-xl border border-border bg-surface p-5">
                <div className="mb-1 flex items-center gap-2">
                    <ShieldCheck className={`h-4 w-4 ${secured ? 'text-success' : 'text-fg-muted'}`} />
                    <h3 className="text-sm font-semibold text-fg">DNSSEC</h3>
                    {secured && (
                        <span className="rounded-md bg-success/10 px-2 py-0.5 text-xs font-medium text-success">
                            {t('dnssec.on')}
                        </span>
                    )}
                </div>
                <ResultUnknown answer={answer} className="my-3" />
                {!shown ? (
                    // The checking line stands in the room the usual answer
                    // (not signed) will take, at any width and in either
                    // language, so the records below do not move when it
                    // arrives. That room is measured by the answer's own
                    // layout, which is not drawn, not read out and cannot be
                    // used: nothing here says the zone is unsigned.
                    // Kontrol satırı, olağan yanıtın (imzasız) kaplayacağı
                    // yerde durur; alttaki kayıtlar yanıt gelince oynamaz. O
                    // yer, yanıtın kendi yerleşimiyle ölçülür; bu yerleşim
                    // çizilmez, okunmaz ve kullanılamaz.
                    <div className="relative">
                        <div className="invisible flex flex-wrap items-center justify-between gap-3" aria-hidden="true">
                            <p className="text-sm">{t('dnssec.offHint')}</p>
                            {!readOnly && <Button variant="primary" disabled>{t('dnssec.sign')}</Button>}
                        </div>
                        <div className="absolute inset-0 flex items-center">
                            <Checking label={t('dnssec.checking')} />
                        </div>
                    </div>
                ) : secured ? (
                    <>
                        <p className="mb-3 text-sm text-fg-muted">{t('dnssec.dsHint')}</p>
                        {/* Field-by-field: most registrars (Hostinger et al.) ask for
                            Key Tag / Algorithm / Digest Type / Digest separately, not
                            the raw line. Show both — the fields to fill the form, the
                            raw line for registrars that take one string.
                            / Alan-alan: çoğu kayıt operatörü (Hostinger vb.) Key Tag /
                            Algorithm / Digest Type / Digest'i ayrı ayrı ister, ham
                            satırı değil. İkisini de göster. */}
                        <div className="space-y-3">
                            {shown.value.ds.map((rec) => {
                                const f = parseDS(rec);
                                return (
                                    <div key={rec} className="rounded-lg border border-border bg-surface-2/40 p-3">
                                        <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                                            <DSField label={t('dnssec.keyTag')} value={f.keyTag} />
                                            <DSField label={t('dnssec.algorithm')} value={f.algo} note={algoLabel[f.algo]} />
                                            <DSField label={t('dnssec.digestType')} value={f.digestType} note={digestLabel[f.digestType]} />
                                            <DSField label={t('dnssec.digest')} value={f.digest} mono />
                                        </div>
                                        <div className="mt-2 flex items-center gap-2 border-t border-border pt-2">
                                            <span className="shrink-0 text-xs text-fg-subtle">{t('dnssec.rawRecord')}</span>
                                            <code className="min-w-0 flex-1 overflow-x-auto rounded bg-surface-2 px-2 py-1 font-mono text-xs text-fg-muted">
                                                {rec}
                                            </code>
                                            <button
                                                onClick={() => navigator.clipboard.writeText(rec).then(() => showToast('success', t('vpn.copied')))}
                                                title={t('vpn.copy')}
                                                className="shrink-0 rounded-md p-1.5 text-fg-muted hover:bg-surface-2 hover:text-fg"
                                            >
                                                <Copy className="h-4 w-4" />
                                            </button>
                                        </div>
                                    </div>
                                );
                            })}
                        </div>
                    </>
                ) : (
                    <div className="flex flex-wrap items-center justify-between gap-3">
                        <p className="text-sm text-fg-muted">{t('dnssec.offHint')}</p>
                        {!readOnly && (
                            <Button
                                variant="primary"
                                disabled={busy || answer.holding || signing.remote.state !== 'known' || signing.reading}
                                onClick={sign}
                            >
                                {t('dnssec.sign')}
                            </Button>
                        )}
                    </div>
                )}
            </section>
        </>
    );
}
