import { useEffect, useRef, useState } from 'react';
import { AlertTriangle, DownloadCloud, FolderInput, Eye, Mail, ArrowRight, Network, Database, FileText, CheckCircle2, XCircle, Info } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { readApiError, apiErrorText } from '../lib/apiError';
import type { TranslationKey } from '../i18n/en';
import { Button, Checking, CouldNotCheck, inputClass } from './ui';
import { useNavigate } from '../router';
import { decodeList, readRemote, useRemote } from '../lib/remote';
import { SUBSCRIPTIONS_URL, decodeSubscriptions } from '../lib/subscriptions';
import { PageHeader } from './PageHeader';

// cPanel import wizard (roadmap 3B): source → preview → result. Nothing is
// applied until the operator sees the honest preview and confirms; the
// result screen reports every step's real outcome, not a single OK/fail.
//
// cPanel içe aktarım sihirbazı (yol haritası 3B): kaynak → önizleme → sonuç.
// Operatör dürüst önizlemeyi görüp onaylayana dek hiçbir şey uygulanmaz;
// sonuç ekranı her adımın gerçek sonucunu raporlar, tek bir tamam/hata değil.

interface Preview {
    username: string;
    main_domain: string;
    domains: string[];
    public_html: boolean;
    site_bytes: number;
    mail_accounts: { domain: string; user: string; quota_mb: number }[];
    forwarders: { source: string; destination: string }[];
    dns_zones: Record<string, unknown[]>;
    databases: { name: string; dump_bytes: number }[];
}

interface StepResult {
    step: string;
    ok: boolean;
    detail: string;
}

type Stage = 'source' | 'preview' | 'result';

// The apply request has no identity the server keeps: when its answer is lost
// nothing can be asked about that request. What can be read is whether the
// domain it creates first is on the server now. `unchecked` is "nobody has
// looked yet"; none of these is "the import failed".
//
// Uygulama isteğinin sunucuda tutulan bir kimliği yoktur: yanıtı kaybolunca o
// istek hakkında soru sorulamaz. Okunabilen, ilk oluşturduğu alan adının şu an
// sunucuda olup olmadığıdır. Bunların hiçbiri "içe aktarım başarısız" değildir.
type UnknownResult =
    | { check: 'unchecked' | 'checking' | 'unreadable' | 'absent'; domain: string }
    | { check: 'present'; domain: string };

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
    const [steps, setSteps] = useState<StepResult[]>([]);

    const inspect = async () => {
        setBusy(true);
        try {
            const res = await fetch('/api/v1/import/cpanel/inspect', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ path }),
            });
            if (!res.ok) {
                showToast('error', apiErrorText(await readApiError(res), t));
                return;
            }
            const p: Preview = await res.json();
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
        setBusy(true);
        try {
            const res = await fetch('/api/v1/import/cpanel/apply', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    path,
                    subscription_id: subID,
                    domain: targetDomain,
                    do_files: opts.files,
                    do_mail: opts.mail,
                    do_dns: opts.dns,
                    do_databases: opts.databases,
                }),
            });
            // A gateway between the browser and the Panel answers like this
            // when it lost the Panel's own answer: that is not a refusal by
            // the Panel, and the import may be running.
            // Tarayıcı ile Panel arasındaki bir geçit, Panelin kendi yanıtını
            // kaybettiğinde böyle yanıt verir: bu Panelin reddi değildir.
            if ([408, 429, 502, 503, 504].includes(res.status)) throw new Error('unanswered');
            if (!res.ok) {
                showToast('error', apiErrorText(await readApiError(res), t));
                return;
            }
            const data = await res.json();
            if (!data || !Array.isArray(data.steps)) throw new Error('steps');
            setSteps(data.steps);
            setUnknownResult(null);
            setStage('result');
        } catch {
            // The connection dropped, or the answer could not be read: the
            // server may have imported everything, a part, or nothing. That is
            // said on the page and stays there; "Start import" does not come
            // back by itself.
            // Bağlantı koptu ya da yanıt okunamadı: sunucu her şeyi, bir kısmını
            // içe aktarmış ya da hiçbir şey yapmamış olabilir. Bu sayfada söylenir
            // ve orada kalır; "İçe aktarımı başlat" kendiliğinden geri gelmez.
            setUnknownResult({ check: 'unchecked', domain });
        } finally {
            setBusy(false);
        }
    };

    // Reads the domain list. It changes nothing and starts nothing.
    // Alan adı listesini okur. Hiçbir şeyi değiştirmez, hiçbir şey başlatmaz.
    const checkResult = async () => {
        if (!unknownResult) return;
        const { domain } = unknownResult;
        setUnknownResult({ check: 'checking', domain });
        const list = await readRemote('/api/v1/domains', decodeList<unknown>, undefined, { cache: 'no-store' });
        setUnknownResult((current) => {
            if (!current || current.domain !== domain) return current;
            if (list.state !== 'known') return { check: 'unreadable', domain };
            return { check: list.value.some((row) => namesDomain(row, domain)) ? 'present' : 'absent', domain };
        });
    };

    const reset = () => {
        setStage('source');
        setPreview(null);
        setSteps([]);
        setPath('');
        setUnknownResult(null);
    };
    const subsKnown = subs.remote.state === 'known';
    const subOptions = subs.remote.state === 'known' ? subs.remote.value : [];
    // While the result is not known the choices that made the request stay as
    // they were sent.
    // Sonuç bilinmezken isteği oluşturan seçimler gönderildiği gibi kalır.
    const frozen = busy || unknownResult !== null;

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
                            <PreviewStat icon={Mail} label={t('import.mailAccounts')} value={String(preview.mail_accounts.length)} detail={preview.mail_accounts.map((m) => `${m.user}@${m.domain}`).join(', ')} />
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
                            <div ref={unknownNotice} role="alert" className="scroll-mb-20 rounded-lg border border-warning-mark/50 bg-warning-mark/20 p-3 text-sm leading-relaxed text-fg">
                                <p className="flex items-start gap-2 font-semibold">
                                    <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" aria-hidden="true" />
                                    <span>{t('import.unknown.title')}</span>
                                </p>
                                <p className="mt-1.5 break-words">{t('import.unknown.body', { domain: unknownResult.domain })}</p>
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

            {stage === 'result' && (
                <div className="mx-auto max-w-2xl rounded-xl border border-border bg-surface p-6">
                    <h3 className="mb-4 flex items-center gap-2 text-base font-semibold text-fg">
                        <DownloadCloud className="h-4 w-4 text-primary" />
                        {t('import.resultTitle')}
                    </h3>
                    <ul className="space-y-2">
                        {steps.map((s, i) => (
                            <li key={i} className="flex items-start gap-2.5 rounded-lg border border-border bg-surface-2/40 px-3 py-2">
                                {s.ok ? (
                                    <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-success" />
                                ) : (
                                    <XCircle className="mt-0.5 h-4 w-4 shrink-0 text-danger" />
                                )}
                                <div className="min-w-0">
                                    <div className="text-sm font-medium text-fg">{s.step}</div>
                                    <div className="break-words text-xs text-fg-muted">{s.detail}</div>
                                </div>
                            </li>
                        ))}
                    </ul>
                    <div className="mt-4 flex justify-end">
                        <Button variant="primary" onClick={reset}>
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

function PreviewStat({ icon: Icon, label, value, detail }: { icon: typeof Mail; label: string; value: string; detail?: string }) {
    return (
        <div className="border-b border-border pb-2 last:border-0">
            <div className="flex items-center justify-between gap-4">
                <dt className="flex items-center gap-2 text-fg-subtle">
                    <Icon className="h-4 w-4" />
                    {label}
                </dt>
                <dd className="font-medium text-fg">{value}</dd>
            </div>
            {detail && <p className="mt-0.5 break-words pl-6 text-xs text-fg-subtle">{detail}</p>}
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
