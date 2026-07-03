import { useState, useEffect } from 'react';
import { Database, Server, Plus, Trash2, Users, Key } from 'lucide-react';
import { showToast } from './Toast';
import { AddDatabaseModalV2 } from './AddDatabaseModalV2';
import { AddUserModalV2 } from './AddUserModalV2';

const API_BASE = '/api/v2';

interface DatabaseServer {
    id: number;
    type_id: number;
    type_name: string;
    type_icon: string;
    name: string;
    version: string;
    host: string;
    port: number;
    is_default: boolean;
    status: string;
    created_at: string;
}

interface DatabaseItem {
    id: number;
    name: string;
    users: string[];
    created_at: string;
}

interface DatabaseUser {
    id: number;
    username: string;
    databases: string[];
    created_at: string;
}

export function DatabaseManagementV2() {
    const [servers, setServers] = useState<DatabaseServer[]>([]);
    const [selectedServer, setSelectedServer] = useState<DatabaseServer | null>(null);
    const [activeTab, setActiveTab] = useState<'databases' | 'users'>('databases');
    const [databases, setDatabases] = useState<DatabaseItem[]>([]);
    const [users, setUsers] = useState<DatabaseUser[]>([]);
    const [loading, setLoading] = useState(false);
    const [showAddDatabase, setShowAddDatabase] = useState(false);
    const [showAddUser, setShowAddUser] = useState(false);

    // Load servers on mount
    useEffect(() => {
        loadServers();
    }, []);

    // Load databases and users when server changes
    useEffect(() => {
        if (selectedServer) {
            loadDatabases(selectedServer.id);
            loadUsers(selectedServer.id);
        }
    }, [selectedServer]);

    const loadServers = async () => {
        try {
            const res = await fetch(`${API_BASE}/database-servers`);
            if (!res.ok) throw new Error('Failed to load servers');
            const data = await res.json();
            setServers(data);
            if (data.length > 0 && !selectedServer) {
                setSelectedServer(data[0]);
            }
        } catch (err: any) {
            showToast('error', err.message);
        }
    };

    const loadDatabases = async (serverId: number) => {
        setLoading(true);
        try {
            const res = await fetch(`${API_BASE}/database-servers/${serverId}/databases`);
            if (!res.ok) throw new Error('Failed to load databases');
            const data = await res.json();
            setDatabases(data);
        } catch (err: any) {
            showToast('error', err.message);
        } finally {
            setLoading(false);
        }
    };

    const loadUsers = async (serverId: number) => {
        setLoading(true);
        try {
            const res = await fetch(`${API_BASE}/database-servers/${serverId}/users`);
            if (!res.ok) throw new Error('Failed to load users');
            const data = await res.json();
            setUsers(data);
        } catch (err: any) {
            showToast('error', err.message);
        } finally {
            setLoading(false);
        }
    };

    const handleDeleteDatabase = async (id: number) => {
        if (!confirm('Are you sure you want to delete this database?')) return;

        try {
            const res = await fetch(`${API_BASE}/databases/${id}`, { method: 'DELETE' });
            if (!res.ok) throw new Error('Failed to delete database');
            showToast('success', 'Database deleted successfully');
            if (selectedServer) loadDatabases(selectedServer.id);
        } catch (err: any) {
            showToast('error', err.message);
        }
    };

    const handleDeleteUser = async (id: number) => {
        if (!confirm('Are you sure you want to delete this user?')) return;

        try {
            const res = await fetch(`${API_BASE}/database-users/${id}`, { method: 'DELETE' });
            if (!res.ok) {
                const text = await res.text();
                throw new Error(text || 'Failed to delete user');
            }
            showToast('success', 'User deleted successfully');
            if (selectedServer) loadUsers(selectedServer.id);
        } catch (err: any) {
            showToast('error', err.message);
        }
    };

    return (
        <div className="p-6 space-y-6">
            {/* Header */}
            <div className="flex justify-between items-center">
                <div>
                    <h2 className="text-2xl font-bold text-gray-100">Databases</h2>
                    <p className="text-sm text-gray-400 mt-1">
                        Manage PostgreSQL and MariaDB servers, databases, and users
                    </p>
                </div>
            </div>

            {/* Server Tabs */}
            <div className="bg-gray-900 border border-gray-800 rounded-xl p-4">
                <div className="flex items-center gap-2 mb-4">
                    <Server className="w-5 h-5 text-blue-400" />
                    <h3 className="text-lg font-bold text-gray-100">Database Servers</h3>
                </div>

                {servers.length === 0 ? (
                    <div className="text-center py-8 text-gray-500">
                        No database servers configured
                    </div>
                ) : (
                    <div className="flex gap-2 flex-wrap">
                        {servers.map(server => (
                            <button
                                key={server.id}
                                onClick={() => setSelectedServer(server)}
                                className={`px-4 py-2 rounded-lg transition-colors flex items-center gap-2 ${selectedServer?.id === server.id
                                    ? 'bg-blue-600 text-white'
                                    : 'bg-gray-800 text-gray-300 hover:bg-gray-700'
                                    }`}
                            >
                                <span>{server.type_icon}</span>
                                <span className="font-medium">{server.name}</span>
                                <span className="text-xs opacity-75">{server.host}:{server.port}</span>
                            </button>
                        ))}
                    </div>
                )}
            </div>

            {/* Content Area */}
            {selectedServer && (
                <div className="bg-gray-900 border border-gray-800 rounded-xl p-6">
                    {/* Tabs */}
                    <div className="flex gap-4 mb-6 border-b border-gray-800">
                        <button
                            onClick={() => setActiveTab('databases')}
                            className={`pb-3 px-2 flex items-center gap-2 transition-colors ${activeTab === 'databases'
                                ? 'border-b-2 border-blue-500 text-blue-400'
                                : 'text-gray-400 hover:text-gray-300'
                                }`}
                        >
                            <Database className="w-4 h-4" />
                            <span className="font-medium">Databases</span>
                            <span className="text-xs bg-gray-800 px-2 py-0.5 rounded">
                                {databases.length}
                            </span>
                        </button>
                        <button
                            onClick={() => setActiveTab('users')}
                            className={`pb-3 px-2 flex items-center gap-2 transition-colors ${activeTab === 'users'
                                ? 'border-b-2 border-blue-500 text-blue-400'
                                : 'text-gray-400 hover:text-gray-300'
                                }`}
                        >
                            <Users className="w-4 h-4" />
                            <span className="font-medium">Users</span>
                            <span className="text-xs bg-gray-800 px-2 py-0.5 rounded">
                                {users.length}
                            </span>
                        </button>
                    </div>

                    {/* Databases Tab */}
                    {activeTab === 'databases' && (
                        <div className="space-y-4">
                            <div className="flex justify-between items-center">
                                <h3 className="text-lg font-bold text-gray-100">Databases</h3>
                                <button
                                    onClick={() => setShowAddDatabase(true)}
                                    className="bg-blue-600 hover:bg-blue-700 text-white px-4 py-2 rounded-lg flex items-center gap-2"
                                >
                                    <Plus className="w-4 h-4" />
                                    Add Database
                                </button>
                            </div>

                            {loading ? (
                                <div className="text-center py-8 text-gray-500">Loading...</div>
                            ) : databases.length === 0 ? (
                                <div className="text-center py-12 bg-gray-800/50 rounded-lg">
                                    <Database className="w-12 h-12 text-gray-600 mx-auto mb-3" />
                                    <p className="text-gray-400">No databases yet</p>
                                    <p className="text-sm text-gray-500 mt-1">Create your first database</p>
                                </div>
                            ) : (
                                <div className="grid gap-4">
                                    {databases.map(db => (
                                        <div
                                            key={db.id}
                                            className="bg-gray-800/50 border border-gray-700 rounded-lg p-4 hover:border-gray-600 transition-colors"
                                        >
                                            <div className="flex justify-between items-start">
                                                <div className="flex-1">
                                                    <h4 className="text-lg font-bold text-gray-100">{db.name}</h4>
                                                    <div className="flex items-center gap-2 mt-2">
                                                        <Users className="w-4 h-4 text-gray-500" />
                                                        <span className="text-sm text-gray-400">
                                                            {db.users.length} user{db.users.length !== 1 ? 's' : ''}: {db.users.join(', ')}
                                                        </span>
                                                    </div>
                                                    <p className="text-xs text-gray-500 mt-2">
                                                        Created: {new Date(db.created_at).toLocaleString()}
                                                    </p>
                                                </div>
                                                <div className="flex gap-2">
                                                    <button
                                                        onClick={() => handleDeleteDatabase(db.id)}
                                                        className="p-2 text-red-400 hover:bg-red-500/10 rounded-lg transition-colors"
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
                    )}

                    {/* Users Tab */}
                    {activeTab === 'users' && (
                        <div className="space-y-4">
                            <div className="flex justify-between items-center">
                                <h3 className="text-lg font-bold text-gray-100">Users</h3>
                                <button
                                    onClick={() => setShowAddUser(true)}
                                    className="bg-blue-600 hover:bg-blue-700 text-white px-4 py-2 rounded-lg flex items-center gap-2"
                                >
                                    <Plus className="w-4 h-4" />
                                    Add User
                                </button>
                            </div>

                            {loading ? (
                                <div className="text-center py-8 text-gray-500">Loading...</div>
                            ) : users.length === 0 ? (
                                <div className="text-center py-12 bg-gray-800/50 rounded-lg">
                                    <Users className="w-12 h-12 text-gray-600 mx-auto mb-3" />
                                    <p className="text-gray-400">No users yet</p>
                                    <p className="text-sm text-gray-500 mt-1">Create your first user</p>
                                </div>
                            ) : (
                                <div className="grid gap-4">
                                    {users.map(user => (
                                        <div
                                            key={user.id}
                                            className="bg-gray-800/50 border border-gray-700 rounded-lg p-4 hover:border-gray-600 transition-colors"
                                        >
                                            <div className="flex justify-between items-start">
                                                <div className="flex-1">
                                                    <div className="flex items-center gap-2">
                                                        <h4 className="text-lg font-bold text-gray-100">{user.username}</h4>
                                                        {user.databases.length > 0 && (
                                                            <span className="text-xs bg-yellow-500/20 text-yellow-400 px-2 py-0.5 rounded border border-yellow-500/30">
                                                                In Use
                                                            </span>
                                                        )}
                                                    </div>
                                                    <div className="flex items-center gap-2 mt-2">
                                                        <Key className="w-4 h-4 text-gray-500" />
                                                        <span className="text-sm text-gray-400">
                                                            Access to {user.databases.length} database{user.databases.length !== 1 ? 's' : ''}
                                                            {user.databases.length > 0 && `: ${user.databases.join(', ')}`}
                                                        </span>
                                                    </div>
                                                    <p className="text-xs text-gray-500 mt-2">
                                                        Created: {new Date(user.created_at).toLocaleString()}
                                                    </p>
                                                </div>
                                                <div className="flex gap-2">
                                                    <button
                                                        onClick={() => handleDeleteUser(user.id)}
                                                        className={`p-2 rounded-lg transition-colors ${user.databases.length > 0
                                                            ? 'text-gray-600 cursor-not-allowed opacity-50'
                                                            : 'text-red-400 hover:bg-red-500/10'
                                                            }`}
                                                        title={
                                                            user.databases.length > 0
                                                                ? 'Cannot delete user with database access. Revoke access first.'
                                                                : 'Delete user'
                                                        }
                                                        disabled={user.databases.length > 0}
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
                    )}
                </div>
            )}

            {/* Modals */}
            {showAddDatabase && selectedServer && (
                <AddDatabaseModalV2
                    serverId={selectedServer.id}
                    serverName={selectedServer.name}
                    existingUsers={users.map(u => ({ id: u.id, username: u.username }))}
                    onClose={() => setShowAddDatabase(false)}
                    onSuccess={() => {
                        if (selectedServer) {
                            loadDatabases(selectedServer.id);
                            // Delay to allow backend to finalize user creation
                            setTimeout(() => loadUsers(selectedServer.id), 500);
                        }
                    }}
                />
            )}

            {showAddUser && selectedServer && (
                <AddUserModalV2
                    serverId={selectedServer.id}
                    serverName={selectedServer.name}
                    onClose={() => setShowAddUser(false)}
                    onSuccess={() => {
                        if (selectedServer) loadUsers(selectedServer.id);
                    }}
                />
            )}
        </div>
    );
}
