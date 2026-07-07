import { useState, useEffect } from 'react';
import {
    Archive, RefreshCw, Download, Trash2, RotateCcw,
    HardDrive, Database, Clock, Info, type LucideIcon,
} from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import { EmptyState, Button, inputClass } from './ui';

interface BackupItem {
    name: string;
    path: string;
    size: number;
    type: string;
    created_at: string;
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

// Real backups via the agent (tar/dump under /var/backups/celikpanel).
// Create, restore, download, delete — no invented rows.
// Agent üzerinden gerçek yedekler (/var/backups/celikpanel altında tar/dump).
// Oluştur, geri yükle, indir, sil — uydurma satır yok.
export function DomainBackupManager({ domainId, domainName }: DomainBackupManagerProps) {
    const { t } = useI18n();
    const [backups, setBackups] = useState<BackupItem[]>([]);
    const [loading, setLoading] = useState(true);
    const [creating, setCreating] = useState(false);
    const [restoring, setRestoring] = useState<string | null>(null);

    useEffect(() => {
        loadBackups();
    }, [domainId]);

    const loadBackups = async () => {
        setLoading(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/backups`);
            if (!res.ok) throw new Error();
            const data = await res.json();
            setBackups(data.backups || []);
        } catch {
            showToast('error', t('common.error'));
        } finally {
            setLoading(false);
        }
    };

    const createBackup = async (type: BackupType) => {
        setCreating(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/backups`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ type }),
            });
            const data = await res.json();
            if (!data.success) throw new Error(data.error);
            showToast('success', t('backup.created'));
            loadBackups();
        } catch {
            showToast('error', t('common.error'));
        } finally {
            setCreating(false);
        }
    };

    const restoreBackup = async (name: string) => {
        if (!confirm(t('backup.restoreConfirm', { name }))) return;
        setRestoring(name);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/backups/restore`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ backup_name: name }),
            });
            const data = await res.json();
            if (!data.success) throw new Error(data.error);
            showToast('success', t('backup.restored'));
        } catch {
            showToast('error', t('common.error'));
        } finally {
            setRestoring(null);
        }
    };

    const deleteBackup = async (name: string) => {
        if (!confirm(t('backup.deleteConfirm', { name }))) return;
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/backups?name=${encodeURIComponent(name)}`, {
                method: 'DELETE',
            });
            const data = await res.json();
            if (!data.success) throw new Error();
            showToast('success', t('backup.deleted'));
            loadBackups();
        } catch {
            showToast('error', t('common.error'));
        }
    };

    return (
        <div className="space-y-5">
            {/* Create */}
            <section>
                <h3 className="mb-3 text-sm font-semibold text-fg">{t('backup.createTitle')}</h3>
                <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
                    {backupTypes.map(({ type, icon: Icon, labelKey, descKey, tone }) => (
                        <button
                            key={type}
                            onClick={() => createBackup(type)}
                            disabled={creating}
                            className="flex items-center gap-3 rounded-xl border border-border bg-surface p-4 text-left transition-colors hover:border-primary/40 hover:bg-surface-2 disabled:cursor-not-allowed disabled:opacity-50"
                        >
                            <span className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-lg ${tone}`}>
                                <Icon className="h-5 w-5" />
                            </span>
                            <span>
                                <span className="block text-sm font-semibold text-fg">{t(labelKey)}</span>
                                <span className="block text-xs text-fg-muted">{t(descKey)}</span>
                            </span>
                        </button>
                    ))}
                </div>
                {creating && (
                    <p className="mt-3 flex items-center gap-2 text-sm text-primary">
                        <RefreshCw className="h-4 w-4 animate-spin" />
                        {t('backup.creating')}
                    </p>
                )}
            </section>

            <AutoBackupSection domainId={domainId} />

            {/* List */}
            <section>
                <div className="mb-3 flex items-center justify-between">
                    <h3 className="text-sm font-semibold text-fg">{t('backup.existing')}</h3>
                    <button
                        onClick={loadBackups}
                        title={t('files.refresh')}
                        className="rounded-md p-1.5 text-fg-muted hover:bg-surface-2 hover:text-fg"
                    >
                        <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
                    </button>
                </div>

                {loading ? (
                    <div className="flex items-center justify-center py-12">
                        <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary" />
                    </div>
                ) : backups.length === 0 ? (
                    <EmptyState icon={Archive} title={t('backup.empty')} hint={t('backup.emptyHint')} />
                ) : (
                    <div className="space-y-2">
                        {backups.map((backup) => {
                            const typeDef = backupTypes.find((b) => b.type === backup.type);
                            const Icon = typeDef?.icon ?? Archive;
                            return (
                                <div
                                    key={backup.name}
                                    className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-border bg-surface p-4"
                                >
                                    <div className="flex min-w-0 items-center gap-3">
                                        <span className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-lg ${typeDef?.tone ?? 'bg-surface-2 text-fg-muted'}`}>
                                            <Icon className="h-4 w-4" />
                                        </span>
                                        <div className="min-w-0">
                                            <p className="truncate text-sm font-medium text-fg">{backup.name}</p>
                                            <div className="mt-0.5 flex flex-wrap items-center gap-3 text-xs text-fg-muted">
                                                <span className="flex items-center gap-1">
                                                    <Clock className="h-3 w-3" />
                                                    {fmtDate(backup.created_at)}
                                                </span>
                                                <span>{fmtSize(backup.size)}</span>
                                                <span className="rounded bg-surface-2 px-1.5 py-0.5">
                                                    {t(typeLabelKey[backup.type] ?? 'backup.type.full')}
                                                </span>
                                            </div>
                                        </div>
                                    </div>

                                    <div className="flex items-center gap-0.5">
                                        <button
                                            onClick={() => restoreBackup(backup.name)}
                                            disabled={restoring === backup.name}
                                            title={t('backup.restore')}
                                            className="rounded-md p-2 text-fg-muted hover:bg-surface-2 hover:text-success disabled:opacity-50"
                                        >
                                            {restoring === backup.name ? (
                                                <RefreshCw className="h-4 w-4 animate-spin" />
                                            ) : (
                                                <RotateCcw className="h-4 w-4" />
                                            )}
                                        </button>
                                        <a
                                            href={`/api/v1/domains/${domainId}/backups/download?name=${encodeURIComponent(backup.name)}`}
                                            title={t('backup.download')}
                                            className="rounded-md p-2 text-fg-muted hover:bg-surface-2 hover:text-primary"
                                        >
                                            <Download className="h-4 w-4" />
                                        </a>
                                        <button
                                            onClick={() => deleteBackup(backup.name)}
                                            title={t('backup.delete')}
                                            className="rounded-md p-2 text-fg-muted hover:bg-surface-2 hover:text-danger"
                                        >
                                            <Trash2 className="h-4 w-4" />
                                        </button>
                                    </div>
                                </div>
                            );
                        })}
                    </div>
                )}
            </section>

            <p className="flex items-start gap-2 text-xs text-fg-subtle">
                <Info className="mt-0.5 h-3.5 w-3.5 shrink-0" />
                {t('backup.storageNote', { path: `/var/backups/celikpanel/${domainName}/` })}
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
function AutoBackupSection({ domainId }: { domainId: number }) {
    const { t } = useI18n();
    const [enabled, setEnabled] = useState(false);
    const [frequency, setFrequency] = useState<'daily' | 'weekly'>('daily');
    const [backupType, setBackupType] = useState<'files' | 'full'>('files');
    const [retention, setRetention] = useState(7);
    const [lastRun, setLastRun] = useState<string | null>(null);
    const [busy, setBusy] = useState(false);

    useEffect(() => {
        fetch(`/api/v1/domains/${domainId}/backups/schedule`)
            .then((r) => (r.ok ? r.json() : null))
            .then((d) => {
                if (d && d.enabled) {
                    setEnabled(true);
                    setFrequency(d.frequency);
                    setBackupType(d.backup_type);
                    setRetention(d.retention);
                    setLastRun(d.last_run);
                }
            })
            .catch(() => {});
    }, [domainId]);

    const save = async () => {
        setBusy(true);
        try {
            const r = await fetch(`/api/v1/domains/${domainId}/backups/schedule`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ frequency, backup_type: backupType, retention }),
            });
            if (!r.ok) throw new Error();
            setEnabled(true);
            showToast('success', t('backup.auto.saved'));
        } catch {
            showToast('error', t('common.error'));
        } finally {
            setBusy(false);
        }
    };

    const turnOff = async () => {
        setBusy(true);
        try {
            const r = await fetch(`/api/v1/domains/${domainId}/backups/schedule`, { method: 'DELETE' });
            if (!r.ok) throw new Error();
            setEnabled(false);
            setLastRun(null);
            showToast('success', t('backup.auto.off'));
        } catch {
            showToast('error', t('common.error'));
        } finally {
            setBusy(false);
        }
    };

    return (
        <section className="rounded-xl border border-border bg-surface p-5">
            <div className="mb-1 flex items-center gap-2">
                <Clock className="h-4 w-4 text-primary" />
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
                    <select value={frequency} onChange={(e) => setFrequency(e.target.value as 'daily' | 'weekly')} className={inputClass}>
                        <option value="daily">{t('backup.auto.daily')}</option>
                        <option value="weekly">{t('backup.auto.weekly')}</option>
                    </select>
                </label>
                <label className="text-sm">
                    <span className="mb-1 block text-xs text-fg-muted">{t('backup.auto.type')}</span>
                    <select value={backupType} onChange={(e) => setBackupType(e.target.value as 'files' | 'full')} className={inputClass}>
                        <option value="files">{t('backup.type.files')}</option>
                        <option value="full">{t('backup.type.full')}</option>
                    </select>
                </label>
                <label className="text-sm">
                    <span className="mb-1 block text-xs text-fg-muted">{t('backup.auto.retention')}</span>
                    <input type="number" min={1} max={60} value={retention} onChange={(e) => setRetention(Math.max(1, Math.min(60, parseInt(e.target.value) || 1)))} className={inputClass} />
                </label>
            </div>

            {enabled && lastRun && (
                <p className="mt-2 text-xs text-fg-subtle">
                    {t('backup.auto.lastRun', { time: new Date(lastRun.replace(' ', 'T')).toLocaleString() })}
                </p>
            )}

            <div className="mt-3 flex gap-2">
                <Button variant="primary" disabled={busy} onClick={save}>
                    {enabled ? t('backup.auto.update') : t('backup.auto.enable')}
                </Button>
                {enabled && (
                    <Button variant="secondary" disabled={busy} onClick={turnOff}>
                        {t('backup.auto.turnOff')}
                    </Button>
                )}
            </div>
        </section>
    );
}
