import { useState } from 'react';
import { User, Key } from 'lucide-react';
import { showToast } from './Toast';
import { Button, Dialog } from './ui';
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
        <Dialog
            id="add-database-user"
            icon={User}
            title="Add User"
            description={`Server: ${serverName}`}
            busy={loading}
            onDismiss={onClose}
            onSubmit={handleSubmit}
            actions={
                <>
                    <Button type="button" variant="secondary" onClick={onClose}>
                        Cancel
                    </Button>
                    <Button type="submit" variant="primary" disabled={loading}>
                        {loading ? 'Creating...' : 'Create User'}
                    </Button>
                </>
            }
        >
            <div className="space-y-4">
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
            </div>
        </Dialog>
    );
}
