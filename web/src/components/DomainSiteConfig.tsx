import { useEffect, useRef, useState, type ReactNode } from 'react';
import { AlertTriangle, FileCode2, FolderPlus, Info } from 'lucide-react';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import { Button, ErrorBanner, RemoteGate, ResultUnknown } from './ui';
import { readApiError, type ApiError } from '../lib/apiError';
import { useRemote } from '../lib/remote';
import { useLostAnswer } from '../lib/lostAnswer';

// A site's nginx configuration file, as the server found it (D-031,
// 2026-10-10). The Panel never overwrites a file that is not its own unchanged
// text: this page says what the file is, shows the difference, and offers the
// owner's three choices. It reads first; nothing here changes the server until
// a button is pressed, and every choice is bound to the file the page showed.
//
// Bir sitenin nginx yapılandırma dosyası, sunucunun bulduğu hâliyle (D-031).
// Panel kendi değişmemiş metni olmayan bir dosyanın üzerine asla yazmaz: bu
// sayfa dosyanın ne olduğunu söyler, farkı gösterir ve sahibin üç seçimini
// sunar. Önce okur; bir düğmeye basılmadan sunucuda hiçbir şey değişmez.

export interface SiteConfigView {
    domain_id: number;
    domain: string;
    kind: string;
    path?: string;
    state: string;
    reason?: string;
    detail?: string;
    adopted_from?: string;
    include_dir?: string;
    file_sha256?: string;
    render_sha256?: string;
    pending_path?: string;
    diff?: string;
    diff_truncated?: boolean;
    actions: string[];
    decision?: { kind: string; decided_at: string; current: boolean };
    outcome?: string;
    backup_path?: string;
    // D-031 step 1b: CelikPanel's own include directory and the line a kept
    // file needs for certificate validation; the certificate reason, if any.
    managed_dir?: string;
    managed_include?: string;
    validation?: string;
    challenge_file?: string;
    pending_reason?: string;
    certificate?: {
        cert_path?: string;
        key_path?: string;
        expires_at?: string;
        served_expires_at?: string;
        served_days_left?: number;
        referenced: boolean;
    };
    // The reason this read ended (the probe found the file ready), and the
    // probe's first name not served with nginx's HTTP status.
    resolved_reason?: string;
    validation_name?: string;
    validation_status?: number;
}

const STATES = new Set([
    'managed_unchanged', 'owner_edited', 'foreign', 'unknown_origin', 'absent', 'unreadable', 'unknown',
]);

export function decodeSiteConfig(raw: unknown): SiteConfigView {
    const body = raw as Partial<SiteConfigView> | null;
    if (!body || typeof body !== 'object' || typeof body.state !== 'string' || !STATES.has(body.state)
        || !Array.isArray(body.actions)) {
        throw new Error('shape');
    }
    return body as SiteConfigView;
}

const KEPT = new Set(['owner_edited', 'foreign', 'unknown_origin']);
const UNREADABLE_REASONS = new Set(['symlink', 'not_regular', 'permission', 'too_large', 'read_failed', 'write_refused']);
const VALIDATION_STATES = new Set(['include_missing', 'names_missing', 'challenge_kept', 'challenge_failed', 'ready', 'unknown']);
// The suffix the API puts on an earlier release's creation-time text.
const CREATION_SUFFIX = ' (creation)';

type Done = { kind: 'keep' | 'take' | 'recreate'; backup?: string };

export function DomainSiteConfig({ domainId, onReasonEnded }: {
    domainId: number;
    domainName: string;
    // Called when a read ended a certificate reason, so the line above the
    // tabs (read from the domains list) is read again.
    onReasonEnded?: () => void;
}) {
    const { t } = useI18n();
    const url = `/api/v1/domains/${domainId}/site-config`;
    const config = useRemote(url, decodeSiteConfig);
    // Once per answer that ended a reason; a later answer carries none.
    const ended = config.remote.state === 'known' && config.remote.value.resolved_reason ? config.remote.value : null;
    const onReasonEndedRef = useRef(onReasonEnded);
    onReasonEndedRef.current = onReasonEnded;
    useEffect(() => {
        if (ended) onReasonEndedRef.current?.();
    }, [ended]);
    const answer = useLostAnswer(() => config.retry());
    const [busy, setBusy] = useState<'keep' | 'take' | 'recreate' | null>(null);
    const [confirming, setConfirming] = useState(false);
    const [merging, setMerging] = useState(false);
    const [refusal, setRefusal] = useState<ApiError | null>(null);
    const [done, setDone] = useState<Done | null>(null);

    const act = async (kind: 'keep' | 'take' | 'recreate', view: SiteConfigView) => {
        if (busy || answer.holding) return;
        setBusy(kind);
        setRefusal(null);
        setDone(null);
        try {
            const body = kind === 'recreate' ? {} : { file_sha256: view.file_sha256, render_sha256: view.render_sha256 };
            const question = {
                // What the state read again shows about the choice.
                shows: ([read]: { value: unknown }[]) => {
                    const after = read.value as SiteConfigView;
                    if (kind === 'keep') return after.decision?.kind === 'keep_mine' && after.decision.current;
                    return after.state === 'managed_unchanged';
                },
            };
            const res = await answer.send(`${url}/${kind}`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(body),
            }, question);
            if (!res) return;
            if (!res.ok) {
                setRefusal(await readApiError(res));
                void config.retry();
                return;
            }
            let result: SiteConfigView;
            try {
                result = (await res.json()) as SiteConfigView;
            } catch {
                // An answer that is not the contract: the result is not known.
                answer.lose(question);
                return;
            }
            answer.settle();
            setConfirming(false);
            setDone({ kind, backup: result.backup_path });
            void config.retry();
        } finally {
            setBusy(null);
        }
    };

    return (
        <div className="space-y-4" data-site-config>
            <div className="flex items-center gap-2">
                <FileCode2 className="h-5 w-5 text-fg-muted" aria-hidden="true" />
                <h2 className="text-base font-semibold text-fg">{t('siteConfig.title')}</h2>
            </div>
            <ResultUnknown answer={answer} />
            {done && (
                <p role="status" className="rounded-lg border border-border-strong bg-surface-2 p-3 text-sm text-fg" data-site-config-done={done.kind}>
                    {done.kind === 'take'
                        ? t('siteConfig.done.take', { backup: done.backup ?? '' })
                        : t(done.kind === 'keep' ? 'siteConfig.done.keep' : 'siteConfig.done.recreate')}
                </p>
            )}
            <ErrorBanner error={refusal} />
            <RemoteGate
                remote={config.remote}
                checking={t('siteConfig.checking')}
                failed={t('siteConfig.unknownRead')}
                onRetry={() => void config.retry()}
                busy={config.reading}
            >
                {({ value, stale }) => {
                    const locked = stale || answer.holding || config.reading || busy !== null;
                    return (
                        <SiteConfigState
                            view={value}
                            locked={locked}
                            busy={busy}
                            confirming={confirming}
                            merging={merging}
                            onKeep={() => void act('keep', value)}
                            onTake={() => (confirming ? void act('take', value) : setConfirming(true))}
                            onCancel={() => setConfirming(false)}
                            onMerge={() => setMerging((open) => !open)}
                            onRecreate={() => void act('recreate', value)}
                        />
                    );
                }}
            </RemoteGate>
        </div>
    );
}

function SiteConfigState({
    view, locked, busy, confirming, merging, onKeep, onTake, onCancel, onMerge, onRecreate,
}: {
    view: SiteConfigView;
    locked: boolean;
    busy: string | null;
    confirming: boolean;
    merging: boolean;
    onKeep: () => void;
    onTake: () => void;
    onCancel: () => void;
    onMerge: () => void;
    onRecreate: () => void;
}) {
    const { t, locale } = useI18n();
    const path = view.path ?? '';
    const include = view.include_dir || view.managed_dir ? (
        <div className="space-y-1 text-sm text-fg-muted" data-site-config-include>
            {view.include_dir && <p>{t('siteConfig.include', { dir: view.include_dir })}</p>}
            {view.managed_dir && <p data-site-config-managed-dir>{t('siteConfig.managedDir', { dir: view.managed_dir })}</p>}
        </div>
    ) : null;
    const adopted = view.adopted_from ? (
        <p className="mt-1">
            {view.adopted_from.endsWith(CREATION_SUFFIX)
                ? t('siteConfig.adopted.creation', { release: view.adopted_from.slice(0, -CREATION_SUFFIX.length) })
                : t('siteConfig.adopted.start', { release: view.adopted_from })}
        </p>
    ) : null;

    if (view.state === 'managed_unchanged') {
        return (
            <div className="space-y-3">
                <Plain icon={Info} title={t('siteConfig.managed.title')}>
                    <p>{t('siteConfig.managed.body')}</p>
                    {adopted}
                </Plain>
                <FileLine label={t('siteConfig.file')} value={path} />
                {include}
            </div>
        );
    }
    if (view.state === 'unknown') {
        return (
            <Attention title={t('siteConfig.unknownState.title')}>
                <p>{t('siteConfig.unknownState.body')}</p>
            </Attention>
        );
    }
    if (view.state === 'unreadable') {
        const reason = view.reason && UNREADABLE_REASONS.has(view.reason) ? view.reason : 'read_failed';
        return (
            <div className="space-y-3">
                <Attention title={t('siteConfig.unreadable.title')}>
                    <p>{t(`siteConfig.unreadable.${reason}` as TranslationKey)}</p>
                    {/* Who acts and the next step for this reason; the server's
                        own line for any other reason. */}
                    <p className="mt-1" data-site-config-next={reason}>
                        {t(reason === 'symlink' ? 'siteConfig.unreadable.next.symlink'
                            : reason === 'permission' ? 'siteConfig.unreadable.next.permission'
                                : 'siteConfig.unreadable.next.other')}
                    </p>
                    {view.detail && reason !== 'symlink' && reason !== 'permission' && (
                        <p className="mt-1 break-words">{t('siteConfig.unreadable.next.detail', { detail: view.detail })}</p>
                    )}
                    <p className="mt-1">{t('siteConfig.unreadable.meanwhile')}</p>
                </Attention>
                <FileLine label={t('siteConfig.file')} value={path} />
            </div>
        );
    }
    if (view.state === 'absent') {
        return (
            <div className="space-y-3">
                <Attention title={t('siteConfig.missing.title')}>
                    <p>{t('siteConfig.missing.body')}</p>
                </Attention>
                <FileLine label={t('siteConfig.file')} value={path} />
                <div className="flex flex-wrap items-center gap-3">
                    <Button type="button" variant="primary" icon={FolderPlus} loading={busy === 'recreate'} disabled={locked} onClick={onRecreate}>
                        {t('siteConfig.recreate')}
                    </Button>
                    <span className="text-sm text-fg-muted">{t('siteConfig.recreate.hint')}</span>
                </div>
            </div>
        );
    }
    // Kept: owner-edited, foreign or unknown origin.
    const kept = KEPT.has(view.state);
    if (!kept) return null;
    const keptByChoice = view.decision?.kind === 'keep_mine' && view.decision.current;
    const decidedAt = view.decision?.decided_at
        ? new Date(view.decision.decided_at).toLocaleString(locale, { dateStyle: 'medium', timeStyle: 'short' })
        : '';
    const title = t(view.state === 'foreign' ? 'siteConfig.foreign.title'
        : view.state === 'unknown_origin' ? 'siteConfig.unknownOrigin.title' : 'siteConfig.ownerEdited.title');
    const body = t(view.state === 'unknown_origin' ? 'siteConfig.unknownOrigin.body' : 'siteConfig.kept.body');
    return (
        <div className="space-y-4">
            {keptByChoice ? (
                <Plain icon={Info} title={title}>
                    <p>{t('siteConfig.keptByChoice', { date: decidedAt })}</p>
                </Plain>
            ) : (
                <Attention title={title}>
                    <p>{body}</p>
                </Attention>
            )}
            <FileLine label={t('siteConfig.file')} value={path} />
            <CertificatePart view={view} />

            <section aria-labelledby="site-config-choose" className="space-y-3">
                <h3 id="site-config-choose" className="text-sm font-semibold text-fg">{t('siteConfig.choose')}</h3>
                <div className="grid gap-3 lg:grid-cols-3">
                    <Choice
                        hint={t('siteConfig.take.hint')}
                        action={(
                            <Button type="button" variant="primary" loading={busy === 'take'} disabled={locked} onClick={onTake} data-site-config-take>
                                {confirming ? t('siteConfig.take.confirm') : t('siteConfig.take')}
                            </Button>
                        )}
                    >
                        {confirming && (
                            <div className="mt-2 space-y-2" data-site-config-confirm>
                                <p className="text-sm text-fg">{t('siteConfig.take.confirmText', { path })}</p>
                                <Button type="button" onClick={onCancel} disabled={busy === 'take'}>{t('siteConfig.cancel')}</Button>
                            </div>
                        )}
                    </Choice>
                    <Choice
                        hint={t('siteConfig.keep.hint')}
                        action={(
                            <Button type="button" loading={busy === 'keep'} disabled={locked || keptByChoice} onClick={onKeep} data-site-config-keep>
                                {t('siteConfig.keep')}
                            </Button>
                        )}
                    />
                    <Choice
                        hint={t('siteConfig.merge.short')}
                        action={(
                            <Button type="button" onClick={onMerge} aria-expanded={merging} data-site-config-merge>
                                {t('siteConfig.merge')}
                            </Button>
                        )}
                    >
                        {merging && (
                            <p className="mt-2 break-words text-sm text-fg" data-site-config-merge-text>
                                {view.pending_path
                                    ? t('siteConfig.merge.hint', { path, pending: view.pending_path })
                                    : t('siteConfig.merge.noPending', { path })}
                            </p>
                        )}
                    </Choice>
                </div>
            </section>

            <section aria-labelledby="site-config-diff" className="space-y-2">
                <h3 id="site-config-diff" className="text-sm font-semibold text-fg">{t('siteConfig.diff.title')}</h3>
                {view.diff ? (
                    <pre
                        tabIndex={0}
                        aria-labelledby="site-config-diff"
                        className="max-h-96 overflow-auto rounded-lg border border-border bg-surface-2 p-3 font-mono text-xs leading-relaxed text-fg"
                        data-site-config-diff
                    >
                        {/* One block as wide as the longest line, so a marked
                            line stays marked when the frame scrolls sideways.
                            En uzun satır kadar geniş tek blok. */}
                        <span className="inline-block min-w-full">
                            {localizeDiffCaptions(view.diff, t).split('\n').map((line, index) => (
                                <span
                                    key={index}
                                    className={`block whitespace-pre ${line.startsWith('+') && !line.startsWith('+++')
                                        ? 'bg-success/10'
                                        : line.startsWith('-') && !line.startsWith('---') ? 'bg-warning-mark/20' : ''}`}
                                >
                                    {line || ' '}
                                </span>
                            ))}
                        </span>
                    </pre>
                ) : (
                    <p className="text-sm text-fg-muted">{t('siteConfig.diff.none')}</p>
                )}
                {view.diff_truncated && <p className="text-sm text-fg-muted">{t('siteConfig.diff.truncated')}</p>}
            </section>
            {include}
        </div>
    );
}

// The two caption lines of the difference come from the server in English;
// they are drawn in the page's language.
// Farkın iki başlık satırı sunucudan İngilizce gelir; sayfanın dilinde çizilir.
const DIFF_HERE = ' (on this server)';
const DIFF_PANEL = "+++ CelikPanel's text";

function localizeDiffCaptions(diff: string, t: ReturnType<typeof useI18n>['t']): string {
    const lines = diff.split('\n');
    if (lines[0]?.startsWith('--- ') && lines[0].endsWith(DIFF_HERE)) {
        lines[0] = `${lines[0].slice(0, -DIFF_HERE.length)} ${t('siteConfig.diff.here')}`;
    }
    if (lines[1] === DIFF_PANEL) {
        lines[1] = `+++ ${t('siteConfig.diff.panel')}`;
    }
    return lines.join('\n');
}

// The certificate part of a kept file (D-031 step 1b): a new certificate that
// the file does not use yet, or a validation the file does not let run.
// Korunan dosyanın sertifika bölümü.
function CertificatePart({ view }: { view: SiteConfigView }) {
    const { t, locale } = useI18n();
    const certificate = view.certificate;
    if (view.resolved_reason === 'certificate_validation') {
        // The probe found the file ready and the wait ended with this read.
        return (
            <div data-site-config-certificate="ready_again">
                <Plain icon={Info} title={t('siteConfig.certificate.validation.readyTitle')}>
                    <p>{t('siteConfig.certificate.validation.ready')}</p>
                </Plain>
            </div>
        );
    }
    if (!view.pending_reason || !certificate) return null;
    const served = certificate.served_expires_at ? (
        <p className="mt-1">
            {certificate.served_days_left !== undefined && certificate.served_days_left < 0
                ? t('siteConfig.certificate.servedExpired', { date: formatDay(certificate.served_expires_at, locale) })
                : t('siteConfig.certificate.servedDays', {
                    date: formatDay(certificate.served_expires_at, locale),
                    days: certificate.served_days_left ?? 0,
                })}
        </p>
    ) : null;
    if (view.pending_reason === 'certificate') {
        return (
            <div data-site-config-certificate="certificate">
                <Attention title={t('siteConfig.certificate.ready.title')}>
                    <p>{t(certificate.referenced ? 'siteConfig.certificate.ready.referenced' : 'siteConfig.certificate.ready.body')}</p>
                    {served}
                    {!certificate.referenced && certificate.cert_path && certificate.key_path && (
                        <div className="mt-2">
                            <p className="text-fg-muted">{t('siteConfig.certificate.lines')}</p>
                            <pre className="mt-1 overflow-x-auto rounded-md border border-border bg-surface p-2 font-mono text-xs text-fg" data-site-config-cert-lines>
                                {`ssl_certificate ${certificate.cert_path};\nssl_certificate_key ${certificate.key_path};`}
                            </pre>
                        </div>
                    )}
                </Attention>
            </div>
        );
    }
    const validation = view.validation && VALIDATION_STATES.has(view.validation) ? view.validation : 'include_missing';
    return (
        <div data-site-config-certificate="certificate_validation" data-site-config-validation={validation}>
            {/* An unanswered probe is not "cannot be validated": its own title. */}
            <Attention title={t(validation === 'unknown' ? 'siteConfig.certificate.validation.unknownTitle' : 'siteConfig.certificate.validation.title')}>
                <p>{t(`siteConfig.certificate.validation.${validation}` as TranslationKey, {
                    domain: view.domain, dir: view.managed_dir ?? '',
                })}</p>
                {validation === 'include_missing' && view.managed_include && (
                    <div className="mt-2">
                        <p className="text-fg-muted">{t('siteConfig.certificate.includeLine')}</p>
                        <pre className="mt-1 overflow-x-auto rounded-md border border-border bg-surface p-2 font-mono text-xs text-fg" data-site-config-include-line>
                            {view.managed_include}
                        </pre>
                    </div>
                )}
                {validation !== 'ready' && validation !== 'unknown' && view.validation_name && view.validation_status ? (
                    <p className="mt-1 break-words" data-site-config-probe>
                        {t('siteConfig.certificate.validation.probe', { name: view.validation_name, status: view.validation_status })}
                    </p>
                ) : null}
                {served}
                <p className="mt-1">{t('siteConfig.certificate.schedule')}</p>
            </Attention>
        </div>
    );
}

function formatDay(value: string, locale: string): string {
    const date = new Date(value);
    return Number.isFinite(date.getTime()) ? date.toLocaleDateString(locale, { dateStyle: 'medium' }) : value;
}

function Attention({ title, children }: { title: string; children: ReactNode }) {
    return (
        <div role="status" className="flex items-start gap-2 rounded-lg border border-warning-mark/50 bg-warning-mark/20 p-3 text-sm leading-relaxed text-fg" data-site-config-state="attention">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" aria-hidden="true" />
            <div className="min-w-0 max-w-[75ch]">
                <p className="font-semibold">{title}</p>
                <div className="mt-1">{children}</div>
            </div>
        </div>
    );
}

function Plain({ icon: Icon, title, children }: { icon: typeof Info; title: string; children: ReactNode }) {
    return (
        <div className="flex items-start gap-2 rounded-lg border border-border-strong bg-surface-2 p-3 text-sm leading-relaxed text-fg" data-site-config-state="plain">
            <Icon className="mt-0.5 h-4 w-4 shrink-0 text-fg-muted" aria-hidden="true" />
            <div className="min-w-0 max-w-[75ch]">
                <p className="font-semibold">{title}</p>
                <div className="mt-1">{children}</div>
            </div>
        </div>
    );
}

function FileLine({ label, value }: { label: string; value: string }) {
    if (!value) return null;
    return (
        <p className="flex flex-wrap items-baseline gap-x-2 text-sm">
            <span className="text-fg-subtle">{label}</span>
            <code className="break-all font-mono text-xs text-fg">{value}</code>
        </p>
    );
}

function Choice({ hint, action, children }: { hint: string; action: ReactNode; children?: ReactNode }) {
    return (
        <div className="flex flex-col gap-2 border-t border-border pt-3">
            <div>{action}</div>
            <p className="text-sm text-fg-muted">{hint}</p>
            {children}
        </div>
    );
}

// The line above the domain's tabs when its file needs the owner, or was kept
// by the owner's choice (the domains list carries the state for an
// administrator). A certificate waiting on the file outranks the choice.
// Dosya sahibin kararını beklediğinde ya da seçimiyle korunduğunda alan adının
// sekmelerinin üstündeki satır.
export function SiteConfigNotice({ state, keptByChoice, pendingReason, onOpen }: {
    state: string;
    keptByChoice?: boolean;
    pendingReason?: string;
    onOpen: () => void;
}) {
    const { t } = useI18n();
    const kept = KEPT.has(state);
    const key: TranslationKey | null = kept && pendingReason === 'certificate' ? 'siteConfig.notice.certificate'
        : kept && pendingReason === 'certificate_validation' ? 'siteConfig.notice.certificateValidation'
            : kept && keptByChoice ? 'siteConfig.notice.keptByChoice'
                : kept ? 'siteConfig.notice.kept'
                    : state === 'missing' ? 'siteConfig.notice.missing'
                        : state === 'unreadable' ? 'siteConfig.notice.unreadable' : null;
    if (!key) return null;
    // Kept as chosen and nothing waiting on it: a plain line, not a warning.
    const plain = key === 'siteConfig.notice.keptByChoice';
    return (
        <div
            role="status"
            className={`mb-4 flex flex-wrap items-center gap-x-3 gap-y-2 rounded-lg border p-3 text-sm text-fg ${plain
                ? 'border-border-strong bg-surface-2'
                : 'border-warning-mark/50 bg-warning-mark/20'}`}
            data-site-config-notice={state}
            data-site-config-notice-kind={plain ? 'kept_by_choice' : pendingReason || 'attention'}
        >
            {plain
                ? <Info className="h-4 w-4 shrink-0 text-fg-muted" aria-hidden="true" />
                : <AlertTriangle className="h-4 w-4 shrink-0 text-warning" aria-hidden="true" />}
            <span className="min-w-0 flex-[1_1_16rem]">{t(key)}</span>
            <Button type="button" onClick={onOpen}>{t('siteConfig.notice.open')}</Button>
        </div>
    );
}
