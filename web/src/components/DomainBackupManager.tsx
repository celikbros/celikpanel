import { useState, useEffect } from 'react';
import {
    Archive, RefreshCw, Download, Trash2, RotateCcw,
    HardDrive, Database, Clock, Info, type LucideIcon,
} from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import { EmptyState } from './ui';

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
