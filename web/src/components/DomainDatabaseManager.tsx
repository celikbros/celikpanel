import { useState, useEffect } from 'react';
import { Database, Plus, Trash2, RefreshCw, ExternalLink } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { readApiError } from '../lib/apiError';

interface DomainDatabaseManagerProps {
    domainId: number;
    domainName: string;
    readOnly?: boolean;
    isAdditionalUser?: boolean;
}

type DatabaseType = 'mysql' | 'postgresql';
type DatabaseEngine = { value: DatabaseType; label: string };

function parseAvailableDatabaseTypes(value: unknown): DatabaseEngine[] {
    if (!Array.isArray(value)) return [];

    const parsed: DatabaseEngine[] = [];
    const seen = new Set<DatabaseType>();
    for (const item of value) {
        if (item !== 'mysql' && item !== 'postgresql') return [];
        if (seen.has(item)) continue;
        seen.add(item);
        parsed.push({
            value: item,
            label: item === 'mysql' ? 'MySQL / MariaDB' : 'PostgreSQL',
        });
    }
    return parsed;
}

// The database web tools (phpMyAdmin / phpPgAdmin). Installed → a launch
// button opening the panel-proxied tool. Parent engine present but the tool
// not → a hint pointing to Services. Neither → nothing (the parent-engine
// requirement means this whole page would be hidden anyway).
// Veritabanı web araçları (phpMyAdmin / phpPgAdmin). Kurulu → panel-vekilli
// aracı açan bir düğme. Üst motor var ama araç yok → Servisler'e yönlendiren
// bir ipucu. Hiçbiri → hiçbir şey.
function DBToolsCard() {
    const { t } = useI18n();
    const [caps, setCaps] = useState<{ database_servers?: string[]; db_tools?: string[] } | null>(null);
    useEffect(() => {
        fetch('/api/v1/hosting/capabilities')
            .then((r) => (r.ok ? r.json() : null))
            .then(setCaps)
            .catch(() => setCaps(null));
    }, []);
    if (!caps) return null;

    const tools = [
        { id: 'phpmyadmin', label: 'phpMyAdmin', engine: 'mariadb' },
        { id: 'phppgadmin', label: 'phpPgAdmin', engine: 'postgresql' },
    ];
    const installed = new Set(caps.db_tools ?? []);
    const engines = new Set(caps.database_servers ?? []);
    // Only tools whose parent engine is installed are relevant here.
    // Yalnız üst motoru kurulu olan araçlar burada anlamlıdır.
    const relevant = tools.filter((tl) => engines.has(tl.engine));
    if (relevant.length === 0) return null;

    return (
        <div className="rounded-lg border border-border bg-surface-2/50 p-4">
            <h4 className="mb-1 text-sm font-semibold text-fg">{t('dbtools.title')}</h4>
            <p className="mb-3 text-xs text-fg-subtle">{t('dbtools.hint')}</p>
            <div className="flex flex-wrap gap-2">
                {relevant.map((tl) =>
                    installed.has(tl.id) ? (
                        <a
                            key={tl.id}
                            href={`/dbtool/${tl.id}/`}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="inline-flex items-center gap-1.5 rounded-lg bg-primary px-3 py-1.5 text-xs font-semibold text-primary-fg hover:bg-primary/90"
                        >
                            <ExternalLink className="h-3.5 w-3.5" />
                            {t('dbtools.open', { name: tl.label })}
                        </a>
                    ) : (
                        <span
                            key={tl.id}
                            className="inline-flex items-center gap-1.5 rounded-lg border border-border bg-surface px-3 py-1.5 text-xs font-medium text-fg-subtle"
                        >
                            {t('dbtools.install', { name: tl.label })}
                        </span>
                    ),
                )}
            </div>
        </div>
    );
}

interface DatabaseInfo {
    id: number;
    name: string;
    type: string;
    user: string;
    created_at: string;
}

export function DomainDatabaseManager({
    domainId,
    domainName,
    readOnly = false,
    isAdditionalUser = false,
}: DomainDatabaseManagerProps) {
    const { t } = useI18n();
    const [databases, setDatabases] = useState<DatabaseInfo[]>([]);
    const [loading, setLoading] = useState(true);
    const [creating, setCreating] = useState(false);
    const [showCreateForm, setShowCreateForm] = useState(false);

    // Only engines that are actually installed may be offered — a dropdown
    // with MySQL and PostgreSQL on a server that runs neither is a settings
    // page for ghosts. The engine ids map to the panel's db types.
    // Yalnız gerçekten kurulu motorlar sunulabilir — ikisi de koşmayan bir
    // sunucuda MySQL+PostgreSQL açılır listesi, hayaletlere ayar sayfasıdır.
    const [engines, setEngines] = useState<DatabaseEngine[]>([]);
    useEffect(() => {
        if (isAdditionalUser) {
            // Server-wide capability inventory is admin-only. For a team
            // member, loadDatabases consumes only the tenant-safe
            // available_types field returned with this domain's databases.
            setEngines([]);
            return;
        }
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
    }, [isAdditionalUser]);

    // Form state
    const [dbName, setDbName] = useState('');
    const [dbType, setDbType] = useState<DatabaseType>('mysql');
    const [dbPassword, setDbPassword] = useState('');

    useEffect(() => {
        loadDatabases();
    }, [domainId, isAdditionalUser]);

    const loadDatabases = async () => {
        setLoading(true);
        if (isAdditionalUser) {
            setEngines([]);
            setDatabases([]);
        }
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/databases`);
            if (res.ok) {
                const data: unknown = await res.json();
                const payload = data && typeof data === 'object' && !Array.isArray(data)
                    ? data as Record<string, unknown>
                    : {};
                const nextDatabases: DatabaseInfo[] = Array.isArray(payload.databases) ? payload.databases as DatabaseInfo[] : [];
                setDatabases(nextDatabases);
                if (isAdditionalUser) {
                    const list = parseAvailableDatabaseTypes(payload.available_types);
                    setEngines(list);
                    if (list.length > 0) setDbType(list[0].value);
                }
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
        if (readOnly || (isAdditionalUser && !engines.some((engine) => engine.value === dbType))) return;

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
                showToast('error', (await readApiError(res)).message || 'Failed to create database');
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to create database');
        } finally {
            setCreating(false);
        }
    };

    const handleDeleteDatabase = async (db: DatabaseInfo) => {
        if (readOnly) return;
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
            {!readOnly && !showCreateForm && (!isAdditionalUser || engines.length > 0) && (
                <button
                    onClick={() => setShowCreateForm(true)}
                    className="px-4 py-2 bg-primary text-white rounded hover:bg-primary-hover flex items-center gap-2"
                >
                    <Plus className="w-4 h-4" />
                    Create Database
                </button>
            )}

            {!readOnly && isAdditionalUser && !loading && engines.length === 0 && (
                <div className="rounded-lg border border-info/30 bg-info/10 px-4 py-3 text-sm text-fg">
                    {t('db.teamEngineUnavailable')}
                </div>
            )}

            {/* Create Database Form */}
            {!readOnly && showCreateForm && (!isAdditionalUser || engines.length > 0) && (
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
                                onChange={(e) => setDbType(e.target.value as DatabaseType)}
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
                                disabled={creating || (isAdditionalUser && !engines.some((engine) => engine.value === dbType))}
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
                            {!readOnly && (!isAdditionalUser || engines.length > 0) && (
                                <p className="text-sm mt-1">Click "Create Database" to get started</p>
                            )}
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
                                        {!readOnly && (
                                            <div className="flex gap-2">
                                                <button
                                                    onClick={() => handleDeleteDatabase(db)}
                                                    className="p-2 text-danger hover:bg-danger/30 rounded transition-colors"
                                                    title="Delete database"
                                                    aria-label="Delete database"
                                                >
                                                    <Trash2 className="w-4 h-4" />
                                                </button>
                                            </div>
                                        )}
                                    </div>
                                </div>
                            ))}
                        </div>
                    )}
                </div>
            </div>

            {!isAdditionalUser && <DBToolsCard />}
        </div>
    );
}
