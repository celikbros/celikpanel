import { useState } from 'react';
import { X, User, Key } from 'lucide-react';
import { showToast } from './Toast';
import { readApiError } from '../lib/apiError';

interface AddUserModalV2Props {
    serverId: number;
    serverName: string;
    onClose: () => void;
    onSuccess: () => void;
}

export function AddUserModalV2({ serverId, serverName, onClose, onSuccess }: AddUserModalV2Props) {
    const [username, setUsername] = useState('');
    const [password, setPassword] = useState('');
    const [loading, setLoading] = useState(false);

    const generatePassword = () => {
        const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789!@#$%^&*';
        let pwd = '';
        for (let i = 0; i < 16; i++) {
            pwd += chars.charAt(Math.floor(Math.random() * chars.length));
        }
        setPassword(pwd);
        navigator.clipboard.writeText(pwd);
        showToast('success', 'Password generated and copied to clipboard');
    };

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        setLoading(true);

        try {
            const res = await fetch(`/api/v1/database-servers/${serverId}/users`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ username, password }),
            });

            if (!res.ok) {
                throw new Error((await readApiError(res)).message || 'Failed to create user');
            }

            const data = await res.json();
            showToast('success', `User created: ${data.username}`);
            showToast('info', `Password: ${data.password}`);

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
            <div className="bg-surface border border-border rounded-xl p-6 w-full max-w-md">
                <div className="flex justify-between items-center mb-6">
                    <div className="flex items-center gap-2">
                        <User className="w-5 h-5 text-primary" />
                        <h3 className="text-xl font-bold text-fg">Add User</h3>
                    </div>
                    <button onClick={onClose} className="text-fg-muted hover:text-fg-muted">
                        <X className="w-5 h-5" />
                    </button>
                </div>

                <div className="mb-4 p-3 bg-surface-2/50 rounded-lg">
                    <p className="text-sm text-fg-muted">Server: <span className="text-fg">{serverName}</span></p>
                </div>

                <form onSubmit={handleSubmit} className="space-y-4">
                    {/* Username */}
                    <div>
                        <label className="block text-sm font-medium text-fg-muted mb-2">
                            Username
                        </label>
                        <input
                            type="text"
                            value={username}
                            onChange={(e) => setUsername(e.target.value)}
                            className="w-full bg-surface-2 border border-border rounded-lg px-4 py-2 text-fg focus:outline-none focus:border-primary"
                            placeholder="myapp_user"
                            required
                            pattern="[a-zA-Z0-9_]+"
                            title="Only letters, numbers, and underscores"
                        />
                        <p className="text-xs text-fg-muted mt-1">
                            The account is created with this subscription's prefix; its full name is shown once it exists.
                        </p>
                    </div>

                    {/* Password */}
                    <div>
                        <label className="block text-sm font-medium text-fg-muted mb-2">
                            Password
                        </label>
                        <div className="flex gap-2">
                            <input
                                type="text"
                                value={password}
                                onChange={(e) => setPassword(e.target.value)}
                                className="flex-1 bg-surface-2 border border-border rounded-lg px-4 py-2 text-fg focus:outline-none focus:border-primary"
                                placeholder="Password"
                                required
                            />
                            <button
                                type="button"
                                onClick={generatePassword}
                                className="px-4 py-2 bg-surface-3 hover:bg-surface-3 text-fg rounded-lg transition-colors flex items-center gap-2"
                                title="Generate password"
                            >
                                <Key className="w-4 h-4" />
                                Generate
                            </button>
                        </div>
                        <p className="text-xs text-fg-subtle mt-1">
                            Password will be copied to clipboard
                        </p>
                    </div>

                    {/* Info */}
                    <div className="p-3 bg-primary/10 border border-primary/20 rounded-lg">
                        <p className="text-sm text-primary">
                            💡 After creating the user, you can grant it access to databases from the Databases tab
                        </p>
                    </div>

                    {/* Actions */}
                    <div className="flex gap-3 pt-4">
                        <button
                            type="button"
                            onClick={onClose}
                            className="flex-1 px-4 py-2 bg-surface-2 hover:bg-surface-3 text-fg-muted rounded-lg transition-colors"
                        >
                            Cancel
                        </button>
                        <button
                            type="submit"
                            disabled={loading}
                            className="flex-1 px-4 py-2 bg-primary hover:bg-primary-hover text-white rounded-lg transition-colors disabled:opacity-50"
                        >
                            {loading ? 'Creating...' : 'Create User'}
                        </button>
                    </div>
                </form>
            </div>
        </div>
    );
}
