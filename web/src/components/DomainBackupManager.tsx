import { useState, useEffect } from 'react';
import {
    Archive, RefreshCw, Download, Trash2, RotateCcw,
    HardDrive, Database, Clock, AlertCircle
} from 'lucide-react';
import { showToast } from './Toast';

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

export function DomainBackupManager({ domainId, domainName }: DomainBackupManagerProps) {
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
            if (res.ok) {
                const data = await res.json();
                setBackups(data.backups || []);
            } else {
                showToast('error', 'Failed to load backups');
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to load backups');
        } finally {
            setLoading(false);
        }
    };

    const createBackup = async (type: 'files' | 'database' | 'full') => {
        setCreating(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/backups`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ type })
            });

            const data = await res.json();
            if (data.success) {
                showToast('success', 'Backup created successfully');
                loadBackups();
            } else {
                showToast('error', data.error || 'Failed to create backup');
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to create backup');
        } finally {
            setCreating(false);
        }
    };

    const restoreBackup = async (backupName: string) => {
        if (!confirm(`Restore backup "${backupName}"? This will overwrite current files.`)) return;

        setRestoring(backupName);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/backups/restore`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ backup_name: backupName })
            });

            const data = await res.json();
            if (data.success) {
                showToast('success', 'Backup restored successfully');
            } else {
                showToast('error', data.error || 'Failed to restore backup');
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to restore backup');
        } finally {
            setRestoring(null);
        }
    };

    const deleteBackup = async (backupName: string) => {
        if (!confirm(`Delete backup "${backupName}"?`)) return;

        try {
            const res = await fetch(`/api/v1/domains/${domainId}/backups?name=${encodeURIComponent(backupName)}`, {
                method: 'DELETE'
            });

            const data = await res.json();
            if (data.success) {
                showToast('success', 'Backup deleted');
                loadBackups();
            } else {
                showToast('error', 'Failed to delete backup');
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to delete backup');
        }
    };

    const formatSize = (bytes: number) => {
        if (bytes < 1024) return `${bytes} B`;
        if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
        if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
        return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
    };

    const formatDate = (dateStr: string) => {
        try {
            return new Date(dateStr).toLocaleString();
        } catch {
            return dateStr;
        }
    };

    const getTypeIcon = (type: string) => {
        switch (type) {
            case 'files': return <HardDrive className="w-4 h-4 text-primary" />;
            case 'database': return <Database className="w-4 h-4 text-success" />;
            case 'full': return <Archive className="w-4 h-4 text-purple-400" />;
            default: return <Archive className="w-4 h-4 text-fg-muted" />;
        }
    };

    const getTypeLabel = (type: string) => {
        switch (type) {
            case 'files': return 'Files';
            case 'database': return 'Database';
            case 'full': return 'Full';
            default: return type;
        }
    };

    return (
        <div className="space-y-6">
            {/* Create Backup Section */}
            <div className="bg-surface-2/50 border border-border rounded-xl p-6">
                <h3 className="text-lg font-semibold text-fg mb-4">Create New Backup</h3>
                <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                    <button
                        onClick={() => createBackup('files')}
                        disabled={creating}
                        className="flex items-center gap-3 p-4 bg-surface-3/50 hover:bg-surface-3 border border-border-strong rounded-lg transition-colors disabled:opacity-50"
                    >
                        <HardDrive className="w-8 h-8 text-primary" />
                        <div className="text-left">
                            <p className="font-medium text-fg">Files Backup</p>
                            <p className="text-xs text-fg-muted">Backup website files only</p>
                        </div>
                    </button>

                    <button
                        onClick={() => createBackup('database')}
                        disabled={creating}
                        className="flex items-center gap-3 p-4 bg-surface-3/50 hover:bg-surface-3 border border-border-strong rounded-lg transition-colors disabled:opacity-50"
                    >
                        <Database className="w-8 h-8 text-success" />
                        <div className="text-left">
                            <p className="font-medium text-fg">Database Backup</p>
                            <p className="text-xs text-fg-muted">Backup databases</p>
                        </div>
                    </button>

                    <button
                        onClick={() => createBackup('full')}
                        disabled={creating}
                        className="flex items-center gap-3 p-4 bg-surface-3/50 hover:bg-surface-3 border border-border-strong rounded-lg transition-colors disabled:opacity-50"
                    >
                        <Archive className="w-8 h-8 text-purple-400" />
                        <div className="text-left">
                            <p className="font-medium text-fg">Full Backup</p>
                            <p className="text-xs text-fg-muted">Files + Database</p>
                        </div>
                    </button>
                </div>

                {creating && (
                    <div className="mt-4 flex items-center gap-2 text-primary">
                        <RefreshCw className="w-4 h-4 animate-spin" />
                        <span className="text-sm">Creating backup...</span>
                    </div>
                )}
            </div>

            {/* Backup List */}
            <div className="bg-surface-2/50 border border-border rounded-xl p-6">
                <div className="flex items-center justify-between mb-4">
                    <h3 className="text-lg font-semibold text-fg">Existing Backups</h3>
                    <button
                        onClick={loadBackups}
                        className="p-2 hover:bg-surface-3 rounded text-fg-muted hover:text-fg"
                        title="Refresh"
                    >
                        <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
                    </button>
                </div>

                {loading ? (
                    <div className="flex items-center justify-center py-12">
                        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
                    </div>
                ) : backups.length === 0 ? (
                    <div className="text-center py-12 text-fg-subtle">
                        <Archive className="w-12 h-12 mx-auto mb-3 opacity-50" />
                        <p>No backups found</p>
                        <p className="text-sm mt-1">Create your first backup above</p>
                    </div>
                ) : (
                    <div className="space-y-3">
                        {backups.map((backup) => (
                            <div
                                key={backup.name}
                                className="flex items-center justify-between p-4 bg-surface-3/30 border border-border rounded-lg"
                            >
                                <div className="flex items-center gap-4">
                                    {getTypeIcon(backup.type)}
                                    <div>
                                        <p className="text-fg font-medium">{backup.name}</p>
                                        <div className="flex items-center gap-4 text-xs text-fg-muted mt-1">
                                            <span className="flex items-center gap-1">
                                                <Clock className="w-3 h-3" />
                                                {formatDate(backup.created_at)}
                                            </span>
                                            <span>{formatSize(backup.size)}</span>
                                            <span className="px-2 py-0.5 bg-surface-3 rounded text-fg-muted">
                                                {getTypeLabel(backup.type)}
                                            </span>
                                        </div>
                                    </div>
                                </div>

                                <div className="flex items-center gap-2">
                                    <button
                                        onClick={() => restoreBackup(backup.name)}
                                        disabled={restoring === backup.name}
                                        className="p-2 hover:bg-surface-3 rounded text-fg-muted hover:text-success disabled:opacity-50"
                                        title="Restore"
                                    >
                                        {restoring === backup.name ? (
                                            <RefreshCw className="w-4 h-4 animate-spin" />
                                        ) : (
                                            <RotateCcw className="w-4 h-4" />
                                        )}
                                    </button>
                                    <a
                                        href={`/api/v1/domains/${domainId}/files/download?path=${encodeURIComponent(backup.path.replace('/var/backups/celikpanel/' + domainName, ''))}`}
                                        className="p-2 hover:bg-surface-3 rounded text-fg-muted hover:text-primary"
                                        title="Download"
                                    >
                                        <Download className="w-4 h-4" />
                                    </a>
                                    <button
                                        onClick={() => deleteBackup(backup.name)}
                                        className="p-2 hover:bg-surface-3 rounded text-fg-muted hover:text-danger"
                                        title="Delete"
                                    >
                                        <Trash2 className="w-4 h-4" />
                                    </button>
                                </div>
                            </div>
                        ))}
                    </div>
                )}
            </div>

            {/* Info */}
            <div className="flex items-start gap-3 p-4 bg-primary/10 border border-primary/30 rounded-lg">
                <AlertCircle className="w-5 h-5 text-primary flex-shrink-0 mt-0.5" />
                <div className="text-sm text-fg-muted">
                    <p className="font-medium text-primary">Backup Storage</p>
                    <p className="mt-1">Backups are stored in <code className="text-primary">/var/backups/celikpanel/{domainName}/</code></p>
                </div>
            </div>
        </div>
    );
}
