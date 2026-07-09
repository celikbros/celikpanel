import { useState, useEffect } from 'react';
import { Database, Plus, Trash2, RefreshCw } from 'lucide-react';
import { showToast } from './Toast';

interface DomainDatabaseManagerProps {
    domainId: number;
    domainName: string;
}

interface DatabaseInfo {
    id: number;
    name: string;
    type: string;
    user: string;
    created_at: string;
}

export function DomainDatabaseManager({ domainId, domainName }: DomainDatabaseManagerProps) {
    const [databases, setDatabases] = useState<DatabaseInfo[]>([]);
    const [loading, setLoading] = useState(true);
    const [creating, setCreating] = useState(false);
    const [showCreateForm, setShowCreateForm] = useState(false);

    // Only engines that are actually installed may be offered — a dropdown
    // with MySQL and PostgreSQL on a server that runs neither is a settings
    // page for ghosts. The engine ids map to the panel's db types.
    // Yalnız gerçekten kurulu motorlar sunulabilir — ikisi de koşmayan bir
    // sunucuda MySQL+PostgreSQL açılır listesi, hayaletlere ayar sayfasıdır.
    const [engines, setEngines] = useState<{ value: 'mysql' | 'postgresql'; label: string }[]>([]);
    useEffect(() => {
        fetch('/api/v1/hosting/capabilities')
            .then((r) => (r.ok ? r.json() : null))
            .then((c: { database_servers?: string[] } | null) => {
                const list: { value: 'mysql' | 'postgresql'; label: string }[] = [];
                for (const id of c?.database_servers ?? []) {
                    if (id === 'mariadb') list.push({ value: 'mysql', label: 'MySQL / MariaDB' });
                    if (id === 'postgresql') list.push({ value: 'postgresql', label: 'PostgreSQL' });
                }
                setEngines(list);
                if (list.length > 0) setDbType(list[0].value);
            })
            .catch(() => setEngines([]));
    }, []);

    // Form state
    const [dbName, setDbName] = useState('');
    const [dbType, setDbType] = useState<'mysql' | 'postgresql'>('mysql');
    const [dbPassword, setDbPassword] = useState('');

    useEffect(() => {
        loadDatabases();
    }, [domainId]);

    const loadDatabases = async () => {
        setLoading(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/databases`);
            if (res.ok) {
                const data = await res.json();
                setDatabases(data.databases || []);
            } else {
                showToast('error', 'Failed to load databases');
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to load databases');
        } finally {
            setLoading(false);
        }
    };

    const handleCreateDatabase = async (e: React.FormEvent) => {
        e.preventDefault();

        if (!dbName || !dbPassword) {
            showToast('error', 'Name and password are required');
            return;
        }

        setCreating(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/databases`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    name: dbName,
                    type: dbType,
                    password: dbPassword
                })
            });

            if (res.ok) {
                const data = await res.json();
                showToast('success', `Database "${data.name}" created successfully`);
                setShowCreateForm(false);
                setDbName('');
                setDbPassword('');
                loadDatabases();
            } else {
                const error = await res.text();
                showToast('error', error || 'Failed to create database');
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to create database');
        } finally {
            setCreating(false);
        }
    };

    const handleDeleteDatabase = async (db: DatabaseInfo) => {
        if (!confirm(`Delete database "${db.name}"?\n\nThis action cannot be undone. All data will be lost.`)) {
            return;
        }

        try {
            const res = await fetch(`/api/v1/domains/${domainId}/databases/${db.id}`, {
                method: 'DELETE'
            });

            if (res.ok) {
                showToast('success', `Database "${db.name}" deleted`);
                loadDatabases();
            } else {
                showToast('error', 'Failed to delete database');
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to delete database');
        }
    };

    return (
        <div className="space-y-6">
            <div>
                <h3 className="text-lg font-bold text-fg mb-2">Database Management</h3>
                <p className="text-sm text-fg-muted">
                    Manage databases for {domainName}
                </p>
            </div>

            {/* Create Database Button */}
            {!showCreateForm && (
                <button
                    onClick={() => setShowCreateForm(true)}
                    className="px-4 py-2 bg-primary text-white rounded hover:bg-primary-hover flex items-center gap-2"
                >
                    <Plus className="w-4 h-4" />
                    Create Database
                </button>
            )}

            {/* Create Database Form */}
            {showCreateForm && (
                <div className="bg-surface-2/50 rounded-lg p-6 border border-border">
                    <h4 className="text-md font-semibold text-fg mb-4">Create New Database</h4>
                    <form onSubmit={handleCreateDatabase} className="space-y-4">
                        <div>
                            <label className="block text-sm text-fg-muted mb-2">Database Name</label>
                            <input
                                type="text"
                                value={dbName}
                                onChange={(e) => setDbName(e.target.value)}
                                placeholder="myapp"
                                className="w-full bg-surface border border-border rounded px-4 py-2 text-fg focus:border-primary focus:outline-none"
                                required
                            />
                            <p className="text-xs text-fg-subtle mt-1">
                                Will be prefixed with domain name: {domainName.replace(/\./g, '_')}_{dbName}
                            </p>
                        </div>

                        <div>
                            <label className="block text-sm text-fg-muted mb-2">Database Type</label>
                            <select
                                value={dbType}
                                onChange={(e) => setDbType(e.target.value as 'mysql' | 'postgresql')}
                                className="w-full bg-surface border border-border rounded px-4 py-2 text-fg focus:border-primary focus:outline-none"
                            >
                                {engines.map((eng) => (
                                    <option key={eng.value} value={eng.value}>{eng.label}</option>
                                ))}
                            </select>
                        </div>

                        <div>
                            <label className="block text-sm text-fg-muted mb-2">Password</label>
                            <input
                                type="password"
                                value={dbPassword}
                                onChange={(e) => setDbPassword(e.target.value)}
                                placeholder="Enter a strong password"
                                className="w-full bg-surface border border-border rounded px-4 py-2 text-fg focus:border-primary focus:outline-none"
                                required
                            />
                        </div>

                        <div className="flex gap-2">
                            <button
                                type="submit"
                                disabled={creating}
                                className="px-6 py-2 bg-success text-white rounded hover:bg-success disabled:opacity-50 flex items-center gap-2"
                            >
                                <Database className="w-4 h-4" />
                                {creating ? 'Creating...' : 'Create Database'}
                            </button>
                            <button
                                type="button"
                                onClick={() => {
                                    setShowCreateForm(false);
                                    setDbName('');
                                    setDbPassword('');
                                }}
                                className="px-6 py-2 bg-surface-3 text-fg rounded hover:bg-surface-3"
                            >
                                Cancel
                            </button>
                        </div>
                    </form>
                </div>
            )}

            {/* Database List */}
            <div className="bg-surface-2/50 rounded-lg border border-border">
                <div className="flex items-center justify-between p-4 border-b border-border">
                    <div className="flex items-center gap-2">
                        <Database className="w-5 h-5 text-primary" />
                        <h4 className="text-md font-semibold text-fg">Databases</h4>
                        <span className="text-sm text-fg-muted">
                            ({databases.length})
                        </span>
                    </div>
                    <button
                        onClick={loadDatabases}
                        disabled={loading}
                        className="p-2 text-fg-muted hover:text-fg transition-colors"
                        title="Refresh"
                    >
                        <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
                    </button>
                </div>

                <div className="p-4">
                    {loading ? (
                        <div className="flex items-center justify-center h-32">
                            <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
                        </div>
                    ) : databases.length === 0 ? (
                        <div className="text-center text-fg-subtle py-12">
                            <Database className="w-12 h-12 mx-auto mb-2 opacity-50" />
                            <p>No databases created yet</p>
                            <p className="text-sm mt-1">Click "Create Database" to get started</p>
                        </div>
                    ) : (
                        <div className="space-y-3">
                            {databases.map((db) => (
                                <div
                                    key={db.id}
                                    className="bg-surface border border-border rounded p-4 hover:border-border-strong transition-colors"
                                >
                                    <div className="flex items-start justify-between">
                                        <div className="flex-1">
                                            <div className="flex items-center gap-2 mb-2">
                                                <Database className="w-4 h-4 text-primary" />
                                                <h5 className="font-mono text-fg font-semibold">{db.name}</h5>
                                                <span className={`text-xs px-2 py-0.5 rounded ${db.type === 'mysql'
                                                        ? 'bg-primary/50 text-primary'
                                                        : 'bg-purple-900/50 text-purple-300'
                                                    }`}>
                                                    {db.type.toUpperCase()}
                                                </span>
                                            </div>
                                            <div className="grid grid-cols-2 gap-2 text-sm">
                                                <div>
                                                    <span className="text-fg-muted">User:</span>
                                                    <span className="ml-2 text-fg font-mono">{db.user}</span>
                                                </div>
                                                <div>
                                                    <span className="text-fg-muted">Created:</span>
                                                    <span className="ml-2 text-fg">
                                                        {new Date(db.created_at).toLocaleDateString()}
                                                    </span>
                                                </div>
                                            </div>
                                        </div>
                                        <div className="flex gap-2">
                                            <button
                                                onClick={() => handleDeleteDatabase(db)}
                                                className="p-2 text-danger hover:bg-danger/30 rounded transition-colors"
                                                title="Delete database"
                                            >
                                                <Trash2 className="w-4 h-4" />
                                            </button>
                                        </div>
                                    </div>
                                </div>
                            ))}
                        </div>
                    )}
                </div>
            </div>
        </div>
    );
}
