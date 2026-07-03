import { useState } from 'react';
import { X, User, Key } from 'lucide-react';
import { showToast } from './Toast';

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
            const res = await fetch(`/api/v2/database-servers/${serverId}/users`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ username, password }),
            });

            if (!res.ok) {
                const text = await res.text();
                throw new Error(text || 'Failed to create user');
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
            <div className="bg-gray-900 border border-gray-800 rounded-xl p-6 w-full max-w-md">
                <div className="flex justify-between items-center mb-6">
                    <div className="flex items-center gap-2">
                        <User className="w-5 h-5 text-blue-400" />
                        <h3 className="text-xl font-bold text-gray-100">Add User</h3>
                    </div>
                    <button onClick={onClose} className="text-gray-400 hover:text-gray-300">
                        <X className="w-5 h-5" />
                    </button>
                </div>

                <div className="mb-4 p-3 bg-gray-800/50 rounded-lg">
                    <p className="text-sm text-gray-400">Server: <span className="text-gray-200">{serverName}</span></p>
                </div>

                <form onSubmit={handleSubmit} className="space-y-4">
                    {/* Username */}
                    <div>
                        <label className="block text-sm font-medium text-gray-300 mb-2">
                            Username
                        </label>
                        <input
                            type="text"
                            value={username}
                            onChange={(e) => setUsername(e.target.value)}
                            className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2 text-gray-100 focus:outline-none focus:border-blue-500"
                            placeholder="myapp_user"
                            required
                            pattern="[a-zA-Z0-9_]+"
                            title="Only letters, numbers, and underscores"
                        />
                        <p className="text-xs text-gray-500 mt-1">
                            User will be created as: 1_{username}
                        </p>
                    </div>

                    {/* Password */}
                    <div>
                        <label className="block text-sm font-medium text-gray-300 mb-2">
                            Password
                        </label>
                        <div className="flex gap-2">
                            <input
                                type="text"
                                value={password}
                                onChange={(e) => setPassword(e.target.value)}
                                className="flex-1 bg-gray-800 border border-gray-700 rounded-lg px-4 py-2 text-gray-100 focus:outline-none focus:border-blue-500"
                                placeholder="Password"
                                required
                            />
                            <button
                                type="button"
                                onClick={generatePassword}
                                className="px-4 py-2 bg-gray-700 hover:bg-gray-600 text-gray-200 rounded-lg transition-colors flex items-center gap-2"
                                title="Generate password"
                            >
                                <Key className="w-4 h-4" />
                                Generate
                            </button>
                        </div>
                        <p className="text-xs text-gray-500 mt-1">
                            Password will be copied to clipboard
                        </p>
                    </div>

                    {/* Info */}
                    <div className="p-3 bg-blue-500/10 border border-blue-500/20 rounded-lg">
                        <p className="text-sm text-blue-300">
                            💡 After creating the user, you can grant it access to databases from the Databases tab
                        </p>
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
                            {loading ? 'Creating...' : 'Create User'}
                        </button>
                    </div>
                </form>
            </div>
        </div>
    );
}
