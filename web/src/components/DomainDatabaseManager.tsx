import { useState, useEffect } from 'react';
import { Database, Plus, Trash2, ExternalLink, RefreshCw } from 'lucide-react';
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

    const getPhpMyAdminUrl = () => {
        return '/phpmyadmin'; // Adjust based on your setup
    };

    const getPgAdminUrl = () => {
        return '/pgadmin'; // Adjust based on your setup
    };

    return (
        <div className="space-y-6">
            <div>
                <h3 className="text-lg font-bold text-gray-100 mb-2">Database Management</h3>
                <p className="text-sm text-gray-400">
                    Manage databases for {domainName}
                </p>
            </div>

            {/* Create Database Button */}
            {!showCreateForm && (
                <button
                    onClick={() => setShowCreateForm(true)}
                    className="px-4 py-2 bg-blue-600 text-white rounded hover:bg-blue-700 flex items-center gap-2"
                >
                    <Plus className="w-4 h-4" />
                    Create Database
                </button>
            )}

            {/* Create Database Form */}
            {showCreateForm && (
                <div className="bg-gray-800/50 rounded-lg p-6 border border-gray-700">
                    <h4 className="text-md font-semibold text-gray-200 mb-4">Create New Database</h4>
                    <form onSubmit={handleCreateDatabase} className="space-y-4">
                        <div>
                            <label className="block text-sm text-gray-400 mb-2">Database Name</label>
                            <input
                                type="text"
                                value={dbName}
                                onChange={(e) => setDbName(e.target.value)}
                                placeholder="myapp"
                                className="w-full bg-gray-900 border border-gray-700 rounded px-4 py-2 text-white focus:border-blue-500 focus:outline-none"
                                required
                            />
                            <p className="text-xs text-gray-500 mt-1">
                                Will be prefixed with domain name: {domainName.replace(/\./g, '_')}_{dbName}
                            </p>
                        </div>

                        <div>
                            <label className="block text-sm text-gray-400 mb-2">Database Type</label>
                            <select
                                value={dbType}
                                onChange={(e) => setDbType(e.target.value as 'mysql' | 'postgresql')}
                                className="w-full bg-gray-900 border border-gray-700 rounded px-4 py-2 text-white focus:border-blue-500 focus:outline-none"
                            >
                                <option value="mysql">MySQL</option>
                                <option value="postgresql">PostgreSQL</option>
                            </select>
                        </div>

                        <div>
                            <label className="block text-sm text-gray-400 mb-2">Password</label>
                            <input
                                type="password"
                                value={dbPassword}
                                onChange={(e) => setDbPassword(e.target.value)}
                                placeholder="Enter a strong password"
                                className="w-full bg-gray-900 border border-gray-700 rounded px-4 py-2 text-white focus:border-blue-500 focus:outline-none"
                                required
                            />
                        </div>

                        <div className="flex gap-2">
                            <button
                                type="submit"
                                disabled={creating}
                                className="px-6 py-2 bg-green-600 text-white rounded hover:bg-green-700 disabled:opacity-50 flex items-center gap-2"
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
                                className="px-6 py-2 bg-gray-700 text-white rounded hover:bg-gray-600"
                            >
                                Cancel
                            </button>
                        </div>
                    </form>
                </div>
            )}

            {/* Database List */}
            <div className="bg-gray-800/50 rounded-lg border border-gray-700">
                <div className="flex items-center justify-between p-4 border-b border-gray-700">
                    <div className="flex items-center gap-2">
                        <Database className="w-5 h-5 text-blue-400" />
                        <h4 className="text-md font-semibold text-gray-200">Databases</h4>
                        <span className="text-sm text-gray-400">
                            ({databases.length})
                        </span>
                    </div>
                    <button
                        onClick={loadDatabases}
                        disabled={loading}
                        className="p-2 text-gray-400 hover:text-white transition-colors"
                        title="Refresh"
                    >
                        <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
                    </button>
                </div>

                <div className="p-4">
                    {loading ? (
                        <div className="flex items-center justify-center h-32">
                            <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-500"></div>
                        </div>
                    ) : databases.length === 0 ? (
                        <div className="text-center text-gray-500 py-12">
                            <Database className="w-12 h-12 mx-auto mb-2 opacity-50" />
                            <p>No databases created yet</p>
                            <p className="text-sm mt-1">Click "Create Database" to get started</p>
                        </div>
                    ) : (
                        <div className="space-y-3">
                            {databases.map((db) => (
                                <div
                                    key={db.id}
                                    className="bg-gray-900 border border-gray-700 rounded p-4 hover:border-gray-600 transition-colors"
                                >
                                    <div className="flex items-start justify-between">
                                        <div className="flex-1">
                                            <div className="flex items-center gap-2 mb-2">
                                                <Database className="w-4 h-4 text-blue-400" />
                                                <h5 className="font-mono text-white font-semibold">{db.name}</h5>
                                                <span className={`text-xs px-2 py-0.5 rounded ${db.type === 'mysql'
                                                        ? 'bg-blue-900/50 text-blue-300'
                                                        : 'bg-purple-900/50 text-purple-300'
                                                    }`}>
                                                    {db.type.toUpperCase()}
                                                </span>
                                            </div>
                                            <div className="grid grid-cols-2 gap-2 text-sm">
                                                <div>
                                                    <span className="text-gray-400">User:</span>
                                                    <span className="ml-2 text-white font-mono">{db.user}</span>
                                                </div>
                                                <div>
                                                    <span className="text-gray-400">Created:</span>
                                                    <span className="ml-2 text-white">
                                                        {new Date(db.created_at).toLocaleDateString()}
                                                    </span>
                                                </div>
                                            </div>
                                        </div>
                                        <div className="flex gap-2">
                                            <a
                                                href={db.type === 'mysql' ? getPhpMyAdminUrl() : getPgAdminUrl()}
                                                target="_blank"
                                                rel="noopener noreferrer"
                                                className="p-2 text-blue-400 hover:bg-blue-900/30 rounded transition-colors"
                                                title={db.type === 'mysql' ? 'Open phpMyAdmin' : 'Open pgAdmin'}
                                            >
                                                <ExternalLink className="w-4 h-4" />
                                            </a>
                                            <button
                                                onClick={() => handleDeleteDatabase(db)}
                                                className="p-2 text-red-400 hover:bg-red-900/30 rounded transition-colors"
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

            {/* Quick Links */}
            <div className="bg-gray-800/50 rounded-lg p-4 border border-gray-700">
                <h4 className="text-sm font-semibold text-gray-200 mb-3">Database Tools</h4>
                <div className="flex gap-3">
                    <a
                        href={getPhpMyAdminUrl()}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="px-4 py-2 bg-blue-600 text-white rounded hover:bg-blue-700 flex items-center gap-2 text-sm"
                    >
                        <ExternalLink className="w-4 h-4" />
                        phpMyAdmin
                    </a>
                    <a
                        href={getPgAdminUrl()}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="px-4 py-2 bg-purple-600 text-white rounded hover:bg-purple-700 flex items-center gap-2 text-sm"
                    >
                        <ExternalLink className="w-4 h-4" />
                        pgAdmin
                    </a>
                </div>
            </div>
        </div>
    );
}
