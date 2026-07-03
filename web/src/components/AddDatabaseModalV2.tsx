import { useState, useEffect } from 'react';
import { X, Database, Key } from 'lucide-react';
import { showToast } from './Toast';

interface AddDatabaseModalV2Props {
    serverId: number;
    serverName: string;
    onClose: () => void;
    onSuccess: () => void;
    existingUsers: Array<{ id: number; username: string }>;
}

export function AddDatabaseModalV2({ serverId, serverName, onClose, onSuccess, existingUsers }: AddDatabaseModalV2Props) {
    const [databaseName, setDatabaseName] = useState('');
    const [domainId, setDomainId] = useState<number | null>(null);
    const [domains, setDomains] = useState<Array<{ id: number; domain_name: string }>>([]);
    const [userMode, setUserMode] = useState<'existing' | 'new'>('new');
    const [selectedUserId, setSelectedUserId] = useState<number>(0);
    const [newUsername, setNewUsername] = useState('');
    const [newPassword, setNewPassword] = useState('');
    const [privileges, setPrivileges] = useState('ALL');
    const [loading, setLoading] = useState(false);

    // Load domains on mount
    useEffect(() => {
        fetch('/api/v1/domains')
            .then(res => res.json())
            .then(data => setDomains(data))
            .catch(err => console.error('Failed to load domains:', err));
    }, []);

    const generatePassword = () => {
        const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789!@#$%^&*';
        let password = '';
        for (let i = 0; i < 16; i++) {
            password += chars.charAt(Math.floor(Math.random() * chars.length));
        }
        setNewPassword(password);
        navigator.clipboard.writeText(password);
        showToast('success', 'Password generated and copied to clipboard');
    };

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        setLoading(true);

        try {
            const body: any = {
                database_name: databaseName,
                domain_id: domainId, // Optional: Related site
                privileges: privileges,
            };

            if (userMode === 'existing') {
                if (!selectedUserId) {
                    showToast('error', 'Please select a user');
                    setLoading(false);
                    return;
                }
                body.user_id = selectedUserId;
            } else {
                if (!newUsername || !newPassword) {
                    showToast('error', 'Username and password are required');
                    setLoading(false);
                    return;
                }
                body.new_username = newUsername;
                body.new_password = newPassword;
            }

            const res = await fetch(`/api/v2/database-servers/${serverId}/databases`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(body),
            });

            if (!res.ok) {
                const text = await res.text();
                throw new Error(text || 'Failed to create database');
            }

            const data = await res.json();
            showToast('success', `Database created: ${data.name}`);

            if (userMode === 'new') {
                showToast('info', `User: ${data.user}, Password: ${data.password}`);
            }

            onSuccess();
            onClose();
        } catch (err: any) {
            showToast('error', err.message);
        } finally {
            setLoading(false);
        }
    };

    return (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
            <div className="bg-gray-900 border border-gray-800 rounded-xl p-6 w-full max-w-md">
                <div className="flex justify-between items-center mb-6">
                    <div className="flex items-center gap-2">
                        <Database className="w-5 h-5 text-blue-400" />
                        <h3 className="text-xl font-bold text-gray-100">Add Database</h3>
                    </div>
                    <button onClick={onClose} className="text-gray-400 hover:text-gray-300">
                        <X className="w-5 h-5" />
                    </button>
                </div>

                <div className="mb-4 p-3 bg-gray-800/50 rounded-lg">
                    <p className="text-sm text-gray-400">Server: <span className="text-gray-200">{serverName}</span></p>
                </div>

                <form onSubmit={handleSubmit} className="space-y-4">
                    {/* Database Name */}
                    <div>
                        <label className="block text-sm font-medium text-gray-300 mb-2">
                            Database Name
                        </label>
                        <input
                            type="text"
                            value={databaseName}
                            onChange={(e) => setDatabaseName(e.target.value)}
                            className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2 text-gray-100 focus:outline-none focus:border-blue-500"
                            placeholder="myapp_db"
                            required
                            pattern="[a-zA-Z0-9_]+"
                            title="Only letters, numbers, and underscores"
                        />
                    </div>

                    {/* Related Site (Optional) */}
                    <div>
                        <label className="block text-sm font-medium text-gray-300 mb-2">
                            Related Site <span className="text-gray-500 text-xs">(Optional)</span>
                        </label>
                        <select
                            value={domainId || ''}
                            onChange={(e) => setDomainId(e.target.value ? Number(e.target.value) : null)}
                            className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2 text-gray-100 focus:outline-none focus:border-blue-500"
                        >
                            <option value="">No site (standalone database)</option>
                            {domains.map(domain => (
                                <option key={domain.id} value={domain.id}>{domain.domain_name}</option>
                            ))}
                        </select>
                        <p className="text-xs text-gray-500 mt-1">
                            💡 If site is deleted, this database will also be deleted
                        </p>
                    </div>

                    {/* User Selection */}
                    <div>
                        <label className="block text-sm font-medium text-gray-300 mb-2">
                            Database User
                        </label>
                        <div className="flex gap-2 mb-3">
                            <button
                                type="button"
                                onClick={() => setUserMode('new')}
                                className={`flex-1 px-4 py-2 rounded-lg transition-colors ${userMode === 'new'
                                    ? 'bg-blue-600 text-white'
                                    : 'bg-gray-800 text-gray-400 hover:bg-gray-700'
                                    }`}
                            >
                                Create New User
                            </button>
                            <button
                                type="button"
                                onClick={() => setUserMode('existing')}
                                className={`flex-1 px-4 py-2 rounded-lg transition-colors ${userMode === 'existing'
                                    ? 'bg-blue-600 text-white'
                                    : 'bg-gray-800 text-gray-400 hover:bg-gray-700'
                                    }`}
                                disabled={existingUsers.length === 0}
                            >
                                Use Existing User
                            </button>
                        </div>

                        {userMode === 'existing' ? (
                            <select
                                value={selectedUserId}
                                onChange={(e) => setSelectedUserId(Number(e.target.value))}
                                className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2 text-gray-100 focus:outline-none focus:border-blue-500"
                                required
                            >
                                <option value={0}>Select a user...</option>
                                {existingUsers.map(user => (
                                    <option key={user.id} value={user.id}>{user.username}</option>
                                ))}
                            </select>
                        ) : (
                            <div className="space-y-3">
                                <div>
                                    <input
                                        type="text"
                                        value={newUsername}
                                        onChange={(e) => setNewUsername(e.target.value)}
                                        className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2 text-gray-100 focus:outline-none focus:border-blue-500"
                                        placeholder="Username"
                                        required={userMode === 'new'}
                                        pattern="[a-zA-Z0-9_]+"
                                    />
                                </div>
                                <div className="flex gap-2">
                                    <input
                                        type="text"
                                        value={newPassword}
                                        onChange={(e) => setNewPassword(e.target.value)}
                                        className="flex-1 bg-gray-800 border border-gray-700 rounded-lg px-4 py-2 text-gray-100 focus:outline-none focus:border-blue-500"
                                        placeholder="Password"
                                        required={userMode === 'new'}
                                    />
                                    <button
                                        type="button"
                                        onClick={generatePassword}
                                        className="px-4 py-2 bg-gray-700 hover:bg-gray-600 text-gray-200 rounded-lg transition-colors"
                                        title="Generate password"
                                    >
                                        <Key className="w-4 h-4" />
                                    </button>
                                </div>
                            </div>
                        )}
                    </div>

                    {/* Privileges */}
                    <div>
                        <label className="block text-sm font-medium text-gray-300 mb-2">
                            Privileges
                        </label>
                        <select
                            value={privileges}
                            onChange={(e) => setPrivileges(e.target.value)}
                            className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2 text-gray-100 focus:outline-none focus:border-blue-500"
                        >
                            <option value="ALL">ALL (Full Access)</option>
                            <option value="SELECT">SELECT (Read Only)</option>
                            <option value="SELECT,INSERT,UPDATE">SELECT, INSERT, UPDATE</option>
                            <option value="SELECT,INSERT,UPDATE,DELETE">SELECT, INSERT, UPDATE, DELETE</option>
                        </select>
                    </div>

                    {/* Actions */}
                    <div className="flex gap-3 pt-4">
                        <button
                            type="button"
                            onClick={onClose}
                            className="flex-1 px-4 py-2 bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-lg transition-colors"
                        >
                            Cancel
                        </button>
                        <button
                            type="submit"
                            disabled={loading}
                            className="flex-1 px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white rounded-lg transition-colors disabled:opacity-50"
                        >
                            {loading ? 'Creating...' : 'Create Database'}
                        </button>
                    </div>
                </form>
            </div>
        </div>
    );
}
