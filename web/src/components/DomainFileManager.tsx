import { useState, useEffect } from 'react';
import {
    Folder, File, ChevronRight, ChevronUp, RefreshCw,
    Trash2, Edit, Download, Upload, Save, X,
    FolderPlus, FilePlus,
} from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { Button, EmptyState, inputClass } from './ui';

interface FileItem {
    name: string;
    path: string;
    is_dir: boolean;
    size: number;
    permissions: string;
    mod_time: string;
}

interface DomainFileManagerProps {
    domainId: number;
    domainName: string;
    readOnly?: boolean;
}

// Real file manager over the agent's file API: browse, edit small text files
// inline, upload, create, delete, download. Fully i18n'd; the create dialog is
// an inline panel (matching the DNS add-form pattern), not a modal.
//
// Agent'ın dosya API'si üzerinde gerçek dosya yöneticisi: gezin, küçük metin
// dosyalarını yerinde düzenle, yükle, oluştur, sil, indir. Tamamen i18n'li;
// oluşturma diyaloğu modal değil, satır içi bir paneldir (DNS ekleme-formu
// kalıbıyla uyumlu).
export function DomainFileManager({ domainId, readOnly = false }: DomainFileManagerProps) {
    const { t } = useI18n();
    const [currentPath, setCurrentPath] = useState('/');
    const [files, setFiles] = useState<FileItem[]>([]);
    const [loading, setLoading] = useState(true);

    const [editingFile, setEditingFile] = useState<string | null>(null);
    const [editContent, setEditContent] = useState('');
    const [saving, setSaving] = useState(false);

    const [createType, setCreateType] = useState<'file' | 'folder' | null>(null);
    const [newName, setNewName] = useState('');

    const [uploading, setUploading] = useState(false);

    useEffect(() => {
        loadFiles();
    }, [domainId, currentPath]);

    const loadFiles = async () => {
        setLoading(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/files?path=${encodeURIComponent(currentPath)}`);
            if (!res.ok) throw new Error();
            const data = await res.json();
            setFiles(data.files || []);
        } catch {
            showToast('error', t('common.error'));
        } finally {
            setLoading(false);
        }
    };

    const navigateTo = (path: string) => {
        setCurrentPath(path);
        setEditingFile(null);
        setCreateType(null);
    };

    const goUp = () => {
        if (currentPath === '/') return;
        navigateTo(currentPath.split('/').slice(0, -1).join('/') || '/');
    };

    const openFile = async (file: FileItem) => {
        if (file.is_dir) {
            navigateTo(file.path);
            return;
        }
        // The file-content API is a POST action and therefore intentionally
        // requires manage access. View access still has a safe GET download.
        if (readOnly) {
            window.open(`/api/v1/domains/${domainId}/files/download?path=${encodeURIComponent(file.path)}`, '_blank');
            return;
        }
        if (file.size > 1024 * 1024) {
            showToast('error', t('files.tooLarge'));
            return;
        }
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/files?path=${encodeURIComponent(file.path)}`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ action: 'read' }),
            });
            if (!res.ok) throw new Error();
            const data = await res.json();
            if (data.is_binary) {
                showToast('error', t('files.binary'));
                return;
            }
            setEditingFile(file.path);
            setEditContent(data.content);
        } catch {
            showToast('error', t('common.error'));
        }
    };

    const saveFile = async () => {
        if (readOnly || !editingFile) return;
        setSaving(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/files?path=${encodeURIComponent(editingFile)}`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ action: 'write', content: editContent }),
            });
            if (!res.ok) throw new Error();
            showToast('success', t('files.saved'));
            setEditingFile(null);
        } catch {
            showToast('error', t('common.error'));
        } finally {
            setSaving(false);
        }
    };

    const createItem = async () => {
        if (readOnly || !newName.trim() || !createType) return;
        const path = currentPath === '/' ? `/${newName}` : `${currentPath}/${newName}`;
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/files?path=${encodeURIComponent(path)}`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ action: 'create', is_dir: createType === 'folder' }),
            });
            if (!res.ok) throw new Error();
            showToast('success', t('files.created'));
            setCreateType(null);
            setNewName('');
            loadFiles();
        } catch {
            showToast('error', t('common.error'));
        }
    };

    const deleteItem = async (file: FileItem) => {
        if (readOnly) return;
        if (!confirm(t('files.deleteConfirm', { name: file.name }))) return;
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/files?path=${encodeURIComponent(file.path)}`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ action: 'delete' }),
            });
            if (!res.ok) throw new Error();
            showToast('success', t('files.deleted'));
            loadFiles();
        } catch {
            showToast('error', t('common.error'));
        }
    };

    const downloadFile = (file: FileItem) => {
        window.open(`/api/v1/domains/${domainId}/files/download?path=${encodeURIComponent(file.path)}`, '_blank');
    };

    const handleUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
        if (readOnly) return;
        const file = e.target.files?.[0];
        if (!file) return;
        setUploading(true);
        const reader = new FileReader();
        reader.onload = async () => {
            try {
                const base64 = (reader.result as string).split(',')[1];
                const res = await fetch(`/api/v1/domains/${domainId}/files?path=${encodeURIComponent(currentPath)}`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ action: 'upload', file_name: file.name, content: base64 }),
                });
                if (!res.ok) throw new Error();
                showToast('success', t('files.uploaded'));
                loadFiles();
            } catch {
                showToast('error', t('common.error'));
            } finally {
                setUploading(false);
            }
        };
        reader.readAsDataURL(file);
        e.target.value = '';
    };

    // Inline text editor takes over the whole panel while a file is open.
    // Bir dosya açıkken satır içi metin düzenleyici panelin tamamını kaplar.
    if (editingFile) {
        return (
            <div className="flex h-[540px] flex-col">
                <div className="mb-3 flex items-center justify-between gap-3">
                    <div className="flex min-w-0 items-center gap-2">
                        <button onClick={() => setEditingFile(null)} className="rounded-md p-1.5 text-fg-muted hover:bg-surface-2 hover:text-fg">
                            <X className="h-4 w-4" />
                        </button>
                        <div className="min-w-0">
                            <h3 className="truncate text-sm font-semibold text-fg">{editingFile.split('/').pop()}</h3>
                            <p className="truncate font-mono text-xs text-fg-subtle">{editingFile}</p>
                        </div>
                    </div>
                    {!readOnly && (
                        <Button variant="primary" icon={Save} onClick={saveFile} disabled={saving}>
                            {saving ? t('files.saving') : t('files.save')}
                        </Button>
                    )}
                </div>
                <textarea
                    value={editContent}
                    onChange={(e) => setEditContent(e.target.value)}
                    readOnly={readOnly}
                    className="flex-1 w-full resize-none rounded-lg border border-border bg-surface-2 p-4 font-mono text-sm text-fg outline-none focus:border-primary focus:ring-2 focus:ring-primary/30"
                    spellCheck={false}
                />
            </div>
        );
    }

    return (
        <div>
            {/* Toolbar: breadcrumb + actions */}
            <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
                <div className="flex min-w-0 items-center gap-1">
                    <button
                        onClick={goUp}
                        disabled={currentPath === '/'}
                        title={t('files.up')}
                        className="rounded-md p-1.5 text-fg-muted hover:bg-surface-2 hover:text-fg disabled:opacity-40"
                    >
                        <ChevronUp className="h-4 w-4" />
                    </button>
                    <div className="flex min-w-0 items-center overflow-x-auto text-sm text-fg-muted">
                        <button onClick={() => navigateTo('/')} className="font-mono text-primary hover:underline">~</button>
                        {currentPath.split('/').filter(Boolean).map((part, i, arr) => (
                            <span key={i} className="flex items-center whitespace-nowrap">
                                <ChevronRight className="mx-0.5 h-4 w-4 shrink-0 text-fg-subtle" />
                                <button onClick={() => navigateTo('/' + arr.slice(0, i + 1).join('/'))} className="hover:text-fg">
                                    {part}
                                </button>
                            </span>
                        ))}
                    </div>
                </div>
                <div className="flex items-center gap-1">
                    {!readOnly && (
                        <>
                            <ToolButton icon={FolderPlus} title={t('files.newFolder')} onClick={() => { setCreateType('folder'); setNewName(''); }} />
                            <ToolButton icon={FilePlus} title={t('files.newFile')} onClick={() => { setCreateType('file'); setNewName(''); }} />
                            <label title={t('files.upload')} className="cursor-pointer rounded-md p-1.5 text-fg-muted hover:bg-surface-2 hover:text-fg">
                                <Upload className="h-4 w-4" />
                                <input type="file" className="hidden" onChange={handleUpload} disabled={uploading} />
                            </label>
                        </>
                    )}
                    <ToolButton icon={RefreshCw} title={t('files.refresh')} onClick={loadFiles} spin={loading} />
                </div>
            </div>

            {/* Inline create panel */}
            {!readOnly && createType && (
                <div className="mb-4 rounded-xl border border-border bg-surface-2/50 p-4">
                    <h4 className="mb-3 text-sm font-semibold text-fg">
                        {createType === 'folder' ? t('files.createFolderTitle') : t('files.createFileTitle')}
                    </h4>
                    <div className="flex flex-wrap items-center gap-2">
                        <input
                            type="text"
                            value={newName}
                            onChange={(e) => setNewName(e.target.value)}
                            onKeyDown={(e) => e.key === 'Enter' && createItem()}
                            placeholder={createType === 'folder' ? 'folder-name' : 'filename.txt'}
                            className={`${inputClass} max-w-xs font-mono`}
                            autoFocus
                        />
                        <Button variant="primary" onClick={createItem} disabled={!newName.trim()}>
                            {t('files.create')}
                        </Button>
                        <Button onClick={() => { setCreateType(null); setNewName(''); }}>{t('files.cancel')}</Button>
                    </div>
                </div>
            )}

            {/* File table */}
            {loading ? (
                <div className="flex items-center justify-center py-16">
                    <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary" />
                </div>
            ) : files.length === 0 ? (
                <EmptyState icon={Folder} title={t('files.empty')} />
            ) : (
                <div className="overflow-x-auto rounded-xl border border-border">
                    <table className="w-full text-sm">
                        <thead>
                            <tr className="border-b border-border bg-surface-2/50 text-left text-xs font-semibold text-fg-muted">
                                <th className="px-4 py-2.5">{t('files.col.name')}</th>
                                <th className="w-24 px-4 py-2.5">{t('files.col.size')}</th>
                                <th className="w-28 px-4 py-2.5">{t('files.col.perms')}</th>
                                <th className="w-44 px-4 py-2.5">{t('files.col.modified')}</th>
                                <th className="w-28 px-4 py-2.5" />
                            </tr>
                        </thead>
                        <tbody>
                            {files.map((file) => (
                                <tr
                                    key={file.path}
                                    onDoubleClick={() => openFile(file)}
                                    className="cursor-pointer border-b border-border last:border-0 hover:bg-surface-2/60"
                                >
                                    <td className="px-4 py-2.5">
                                        <button onClick={() => openFile(file)} className="flex items-center gap-2 text-left">
                                            {file.is_dir ? (
                                                <Folder className="h-4 w-4 shrink-0 text-primary" />
                                            ) : (
                                                <File className="h-4 w-4 shrink-0 text-fg-subtle" />
                                            )}
                                            <span className="text-fg">{file.name}</span>
                                        </button>
                                    </td>
                                    <td className="px-4 py-2.5 text-fg-muted">{file.is_dir ? '—' : fmtSize(file.size)}</td>
                                    <td className="px-4 py-2.5 font-mono text-xs text-fg-muted">{file.permissions}</td>
                                    <td className="px-4 py-2.5 text-fg-muted">{fmtDate(file.mod_time)}</td>
                                    <td className="px-4 py-2.5">
                                        <div className="flex items-center justify-end gap-0.5">
                                            {!file.is_dir && (
                                                <>
                                                    {!readOnly && <ToolButton icon={Edit} title={t('files.edit')} onClick={() => openFile(file)} small />}
                                                    <ToolButton icon={Download} title={t('files.download')} onClick={() => downloadFile(file)} small />
                                                </>
                                            )}
                                            {!readOnly && <ToolButton icon={Trash2} title={t('files.delete')} onClick={() => deleteItem(file)} small danger />}
                                        </div>
                                    </td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            )}
        </div>
    );
}

function ToolButton({
    icon: Icon,
    title,
    onClick,
    spin,
    small,
    danger,
}: {
    icon: typeof Folder;
    title: string;
    onClick: () => void;
    spin?: boolean;
    small?: boolean;
    danger?: boolean;
}) {
    return (
        <button
            onClick={(e) => { e.stopPropagation(); onClick(); }}
            title={title}
            aria-label={title}
            className={`rounded-md p-1.5 text-fg-muted transition-colors hover:bg-surface-2 ${danger ? 'hover:text-danger' : 'hover:text-fg'}`}
        >
            <Icon className={`${small ? 'h-3.5 w-3.5' : 'h-4 w-4'} ${spin ? 'animate-spin' : ''}`} />
        </button>
    );
}

function fmtSize(bytes: number): string {
    if (!bytes) return '—';
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function fmtDate(dateStr: string): string {
    try {
        return new Date(dateStr).toLocaleString();
    } catch {
        return dateStr;
    }
}
