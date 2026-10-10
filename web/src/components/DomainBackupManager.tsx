import { useState, useEffect } from 'react';
import {
    Archive, RefreshCw, Download, Trash2, RotateCcw,
    HardDrive, Database, Clock, Info, type LucideIcon,
} from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import { apiErrorText, readApiError } from '../lib/apiError';
import { Button, CouldNotCheck, KnownEmpty, RemoteGate, ResultUnknown, inputClass } from './ui';
import { CurrentGate, StaleNotice, isStaleWrite, readCurrent, type Current } from './CurrentSettings';
import { decodeListIn, mapRemote, useRemote } from '../lib/remote';
import { useLostAnswer } from '../lib/lostAnswer';
import { decodeDomainDatabases } from '../lib/domainDatabases';
import { saveDownload } from '../lib/download';

interface BackupItem {
    name: string;
    size: number;
    type: string;
    origin: 'manual' | 'scheduled' | 'pre_restore' | string;
    database_id?: number;
    legacy: boolean;
    restorable: boolean;
    created_at: string;
}

interface DomainBackupManagerProps {
    domainId: number;
    domainName: string;
    readOnly?: boolean;
}

type BackupType = 'files' | 'database' | 'full';

// The three backup flavours, with categorical colours readable in both themes.
// Üç yedek türü; iki temada da okunur kategorik renklerle.
const backupTypes: { type: BackupType; icon: LucideIcon; labelKey: TranslationKey; descKey: TranslationKey; tone: string }[] = [
    { type: 'files', icon: HardDrive, labelKey: 'backup.files', descKey: 'backup.filesDesc', tone: 'text-primary bg-primary/10' },
    { type: 'database', icon: Database, labelKey: 'backup.database', descKey: 'backup.databaseDesc', tone: 'text-success bg-success/10' },
    { type: 'full', icon: Archive, labelKey: 'backup.full', descKey: 'backup.fullDesc', tone: 'text-warning bg-warning/15' },
];

const typeLabelKey: Record<string, TranslationKey> = {
    files: 'backup.type.files',
    database: 'backup.type.database',
    full: 'backup.type.full',
};

const originLabelKey: Record<string, TranslationKey> = {
    manual: 'backup.origin.manual',
    scheduled: 'backup.origin.scheduled',
    pre_restore: 'backup.origin.preRestore',
};

type Translate = (key: TranslationKey, vars?: Record<string, string | number>) => string;

async function readJSONResponse<T>(res: Response, t: Translate): Promise<T> {
    if (!res.ok) {
        throw new Error(apiErrorText(await readApiError(res), t));
    }
    const text = (await res.text()).trim();
    if (!text) return {} as T;

    let data: T & { success?: boolean; error?: string };
    try {
        data = JSON.parse(text);
    } catch {
        throw new Error(t('common.error'));
    }
    if (data.success === false || (data.error && data.success !== true)) {
        throw new Error(data.error || t('common.error'));
    }
    return data;
}

const decodeBackups = (raw: unknown) => decodeListIn<BackupItem>(raw, 'backups');

function errorText(error: unknown, t: Translate): string {
    return error instanceof Error && error.message ? error.message : t('common.error');
}

// Real backups via the agent (tar/dump under /var/backups/celikpanel).
// Create, restore, download, delete — no invented rows.
// Agent üzerinden gerçek yedekler (/var/backups/celikpanel altında tar/dump).
// Oluştur, geri yükle, indir, sil — uydurma satır yok.
export function DomainBackupManager({ domainId, domainName, readOnly = false }: DomainBackupManagerProps) {
    const { t } = useI18n();
    // The backups and the linked databases are read from the server; neither
    // is ever an empty list by default. "No backups yet" and "No linked
    // databases" are said only for an answer. A backup is made, restored or
    // deleted only against the list the server last sent, and a database or
    // full backup only for databases it named.
    // Yedekler ve bağlı veritabanları sunucudan okunur; hiçbiri varsayılan
    // olarak boş liste değildir. "Henüz yedek yok" ve "Bağlı veritabanı yok"
    // yalnız bir yanıt için söylenir.
    const backups = useRemote(`/api/v1/domains/${domainId}/backups`, decodeBackups);
    const databasesRead = useRemote(`/api/v1/domains/${domainId}/databases`, decodeDomainDatabases);
    const databasesRemote = mapRemote(databasesRead.remote, (value) => value.databases);
    const databases = databasesRemote.state === 'known' ? databasesRemote.value : [];
    const databasesKnown = databasesRemote.state === 'known';
    const databaseLoading = databasesRemote.state === 'loading';
    const [pickedDatabaseId, setSelectedDatabaseId] = useState('');
    // The picked database, as long as the server still lists it; otherwise the
    // first one it lists.
    // Sunucu hâlâ listeliyorsa seçilen veritabanı; yoksa listelediği ilki.
    const selectedDatabaseId = databases.some((database) => String(database.id) === pickedDatabaseId)
        ? pickedDatabaseId
        : databases[0] ? String(databases[0].id) : '';
    const [creating, setCreating] = useState<BackupType | null>(null);
    const [restoring, setRestoring] = useState<string | null>(null);
    const [deleting, setDeleting] = useState<string | null>(null);
    const [downloading, setDownloading] = useState<string | null>(null);
    const answer = useLostAnswer(() => backups.retry());

    const loading = backups.reading;
    const loadBackups = () => backups.retry();
    // A change needs the list as the server has it now, and no earlier change
    // whose result is still unknown.
    // Değişiklik, listenin sunucudaki güncel hâlini ve sonucu hâlâ bilinmeyen
    // önceki bir değişikliğin olmamasını ister.
    const listKnown = backups.remote.state === 'known' && !backups.reading && !answer.holding;
    const busy = creating !== null || restoring !== null || deleting !== null || downloading !== null;

    // The answer to a change: the server's refusal as its own sentence, a
    // body that says the change failed, or the data of a change that was made.
    // Değişikliğin yanıtı: sunucunun reddi kendi cümlesiyle, değişikliğin
    // başarısız olduğunu söyleyen gövde ya da yapılan değişikliğin verisi.
    const answered = async <T,>(res: Response): Promise<T | null> => {
        try {
            return await readJSONResponse<T>(res, t);
        } catch (error) {
            showToast('error', errorText(error, t));
            return null;
        }
    };

    const createBackup = async (type: BackupType) => {
        if (readOnly || !listKnown) return;
        if (type === 'database' && !selectedDatabaseId) return;
        if (type !== 'files' && !databasesKnown) return;
        setCreating(type);
        try {
            const res = await answer.send(`/api/v1/domains/${domainId}/backups`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    type,
                    ...(type === 'database' ? { database_id: Number(selectedDatabaseId) } : {}),
                }),
            });
            if (!res) return;
            if (!(await answered<{ success?: boolean; error?: string }>(res))) return;
            showToast('success', t('backup.created'));
            answer.settle();
            await loadBackups();
        } finally {
            setCreating(null);
        }
    };

    const restoreBackup = async (backup: BackupItem) => {
        if (readOnly || !listKnown) return;
        if (!backup.restorable || !confirm(t('backup.restoreConfirm', { name: backup.name }))) return;
        setRestoring(backup.name);
        try {
            const res = await answer.send(`/api/v1/domains/${domainId}/backups/restore`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ backup_name: backup.name }),
            });
            if (!res) return;
            const data = await answered<{ success?: boolean; error?: string; safety_backup?: BackupItem }>(res);
            if (!data) return;
            showToast('success', data.safety_backup
                ? t('backup.restoredWithSafety', { name: data.safety_backup.name })
                : t('backup.restored'));
            answer.settle();
            await loadBackups();
        } finally {
            setRestoring(null);
        }
    };

    const deleteBackup = async (name: string) => {
        if (readOnly || !listKnown) return;
        if (!confirm(t('backup.deleteConfirm', { name }))) return;
        setDeleting(name);
        try {
            const res = await answer.send(`/api/v1/domains/${domainId}/backups?name=${encodeURIComponent(name)}`, {
                method: 'DELETE',
            });
            if (!res) return;
            if (!(await answered<{ success?: boolean; error?: string }>(res))) return;
            showToast('success', t('backup.deleted'));
            answer.settle();
            await loadBackups();
        } finally {
            setDeleting(null);
        }
    };

    const downloadBackup = async (name: string) => {
        setDownloading(name);
        try {
            const refusal = await saveDownload(`/api/v1/domains/${domainId}/backups/download?name=${encodeURIComponent(name)}`, name);
            if (refusal) showToast('error', apiErrorText(refusal, t));
        } finally {
            setDownloading(null);
        }
    };

    return (
        <div className="space-y-5">
            {/* Create */}
            {!readOnly && <section aria-busy={creating !== null}>
                <h3 className="mb-3 text-sm font-semibold text-fg">{t('backup.createTitle')}</h3>
                <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
                    {backupTypes.filter(({ type }) => type === 'files').map(({ type, icon: Icon, labelKey, descKey, tone }) => (
                        <button
                            key={type}
                            type="button"
                            onClick={() => void createBackup(type)}
                            disabled={busy || !listKnown}
                            className="flex items-center gap-3 rounded-xl border border-border bg-surface p-4 text-left transition-colors hover:border-primary/40 hover:bg-surface-2 disabled:cursor-not-allowed disabled:opacity-50"
                        >
                            <span className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-lg ${tone}`}>
                                <Icon className="h-5 w-5" aria-hidden="true" />
                            </span>
                            <span>
                                <span className="block text-sm font-semibold text-fg">{t(labelKey)}</span>
                                <span className="block text-xs text-fg-muted">{t(descKey)}</span>
                            </span>
                        </button>
                    ))}

                    <div className="rounded-xl border border-border bg-surface p-4">
                        <div className="mb-3 flex items-start gap-3">
                            <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-success/10 text-success">
                                <Database className="h-5 w-5" aria-hidden="true" />
                            </span>
                            <span>
                                <span className="block text-sm font-semibold text-fg">{t('backup.database')}</span>
                                <span className="block text-xs text-fg-muted">{t('backup.databaseDesc')}</span>
                            </span>
                        </div>
                        <label htmlFor="backup-database" className="sr-only">{t('backup.databaseSelect')}</label>
                        <select
                            id="backup-database"
                            value={selectedDatabaseId}
                            onChange={(event) => setSelectedDatabaseId(event.target.value)}
                            disabled={busy || !databasesKnown || databases.length === 0}
                            className={`${inputClass} mb-2`}
                        >
                            {/* "No linked databases" is an option only for an
                                answer that lists none.
                                "Bağlı veritabanı yok" yalnız hiçbirini
                                listelemeyen yanıt için bir seçenektir. */}
                            {databaseLoading ? (
                                <option value="">{t('backup.loadingDatabases')}</option>
                            ) : !databasesKnown ? (
                                <option value="">{t('backup.databasesNotRead')}</option>
                            ) : databases.length === 0 ? (
                                <option value="">{t('backup.noDatabases')}</option>
                            ) : databases.map((database) => (
                                <option key={database.id} value={database.id}>
                                    {database.name} ({database.type})
                                </option>
                            ))}
                        </select>
                        <Button
                            type="button"
                            variant="secondary"
                            disabled={busy || !listKnown || !databasesKnown || !selectedDatabaseId}
                            onClick={() => void createBackup('database')}
                            className="w-full justify-center"
                        >
                            {creating === 'database' ? t('backup.creating') : t('backup.create')}
                        </Button>
                    </div>

                    <button
                        type="button"
                        onClick={() => void createBackup('full')}
                        disabled={busy || !listKnown || !databasesKnown}
                        className="flex items-center gap-3 rounded-xl border border-border bg-surface p-4 text-left transition-colors hover:border-primary/40 hover:bg-surface-2 disabled:cursor-not-allowed disabled:opacity-50"
                    >
                        <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-warning/15 text-warning">
                            <Archive className="h-5 w-5" aria-hidden="true" />
                        </span>
                        <span>
                            <span className="block text-sm font-semibold text-fg">{t('backup.full')}</span>
                            <span className="block text-xs text-fg-muted">
                                {databasesKnown ? t('backup.fullDesc', { count: databases.length }) : t('backup.fullDescLoading')}
                            </span>
                        </span>
                    </button>
                </div>
                {databasesRemote.state === 'unknown' && (
                    <CouldNotCheck
                        className="mt-3"
                        text={t('backup.databasesUnknown')}
                        onRetry={() => void databasesRead.retry()}
                        busy={databasesRead.reading}
                    />
                )}
                {creating !== null && (
                    <p className="mt-3 flex items-center gap-2 text-sm text-primary" role="status" aria-live="polite">
                        <RefreshCw className="h-4 w-4 animate-spin" aria-hidden="true" />
                        {t('backup.creating')}
                    </p>
                )}
            </section>}

            <AutoBackupSection domainId={domainId} databaseCount={databases.length} readOnly={readOnly} />

            {/* List */}
            <section aria-busy={loading || busy}>
                <div className="mb-3 flex items-center justify-between">
                    <h3 className="text-sm font-semibold text-fg">{t('backup.existing')}</h3>
                    <button
                        type="button"
                        onClick={() => void loadBackups()}
                        disabled={loading || busy}
                        title={t('files.refresh')}
                        aria-label={t('files.refresh')}
                        className="rounded-md p-1.5 text-fg-muted hover:bg-surface-2 hover:text-fg disabled:cursor-not-allowed disabled:opacity-50"
                    >
                        <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} aria-hidden="true" />
                    </button>
                </div>

                <ResultUnknown answer={answer} className="mb-3" />

                <RemoteGate
                    remote={backups.remote}
                    checking={t('backup.checking')}
                    failed={t('backup.unknown')}
                    onRetry={() => void loadBackups()}
                    busy={loading}
                >
                {(shown) => shown.value.length === 0 ? (
                    <KnownEmpty of={shown} icon={Archive} title={t('backup.empty')} hint={readOnly ? undefined : t('backup.emptyHint')} />
                ) : (
                    <div className="space-y-2">
                        {shown.value.map((backup) => {
                            const typeDef = backupTypes.find((b) => b.type === backup.type);
                            const Icon = typeDef?.icon ?? Archive;
                            const restoreBlocked = !backup.restorable;
                            const blockedReason = backup.legacy && backup.type === 'database'
                                ? t('backup.legacyDatabaseUnrestorable')
                                : t('backup.notRestorable');
                            return (
                                <div
                                    key={backup.name}
                                    className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-border bg-surface p-4"
                                >
                                    <div className="flex min-w-0 items-center gap-3">
                                        <span className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-lg ${typeDef?.tone ?? 'bg-surface-2 text-fg-muted'}`}>
                                            <Icon className="h-4 w-4" aria-hidden="true" />
                                        </span>
                                        <div className="min-w-0">
                                            <p className="truncate text-sm font-medium text-fg">{backup.name}</p>
                                            <div className="mt-0.5 flex flex-wrap items-center gap-3 text-xs text-fg-muted">
                                                <span className="flex items-center gap-1">
                                                    <Clock className="h-3 w-3" aria-hidden="true" />
                                                    {fmtDate(backup.created_at)}
                                                </span>
                                                <span>{fmtSize(backup.size)}</span>
                                                <span className="rounded bg-surface-2 px-1.5 py-0.5">
                                                    {t(typeLabelKey[backup.type] ?? 'backup.type.full')}
                                                </span>
                                                <span className="rounded bg-surface-2 px-1.5 py-0.5">
                                                    {t(originLabelKey[backup.origin] ?? 'backup.origin.unknown')}
                                                </span>
                                                <span className="rounded bg-surface-2 px-1.5 py-0.5">
                                                    {t(backup.legacy ? 'backup.format.legacy' : 'backup.format.current')}
                                                </span>
                                                <span className={`rounded px-1.5 py-0.5 ${backup.restorable ? 'bg-success/10 text-success' : 'bg-warning/15 text-warning'}`}>
                                                    {t(backup.restorable ? 'backup.restorable.yes' : 'backup.restorable.no')}
                                                </span>
                                                {backup.database_id ? (
                                                    <span>{t('backup.databaseId', { id: backup.database_id })}</span>
                                                ) : null}
                                            </div>
                                            {restoreBlocked && (
                                                <p className="mt-1 text-xs text-warning" role="note">{blockedReason}</p>
                                            )}
                                        </div>
                                    </div>

                                    <div className="flex items-center gap-0.5">
                                        {!readOnly && (
                                            <button
                                                type="button"
                                                onClick={() => void restoreBackup(backup)}
                                                disabled={busy || restoreBlocked || !listKnown || shown.stale}
                                                title={restoreBlocked ? blockedReason : t('backup.restore')}
                                                aria-label={restoreBlocked ? blockedReason : t('backup.restore')}
                                                className="rounded-md p-2 text-fg-muted hover:bg-surface-2 hover:text-success disabled:cursor-not-allowed disabled:opacity-50"
                                            >
                                                {restoring === backup.name ? (
                                                    <RefreshCw className="h-4 w-4 animate-spin" aria-hidden="true" />
                                                ) : (
                                                    <RotateCcw className="h-4 w-4" aria-hidden="true" />
                                                )}
                                            </button>
                                        )}
                                        <button
                                            type="button"
                                            onClick={() => void downloadBackup(backup.name)}
                                            disabled={busy}
                                            title={t('backup.download')}
                                            aria-label={t('backup.download')}
                                            className="rounded-md p-2 text-fg-muted hover:bg-surface-2 hover:text-primary disabled:cursor-not-allowed disabled:opacity-50"
                                        >
                                            {downloading === backup.name
                                                ? <RefreshCw className="h-4 w-4 animate-spin" aria-hidden="true" />
                                                : <Download className="h-4 w-4" aria-hidden="true" />}
                                        </button>
                                        {!readOnly && (
                                            <button
                                                type="button"
                                                onClick={() => void deleteBackup(backup.name)}
                                                disabled={busy || !listKnown || shown.stale}
                                                title={t('backup.delete')}
                                                aria-label={t('backup.delete')}
                                                className="rounded-md p-2 text-fg-muted hover:bg-surface-2 hover:text-danger disabled:cursor-not-allowed disabled:opacity-50"
                                            >
                                                {deleting === backup.name
                                                    ? <RefreshCw className="h-4 w-4 animate-spin" aria-hidden="true" />
                                                    : <Trash2 className="h-4 w-4" aria-hidden="true" />}
                                            </button>
                                        )}
                                    </div>
                                </div>
                            );
                        })}
                    </div>
                )}
                </RemoteGate>
            </section>

            <p className="flex items-start gap-2 text-xs text-fg-subtle">
                <Info className="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden="true" />
                {t('backup.storageNote', { domain: domainName })}
            </p>
        </div>
    );
}

function fmtSize(bytes: number): string {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
    return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

function fmtDate(dateStr: string): string {
    try {
        return new Date(dateStr).toLocaleString();
    } catch {
        return dateStr;
    }
}

interface BackupSchedule {
    enabled?: boolean;
    frequency?: 'daily' | 'weekly';
    backup_type?: 'files' | 'full';
    retention?: number;
    last_run?: string | null;
    last_attempt?: string | null;
    last_status?: string | null;
    last_error?: string | null;
    // Identifies the settings this schedule was read at; writes carry it back.
    // Bu zamanlamanın okunduğu ayarları tanımlar; yazılar onu geri taşır.
    version: string;
}

// Automatic backups: turn a daily/weekly schedule on for this domain and pick
// how many copies to keep. The panel runs it in the background; older copies
// beyond the retention are pruned. Reads and writes the schedule endpoint.
// Otomatik yedekler: bu domain için günlük/haftalık zamanlamayı aç ve kaç
// kopya tutulacağını seç. Panel arka planda koşar; saklamayı aşan eski
// kopyalar budanır.
function AutoBackupSection({
    domainId,
    databaseCount,
    readOnly = false,
}: {
    domainId: number;
    databaseCount: number;
    readOnly?: boolean;
}) {
    const { t } = useI18n();
    // The schedule as the server holds it. Until it is known the section shows
    // no form: "off / daily / files / 7" on screen after a failed read was one
    // click away from replacing the real schedule.
    // Sunucunun tuttuğu zamanlama. Bilinene kadar bölüm form göstermez.
    const [current, setCurrent] = useState<Current<BackupSchedule>>({ state: 'loading' });
    const [frequency, setFrequency] = useState<'daily' | 'weekly'>('daily');
    const [backupType, setBackupType] = useState<'files' | 'full'>('files');
    const [retention, setRetention] = useState(7);
    const [saving, setSaving] = useState(false);
    const [stale, setStale] = useState(false);
    const url = `/api/v1/domains/${domainId}/backups/schedule`;
    const schedule = current.state === 'known' ? current.value : null;
    const enabled = Boolean(schedule?.enabled);
    const lastStatus = schedule?.last_status;
    const lastAttempt = schedule?.last_attempt;
    const lastRun = schedule?.last_run;
    const busy = saving || readOnly;

    const load = async () => {
        setCurrent({ state: 'loading' });
        const next = await readCurrent<BackupSchedule>(url);
        if (next.state === 'known') {
            setFrequency(next.value.frequency || 'daily');
            setBackupType(next.value.backup_type || 'files');
            setRetention(next.value.retention || 7);
            setStale(false);
        }
        setCurrent(next);
    };

    useEffect(() => {
        void load();
    }, [domainId]);

    // Both writes carry the version the schedule was read at; the server
    // refuses them when the schedule is no longer that one.
    // İki yazı da zamanlamanın okunduğu sürümü taşır.
    const write = async (off: boolean) => {
        if (readOnly || !schedule) return;
        setSaving(true);
        try {
            const r = off
                ? await fetch(`${url}?version=${encodeURIComponent(schedule.version)}`, { method: 'DELETE' })
                : await fetch(url, {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ frequency, backup_type: backupType, retention, version: schedule.version }),
                });
            if (!r.ok) {
                const error = await readApiError(r);
                if (isStaleWrite(error)) setStale(true);
                else showToast('error', apiErrorText(error, t));
                return;
            }
            const { version } = await r.json();
            setCurrent({
                state: 'known',
                value: off
                    ? { enabled: false, version }
                    : { ...schedule, enabled: true, frequency, backup_type: backupType, retention, version },
            });
            showToast('success', t(off ? 'backup.auto.off' : 'backup.auto.saved'));
        } catch (error) {
            showToast('error', errorText(error, t));
        } finally {
            setSaving(false);
        }
    };

    return (
        <section className="rounded-xl border border-border bg-surface p-5" aria-busy={current.state === 'loading' || saving}>
            <div className="mb-1 flex items-center gap-2">
                <Clock className="h-4 w-4 text-primary" aria-hidden="true" />
                <h3 className="text-sm font-semibold text-fg">{t('backup.auto.title')}</h3>
                {enabled && (
                    <span className="ml-auto rounded-md bg-success/10 px-2 py-0.5 text-xs font-medium text-success">
                        {t('backup.auto.on')}
                    </span>
                )}
            </div>
            <p className="mb-4 text-sm text-fg-muted">{t('backup.auto.desc')}</p>

            <CurrentGate state={current.state} unknownKey="backup.auto.unknown" onRetry={load} />
            {schedule && <>
            {stale && <StaleNotice textKey="backup.auto.stale" onReload={load} />}

            <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
                <label className="text-sm">
                    <span className="mb-1 block text-xs text-fg-muted">{t('backup.auto.frequency')}</span>
                    <select disabled={busy} value={frequency} onChange={(e) => setFrequency(e.target.value as 'daily' | 'weekly')} className={inputClass}>
                        <option value="daily">{t('backup.auto.daily')}</option>
                        <option value="weekly">{t('backup.auto.weekly')}</option>
                    </select>
                </label>
                <label className="text-sm">
                    <span className="mb-1 block text-xs text-fg-muted">{t('backup.auto.type')}</span>
                    <select disabled={busy} value={backupType} onChange={(e) => setBackupType(e.target.value as 'files' | 'full')} className={inputClass}>
                        <option value="files">{t('backup.type.files')}</option>
                        <option value="full">{t('backup.type.full')}</option>
                    </select>
                </label>
                <label className="text-sm">
                    <span className="mb-1 block text-xs text-fg-muted">{t('backup.auto.retention')}</span>
                    <input disabled={busy} type="number" min={1} max={60} value={retention} onChange={(e) => setRetention(Math.max(1, Math.min(60, parseInt(e.target.value) || 1)))} className={inputClass} />
                </label>
            </div>

            {backupType === 'full' && (
                <p className="mt-2 text-xs text-fg-subtle">
                    {t('backup.auto.fullHint', { count: databaseCount })}
                </p>
            )}

            {enabled && lastStatus === 'running' && (
                <p className="mt-2 rounded-lg bg-primary/10 px-3 py-2 text-xs text-primary" role="status">
                    {t('backup.auto.running')}
                </p>
            )}

            {enabled && lastStatus === 'failed' && (
                <p className="mt-2 rounded-lg bg-danger/10 px-3 py-2 text-xs text-danger" role="alert">
                    {t(
                        schedule.last_error === 'BACKUP_JOB_TIMED_OUT'
                            ? 'backup.auto.timedOut'
                            : 'backup.auto.failed',
                        { time: lastAttempt ? new Date(lastAttempt.replace(' ', 'T')).toLocaleString() : '—' },
                    )}
                </p>
            )}

            {enabled && lastStatus === 'success' && lastRun && (
                <p className="mt-2 text-xs text-fg-subtle">
                    {t('backup.auto.lastRun', { time: new Date(lastRun.replace(' ', 'T')).toLocaleString() })}
                </p>
            )}

            {!readOnly && (
                <div className="mt-3 flex gap-2">
                    <Button type="button" variant="primary" disabled={saving || stale} onClick={() => void write(false)}>
                        {enabled ? t('backup.auto.update') : t('backup.auto.enable')}
                    </Button>
                    {enabled && (
                        <Button type="button" variant="secondary" disabled={saving || stale} onClick={() => void write(true)}>
                            {t('backup.auto.turnOff')}
                        </Button>
                    )}
                </div>
            )}
            </>}
            {saving && <span className="sr-only" role="status" aria-live="polite">{t('common.loading')}</span>}
        </section>
    );
}
