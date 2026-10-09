import { useEffect, useRef, useState } from 'react';
import { AlertTriangle, DownloadCloud, FolderInput, Eye, Mail, ArrowRight, Network, Database, FileText, CheckCircle2, XCircle, Info } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { readApiError, type ApiError } from '../lib/apiError';
import { siteWebServerRefusedIn } from '../lib/siteWebServerRefused';
import type { TranslationKey } from '../i18n/en';
import { Button, Checking, CouldNotCheck, ErrorBanner, inputClass } from './ui';
import { useNavigate } from '../router';
import { decodeList, readRemote, useRemote } from '../lib/remote';
import { SUBSCRIPTIONS_URL, decodeSubscriptions } from '../lib/subscriptions';
import { PageHeader } from './PageHeader';
import { REQUEST_ID_HEADER, newRequestId } from '../lib/requestIdentity';
import { unansweredCause, type LostCause } from '../lib/lostAnswer';

// cPanel import wizard (roadmap 3B): source → preview → result. Nothing is
// applied until the operator sees the honest preview and confirms; the
// result screen reports every step's real outcome, not a single OK/fail.
//
// cPanel içe aktarım sihirbazı (yol haritası 3B): kaynak → önizleme → sonuç.
// Operatör dürüst önizlemeyi görüp onaylayana dek hiçbir şey uygulanmaz;
// sonuç ekranı her adımın gerçek sonucunu raporlar, tek bir tamam/hata değil.

// What the server says about an archive before anything is imported. About a
// mailbox it says three things: its address, its quota, and whether the
// archive holds a password that the import will keep (11 Oct 2026). The
// password's hash stays on the server; this page never receives it.
//
// Sunucunun, hiçbir şey içe aktarılmadan önce bir arşiv hakkında söylediği.
// Bir posta kutusu için üç şey söyler: adresi, kotası ve arşivde içe aktarımın
// koruyacağı bir parolanın olup olmadığı. Parolanın özeti sunucuda kalır; bu
// sayfa onu hiçbir zaman almaz.
interface Preview {
    username: string;
    main_domain: string;
    domains: string[];
    public_html: boolean;
    site_bytes: number;
    mail_accounts: { domain: string; user: string; quota_mb: number; has_password: boolean }[];
    forwarders: { source: string; destination: string }[];
    dns_zones: Record<string, unknown[]>;
    databases: { name: string; dump_bytes: number }[];
}

// An answer that is not a preview is not drawn as one: no list is made up for
// a field that did not arrive.
// Önizleme olmayan bir yanıt önizleme diye çizilmez.
function readPreview(raw: unknown): Preview | null {
    if (!raw || typeof raw !== 'object') return null;
    const p = raw as Partial<Preview>;
    const lists = [p.domains, p.mail_accounts, p.forwarders, p.databases];
    if (!lists.every(Array.isArray) || !p.dns_zones || typeof p.dns_zones !== 'object') return null;
    return {
        username: String(p.username ?? ''),
        main_domain: String(p.main_domain ?? ''),
        domains: p.domains!.map(String),
        public_html: p.public_html === true,
        site_bytes: Number(p.site_bytes) || 0,
        mail_accounts: p.mail_accounts!.map((m) => ({
            domain: String(m?.domain ?? ''),
            user: String(m?.user ?? ''),
            quota_mb: Number(m?.quota_mb) || 0,
            has_password: m?.has_password === true,
        })),
        forwarders: p.forwarders!,
        dns_zones: p.dns_zones as Record<string, unknown[]>,
        databases: p.databases!.map((d) => ({ name: String(d?.name ?? ''), dump_bytes: Number(d?.dump_bytes) || 0 })),
    };
}

interface StepResult {
    step: string;
    ok: boolean;
    detail: string;
}

// The result of an import whose every step has ended (11 Oct 2026). It is
// either complete, or a verified partial result: the parts that were imported
// and the parts that were not, by name. It is never "pending": nothing is
// still running, and nothing completes it by itself.
//
// Her adımı bitmiş bir içe aktarımın sonucu. Ya tamdır ya da doğrulanmış kısmi
// bir sonuçtur. Asla "beklemede" değildir: süren bir şey yoktur.
interface ImportResult {
    domain: string;
    partial: boolean;
    imported: string[];
    notImported: string[];
    steps: StepResult[];
}

function readImportResult(raw: unknown, domain: string): ImportResult | null {
    if (!raw || typeof raw !== 'object') return null;
    const data = raw as { steps?: unknown; status?: unknown; domain?: unknown };
    if (!Array.isArray(data.steps)) return null;
    const steps: StepResult[] = data.steps.map((s) => ({
        step: String((s as StepResult)?.step ?? ''),
        ok: (s as StepResult)?.ok === true,
        detail: String((s as StepResult)?.detail ?? ''),
    }));
    // The lists are the steps', so a part is listed exactly as its own step
    // ended. Marking the domain as finished is not a part of the archive.
    // Listeler adımlardan gelir; bir parça, kendi adımı nasıl bittiyse öyle listelenir.
    const parts = steps.filter((s) => s.step !== 'finalize');
    return {
        domain: typeof data.domain === 'string' && data.domain ? data.domain : domain,
        partial: data.status === 'partial' || steps.some((s) => !s.ok),
        imported: parts.filter((s) => s.ok).map((s) => s.step),
        notImported: parts.filter((s) => !s.ok).map((s) => s.step),
        steps,
    };
}

// A step's name as the server sends it ("files", "mail:info@example.com",
// "database:shop") in the page's own words.
// Sunucunun gönderdiği adım adı, sayfanın kendi sözleriyle.
const partKeys: Record<string, TranslationKey> = {
    domain: 'import.part.domain',
    files: 'import.part.files',
    mail: 'import.part.mail',
    forwarders: 'import.part.forwarders',
    dns: 'import.part.dns',
    databases: 'import.part.databases',
    finalize: 'import.part.finalize',
};
const namedPartKeys: Record<string, TranslationKey> = {
    mail: 'import.part.mailbox',
    forwarder: 'import.part.forwarder',
    database: 'import.part.database',
    // An entry of the archive the files step refused by its name, and the
    // count of those that are not listed one by one (12 Oct 2026).
    // Dosya adımının adı yüzünden reddettiği arşiv girdisi ve tek tek
    // listelenmeyenlerin sayısı.
    member: 'import.part.member',
    members: 'import.part.moreMembers',
};
type Say = (key: TranslationKey, vars?: Record<string, string | number>) => string;

function partLabel(step: string, t: Say): string {
    if (partKeys[step]) return t(partKeys[step]);
    const colon = step.indexOf(':');
    const named = colon > 0 ? namedPartKeys[step.slice(0, colon)] : undefined;
    return named ? t(named, { name: step.slice(colon + 1) }) : step;
}

// The lines of the server's that this page has its own words for.
const noPasswordDetail = 'not imported: the archive holds no password for this mailbox';
const absoluteMemberDetail = "not imported: the archive names this entry with an absolute path, and an import writes only below the site's own folder; nothing was written for it";
const detailKeys: Record<string, TranslationKey> = {
    [noPasswordDetail]: 'import.detail.noPassword',
    [absoluteMemberDetail]: 'import.detail.absoluteMember',
};

// An archive entry that was refused by its name is not a chosen part that is
// missing: when nothing else is missing, every chosen part was imported and
// the domain is in service.
// Adı yüzünden reddedilen arşiv girdisi, eksik kalan seçilmiş bir parça
// değildir: başka eksik yoksa seçilen her parça içe aktarılmıştır.
const refusedEntry = (step: string) => step.startsWith('member:') || step.startsWith('members:');

type Stage = 'source' | 'preview' | 'result';

// The apply request carries an identity the server keeps for a day (D-029):
// when its answer is lost the same request is asked for once more under the
// same identity, and the server answers it from the first run instead of
// importing twice. This state is what remains when that second asking got no
// answer either (`why` is `asked`), or when the Panel itself answered that the
// import is still running (`running`) or that it restarted while the import
// ran (`interrupted`): the notice says which. What can still be read is
// whether the domain the import creates first is on the server now.
// `unchecked` is "nobody has looked yet"; none of these is "the import failed".
//
// Uygulama isteği, sunucunun bir gün tuttuğu bir kimlik taşır (D-029): yanıtı
// kaybolunca aynı istek aynı kimlikle bir kez daha sorulur ve sunucu onu iki
// kez içe aktarmak yerine ilk çalışmadan yanıtlar. Bu durum, o ikinci sorunun da
// yanıtsız kaldığı zaman kalandır. Bunların hiçbiri "içe aktarım başarısız"
// değildir.
type UnknownResult = {
    check: 'unchecked' | 'checking' | 'unreadable' | 'absent' | 'present';
    domain: string;
    why: LostCause;
};

const unknownBody: Record<LostCause, 'import.unknown.body' | 'import.unknown.bodyRunning' | 'import.unknown.bodyInterrupted'> = {
    dropped: 'import.unknown.body',
    asked: 'import.unknown.body',
    running: 'import.unknown.bodyRunning',
    interrupted: 'import.unknown.bodyInterrupted',
};

const namesDomain = (row: unknown, domain: string) =>
    !!row && typeof row === 'object' && String((row as { domain_name?: unknown }).domain_name ?? '').toLowerCase() === domain.toLowerCase();

export function ImportPage() {
    const { t } = useI18n();
    const [stage, setStage] = useState<Stage>('source');
    const [path, setPath] = useState('');
    const [busy, setBusy] = useState(false);
    const [preview, setPreview] = useState<Preview | null>(null);

    const navigate = useNavigate();
    const subs = useRemote(SUBSCRIPTIONS_URL, decodeSubscriptions);
    const [unknownResult, setUnknownResult] = useState<UnknownResult | null>(null);
    // The notice takes the place of the button that was pressed, at the end of
    // a long column; it is brought into view when it appears.
    // Bildirim, basılan düğmenin yerini uzun bir sütunun sonunda alır;
    // belirdiğinde görünür alana getirilir.
    const unknownNotice = useRef<HTMLDivElement>(null);
    const resultUnknown = unknownResult !== null;
    useEffect(() => {
        if (resultUnknown) unknownNotice.current?.scrollIntoView?.({ block: 'nearest' });
    }, [resultUnknown]);
    const [targetDomain, setTargetDomain] = useState('');
    const [subID, setSubID] = useState(0);
    const [opts, setOpts] = useState({ files: true, mail: true, dns: true, databases: true });
    const [result, setResult] = useState<ImportResult | null>(null);
    // A refusal the Panel answered: it stays on the page until the next
    // attempt, with every word of it readable.
    // Panel'in verdiği ret: bir sonraki denemeye dek sayfada kalır.
    const [refusal, setRefusal] = useState<ApiError | null>(null);
    // The apply request whose answer has not arrived yet: its identity and the
    // exact body it was sent with.
    // Yanıtı henüz gelmemiş uygulama isteği: kimliği ve gönderildiği gövde.
    const applyRequest = useRef<{ id: string; body: string } | null>(null);

    const inspect = async () => {
        setRefusal(null);
        setBusy(true);
        try {
            const res = await fetch('/api/v1/import/cpanel/inspect', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ path }),
            });
            if (!res.ok) {
                setRefusal(await readApiError(res));
                return;
            }
            let p: Preview | null = null;
            try {
                p = readPreview(await res.json());
            } catch {
                p = null;
            }
            if (!p) {
                // The server answered, and the answer is not a preview.
                // Sunucu yanıt verdi; yanıt bir önizleme değil.
                showToast('error', t('import.inspectUnreadable'));
                return;
            }
            setRefusal(null);
            setPreview(p);
            setTargetDomain(p.main_domain || p.domains[0] || '');
            setUnknownResult(null);
            setStage('preview');
        } catch {
            // Inspecting only reads the archive, so it can simply be asked again.
            showToast('error', t('import.inspectUnanswered'));
        } finally {
            setBusy(false);
        }
    };

    const runImport = async () => {
        // After a lost answer the import is started again only once a check
        // has shown that the domain it creates first is not on the server.
        // Yanıt kaybolduktan sonra içe aktarım, ancak ilk oluşturduğu alan
        // adının sunucuda olmadığı bir kontrolle görüldükten sonra yeniden
        // başlatılır.
        if (unknownResult && unknownResult.check !== 'absent') return;
        const domain = targetDomain;
        // A first start has a new identity. A start after a lost answer is the
        // same request under the same identity: the server answers it from the
        // first run if there was one, and runs it only if it never arrived.
        // İlk başlatmanın yeni bir kimliği vardır. Kaybolan yanıttan sonraki
        // başlatma, aynı kimlikle aynı istektir: sunucu onu varsa ilk
        // çalışmadan yanıtlar, yalnızca hiç ulaşmadıysa çalıştırır.
        const request = (unknownResult && applyRequest.current) || {
            id: newRequestId(),
            body: JSON.stringify({
                path,
                subscription_id: subID,
                domain: targetDomain,
                do_files: opts.files,
                do_mail: opts.mail,
                do_dns: opts.dns,
                do_databases: opts.databases,
            }),
        };
        applyRequest.current = request;
        setRefusal(null);
        setBusy(true);
        // Why the import has no result of its own, when it has none.
        // İçe aktarımın kendi sonucu yoksa nedeni.
        let why: LostCause = 'asked';
        try {
            let res: Response | null = null;
            try {
                res = await fetch('/api/v1/import/cpanel/apply', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json', [REQUEST_ID_HEADER]: request.id },
                    body: request.body,
                });
            } catch {
                res = null;
            }
            // No answer after the second asking, an answer a gateway gave in
            // the Panel's place, or the Panel's own word that the import is
            // still running or was interrupted: the result is not known, and
            // the page says which of these it was. One definition for every
            // screen (lib/lostAnswer.ts).
            // İkinci sorudan sonra da yanıt yok, Panel'in yerine bir geçidin
            // yanıtı ya da Panel'in içe aktarımın sürdüğünü ya da kesildiğini
            // kendisinin söylemesi: sonuç bilinmiyor; sayfa hangisi olduğunu
            // söyler.
            const cause = await unansweredCause('POST', '/api/v1/import/cpanel/apply', res);
            if (cause || !res) {
                why = cause ?? 'asked';
                // The Panel lost track of the import: a later start is a new
                // request. In every other case a later start asks for the
                // same request again.
                // Panel içe aktarımı izleyemedi: sonraki başlatma yeni bir
                // istektir. Öteki durumlarda aynı istek yeniden sorulur.
                if (why === 'interrupted') applyRequest.current = null;
                throw new Error(why);
            }
            if (!res.ok) {
                // Any other answer is the Panel's own: the result is known.
                // Başka her yanıt Panel'in kendi yanıtıdır: sonuç bilinir.
                setRefusal(siteWebServerRefusedIn(await readApiError(res), t));
                applyRequest.current = null;
                setUnknownResult(null);
                return;
            }
            const answered = readImportResult(await res.json(), domain);
            if (!answered) throw new Error('steps');
            applyRequest.current = null;
            setResult(answered);
            setUnknownResult(null);
            setStage('result');
        } catch {
            // No result, or an answer that could not be read: the server may
            // have imported everything, a part, or nothing. That is said on
            // the page and stays there; "Start import" does not come back by
            // itself.
            // Sonuç yok ya da yanıt okunamadı: sunucu her şeyi, bir kısmını
            // içe aktarmış ya da hiçbir şey yapmamış olabilir. Bu sayfada söylenir
            // ve orada kalır; "İçe aktarımı başlat" kendiliğinden geri gelmez.
            setUnknownResult({ check: 'unchecked', domain, why });
        } finally {
            setBusy(false);
        }
    };

    // Reads the domain list. It changes nothing and starts nothing.
    // Alan adı listesini okur. Hiçbir şeyi değiştirmez, hiçbir şey başlatmaz.
    const checkResult = async () => {
        if (!unknownResult) return;
        const { domain, why } = unknownResult;
        setUnknownResult({ check: 'checking', domain, why });
        const list = await readRemote('/api/v1/domains', decodeList<unknown>, undefined, { cache: 'no-store' });
        setUnknownResult((current) => {
            if (!current || current.domain !== domain) return current;
            if (list.state !== 'known') return { check: 'unreadable', domain, why };
            return { check: list.value.some((row) => namesDomain(row, domain)) ? 'present' : 'absent', domain, why };
        });
    };

    const reset = () => {
        setStage('source');
        setPreview(null);
        setResult(null);
        setRefusal(null);
        setPath('');
        setUnknownResult(null);
        applyRequest.current = null;
    };
    const subsKnown = subs.remote.state === 'known';
    const subOptions = subs.remote.state === 'known' ? subs.remote.value : [];
    // While the result is not known the choices that made the request stay as
    // they were sent.
    // Sonuç bilinmezken isteği oluşturan seçimler gönderildiği gibi kalır.
    const frozen = busy || unknownResult !== null;
    // Which mailboxes the archive holds a password for. The import keeps that
    // password; a mailbox without one is not created by it.
    // Arşivde hangi posta kutularının parolası var.
    const mailboxes = preview?.mail_accounts ?? [];
    const withoutPassword = mailboxes.filter((m) => !m.has_password).map((m) => `${m.user}@${m.domain}`);

    return (
        <div className="p-6 md:p-8">
            <PageHeader title={t('import.title')} subtitle={t('import.subtitle')} breadcrumb={[t('common.home'), t('nav.import')]} />

            <Stepper stage={stage} />

            {stage === 'source' && (
                <div className="mx-auto max-w-2xl rounded-xl border border-border bg-surface p-6">
                    <label className="block">
                        <span className="mb-1.5 block text-sm font-medium text-fg-muted">{t('import.pathLabel')}</span>
                        <input
                            value={path}
                            onChange={(e) => setPath(e.target.value)}
                            onKeyDown={(e) => e.key === 'Enter' && path && inspect()}
                            placeholder="/var/lib/celikpanel-imports/cpmove-user.tar.gz"
                            className={`${inputClass} font-mono`}
                            autoFocus
                        />
                        <span className="mt-1.5 block text-xs text-fg-subtle">{t('import.pathHint')}</span>
                    </label>
                    <ErrorBanner error={refusal} className="mt-4" />
                    <div className="mt-4 flex justify-end">
                        <Button variant="primary" icon={Eye} onClick={inspect} disabled={busy || !path.trim()}>
                            {busy ? t('import.inspecting') : t('import.inspect')}
                        </Button>
                    </div>
                </div>
            )}

            {stage === 'preview' && preview && (
                <div className="grid grid-cols-1 gap-5 lg:grid-cols-[1fr_360px]">
                    {/* Preview */}
                    <div className="rounded-xl border border-border bg-surface p-5">
                        <h3 className="mb-4 text-base font-semibold text-fg">{t('import.previewOf')}</h3>
                        <dl className="space-y-2.5 text-sm">
                            <Row label={t('import.account')} value={preview.username || '—'} />
                            <Row label={t('import.mainDomain')} value={preview.main_domain || '—'} />
                            <PreviewStat icon={FileText} label={t('import.siteFiles')} value={preview.public_html ? fmtBytes(preview.site_bytes) : t('import.none')} />
                            <PreviewStat
                                icon={Mail}
                                label={t('import.mailAccounts')}
                                value={String(mailboxes.length)}
                                detail={mailboxes.map((m) => `${m.user}@${m.domain}`).join(', ')}
                                note={mailboxes.length === 0 ? undefined : withoutPassword.length === 0
                                    ? t('import.mailPasswords.all')
                                    : t('import.mailPasswords.some', {
                                        kept: mailboxes.length - withoutPassword.length,
                                        total: mailboxes.length,
                                        missing: withoutPassword.join(', '),
                                    })}
                            />
                            <PreviewStat icon={ArrowRight} label={t('import.forwarders')} value={String(preview.forwarders.length)} />
                            <PreviewStat icon={Network} label={t('import.dnsRecords')} value={String(Object.values(preview.dns_zones).reduce((n, z) => n + z.length, 0))} />
                            <PreviewStat icon={Database} label={t('import.databases')} value={String(preview.databases.length)} detail={preview.databases.map((d) => d.name).join(', ')} />
                        </dl>
                    </div>

                    {/* Target + options */}
                    <div className="space-y-4 rounded-xl border border-border bg-surface p-5">
                        <h3 className="text-base font-semibold text-fg">{t('import.targetTitle')}</h3>

                        <label className="block">
                            <span className="mb-1 block text-xs text-fg-muted">{t('import.targetDomain')}</span>
                            <select value={targetDomain} onChange={(e) => setTargetDomain(e.target.value)} className={inputClass} disabled={frozen}>
                                {preview.domains.map((d) => (
                                    <option key={d} value={d}>
                                        {d}
                                    </option>
                                ))}
                            </select>
                        </label>

                        <div>
                            <label className="block">
                                <span className="mb-1 block text-xs text-fg-muted">{t('import.targetSub')}</span>
                                <select value={subID} onChange={(e) => setSubID(Number(e.target.value))} className={inputClass} disabled={frozen || !subsKnown}>
                                    <option value={0}>{t('import.subChoose')}</option>
                                    {subOptions.map((s) => (
                                        <option key={s.id} value={s.id}>
                                            {s.owner} · {s.name}
                                        </option>
                                    ))}
                                </select>
                            </label>
                            {/* The line under the list is always there, so the
                                options below do not move when the answer arrives.
                                Listenin altındaki satır hep yerindedir; yanıt
                                gelince aşağıdaki seçenekler yer değiştirmez. */}
                            <div className="mt-1.5 min-h-5">
                                {subs.remote.state === 'loading' && <Checking label={t('import.checkingSubs')} />}
                                {subs.remote.state === 'unknown' && (
                                    <CouldNotCheck text={t('import.subsUnknown')} onRetry={() => void subs.retry()} busy={subs.reading} />
                                )}
                                {subs.remote.state === 'known' && subs.remote.value.length === 0 && (
                                    <p className="text-xs text-fg-muted">{t('import.noSubs')}</p>
                                )}
                            </div>
                        </div>

                        <div>
                            <span className="mb-1.5 block text-xs text-fg-muted">{t('import.whatToImport')}</span>
                            <div className="space-y-1.5">
                                <Opt checked={opts.files} disabled={frozen} onChange={(v) => setOpts({ ...opts, files: v })} label={t('import.optFiles')} />
                                <Opt checked={opts.mail} disabled={frozen} onChange={(v) => setOpts({ ...opts, mail: v })} label={t('import.optMail')} />
                                <Opt checked={opts.dns} disabled={frozen} onChange={(v) => setOpts({ ...opts, dns: v })} label={t('import.optDNS')} />
                                <Opt checked={opts.databases} disabled={frozen} onChange={(v) => setOpts({ ...opts, databases: v })} label={t('import.optDatabases')} />
                            </div>
                        </div>

                        {opts.mail && <Note text={t('import.mailNote')} />}
                        {opts.databases && <Note text={t('import.dbNote')} />}

                        {unknownResult && (
                            <div ref={unknownNotice} role="alert" data-import-unknown={unknownResult.why} className="scroll-mb-20 rounded-lg border border-warning-mark/50 bg-warning-mark/20 p-3 text-sm leading-relaxed text-fg">
                                <p className="flex items-start gap-2 font-semibold">
                                    <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" aria-hidden="true" />
                                    <span>{t('import.unknown.title')}</span>
                                </p>
                                <p className="mt-1.5 break-words">{t(unknownBody[unknownResult.why], { domain: unknownResult.domain })}</p>
                                <div className="mt-2 min-h-5" aria-live="polite">
                                    {unknownResult.check === 'checking' && <Checking label={t('domains.checking')} />}
                                    {unknownResult.check === 'unreadable' && <p className="break-words">{t('import.unknown.unreadable')}</p>}
                                    {unknownResult.check === 'present' && <p className="break-words">{t('import.unknown.present', { domain: unknownResult.domain })}</p>}
                                    {unknownResult.check === 'absent' && <p className="break-words">{t('import.unknown.absent', { domain: unknownResult.domain })}</p>}
                                </div>
                                <div className="mt-2 flex flex-wrap gap-2">
                                    {unknownResult.check === 'present' ? (
                                        <Button type="button" onClick={() => navigate(`/domains/${encodeURIComponent(unknownResult.domain)}`)}>
                                            {t('import.unknown.open', { domain: unknownResult.domain })}
                                        </Button>
                                    ) : (
                                        <Button type="button" loading={unknownResult.check === 'checking'} onClick={() => void checkResult()}>
                                            {t(unknownResult.check === 'unchecked' ? 'import.unknown.check' : 'import.unknown.checkAgain', { domain: unknownResult.domain })}
                                        </Button>
                                    )}
                                </div>
                            </div>
                        )}

                        {/* The one refusal this page has its own words for:
                            the site could not be created, so nothing of the
                            archive was imported.
                            Bu sayfanın kendi sözleri olan tek ret. */}
                        <ErrorBanner error={refusal?.code === 'IMPORT_SITE_NOT_CREATED' ? { message: t('import.siteNotCreated') } : refusal} />

                        <div className="flex justify-between gap-2 pt-1">
                            <Button onClick={reset} disabled={busy}>{t(unknownResult ? 'import.importAnother' : 'import.back')}</Button>
                            {/* Not offered again after a lost answer, except
                                once a check has shown the domain absent.
                                Kaybolan yanıttan sonra, bir kontrol alan adının
                                olmadığını göstermedikçe yeniden sunulmaz. */}
                            {(!unknownResult || unknownResult.check === 'absent') && (
                                <Button variant="primary" icon={FolderInput} onClick={runImport} disabled={busy || !subsKnown || subID === 0 || !targetDomain}>
                                    {busy ? t('import.running') : t(unknownResult ? 'import.runAgain' : 'import.run')}
                                </Button>
                            )}
                        </div>
                    </div>
                </div>
            )}

            {stage === 'result' && result && (
                <div className="mx-auto max-w-2xl rounded-xl border border-border bg-surface p-6" data-import-result={result.partial ? 'partial' : 'complete'}>
                    {/* What the import came to, before the list of steps
                        (D-024): the state, what is and is not on the server,
                        and what the owner can do.
                        İçe aktarımın neye vardığı, adım listesinden önce. */}
                    {result.partial ? (
                        <div role="alert" className="rounded-lg border border-warning-mark/50 bg-warning-mark/20 p-4 text-sm leading-relaxed text-fg">
                            <h3 className="flex items-start gap-2 text-base font-semibold">
                                <AlertTriangle className="mt-1 h-4 w-4 shrink-0 text-warning" aria-hidden="true" />
                                <span className="min-w-0 break-words">{t('import.partial.title', { domain: result.domain })}</span>
                            </h3>
                            <p className="mt-1.5 max-w-[75ch] break-words">
                                {t(result.notImported.length === 0 ? 'import.partial.unfinished'
                                    : result.notImported.every(refusedEntry) ? 'import.partial.entriesBody'
                                        : 'import.partial.body', { domain: result.domain })}
                            </p>
                            <dl className="mt-3 grid gap-x-6 gap-y-3 sm:grid-cols-2">
                                <PartList label={t('import.partial.notImported')} parts={result.notImported} />
                                <PartList label={t('import.partial.imported')} parts={result.imported} />
                            </dl>
                            <p className="mt-3 max-w-[75ch] break-words">
                                {t(result.notImported.length > 0 && result.notImported.every(refusedEntry) ? 'import.partial.entriesNext' : 'import.partial.next', { domain: result.domain })}
                            </p>
                            <div className="mt-3 flex flex-wrap gap-2">
                                <Button type="button" onClick={() => navigate(`/domains/${encodeURIComponent(result.domain)}`)}>
                                    {t('import.unknown.open', { domain: result.domain })}
                                </Button>
                                <Button type="button" onClick={() => navigate('/domains')}>{t('import.partial.domains')}</Button>
                            </div>
                        </div>
                    ) : (
                        <>
                            <h3 className="flex items-center gap-2 text-base font-semibold text-fg">
                                <DownloadCloud className="h-4 w-4 shrink-0 text-primary" aria-hidden="true" />
                                {t('import.resultTitle')}
                            </h3>
                            <p className="mt-1.5 max-w-[75ch] break-words text-sm text-fg-muted">{t('import.result.complete', { domain: result.domain })}</p>
                        </>
                    )}
                    <h4 className="mb-2 mt-6 text-sm font-semibold text-fg">{t('import.stepsTitle')}</h4>
                    <ul className="space-y-2">
                        {result.steps.map((s, i) => (
                            <li key={i} className="flex items-start gap-2.5 rounded-lg border border-border bg-surface-2/40 px-3 py-2">
                                {s.ok ? (
                                    <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-success" aria-hidden="true" />
                                ) : (
                                    <XCircle className="mt-0.5 h-4 w-4 shrink-0 text-danger" aria-hidden="true" />
                                )}
                                <div className="min-w-0">
                                    <div className="break-words text-sm font-medium text-fg">
                                        {partLabel(s.step, t)}
                                        <span className="sr-only">: {t(s.ok ? 'import.step.done' : 'import.step.notDone')}</span>
                                    </div>
                                    <div className="break-words text-xs text-fg-muted">{detailKeys[s.detail] ? t(detailKeys[s.detail]) : s.detail}</div>
                                </div>
                            </li>
                        ))}
                    </ul>
                    <div className="mt-4 flex justify-end">
                        <Button variant={result.partial ? 'secondary' : 'primary'} onClick={reset}>
                            {t('import.importAnother')}
                        </Button>
                    </div>
                </div>
            )}
        </div>
    );
}

function Stepper({ stage }: { stage: Stage }) {
    const { t } = useI18n();
    const order: Stage[] = ['source', 'preview', 'result'];
    const labels: Record<Stage, TranslationKey> = {
        source: 'import.step.source',
        preview: 'import.step.preview',
        result: 'import.step.result',
    };
    const activeIdx = order.indexOf(stage);
    return (
        <div className="mb-5 flex items-center gap-2">
            {order.map((s, i) => (
                <div key={s} className="flex items-center gap-2">
                    <span
                        className={`flex h-6 w-6 items-center justify-center rounded-full text-xs font-semibold ${
                            i <= activeIdx ? 'bg-primary text-primary-fg' : 'bg-surface-2 text-fg-subtle'
                        }`}
                    >
                        {i + 1}
                    </span>
                    <span className={`text-sm ${i === activeIdx ? 'font-semibold text-fg' : 'text-fg-muted'}`}>{t(labels[s])}</span>
                    {i < order.length - 1 && <span className="mx-1 h-px w-8 bg-border" />}
                </div>
            ))}
        </div>
    );
}

function Row({ label, value }: { label: string; value: string }) {
    return (
        <div className="flex items-center justify-between gap-4 border-b border-border pb-2 last:border-0">
            <dt className="text-fg-subtle">{label}</dt>
            <dd className="truncate font-medium text-fg">{value}</dd>
        </div>
    );
}

function PreviewStat({ icon: Icon, label, value, detail, note }: { icon: typeof Mail; label: string; value: string; detail?: string; note?: string }) {
    return (
        <div className="border-b border-border pb-2 last:border-0">
            <div className="flex items-center justify-between gap-4">
                <dt className="flex items-center gap-2 text-fg-subtle">
                    <Icon className="h-4 w-4" aria-hidden="true" />
                    {label}
                </dt>
                <dd className="font-medium text-fg">{value}</dd>
            </div>
            {detail && <p className="mt-0.5 break-words pl-6 text-xs text-fg-subtle">{detail}</p>}
            {note && <p className="mt-1 max-w-[75ch] break-words pl-6 text-xs text-fg-muted" data-import-mail-passwords>{note}</p>}
        </div>
    );
}

// One of the two lists of a partial result, in the page's own words.
// Kısmi bir sonucun iki listesinden biri.
function PartList({ label, parts }: { label: string; parts: string[] }) {
    const { t } = useI18n();
    if (parts.length === 0) return null;
    return (
        <div className="min-w-0">
            <dt className="font-semibold">{label}</dt>
            <dd className="mt-1">
                <ul className="list-disc space-y-0.5 pl-5">
                    {parts.map((part) => <li key={part} className="break-words">{partLabel(part, t)}</li>)}
                </ul>
            </dd>
        </div>
    );
}

function Opt({ checked, onChange, label, disabled }: { checked: boolean; onChange: (v: boolean) => void; label: string; disabled?: boolean }) {
    return (
        <label className={`flex items-center gap-2 text-sm text-fg ${disabled ? '' : 'cursor-pointer'}`}>
            <input type="checkbox" checked={checked} disabled={disabled} onChange={(e) => onChange(e.target.checked)} className="h-4 w-4 accent-primary" />
            {label}
        </label>
    );
}

function Note({ text }: { text: string }) {
    return (
        <p className="flex items-start gap-2 rounded-lg border border-warning-mark/50 bg-warning-mark/20 px-3 py-2 text-xs text-fg-muted">
            <Info className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning" />
            {text}
        </p>
    );
}

function fmtBytes(n: number): string {
    if (n < 1024) return `${n} B`;
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
    if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MB`;
    return `${(n / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}
