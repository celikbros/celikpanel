import { useState, useEffect } from 'react';
import { Database, Key } from 'lucide-react';
import { showToast } from './Toast';
import { Button, Dialog, ResultUnknown } from './ui';
import { apiErrorText, readApiError } from '../lib/apiError';
import { useI18n } from '../i18n';
import type { LostAnswerHandle } from '../lib/lostAnswer';
import { resultNotKept } from './OnceOnlyNotice';

interface AddDatabaseModalV2Props {
    serverId: number;
    serverName: string;
    onClose: () => void;
    onSuccess: () => void;
    existingUsers: Array<{ id: number; username: string }>;
    /** The page's lost-answer handle: its re-read is the page's own lists. */
    answer: LostAnswerHandle;
    /** The database was made; the answer with its new user's password is not kept. */
    onPasswordNotShown: (name: string, user: string) => void;
}

export function AddDatabaseModalV2({ serverId, serverName, onClose, onSuccess, existingUsers, answer, onPasswordNotShown }: AddDatabaseModalV2Props) {
    const { t } = useI18n();
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
        // Nothing is created while an earlier create has no result and the
        // lists have not been read again.
        if (loading || answer.holding) return;
        const name = databaseName;
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

            // The request carries an identity the server keeps (D-029), and a
            // lost answer has been asked for once more before `send` gives up.
            // When there is still no result, the page's lists are read again
            // and asked whether they name this database: if they do the dialog
            // closes and the notice on the page says it was made; if not, the
            // dialog stays with what was typed. A dropped connection used to
            // be shown as the browser's own "Failed to fetch".
            // İstek, sunucunun sakladığı bir kimlik taşır (D-029). Sonuç yine
            // yoksa sayfanın listeleri yeniden okunur ve bu veritabanını
            // adlandırıp adlandırmadıkları sorulur.
            const res = await answer.send(`/api/v1/database-servers/${serverId}/databases`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(body),
            }, {
                shows: ([databases]) => {
                    const rows = databases?.value;
                    return Array.isArray(rows) ? rows.some((row) => (row as { name?: unknown })?.name === name) : null;
                },
                // The lists show the database, so it was made. With a new
                // user, the answer that carried its password never arrived
                // either: the page says that, instead of only "it was made".
                // Listeler veritabanını gösteriyor, yani yapıldı. Yeni
                // kullanıcıda parolayı taşıyan yanıt da ulaşmadı: sayfa bunu
                // söyler.
                made: () => {
                    if (userMode !== 'new') { onClose(); return; }
                    answer.dismiss();
                    onPasswordNotShown(name, newUsername);
                },
            });
            if (!res) return;
            answer.settle();

            if (!res.ok) {
                const problem = await readApiError(res);
                // The answer was lost and asked for again: the database was
                // made, and the answer that carried its new user's password is
                // not kept. The page says so, with what to do.
                // Yanıt kayboldu ve yeniden soruldu: veritabanı yapıldı; yeni
                // kullanıcısının parolasını taşıyan yanıt saklanmıyor.
                if (resultNotKept(problem)) {
                    onPasswordNotShown(name, userMode === 'new' ? newUsername : '');
                    return;
                }
                throw new Error(apiErrorText(problem, t, 'common.error'));
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
        <Dialog
            id="add-database"
            icon={Database}
            title="Add Database"
            description={`Server: ${serverName}`}
            busy={loading}
            onDismiss={onClose}
            onSubmit={handleSubmit}
            actions={
                <>
                    <Button type="button" variant="secondary" onClick={onClose}>
                        Cancel
                    </Button>
                    <Button type="submit" variant="primary" disabled={loading || answer.holding}>
                        {loading ? 'Creating...' : 'Create Database'}
                    </Button>
                </>
            }
        >
            <div className="space-y-4">
                <ResultUnknown answer={answer} />
                {/* Database Name */}
                <div>
                    <label className="block text-sm font-medium text-fg-muted mb-2">
                        Database Name
                    </label>
                    <input
                        type="text"
                        value={databaseName}
                        onChange={(e) => setDatabaseName(e.target.value)}
                        className="w-full bg-surface-2 border border-border rounded-lg px-4 py-2 text-fg focus:border-primary"
                        placeholder="myapp_db"
                        required
                        pattern="[a-zA-Z0-9_]+"
                        title="Only letters, numbers, and underscores"
                    />
                </div>

                {/* Related Site (Optional) */}
                <div>
                    <label className="block text-sm font-medium text-fg-muted mb-2">
                        Related Site <span className="text-fg-subtle text-xs">(Optional)</span>
                    </label>
                    <select
                        value={domainId || ''}
                        onChange={(e) => setDomainId(e.target.value ? Number(e.target.value) : null)}
                        className="w-full bg-surface-2 border border-border rounded-lg px-4 py-2 text-fg focus:border-primary"
                    >
                        <option value="">No site (standalone database)</option>
                        {domains.map(domain => (
                            <option key={domain.id} value={domain.id}>{domain.domain_name}</option>
                        ))}
                    </select>
                    <p className="text-xs text-fg-subtle mt-1">
                        💡 If site is deleted, this database will also be deleted
                    </p>
                </div>

                {/* User Selection */}
                <div>
                    <label className="block text-sm font-medium text-fg-muted mb-2">
                        Database User
                    </label>
                    <div className="flex gap-2 mb-3">
                        <button
                            type="button"
                            onClick={() => setUserMode('new')}
                            className={`flex-1 px-4 py-2 rounded-lg transition-colors ${userMode === 'new'
                                ? 'bg-primary text-white'
                                : 'bg-surface-2 text-fg-muted hover:bg-surface-3'
                                }`}
                        >
                            Create New User
                        </button>
                        <button
                            type="button"
                            onClick={() => setUserMode('existing')}
                            className={`flex-1 px-4 py-2 rounded-lg transition-colors ${userMode === 'existing'
                                ? 'bg-primary text-white'
                                : 'bg-surface-2 text-fg-muted hover:bg-surface-3'
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
                            className="w-full bg-surface-2 border border-border rounded-lg px-4 py-2 text-fg focus:border-primary"
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
                                    className="w-full bg-surface-2 border border-border rounded-lg px-4 py-2 text-fg focus:border-primary"
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
                                    className="flex-1 bg-surface-2 border border-border rounded-lg px-4 py-2 text-fg focus:border-primary"
                                    placeholder="Password"
                                    required={userMode === 'new'}
                                />
                                <button
                                    type="button"
                                    onClick={generatePassword}
                                    className="px-4 py-2 bg-surface-3 hover:bg-surface-3 text-fg rounded-lg transition-colors"
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
                    <label className="block text-sm font-medium text-fg-muted mb-2">
                        Privileges
                    </label>
                    <select
                        value={privileges}
                        onChange={(e) => setPrivileges(e.target.value)}
                        className="w-full bg-surface-2 border border-border rounded-lg px-4 py-2 text-fg focus:border-primary"
                    >
                        <option value="ALL">ALL (Full Access)</option>
                        <option value="SELECT">SELECT (Read Only)</option>
                        <option value="SELECT,INSERT,UPDATE">SELECT, INSERT, UPDATE</option>
                        <option value="SELECT,INSERT,UPDATE,DELETE">SELECT, INSERT, UPDATE, DELETE</option>
                    </select>
                </div>
            </div>
        </Dialog>
    );
}
