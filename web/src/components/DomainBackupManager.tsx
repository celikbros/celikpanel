import { useState, useEffect } from 'react';
import {
    Archive, RefreshCw, Download, Trash2, RotateCcw,
    HardDrive, Database, Clock, Info, type LucideIcon,
} from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import { apiErrorText, readApiError } from '../lib/apiError';
import { EmptyState, Button, inputClass } from './ui';

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

interface DatabaseInfo {
    id: number;
    name: string;
    type: string;
}

interface DomainBackupManagerProps {
    domainId: number;
    domainName: string;
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

function errorText(error: unknown, t: Translate): string {
    return error instanceof Error && error.message ? error.message : t('common.error');
}

// Real backups via the agent (tar/dump under /var/backups/celikpanel).
// Create, restore, download, delete — no invented rows.
// Agent üzerinden gerçek yedekler (/var/backups/celikpanel altında tar/dump).
// Oluştur, geri yükle, indir, sil — uydurma satır yok.
export function DomainBackupManager({ domainId, domainName }: DomainBackupManagerProps) {
    const { t } = useI18n();
    const [backups, setBackups] = useState<BackupItem[]>([]);
    const [databases, setDatabases] = useState<DatabaseInfo[]>([]);
    const [selectedDatabaseId, setSelectedDatabaseId] = useState('');
    const [loading, setLoading] = useState(true);
    const [databaseLoading, setDatabaseLoading] = useState(true);
    const [databaseLoadError, setDatabaseLoadError] = useState('');
    const [creating, setCreating] = useState<BackupType | null>(null);
    const [restoring, setRestoring] = useState<string | null>(null);
    const [deleting, setDeleting] = useState<string | null>(null);
    const [downloading, setDownloading] = useState<string | null>(null);

    useEffect(() => {
        void loadBackups();
        void loadDatabases();
    }, [domainId]);

    const busy = creating !== null || restoring !== null || deleting !== null || downloading !== null;

    const loadBackups = async () => {
        setLoading(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/backups`);
            const data = await readJSONResponse<{ backups?: BackupItem[] }>(res, t);
            setBackups(data.backups || []);
        } catch (error) {
            showToast('error', errorText(error, t));
        } finally {
            setLoading(false);
        }
    };

    const loadDatabases = async () => {
        setDatabaseLoading(true);
        setDatabaseLoadError('');
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/databases`);
            const data = await readJSONResponse<{ databases?: DatabaseInfo[] }>(res, t);
            const next = data.databases || [];
            setDatabases(next);
            setSelectedDatabaseId((current) =>
                next.some((database) => String(database.id) === current) ? current : next[0] ? String(next[0].id) : '',
            );
        } catch (error) {
            const message = errorText(error, t);
            setDatabaseLoadError(message);
            setDatabases([]);
            setSelectedDatabaseId('');
            showToast('error', message);
        } finally {
            setDatabaseLoading(false);
        }
    };

    const createBackup = async (type: BackupType) => {
        if (type === 'database' && !selectedDatabaseId) return;
        setCreating(type);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/backups`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    type,
                    ...(type === 'database' ? { database_id: Number(selectedDatabaseId) } : {}),
                }),
            });
            await readJSONResponse<{ success?: boolean; error?: string }>(res, t);
            showToast('success', t('backup.created'));
            await loadBackups();
        } catch (error) {
            showToast('error', errorText(error, t));
        } finally {
            setCreating(null);
        }
    };

    const restoreBackup = async (backup: BackupItem) => {
        if (!backup.restorable || !confirm(t('backup.restoreConfirm', { name: backup.name }))) return;
        setRestoring(backup.name);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/backups/restore`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ backup_name: backup.name }),
            });
            const data = await readJSONResponse<{ success?: boolean; error?: string; safety_backup?: BackupItem }>(res, t);
            showToast('success', data.safety_backup
                ? t('backup.restoredWithSafety', { name: data.safety_backup.name })
                : t('backup.restored'));
            await loadBackups();
        } catch (error) {
            showToast('error', errorText(error, t));
        } finally {
            setRestoring(null);
        }
    };

    const deleteBackup = async (name: string) => {
        if (!confirm(t('backup.deleteConfirm', { name }))) return;
        setDeleting(name);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/backups?name=${encodeURIComponent(name)}`, {
                method: 'DELETE',
            });
            await readJSONResponse<{ success?: boolean; error?: string }>(res, t);
            showToast('success', t('backup.deleted'));
            await loadBackups();
        } catch (error) {
            showToast('error', errorText(error, t));
        } finally {
            setDeleting(null);
        }
    };

    const downloadBackup = async (name: string) => {
        setDownloading(name);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/backups/download?name=${encodeURIComponent(name)}`);
            if (!res.ok) throw new Error(apiErrorText(await readApiError(res), t));
            const url = URL.createObjectURL(await res.blob());
            const link = document.createElement('a');
            link.href = url;
            link.download = name;
            document.body.appendChild(link);
            link.click();
            link.remove();
            URL.revokeObjectURL(url);
        } catch (error) {
            showToast('error', errorText(error, t));
        } finally {
            setDownloading(null);
        }
    };

    return (
        <div className="space-y-5">
            {/* Create */}
            <section aria-busy={creating !== null}>
                <h3 className="mb-3 text-sm font-semibold text-fg">{t('backup.createTitle')}</h3>
                <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
                    {backupTypes.filter(({ type }) => type === 'files').map(({ type, icon: Icon, labelKey, descKey, tone }) => (
                        <button
                            key={type}
                            type="button"
                            onClick={() => void createBackup(type)}
                            disabled={busy}
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
                            disabled={busy || databaseLoading || databases.length === 0}
                            className={`${inputClass} mb-2`}
                        >
                            {databaseLoading ? (
                                <option value="">{t('backup.loadingDatabases')}</option>
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
                            disabled={busy || databaseLoading || !selectedDatabaseId}
                            onClick={() => void createBackup('database')}
                            className="w-full justify-center"
                        >
                            {creating === 'database' ? t('backup.creating') : t('backup.create')}
                        </Button>
                    </div>

                    <button
                        type="button"
                        onClick={() => void createBackup('full')}
                        disabled={busy || databaseLoading || Boolean(databaseLoadError)}
                        className="flex items-center gap-3 rounded-xl border border-border bg-surface p-4 text-left transition-colors hover:border-primary/40 hover:bg-surface-2 disabled:cursor-not-allowed disabled:opacity-50"
                    >
                        <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-warning/15 text-warning">
                            <Archive className="h-5 w-5" aria-hidden="true" />
                        </span>
                        <span>
                            <span className="block text-sm font-semibold text-fg">{t('backup.full')}</span>
                            <span className="block text-xs text-fg-muted">
                                {databaseLoading ? t('backup.fullDescLoading') : t('backup.fullDesc', { count: databases.length })}
                            </span>
                        </span>
                    </button>
                </div>
                {databaseLoadError && (
                    <div className="mt-3 flex flex-wrap items-center gap-2 text-sm text-danger" role="alert">
                        <span>{t('backup.databaseLoadError', { error: databaseLoadError })}</span>
                        <Button type="button" variant="secondary" disabled={databaseLoading || busy} onClick={() => void loadDatabases()}>
                            {t('backup.retry')}
                        </Button>
                    </div>
                )}
                {creating !== null && (
                    <p className="mt-3 flex items-center gap-2 text-sm text-primary" role="status" aria-live="polite">
                        <RefreshCw className="h-4 w-4 animate-spin" aria-hidden="true" />
                        {t('backup.creating')}
                    </p>
                )}
            </section>

            <AutoBackupSection domainId={domainId} databaseCount={databases.length} />

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

                {loading ? (
                    <div className="flex items-center justify-center py-12">
                        <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary" aria-hidden="true" />
                        <span className="sr-only" role="status" aria-live="polite">{t('common.loading')}</span>
                    </div>
                ) : backups.length === 0 ? (
                    <EmptyState icon={Archive} title={t('backup.empty')} hint={t('backup.emptyHint')} />
                ) : (
                    <div className="space-y-2">
                        {backups.map((backup) => {
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
                                        <button
                                            type="button"
                                            onClick={() => void restoreBackup(backup)}
                                            disabled={busy || restoreBlocked}
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
                                        <button
                                            type="button"
                                            onClick={() => void deleteBackup(backup.name)}
                                            disabled={busy}
                                            title={t('backup.delete')}
                                            aria-label={t('backup.delete')}
                                            className="rounded-md p-2 text-fg-muted hover:bg-surface-2 hover:text-danger disabled:cursor-not-allowed disabled:opacity-50"
                                        >
                                            {deleting === backup.name
                                                ? <RefreshCw className="h-4 w-4 animate-spin" aria-hidden="true" />
                                                : <Trash2 className="h-4 w-4" aria-hidden="true" />}
                                        </button>
                                    </div>
                                </div>
                            );
                        })}
                    </div>
                )}
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

// Automatic backups: turn a daily/weekly schedule on for this domain and pick
// how many copies to keep. The panel runs it in the background; older copies
// beyond the retention are pruned. Reads and writes the schedule endpoint.
// Otomatik yedekler: bu domain için günlük/haftalık zamanlamayı aç ve kaç
// kopya tutulacağını seç. Panel arka planda koşar; saklamayı aşan eski
// kopyalar budanır.
function AutoBackupSection({ domainId, databaseCount }: { domainId: number; databaseCount: number }) {
    const { t } = useI18n();
    const [enabled, setEnabled] = useState(false);
    const [frequency, setFrequency] = useState<'daily' | 'weekly'>('daily');
    const [backupType, setBackupType] = useState<'files' | 'full'>('files');
    const [retention, setRetention] = useState(7);
    const [lastRun, setLastRun] = useState<string | null>(null);
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const busy = loading || saving;

    useEffect(() => {
        const load = async () => {
            setLoading(true);
            try {
                const response = await fetch(`/api/v1/domains/${domainId}/backups/schedule`);
                const data = await readJSONResponse<{
                    enabled?: boolean;
                    frequency?: 'daily' | 'weekly';
                    backup_type?: 'files' | 'full';
                    retention?: number;
                    last_run?: string | null;
                }>(response, t);
                setEnabled(Boolean(data.enabled));
                if (data.frequency) setFrequency(data.frequency);
                if (data.backup_type) setBackupType(data.backup_type);
                if (data.retention) setRetention(data.retention);
                setLastRun(data.last_run || null);
            } catch (error) {
                showToast('error', errorText(error, t));
            } finally {
                setLoading(false);
            }
        };
        void load();
    }, [domainId]);

    const save = async () => {
        setSaving(true);
        try {
            const r = await fetch(`/api/v1/domains/${domainId}/backups/schedule`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ frequency, backup_type: backupType, retention }),
            });
            await readJSONResponse<{ success?: boolean; error?: string }>(r, t);
            setEnabled(true);
            showToast('success', t('backup.auto.saved'));
        } catch (error) {
            showToast('error', errorText(error, t));
        } finally {
            setSaving(false);
        }
    };

    const turnOff = async () => {
        setSaving(true);
        try {
            const r = await fetch(`/api/v1/domains/${domainId}/backups/schedule`, { method: 'DELETE' });
            await readJSONResponse<{ success?: boolean; error?: string }>(r, t);
            setEnabled(false);
            setLastRun(null);
            showToast('success', t('backup.auto.off'));
        } catch (error) {
            showToast('error', errorText(error, t));
        } finally {
            setSaving(false);
        }
    };

    return (
        <section className="rounded-xl border border-border bg-surface p-5" aria-busy={busy}>
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

            {enabled && lastRun && (
                <p className="mt-2 text-xs text-fg-subtle">
                    {t('backup.auto.lastRun', { time: new Date(lastRun.replace(' ', 'T')).toLocaleString() })}
                </p>
            )}

            <div className="mt-3 flex gap-2">
                <Button type="button" variant="primary" disabled={busy} onClick={() => void save()}>
                    {enabled ? t('backup.auto.update') : t('backup.auto.enable')}
                </Button>
                {enabled && (
                    <Button type="button" variant="secondary" disabled={busy} onClick={() => void turnOff()}>
                        {t('backup.auto.turnOff')}
                    </Button>
                )}
            </div>
            {saving && <span className="sr-only" role="status" aria-live="polite">{t('common.loading')}</span>}
        </section>
    );
}
