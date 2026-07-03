import { useState, useEffect } from 'react';
import {
    Folder, File, ChevronRight, ChevronUp, RefreshCw,
    Trash2, Edit, Download, Upload, Save, X,
    FolderPlus, FilePlus
} from 'lucide-react';
import { showToast } from './Toast';

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
}

export function DomainFileManager({ domainId }: DomainFileManagerProps) {
    const [currentPath, setCurrentPath] = useState('/');
    const [files, setFiles] = useState<FileItem[]>([]);
    const [loading, setLoading] = useState(true);
    const [selectedFile, setSelectedFile] = useState<FileItem | null>(null);

    const [editingFile, setEditingFile] = useState<string | null>(null);
    const [editContent, setEditContent] = useState('');
    const [saving, setSaving] = useState(false);

    const [showCreateDialog, setShowCreateDialog] = useState(false);
    const [createType, setCreateType] = useState<'file' | 'folder'>('file');
    const [newName, setNewName] = useState('');

    const [uploading, setUploading] = useState(false);

    useEffect(() => {
        loadFiles();
    }, [domainId, currentPath]);

    const loadFiles = async () => {
        setLoading(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/files?path=${encodeURIComponent(currentPath)}`);
            if (res.ok) {
                const data = await res.json();
                setFiles(data.files || []);
            } else {
                showToast('error', 'Failed to load files');
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to load files');
        } finally {
            setLoading(false);
        }
    };

    const navigateTo = (path: string) => {
        setCurrentPath(path);
        setSelectedFile(null);
        setEditingFile(null);
    };

    const goUp = () => {
        if (currentPath === '/') return;
        const parent = currentPath.split('/').slice(0, -1).join('/') || '/';
        navigateTo(parent);
    };

    const openFile = async (file: FileItem) => {
        if (file.is_dir) {
            navigateTo(file.path);
            return;
        }

        if (file.size > 1024 * 1024) {
            showToast('error', 'File too large to edit. Max 1MB.');
            return;
        }

        try {
            const res = await fetch(`/api/v1/domains/${domainId}/files?path=${encodeURIComponent(file.path)}`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ action: 'read' })
            });

            if (res.ok) {
                const data = await res.json();
                if (data.is_binary) {
                    showToast('error', 'Cannot edit binary files');
                    return;
                }
                setEditingFile(file.path);
                setEditContent(data.content);
            } else {
                showToast('error', 'Failed to read file');
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to read file');
        }
    };

    const saveFile = async () => {
        if (!editingFile) return;

        setSaving(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/files?path=${encodeURIComponent(editingFile)}`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ action: 'write', content: editContent })
            });

            if (res.ok) {
                showToast('success', 'File saved successfully');
                setEditingFile(null);
            } else {
                showToast('error', 'Failed to save file');
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to save file');
        } finally {
            setSaving(false);
        }
    };

    const createItem = async () => {
        if (!newName.trim()) return;

        const path = currentPath === '/' ? `/${newName}` : `${currentPath}/${newName}`;

        try {
            const res = await fetch(`/api/v1/domains/${domainId}/files?path=${encodeURIComponent(path)}`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ action: 'create', is_dir: createType === 'folder' })
            });

            if (res.ok) {
                showToast('success', `${createType === 'folder' ? 'Folder' : 'File'} created`);
                setShowCreateDialog(false);
                setNewName('');
                loadFiles();
            } else {
                showToast('error', 'Failed to create');
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to create');
        }
    };

    const deleteItem = async (file: FileItem) => {
        if (!confirm(`Delete ${file.name}?`)) return;

        try {
            const res = await fetch(`/api/v1/domains/${domainId}/files?path=${encodeURIComponent(file.path)}`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ action: 'delete' })
            });

            if (res.ok) {
                showToast('success', 'Deleted successfully');
                loadFiles();
            } else {
                showToast('error', 'Failed to delete');
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to delete');
        }
    };

    const downloadFile = (file: FileItem) => {
        window.open(`/api/v1/domains/${domainId}/files/download?path=${encodeURIComponent(file.path)}`, '_blank');
    };

    const handleUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
        const file = e.target.files?.[0];
        if (!file) return;

        setUploading(true);
        try {
            const reader = new FileReader();
            reader.onload = async () => {
                const base64 = (reader.result as string).split(',')[1];

                const res = await fetch(`/api/v1/domains/${domainId}/files?path=${encodeURIComponent(currentPath)}`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        action: 'upload',
                        file_name: file.name,
                        content: base64
                    })
                });

                if (res.ok) {
                    showToast('success', 'File uploaded successfully');
                    loadFiles();
                } else {
                    showToast('error', 'Failed to upload file');
                }
                setUploading(false);
            };
            reader.readAsDataURL(file);
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to upload file');
            setUploading(false);
        }

        e.target.value = '';
    };

    const formatSize = (bytes: number) => {
        if (bytes === 0) return '-';
        if (bytes < 1024) return `${bytes} B`;
        if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
        return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
    };

    const formatDate = (dateStr: string) => {
        try {
            return new Date(dateStr).toLocaleString();
        } catch {
            return dateStr;
        }
    };

    if (editingFile) {
        const fileName = editingFile.split('/').pop();
        return (
            <div className="h-full flex flex-col">
                <div className="flex items-center justify-between p-4 border-b border-slate-700">
                    <div className="flex items-center gap-3">
                        <button onClick={() => setEditingFile(null)} className="p-2 hover:bg-slate-700 rounded">
                            <X className="w-4 h-4 text-gray-400" />
                        </button>
                        <div>
                            <h3 className="text-white font-medium">{fileName}</h3>
                            <p className="text-xs text-gray-500">{editingFile}</p>
                        </div>
                    </div>
                    <button
                        onClick={saveFile}
                        disabled={saving}
                        className="flex items-center gap-2 px-4 py-2 bg-green-600 hover:bg-green-700 rounded text-white text-sm disabled:opacity-50"
                    >
                        <Save className="w-4 h-4" />
                        {saving ? 'Saving...' : 'Save'}
                    </button>
                </div>
                <textarea
                    value={editContent}
                    onChange={(e) => setEditContent(e.target.value)}
                    className="flex-1 w-full p-4 bg-slate-900 text-gray-100 font-mono text-sm resize-none focus:outline-none"
                    spellCheck={false}
                />
            </div>
        );
    }

    return (
        <div className="h-full flex flex-col">
            <div className="flex items-center justify-between p-4 border-b border-slate-700">
                <div className="flex items-center gap-2">
                    <button onClick={goUp} disabled={currentPath === '/'} className="p-2 hover:bg-slate-700 rounded disabled:opacity-50" title="Go up">
                        <ChevronUp className="w-4 h-4 text-gray-400" />
                    </button>
                    <div className="flex items-center text-sm text-gray-400">
                        <span className="text-blue-400">~</span>
                        {currentPath.split('/').filter(Boolean).map((part, i, arr) => (
                            <span key={i} className="flex items-center">
                                <ChevronRight className="w-4 h-4 mx-1" />
                                <button onClick={() => navigateTo('/' + arr.slice(0, i + 1).join('/'))} className="hover:text-white">{part}</button>
                            </span>
                        ))}
                    </div>
                </div>
                <div className="flex items-center gap-2">
                    <button onClick={() => { setCreateType('folder'); setShowCreateDialog(true); }} className="p-2 hover:bg-slate-700 rounded text-gray-400 hover:text-white" title="New folder">
                        <FolderPlus className="w-4 h-4" />
                    </button>
                    <button onClick={() => { setCreateType('file'); setShowCreateDialog(true); }} className="p-2 hover:bg-slate-700 rounded text-gray-400 hover:text-white" title="New file">
                        <FilePlus className="w-4 h-4" />
                    </button>
                    <label className="p-2 hover:bg-slate-700 rounded text-gray-400 hover:text-white cursor-pointer" title="Upload">
                        <Upload className="w-4 h-4" />
                        <input type="file" className="hidden" onChange={handleUpload} disabled={uploading} />
                    </label>
                    <button onClick={loadFiles} className="p-2 hover:bg-slate-700 rounded text-gray-400 hover:text-white" title="Refresh">
                        <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
                    </button>
                </div>
            </div>

            <div className="flex-1 overflow-auto">
                {loading ? (
                    <div className="flex items-center justify-center py-12">
                        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-500"></div>
                    </div>
                ) : files.length === 0 ? (
                    <div className="text-center py-12 text-gray-500">
                        <Folder className="w-12 h-12 mx-auto mb-3 opacity-50" />
                        <p>Empty folder</p>
                    </div>
                ) : (
                    <table className="w-full">
                        <thead className="bg-slate-800/50 text-xs text-gray-500 uppercase">
                            <tr>
                                <th className="text-left px-4 py-2">Name</th>
                                <th className="text-left px-4 py-2 w-24">Size</th>
                                <th className="text-left px-4 py-2 w-24">Permissions</th>
                                <th className="text-left px-4 py-2 w-40">Modified</th>
                                <th className="text-right px-4 py-2 w-24">Actions</th>
                            </tr>
                        </thead>
                        <tbody className="divide-y divide-slate-800">
                            {files.map((file) => (
                                <tr
                                    key={file.path}
                                    className={`hover:bg-slate-800/30 cursor-pointer ${selectedFile?.path === file.path ? 'bg-slate-800/50' : ''}`}
                                    onClick={() => setSelectedFile(file)}
                                    onDoubleClick={() => openFile(file)}
                                >
                                    <td className="px-4 py-2">
                                        <div className="flex items-center gap-2">
                                            {file.is_dir ? <Folder className="w-4 h-4 text-blue-400" /> : <File className="w-4 h-4 text-gray-400" />}
                                            <span className="text-white text-sm">{file.name}</span>
                                        </div>
                                    </td>
                                    <td className="px-4 py-2 text-sm text-gray-400">{file.is_dir ? '-' : formatSize(file.size)}</td>
                                    <td className="px-4 py-2 text-sm text-gray-400 font-mono">{file.permissions}</td>
                                    <td className="px-4 py-2 text-sm text-gray-400">{formatDate(file.mod_time)}</td>
                                    <td className="px-4 py-2">
                                        <div className="flex items-center justify-end gap-1">
                                            {!file.is_dir && (
                                                <>
                                                    <button onClick={(e) => { e.stopPropagation(); openFile(file); }} className="p-1 hover:bg-slate-700 rounded text-gray-400 hover:text-white" title="Edit">
                                                        <Edit className="w-3.5 h-3.5" />
                                                    </button>
                                                    <button onClick={(e) => { e.stopPropagation(); downloadFile(file); }} className="p-1 hover:bg-slate-700 rounded text-gray-400 hover:text-white" title="Download">
                                                        <Download className="w-3.5 h-3.5" />
                                                    </button>
                                                </>
                                            )}
                                            <button onClick={(e) => { e.stopPropagation(); deleteItem(file); }} className="p-1 hover:bg-slate-700 rounded text-gray-400 hover:text-red-400" title="Delete">
                                                <Trash2 className="w-3.5 h-3.5" />
                                            </button>
                                        </div>
                                    </td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                )}
            </div>

            {showCreateDialog && (
                <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
                    <div className="bg-slate-800 rounded-xl p-6 w-96 border border-slate-700">
                        <h3 className="text-lg font-semibold text-white mb-4">Create New {createType === 'folder' ? 'Folder' : 'File'}</h3>
                        <input
                            type="text"
                            value={newName}
                            onChange={(e) => setNewName(e.target.value)}
                            placeholder={createType === 'folder' ? 'folder-name' : 'filename.txt'}
                            className="w-full px-4 py-2 bg-slate-900 border border-slate-600 rounded text-white mb-4 focus:border-blue-500 focus:outline-none"
                            autoFocus
                            onKeyDown={(e) => e.key === 'Enter' && createItem()}
                        />
                        <div className="flex justify-end gap-2">
                            <button onClick={() => { setShowCreateDialog(false); setNewName(''); }} className="px-4 py-2 bg-slate-700 hover:bg-slate-600 rounded text-white">Cancel</button>
                            <button onClick={createItem} disabled={!newName.trim()} className="px-4 py-2 bg-blue-600 hover:bg-blue-700 rounded text-white disabled:opacity-50">Create</button>
                        </div>
                    </div>
                </div>
            )}
        </div>
    );
}
