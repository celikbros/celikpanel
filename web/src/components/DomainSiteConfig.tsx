import { useState, type ReactNode } from 'react';
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

type Done = { kind: 'keep' | 'take' | 'recreate'; backup?: string };

export function DomainSiteConfig({ domainId }: { domainId: number; domainName: string }) {
    const { t } = useI18n();
    const url = `/api/v1/domains/${domainId}/site-config`;
    const config = useRemote(url, decodeSiteConfig);
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
    const include = view.include_dir ? (
        <p className="text-sm text-fg-muted" data-site-config-include>
            {t('siteConfig.include', { dir: view.include_dir })}
        </p>
    ) : null;

    if (view.state === 'managed_unchanged') {
        return (
            <div className="space-y-3">
                <Plain icon={Info} title={t('siteConfig.managed.title')}>
                    <p>{t('siteConfig.managed.body')}</p>
                    {view.adopted_from && <p className="mt-1">{t('siteConfig.adopted', { release: view.adopted_from })}</p>}
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
                    <p className="mt-1">{t('siteConfig.unreadable.next')}</p>
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
                            {view.diff.split('\n').map((line, index) => (
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

// The line above the domain's tabs when its file needs the owner (the
// domains list carries the state for an administrator).
// Dosya sahibin kararını beklediğinde alan adının sekmelerinin üstündeki satır.
export function SiteConfigNotice({ state, onOpen }: { state: string; onOpen: () => void }) {
    const { t } = useI18n();
    const key: TranslationKey | null = KEPT.has(state) ? 'siteConfig.notice.kept'
        : state === 'missing' ? 'siteConfig.notice.missing'
            : state === 'unreadable' ? 'siteConfig.notice.unreadable' : null;
    if (!key) return null;
    return (
        <div role="status" className="mb-4 flex flex-wrap items-center gap-x-3 gap-y-2 rounded-lg border border-warning-mark/50 bg-warning-mark/20 p-3 text-sm text-fg" data-site-config-notice={state}>
            <AlertTriangle className="h-4 w-4 shrink-0 text-warning" aria-hidden="true" />
            <span className="min-w-0 flex-[1_1_16rem]">{t(key)}</span>
            <Button type="button" onClick={onOpen}>{t('siteConfig.notice.open')}</Button>
        </div>
    );
}
