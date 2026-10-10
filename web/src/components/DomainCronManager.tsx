import { useState, useEffect } from 'react';
import {
    Clock, Plus, Trash2, Edit2, RefreshCw,
    Play, Pause, Save, X, Info, AlertTriangle,
} from 'lucide-react';
import { showToast } from './Toast';
import { apiErrorText, readApiError, type ApiError } from '../lib/apiError';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import { Button, CouldNotCheck, KnownEmpty, inputClass } from './ui';
import type { Observed } from '../lib/remote';
import { CurrentGate, StaleNotice, isStaleWrite, readCurrent } from './CurrentSettings';

interface CronJob {
    id: string;
    schedule: string;
    command: string;
    enabled: boolean;
    comment: string;
}

interface DomainCronManagerProps {
    domainId: number;
    domainName: string;
    readOnly?: boolean;
}

const schedulePresets: { labelKey: TranslationKey; value: string }[] = [
    { labelKey: 'cron.preset.everyMinute', value: '* * * * *' },
    { labelKey: 'cron.preset.every5', value: '*/5 * * * *' },
    { labelKey: 'cron.preset.every15', value: '*/15 * * * *' },
    { labelKey: 'cron.preset.hourly', value: '0 * * * *' },
    { labelKey: 'cron.preset.every6h', value: '0 */6 * * *' },
    { labelKey: 'cron.preset.dailyMidnight', value: '0 0 * * *' },
    { labelKey: 'cron.preset.dailyNoon', value: '0 12 * * *' },
    { labelKey: 'cron.preset.weekly', value: '0 0 * * 0' },
    { labelKey: 'cron.preset.monthly', value: '0 0 1 * *' },
];

// The causes of an unreadable crontab the server verifies itself (10 Oct 2026).
// Each is a rule the server owner set, not something that broke: the notice is
// drawn on the neutral surface, never as a warning. Any other answer gets the sentence
// that names no cause.
// Sunucunun kendisinin doğruladığı okunamayan-crontab nedenleri. Her biri
// sunucu sahibinin koyduğu bir kuraldır, bozulan bir şey değil.
const unreadableCauses: Record<string, TranslationKey> = {
    cron_allow: 'cron.unknown.cron_allow',
    cron_deny: 'cron.unknown.cron_deny',
};

// Real crontab management through the agent: add, edit, enable/disable and
// delete the domain user's scheduled tasks, with human-readable presets.
//
// Agent üzerinden gerçek crontab yönetimi: domain kullanıcısının zamanlanmış
// görevlerini ekle, düzenle, etkinleştir/devre dışı bırak ve sil; insan-okur
// hazır kalıplarla.
export function DomainCronManager({ domainId, readOnly = false }: DomainCronManagerProps) {
    const { t } = useI18n();
    const [jobs, setJobs] = useState<CronJob[]>([]);
    const [loading, setLoading] = useState(true);
    const [showForm, setShowForm] = useState(false);
    const [editingJob, setEditingJob] = useState<CronJob | null>(null);

    const [schedule, setSchedule] = useState('0 * * * *');
    const [command, setCommand] = useState('');
    const [comment, setComment] = useState('');
    const [saving, setSaving] = useState(false);
    // A known server condition that stops every scheduled task on this server
    // (today: CRON_NOT_INSTALLED). It stays on screen with the owner's next
    // action instead of a toast, and is cleared only by a successful read.
    // Bu sunucudaki her zamanlanmış görevi durduran bilinen bir sunucu durumu;
    // bir bildirim yerine sahibin sonraki adımıyla ekranda kalır ve yalnız
    // başarılı bir okumayla temizlenir.
    const [blocked, setBlocked] = useState('');
    // The crontab the list was read from; every change carries it back, so a
    // change built from an older list is refused instead of written.
    // Listenin okunduğu crontab; her değişiklik onu geri taşır.
    const [version, setVersion] = useState('');
    // The list could not be read. That is not "no tasks": nothing is listed,
    // nothing can be added, and the screen says so with a way to try again.
    // Liste okunamadı. Bu "görev yok" değildir.
    const [unknown, setUnknown] = useState(false);
    // What the server said when it could not read the list: the cause it
    // verified, when it verified one, and the line crontab itself printed.
    // Sunucunun listeyi okuyamadığında söylediği: doğruladığı neden ve
    // crontab'ın kendi yazdığı satır.
    const [unreadable, setUnreadable] = useState<ApiError | null>(null);
    // The answer that listed the tasks, kept as the proof of an empty list.
    // Görevleri listeleyen yanıt; boş listenin kanıtı olarak tutulur.
    const [listed, setListed] = useState<Observed<CronJob[]> | null>(null);
    // The server refused a change because the crontab changed after this list
    // loaded. The form keeps what was typed; the notice says how to go on.
    // Sunucu, liste yüklendikten sonra crontab değiştiği için reddetti.
    const [stale, setStale] = useState(false);

    // refused reads the coded API error once. A stale change becomes the
    // on-screen reload notice and the cron-missing answer the on-screen
    // explanation; everything else is the error toast.
    // refused kodlu API hatasını bir kez okur.
    const refused = async (res: Response) => {
        const error = await readApiError(res);
        if (isStaleWrite(error)) {
            setStale(true);
            return;
        }
        const text = apiErrorText(error, t);
        if (error.code === 'CRON_NOT_INSTALLED') setBlocked(text);
        showToast('error', text);
    };

    useEffect(() => {
        loadJobs();
    }, [domainId]);

    const loadJobs = async () => {
        setLoading(true);
        const next = await readCurrent<{ jobs?: CronJob[] | null; version?: string }>(`/api/v1/domains/${domainId}/cron`);
        const missing = next.state === 'unknown' && next.error.code === 'CRON_NOT_INSTALLED';
        // The list is proven empty only by an answer that carries it: `jobs` as a
        // list, or the `null` the server writes for a list with no rows.
        // Liste, ancak onu taşıyan yanıtla boş sayılır.
        const list = next.state === 'known' && (next.value.jobs === null || Array.isArray(next.value.jobs)) ? next.value.jobs ?? [] : null;
        setJobs(list ?? []);
        setListed(list ? { value: list, observedAt: Date.now() } : null);
        setVersion(next.state === 'known' && list ? next.value.version || '' : '');
        setBlocked(missing ? apiErrorText(next.error, t) : '');
        setUnknown((next.state === 'unknown' && !missing) || (next.state === 'known' && list === null));
        setUnreadable(next.state === 'unknown' && !missing ? next.error : null);
        setStale(false);
        setLoading(false);
    };

    const resetForm = () => {
        setSchedule('0 * * * *');
        setCommand('');
        setComment('');
        setEditingJob(null);
        setShowForm(false);
    };

    const submitForm = async () => {
        if (readOnly) return;
        if (!command.trim()) {
            showToast('error', t('cron.commandRequired'));
            return;
        }
        setSaving(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/cron`, {
                method: editingJob ? 'PUT' : 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(
                    editingJob
                        ? { id: editingJob.id, schedule, command, enabled: editingJob.enabled, comment, version }
                        : { schedule, command, comment, version },
                ),
            });
            if (!res.ok) {
                await refused(res);
                return;
            }
            const data = await res.json();
            if (!data.success) throw new Error(data.error);
            showToast('success', editingJob ? t('cron.updated') : t('cron.added'));
            resetForm();
            loadJobs();
        } catch {
            showToast('error', t('common.error'));
        } finally {
            setSaving(false);
        }
    };

    const toggleJob = async (job: CronJob) => {
        if (readOnly) return;
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/cron`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ ...job, enabled: !job.enabled, version }),
            });
            if (!res.ok) {
                await refused(res);
                return;
            }
            const data = await res.json();
            if (!data.success) throw new Error(data.error);
            showToast('success', job.enabled ? t('cron.disabledMsg') : t('cron.enabledMsg'));
            loadJobs();
        } catch {
            showToast('error', t('common.error'));
        }
    };

    const deleteJob = async (job: CronJob) => {
        if (readOnly) return;
        if (!confirm(`${t('cron.deleteConfirm')}\n${job.command}`)) return;
        try {
            const res = await fetch(
                `/api/v1/domains/${domainId}/cron?id=${encodeURIComponent(job.id)}&version=${encodeURIComponent(version)}`,
                { method: 'DELETE' },
            );
            if (!res.ok) {
                await refused(res);
                return;
            }
            const data = await res.json();
            if (!data.success) throw new Error();
            showToast('success', t('cron.deleted'));
            loadJobs();
        } catch {
            showToast('error', t('common.error'));
        }
    };

    const startEdit = (job: CronJob) => {
        if (readOnly) return;
        setEditingJob(job);
        setSchedule(job.schedule);
        setCommand(job.command);
        setComment(job.comment || '');
        setShowForm(true);
    };

    // Prefer the preset label when the expression matches one; otherwise show
    // the raw cron expression.
    // İfade bir kalıpla eşleşiyorsa kalıp etiketini, yoksa ham cron ifadesini
    // göster.
    const describeSchedule = (expr: string) => {
        const preset = schedulePresets.find((p) => p.value === expr);
        return preset ? t(preset.labelKey) : expr;
    };

    return (
        <div className="space-y-5">
            {/* Add / edit form. Withdrawn while the list is unknown: a task
                cannot be saved against a crontab that was not read.
                Liste bilinmezken form geri çekilir. */}
            {!readOnly && showForm && !unknown && (
                <div className="rounded-xl border border-border bg-surface-2/50 p-4">
                    <div className="mb-4 flex items-center justify-between">
                        <h3 className="text-sm font-semibold text-fg">
                            {editingJob ? t('cron.editTitle') : t('cron.addTitle')}
                        </h3>
                        <button onClick={resetForm} className="rounded-md p-1 text-fg-muted hover:bg-surface-2 hover:text-fg">
                            <X className="h-4 w-4" />
                        </button>
                    </div>

                    <div className="space-y-4">
                        <div>
                            <label className="mb-1.5 block text-sm font-medium text-fg-muted">{t('cron.schedule')}</label>
                            <div className="flex flex-wrap gap-2">
                                <select
                                    value={schedulePresets.find((p) => p.value === schedule)?.value ?? ''}
                                    onChange={(e) => e.target.value && setSchedule(e.target.value)}
                                    className={`${inputClass} w-auto`}
                                >
                                    <option value="">{t('cron.custom')}</option>
                                    {schedulePresets.map((p) => (
                                        <option key={p.value} value={p.value}>
                                            {t(p.labelKey)}
                                        </option>
                                    ))}
                                </select>
                                <input
                                    type="text"
                                    value={schedule}
                                    onChange={(e) => setSchedule(e.target.value)}
                                    placeholder="* * * * *"
                                    className={`${inputClass} flex-1 font-mono`}
                                />
                            </div>
                            <p className="mt-1 text-xs text-fg-subtle">{t('cron.scheduleFormat')}</p>
                        </div>

                        <div>
                            <label className="mb-1.5 block text-sm font-medium text-fg-muted">{t('cron.command')}</label>
                            <input
                                type="text"
                                value={command}
                                onChange={(e) => setCommand(e.target.value)}
                                placeholder="/usr/bin/php /var/www/example.com/cron.php"
                                className={`${inputClass} font-mono`}
                            />
                        </div>

                        <div>
                            <label className="mb-1.5 block text-sm font-medium text-fg-muted">{t('cron.comment')}</label>
                            <input
                                type="text"
                                value={comment}
                                onChange={(e) => setComment(e.target.value)}
                                className={inputClass}
                            />
                        </div>

                        <div className="flex justify-end gap-2">
                            <Button onClick={resetForm}>{t('cron.cancel')}</Button>
                            <Button variant="primary" icon={Save} onClick={submitForm} disabled={saving || loading || stale || !command.trim()}>
                                {saving ? t('cron.saving') : editingJob ? t('cron.update') : t('cron.save')}
                            </Button>
                        </div>
                    </div>
                </div>
            )}

            {/* Job list */}
            <section>
                <div className="mb-3 flex items-center justify-between">
                    <h3 className="text-sm font-semibold text-fg">{t('cron.title')}</h3>
                    <div className="flex items-center gap-2">
                        {/* A new task cannot be saved while cron is missing; the
                            guidance below says who installs it. Refresh re-reads.
                            Cron yokken yeni görev kaydedilemez; aşağıdaki
                            yönlendirme onu kimin kuracağını söyler. */}
                        {!readOnly && !showForm && !blocked && !loading && !unknown && (
                            <Button variant="primary" icon={Plus} onClick={() => setShowForm(true)}>
                                {t('cron.add')}
                            </Button>
                        )}
                        <button
                            onClick={loadJobs}
                            title={t('files.refresh')}
                            aria-label={t('files.refresh')}
                            className="rounded-md p-1.5 text-fg-muted hover:bg-surface-2 hover:text-fg"
                        >
                            <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} aria-hidden="true" />
                        </button>
                    </div>
                </div>

                {/* The live region stays mounted so a later refusal is
                    announced; a condition found on load is polite, like the
                    domain list's pending-deletion guidance.
                    Canlı bölge hep takılı kalır; yüklemede bulunan durum,
                    alan adı listesindeki bekleyen silme gibi nazikçe okunur. */}
                <div role="status">
                    {blocked && (
                        <div className="mb-3 flex items-start gap-2 rounded-lg border border-warning-mark/50 bg-warning-mark/20 p-3 text-sm leading-relaxed text-fg">
                            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" aria-hidden="true" />
                            <p className="min-w-0 max-w-[75ch] break-words">{blocked}</p>
                        </div>
                    )}
                </div>

                {stale && <StaleNotice textKey="cron.stale" actionKey="cron.reload" onReload={loadJobs} busy={loading} />}

                {/* Reading and "could not read" come before any list: neither
                    is "no scheduled tasks".
                    Okunuyor ve "okunamadı", her listeden önce gelir. */}
                {loading ? (
                    <CurrentGate state="loading" unknownKey="cron.unknown" onRetry={loadJobs} />
                ) : unknown ? (
                    <CronUnreadable error={unreadable} onRetry={loadJobs} />
                ) : !listed ? null : blocked && jobs.length === 0 ? null : jobs.length === 0 ? (
                    <KnownEmpty of={listed} icon={Clock} title={t('cron.empty')} hint={t('cron.emptyHint')} />
                ) : (
                    <div className="space-y-2">
                        {jobs.map((job) => (
                            <div
                                key={job.id}
                                className={`rounded-xl border border-border bg-surface p-4 ${job.enabled ? '' : 'opacity-60'}`}
                            >
                                <div className="flex items-start justify-between gap-3">
                                    <div className="min-w-0 flex-1">
                                        {job.comment && <p className="mb-1 text-sm text-fg-muted">{job.comment}</p>}
                                        <p className="truncate font-mono text-sm text-fg">{job.command}</p>
                                        <div className="mt-2 flex flex-wrap items-center gap-2 text-xs text-fg-muted">
                                            <span className="flex items-center gap-1">
                                                <Clock className="h-3 w-3" />
                                                {describeSchedule(job.schedule)}
                                            </span>
                                            <code className="rounded bg-surface-2 px-1.5 py-0.5">{job.schedule}</code>
                                            {!job.enabled && (
                                                <span className="rounded bg-warning/15 px-1.5 py-0.5 text-warning">
                                                    {t('cron.disabledBadge')}
                                                </span>
                                            )}
                                        </div>
                                    </div>

                                    {!readOnly && (
                                        <div className="flex items-center gap-0.5">
                                            <button
                                                onClick={() => toggleJob(job)}
                                                title={job.enabled ? t('cron.disable') : t('cron.enable')}
                                                className={`rounded-md p-2 hover:bg-surface-2 ${job.enabled ? 'text-success' : 'text-fg-muted hover:text-fg'}`}
                                            >
                                                {job.enabled ? <Pause className="h-4 w-4" /> : <Play className="h-4 w-4" />}
                                            </button>
                                            <button
                                                onClick={() => startEdit(job)}
                                                title={t('cron.edit')}
                                                className="rounded-md p-2 text-fg-muted hover:bg-surface-2 hover:text-fg"
                                            >
                                                <Edit2 className="h-4 w-4" />
                                            </button>
                                            <button
                                                onClick={() => deleteJob(job)}
                                                title={t('cron.delete')}
                                                className="rounded-md p-2 text-fg-muted hover:bg-surface-2 hover:text-danger"
                                            >
                                                <Trash2 className="h-4 w-4" />
                                            </button>
                                        </div>
                                    )}
                                </div>
                            </div>
                        ))}
                    </div>
                )}
            </section>

            <p className="flex items-start gap-2 text-xs text-fg-subtle">
                <Info className="mt-0.5 h-3.5 w-3.5 shrink-0" />
                {t('cron.formatNote')}
            </p>
        </div>
    );
}

// Why the list could not be read, in the order D-024 asks for: the reason, who
// acts, the action, how work resumes, and then the read again.
//
// A cause the server verified (the user is not in /etc/cron.allow, or is in
// /etc/cron.deny) is the server owner's rule. It is said on the neutral
// surface with the plain mark, not the attention one: nothing failed, and Retry
// is how the list comes back once the owner has changed the rule. Every other answer is the ordinary
// could-not-check notice, which names no cause, followed by the line the
// server's crontab program printed when it printed one. That line is the
// program's own, so it is in the mono face.
//
// Listenin neden okunamadığı, D-024 sırasıyla. Sunucunun doğruladığı neden
// sunucu sahibinin kuralıdır: yansız yüzeyde söylenir, hiçbir şey başarısız
// olmamıştır. Diğer her yanıt, neden adlandırmayan olağan bildirimdir; ardından
// crontab programının yazdığı satır gelir.
function CronUnreadable({ error, onRetry }: { error: ApiError | null; onRetry: () => void }) {
    const { t } = useI18n();
    const cause = error?.code === 'CURRENT_SETTINGS_UNREADABLE' ? unreadableCauses[error.detail ?? ''] : undefined;
    if (cause) {
        return (
            <div
                role="status"
                data-cron-unreadable={error?.detail}
                className="flex items-start gap-2 rounded-lg border border-border-strong bg-surface-2 p-3 text-sm leading-relaxed text-fg"
            >
                <Info className="mt-0.5 h-4 w-4 shrink-0 text-fg-muted" aria-hidden="true" />
                <div className="min-w-0">
                    <p className="max-w-[75ch] break-words">{t(cause)}</p>
                    <div className="mt-2">
                        <Button type="button" onClick={onRetry}>
                            {t('common.retry')}
                        </Button>
                    </div>
                </div>
            </div>
        );
    }
    const said = error?.code === 'CURRENT_SETTINGS_UNREADABLE' ? error.vars?.detail : undefined;
    if (!said) return <CouldNotCheck text={t('cron.unknown')} onRetry={onRetry} />;
    // The sentence around the program's line comes from the catalogue; the
    // line itself is set apart in the mono face.
    // Satırın çevresindeki cümle katalogdandır; satırın kendisi mono yazılır.
    const mark = String.fromCharCode(1);
    const [lead, tail = ''] = t('cron.unknown.said', { detail: mark }).split(mark);
    return (
        <CouldNotCheck
            text={
                <>
                    {t('cron.unknown')}
                    <span data-cron-said className="mt-1.5 block text-xs text-fg-muted">
                        {lead}
                        <span className="break-words font-mono text-fg">{said}</span>
                        {tail}
                    </span>
                </>
            }
            onRetry={onRetry}
        />
    );
}
