import { useState } from 'react';
import { Database, X, RefreshCw } from 'lucide-react';
import { showToast } from './Toast';
import { readApiError } from '../lib/apiError';

interface AddDatabaseModalProps {
    onClose: () => void;
    onSuccess: () => void;
}

const API_BASE = '/api/v1';

export function AddDatabaseModal({ onClose, onSuccess }: AddDatabaseModalProps) {
    const [databaseName, setDatabaseName] = useState('');
    const [databaseType, setDatabaseType] = useState('postgresql'); // Default to PostgreSQL
    const [username, setUsername] = useState('');
    const [password, setPassword] = useState('');
    const [showPassword, setShowPassword] = useState(false);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);

    const generatePassword = () => {
        const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz23456789!@#$%^&*';
        let password = '';
        for (let i = 0; i < 16; i++) {
            password += chars.charAt(Math.floor(Math.random() * chars.length));
        }
        setPassword(password);
        setShowPassword(true);
    };

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        setLoading(true);
        setError(null);

        try {
            const res = await fetch(`${API_BASE}/databases/create`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    database_name: databaseName,
                    database_type: databaseType,
                    username: username,
                    password: password || undefined,
                }),
            });

            if (!res.ok) {
                throw new Error((await readApiError(res)).message || 'Failed to create database');
            }

            const data = await res.json();
            showToast('success', `Database ${databaseName} created successfully!`);

            // Show credentials
            const credentials = `Database: ${data.database_name}\nUser: ${data.db_user}\nPassword: ${data.db_password}\nHost: ${data.host}:${data.port}`;
            navigator.clipboard.writeText(credentials);
            showToast('info', 'Credentials copied to clipboard!');

            onSuccess();
        } catch (err: any) {
            setError(err.message);
            showToast('error', err.message);
        } finally {
            setLoading(false);
        }
    };

    return (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
            <div className="bg-surface border border-border rounded-xl p-8 max-w-2xl w-full mx-4">
                <div className="flex justify-between items-center mb-6">
                    <div className="flex items-center gap-3">
                        <div className="p-2 bg-primary/10 rounded-lg">
                            <Database className="w-6 h-6 text-primary" />
                        </div>
                        <div>
                            <h2 className="text-xl font-bold text-fg">Add Database</h2>
                            <p className="text-sm text-fg-subtle">Create a new database (PostgreSQL or MariaDB)</p>
                        </div>
                    </div>
                    <button
                        onClick={onClose}
                        className="p-2 hover:bg-surface-2 rounded-lg transition-colors"
                    >
                        <X className="w-5 h-5 text-fg-muted" />
                    </button>
                </div>

                {error && (
                    <div className="mb-6 p-4 bg-danger/10 border border-danger/20 rounded-lg text-danger text-sm">
                        {error}
                    </div>
                )}

                <form onSubmit={handleSubmit} className="space-y-4">
                    <div>
                        <label className="block text-sm font-medium text-fg-muted mb-2">
                            Database Name
                        </label>
                        <input
                            type="text"
                            value={databaseName}
                            onChange={(e) => setDatabaseName(e.target.value.replace(/[^a-z0-9_]/gi, ''))}
                            className="w-full bg-surface-2 border border-border rounded-lg px-4 py-3 text-fg focus:outline-none focus:border-primary"
                            placeholder="myapp_db"
                            required
                            maxLength={32}
                        />
                        <p className="text-xs text-fg-subtle mt-1">Alphanumeric and underscore only (max 32 chars)</p>
                    </div>

                    <div>
                        <label className="block text-sm font-medium text-fg-muted mb-2">
                            Database Type
                        </label>
                        <select
                            value={databaseType}
                            onChange={(e) => setDatabaseType(e.target.value)}
                            className="w-full bg-surface-2 border border-border rounded-lg px-4 py-3 text-fg focus:outline-none focus:border-primary"
                        >
                            <option value="postgresql">PostgreSQL (Default)</option>
                            <option value="mariadb">MariaDB</option>
                        </select>
                        <p className="text-xs text-fg-subtle mt-1">
                            {databaseType === 'postgresql' ? '🐘 Port 5432' : '🐬 Port 3306'}
                        </p>
                    </div>

                    <div>
                        <label className="block text-sm font-medium text-fg-muted mb-2">
                            Username
                        </label>
                        <input
                            type="text"
                            value={username}
                            onChange={(e) => setUsername(e.target.value.replace(/[^a-z0-9_]/gi, ''))}
                            className="w-full bg-surface-2 border border-border rounded-lg px-4 py-3 text-fg focus:outline-none focus:border-primary"
                            placeholder="myapp_user"
                            required
                            maxLength={32}
                        />
                        <p className="text-xs text-fg-subtle mt-1">Alphanumeric and underscore only (max 32 chars)</p>
                    </div>

                    <div>
                        <label className="block text-sm font-medium text-fg-muted mb-2">
                            Password
                        </label>
                        <div className="flex gap-2">
                            <input
                                type={showPassword ? "text" : "password"}
                                value={password}
                                onChange={(e) => setPassword(e.target.value)}
                                className="flex-1 bg-surface-2 border border-border rounded-lg px-4 py-3 text-fg focus:outline-none focus:border-primary font-mono"
                                placeholder="Leave empty to auto-generate"
                            />
                            <button
                                type="button"
                                onClick={generatePassword}
                                className="px-4 py-3 bg-surface-2 hover:bg-surface-3 border border-border text-fg-muted rounded-lg transition-colors flex items-center gap-2"
                                title="Generate password"
                            >
                                <RefreshCw className="w-4 h-4" />
                                Generate
                            </button>
                        </div>
                        <div className="flex items-center gap-2 mt-2">
                            <input
                                type="checkbox"
                                id="showPassword"
                                checked={showPassword}
                                onChange={(e) => setShowPassword(e.target.checked)}
                            />
                            <label htmlFor="showPassword" className="text-xs text-fg-subtle cursor-pointer">
                                Show password
                            </label>
                        </div>
                    </div>

                    <div className="bg-primary/10 border border-primary/20 rounded-lg p-4">
                        <p className="text-sm text-primary">
                            💡 <strong>Tip:</strong> Credentials will be copied to clipboard after creation
                        </p>
                    </div>

                    <div className="flex gap-3 pt-4">
                        <button
                            type="submit"
                            disabled={loading}
                            className="flex-1 bg-primary hover:bg-primary-hover disabled:bg-surface-3 text-white px-6 py-3 rounded-lg transition-colors font-medium"
                        >
                            {loading ? 'Creating...' : 'Create Database'}
                        </button>
                        <button
                            type="button"
                            onClick={onClose}
                            className="px-6 py-3 bg-surface-2 hover:bg-surface-3 text-fg-muted rounded-lg transition-colors"
                        >
                            Cancel
                        </button>
                    </div>
                </form>
            </div>
        </div>
    );
}
