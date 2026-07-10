import { useState, useEffect } from 'react';
import { DownloadCloud, FolderInput, Eye, Mail, ArrowRight, Network, Database, FileText, CheckCircle2, XCircle, Info } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import { PageHeader, Button, inputClass } from './ui';

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

interface Subscription {
    id: number;
    name: string;
    owner: string;
}

interface StepResult {
    step: string;
    ok: boolean;
    detail: string;
}

type Stage = 'source' | 'preview' | 'result';

export function ImportPage() {
    const { t } = useI18n();
    const [stage, setStage] = useState<Stage>('source');
    const [path, setPath] = useState('');
    const [busy, setBusy] = useState(false);
    const [preview, setPreview] = useState<Preview | null>(null);

    const [subs, setSubs] = useState<Subscription[]>([]);
    const [targetDomain, setTargetDomain] = useState('');
    const [subID, setSubID] = useState(0);
    const [opts, setOpts] = useState({ files: true, mail: true, dns: true, databases: true });
    const [steps, setSteps] = useState<StepResult[]>([]);

    useEffect(() => {
        fetch('/api/v1/subscriptions')
            .then((r) => (r.ok ? r.json() : { subscriptions: [] }))
            .then((d) => setSubs(d.subscriptions || []))
            .catch(() => {});
    }, []);

    const inspect = async () => {
        setBusy(true);
        try {
            const res = await fetch('/api/v1/import/cpanel/inspect', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ path }),
            });
            if (!res.ok) {
                showToast('error', (await res.text()).trim() || t('common.error'));
                return;
            }
            const p: Preview = await res.json();
            setPreview(p);
            setTargetDomain(p.main_domain || p.domains[0] || '');
            setStage('preview');
        } finally {
            setBusy(false);
        }
    };

    const runImport = async () => {
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
            if (!res.ok) {
                showToast('error', (await res.text()).trim() || t('common.error'));
                return;
            }
            const data = await res.json();
            setSteps(data.steps || []);
            setStage('result');
        } finally {
            setBusy(false);
        }
    };

    const reset = () => {
        setStage('source');
        setPreview(null);
        setSteps([]);
        setPath('');
    };

    return (
        <div className="p-6 md:p-8">
            <PageHeader title={t('import.title')} subtitle={t('import.subtitle')} breadcrumb={[t('common.home'), t('nav.import')]} />

            <Stepper stage={stage} />

            {stage === 'source' && (
                <div className="mx-auto max-w-2xl rounded-xl border border-border bg-surface p-6 shadow-card">
                    <label className="block">
                        <span className="mb-1.5 block text-sm font-medium text-fg-muted">{t('import.pathLabel')}</span>
                        <input
                            value={path}
                            onChange={(e) => setPath(e.target.value)}
                            onKeyDown={(e) => e.key === 'Enter' && path && inspect()}
                            placeholder="/home/backup/cpmove-user.tar.gz"
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
                    <div className="rounded-xl border border-border bg-surface p-5 shadow-card">
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
                    <div className="space-y-4 rounded-xl border border-border bg-surface p-5 shadow-card">
                        <h3 className="text-base font-semibold text-fg">{t('import.targetTitle')}</h3>

                        <label className="block">
                            <span className="mb-1 block text-xs text-fg-muted">{t('import.targetDomain')}</span>
                            <select value={targetDomain} onChange={(e) => setTargetDomain(e.target.value)} className={inputClass}>
                                {preview.domains.map((d) => (
                                    <option key={d} value={d}>
                                        {d}
                                    </option>
                                ))}
                            </select>
                        </label>

                        <label className="block">
                            <span className="mb-1 block text-xs text-fg-muted">{t('import.targetSub')}</span>
                            <select value={subID} onChange={(e) => setSubID(Number(e.target.value))} className={inputClass}>
                                <option value={0}>{t('import.subChoose')}</option>
                                {subs.map((s) => (
                                    <option key={s.id} value={s.id}>
                                        {s.owner} · {s.name}
                                    </option>
                                ))}
                            </select>
                        </label>

                        <div>
                            <span className="mb-1.5 block text-xs text-fg-muted">{t('import.whatToImport')}</span>
                            <div className="space-y-1.5">
                                <Opt checked={opts.files} onChange={(v) => setOpts({ ...opts, files: v })} label={t('import.optFiles')} />
                                <Opt checked={opts.mail} onChange={(v) => setOpts({ ...opts, mail: v })} label={t('import.optMail')} />
                                <Opt checked={opts.dns} onChange={(v) => setOpts({ ...opts, dns: v })} label={t('import.optDNS')} />
                                <Opt checked={opts.databases} onChange={(v) => setOpts({ ...opts, databases: v })} label={t('import.optDatabases')} />
                            </div>
                        </div>

                        {opts.mail && <Note text={t('import.mailNote')} />}
                        {opts.databases && <Note text={t('import.dbNote')} />}

                        <div className="flex justify-between gap-2 pt-1">
                            <Button onClick={reset}>{t('import.back')}</Button>
                            <Button variant="primary" icon={FolderInput} onClick={runImport} disabled={busy || subID === 0 || !targetDomain}>
                                {busy ? t('import.running') : t('import.run')}
                            </Button>
                        </div>
                    </div>
                </div>
            )}

            {stage === 'result' && (
                <div className="mx-auto max-w-2xl rounded-xl border border-border bg-surface p-6 shadow-card">
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

function Opt({ checked, onChange, label }: { checked: boolean; onChange: (v: boolean) => void; label: string }) {
    return (
        <label className="flex cursor-pointer items-center gap-2 text-sm text-fg">
            <input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} className="h-4 w-4 accent-primary" />
            {label}
        </label>
    );
}

function Note({ text }: { text: string }) {
    return (
        <p className="flex items-start gap-2 rounded-lg border border-warning/30 bg-warning/10 px-3 py-2 text-xs text-fg-muted">
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
