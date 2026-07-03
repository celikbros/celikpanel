import { useState, useEffect } from 'react';
import {
    Mail, Plus, Trash2, ArrowRight, AlertCircle
} from 'lucide-react';
import { showToast } from './Toast';

interface EmailAccount {
    id: number;
    address: string;
    quota_mb: number;
    created_at: string;
}

interface Forwarding {
    id: number;
    source: string;
    destination: string;
    created_at: string;
}

interface DomainMailManagerProps {
    domainId: number;
    domainName: string;
}

export function DomainMailManager({ domainId, domainName }: DomainMailManagerProps) {
    const [accounts, setAccounts] = useState<EmailAccount[]>([]);
    const [forwardings, setForwardings] = useState<Forwarding[]>([]);
    const [loading, setLoading] = useState(true);
    const [activeTab, setActiveTab] = useState<'accounts' | 'forwarding'>('accounts');

    // Forms
    const [showAddAccount, setShowAddAccount] = useState(false);
    const [showAddForwarding, setShowAddForwarding] = useState(false);

    // Account Form
    const [newAccountUser, setNewAccountUser] = useState('');
    const [newAccountPass, setNewAccountPass] = useState('');
    const [newAccountQuota, setNewAccountQuota] = useState(1024);

    // Forwarding Form
    const [fwdSource, setFwdSource] = useState('');
    const [fwdDest, setFwdDest] = useState('');

    useEffect(() => {
        loadData();
    }, [domainId, activeTab]);

    const loadData = async () => {
        setLoading(true);
        try {
            if (activeTab === 'accounts') {
                const res = await fetch(`/api/v1/domains/${domainId}/mail/accounts`);
                if (res.ok) {
                    const data = await res.json();
                    setAccounts(data.accounts || []);
                }
            } else {
                const res = await fetch(`/api/v1/domains/${domainId}/mail/forwardings`);
                if (res.ok) {
                    const data = await res.json();
                    setForwardings(data.forwardings || []);
                }
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to load mail data');
        } finally {
            setLoading(false);
        }
    };

    if (loading && accounts.length === 0 && forwardings.length === 0) {
        return <div className="p-8 text-center text-fg-muted">Loading mail configuration...</div>;
    }

    const createAccount = async () => {
        if (!newAccountUser || !newAccountPass) return;

        try {
            const fullAddress = `${newAccountUser}@${domainName}`;
            const res = await fetch(`/api/v1/domains/${domainId}/mail/accounts`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    address: fullAddress,
                    password: newAccountPass,
                    quota_mb: newAccountQuota
                })
            });

            const data = await res.json();
            if (data.success) {
                showToast('success', 'Email account created');
                setShowAddAccount(false);
                setNewAccountUser('');
                setNewAccountPass('');
                loadData();
            } else {
                showToast('error', data.error || 'Failed to create account');
            }
        } catch (err) {
            showToast('error', 'Failed to create account');
        }
    };

    const deleteAccount = async (id: number) => {
        if (!confirm('Delete this email account?')) return;

        try {
            const res = await fetch(`/api/v1/domains/${domainId}/mail/accounts?id=${id}`, {
                method: 'DELETE'
            });
            if (res.ok) {
                showToast('success', 'Account deleted');
                loadData();
            }
        } catch (err) {
            showToast('error', 'Failed to delete account');
        }
    };

    const createForwarding = async () => {
        if (!fwdSource || !fwdDest) return;

        try {
            // Auto append domain if missing
            const source = fwdSource.includes('@') ? fwdSource : `${fwdSource}@${domainName}`;

            const res = await fetch(`/api/v1/domains/${domainId}/mail/forwardings`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    source: source,
                    destination: fwdDest
                })
            });

            const data = await res.json();
            if (data.success) {
                showToast('success', 'Forwarder created');
                setShowAddForwarding(false);
                setFwdSource('');
                setFwdDest('');
                loadData();
            } else {
                showToast('error', data.error || 'Failed to create forwarder');
            }
        } catch (err) {
            showToast('error', 'Failed to create forwarder');
        }
    };

    const deleteForwarding = async (id: number) => {
        if (!confirm('Delete this fowarder?')) return;

        try {
            const res = await fetch(`/api/v1/domains/${domainId}/mail/forwardings?id=${id}`, {
                method: 'DELETE'
            });
            if (res.ok) {
                showToast('success', 'Forwarder deleted');
                loadData();
            }
        } catch (err) {
            showToast('error', 'Failed to delete forwarder');
        }
    };

    return (
        <div className="space-y-6">
            {/* Tabs */}
            <div className="flex gap-4 border-b border-border">
                <button
                    onClick={() => setActiveTab('accounts')}
                    className={`pb-3 px-1 text-sm font-medium transition-colors ${activeTab === 'accounts'
                        ? 'text-primary border-b-2 border-primary'
                        : 'text-fg-muted hover:text-fg'
                        }`}
                >
                    Email Accounts
                </button>
                <button
                    onClick={() => setActiveTab('forwarding')}
                    className={`pb-3 px-1 text-sm font-medium transition-colors ${activeTab === 'forwarding'
                        ? 'text-primary border-b-2 border-primary'
                        : 'text-fg-muted hover:text-fg'
                        }`}
                >
                    Forwarding
                </button>
            </div>

            {/* Content */}
            {activeTab === 'accounts' ? (
                <div className="space-y-4">
                    <div className="flex justify-between items-center">
                        <h3 className="text-lg font-semibold text-fg">Email Accounts</h3>
                        <button
                            onClick={() => setShowAddAccount(true)}
                            className="flex items-center gap-2 px-3 py-1.5 bg-primary hover:bg-primary-hover rounded text-white text-sm"
                        >
                            <Plus className="w-4 h-4" />
                            Create Account
                        </button>
                    </div>

                    {showAddAccount && (
                        <div className="bg-surface-2/50 border border-border rounded-lg p-4 mb-4">
                            <h4 className="text-fg font-medium mb-3">New Email Account</h4>
                            <div className="grid grid-cols-1 md:grid-cols-3 gap-3 mb-3">
                                <div>
                                    <label className="text-xs text-fg-muted block mb-1">Username</label>
                                    <div className="flex">
                                        <input
                                            type="text"
                                            value={newAccountUser}
                                            onChange={e => setNewAccountUser(e.target.value)}
                                            className="w-full px-3 py-2 bg-surface border border-border-strong rounded-l text-fg text-sm"
                                            placeholder="info"
                                        />
                                        <span className="px-3 py-2 bg-surface-3 border border-border-strong border-l-0 rounded-r text-fg-muted text-sm">
                                            @{domainName}
                                        </span>
                                    </div>
                                </div>
                                <div>
                                    <label className="text-xs text-fg-muted block mb-1">Password</label>
                                    <input
                                        type="password"
                                        value={newAccountPass}
                                        onChange={e => setNewAccountPass(e.target.value)}
                                        className="w-full px-3 py-2 bg-surface border border-border-strong rounded text-fg text-sm"
                                        placeholder="••••••••"
                                    />
                                </div>
                                <div>
                                    <label className="text-xs text-fg-muted block mb-1">Quota (MB)</label>
                                    <input
                                        type="number"
                                        value={newAccountQuota}
                                        onChange={e => setNewAccountQuota(parseInt(e.target.value) || 0)}
                                        className="w-full px-3 py-2 bg-surface border border-border-strong rounded text-fg text-sm"
                                    />
                                </div>
                            </div>
                            <div className="flex justify-end gap-2">
                                <button
                                    onClick={() => setShowAddAccount(false)}
                                    className="px-3 py-1.5 bg-surface-3 hover:bg-surface-3 rounded text-fg text-sm"
                                >
                                    Cancel
                                </button>
                                <button
                                    onClick={createAccount}
                                    className="px-3 py-1.5 bg-primary hover:bg-primary-hover rounded text-white text-sm"
                                >
                                    Create
                                </button>
                            </div>
                        </div>
                    )}

                    <div className="bg-surface-2/50 border border-border rounded-lg overflow-hidden">
                        {accounts.length === 0 ? (
                            <div className="p-8 text-center text-fg-subtle">
                                <Mail className="w-8 h-8 mx-auto mb-2 opacity-50" />
                                <p>No email accounts found</p>
                            </div>
                        ) : (
                            <table className="w-full text-left text-sm text-fg-muted">
                                <thead className="bg-surface/50 uppercase text-xs font-semibold">
                                    <tr>
                                        <th className="px-4 py-3">Address</th>
                                        <th className="px-4 py-3">Quota</th>
                                        <th className="px-4 py-3">Created</th>
                                        <th className="px-4 py-3 text-right">Actions</th>
                                    </tr>
                                </thead>
                                <tbody className="divide-y divide-border">
                                    {accounts.map(acc => (
                                        <tr key={acc.id} className="hover:bg-surface-3/30">
                                            <td className="px-4 py-3 text-fg font-medium">{acc.address}</td>
                                            <td className="px-4 py-3">{acc.quota_mb} MB</td>
                                            <td className="px-4 py-3">{new Date(acc.created_at).toLocaleDateString()}</td>
                                            <td className="px-4 py-3 text-right">
                                                <button
                                                    onClick={() => deleteAccount(acc.id)}
                                                    className="p-1 hover:bg-surface-3 rounded text-fg-muted hover:text-danger"
                                                >
                                                    <Trash2 className="w-4 h-4" />
                                                </button>
                                            </td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                        )}
                    </div>
                </div>
            ) : (
                <div className="space-y-4">
                    <div className="flex justify-between items-center">
                        <h3 className="text-lg font-semibold text-fg">Email Forwarding</h3>
                        <button
                            onClick={() => setShowAddForwarding(true)}
                            className="flex items-center gap-2 px-3 py-1.5 bg-primary hover:bg-primary-hover rounded text-white text-sm"
                        >
                            <Plus className="w-4 h-4" />
                            Add Forwarder
                        </button>
                    </div>

                    {showAddForwarding && (
                        <div className="bg-surface-2/50 border border-border rounded-lg p-4 mb-4">
                            <h4 className="text-fg font-medium mb-3">New Forwarder</h4>
                            <div className="grid grid-cols-1 md:grid-cols-2 gap-3 mb-3">
                                <div>
                                    <label className="text-xs text-fg-muted block mb-1">Source Email</label>
                                    <input
                                        type="text"
                                        value={fwdSource}
                                        onChange={e => setFwdSource(e.target.value)}
                                        className="w-full px-3 py-2 bg-surface border border-border-strong rounded text-fg text-sm"
                                        placeholder={`info@${domainName}`}
                                    />
                                </div>
                                <div>
                                    <label className="text-xs text-fg-muted block mb-1">Destination Email</label>
                                    <input
                                        type="email"
                                        value={fwdDest}
                                        onChange={e => setFwdDest(e.target.value)}
                                        className="w-full px-3 py-2 bg-surface border border-border-strong rounded text-fg text-sm"
                                        placeholder="personal@gmail.com"
                                    />
                                </div>
                            </div>
                            <div className="flex justify-end gap-2">
                                <button
                                    onClick={() => setShowAddForwarding(false)}
                                    className="px-3 py-1.5 bg-surface-3 hover:bg-surface-3 rounded text-fg text-sm"
                                >
                                    Cancel
                                </button>
                                <button
                                    onClick={createForwarding}
                                    className="px-3 py-1.5 bg-primary hover:bg-primary-hover rounded text-white text-sm"
                                >
                                    Add
                                </button>
                            </div>
                        </div>
                    )}

                    <div className="bg-surface-2/50 border border-border rounded-lg overflow-hidden">
                        {forwardings.length === 0 ? (
                            <div className="p-8 text-center text-fg-subtle">
                                <ArrowRight className="w-8 h-8 mx-auto mb-2 opacity-50" />
                                <p>No forwarders found</p>
                            </div>
                        ) : (
                            <table className="w-full text-left text-sm text-fg-muted">
                                <thead className="bg-surface/50 uppercase text-xs font-semibold">
                                    <tr>
                                        <th className="px-4 py-3">Source</th>
                                        <th className="px-4 py-3">Destination</th>
                                        <th className="px-4 py-3 text-right">Actions</th>
                                    </tr>
                                </thead>
                                <tbody className="divide-y divide-border">
                                    {forwardings.map(fwd => (
                                        <tr key={fwd.id} className="hover:bg-surface-3/30">
                                            <td className="px-4 py-3 text-fg font-medium">{fwd.source}</td>
                                            <td className="px-4 py-3">{fwd.destination}</td>
                                            <td className="px-4 py-3 text-right">
                                                <button
                                                    onClick={() => deleteForwarding(fwd.id)}
                                                    className="p-1 hover:bg-surface-3 rounded text-fg-muted hover:text-danger"
                                                >
                                                    <Trash2 className="w-4 h-4" />
                                                </button>
                                            </td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                        )}
                    </div>
                </div>
            )}

            {/* Info */}
            <div className="flex items-start gap-3 p-4 bg-primary/10 border border-primary/30 rounded-lg">
                <AlertCircle className="w-5 h-5 text-primary flex-shrink-0 mt-0.5" />
                <div className="text-sm text-fg-muted">
                    <p className="font-medium text-primary">Mail Configuration</p>
                    <p className="mt-1">
                        IMAP/SMTP Server: <code className="text-primary">mail.{domainName}</code> (or server IP)<br />
                        Username: Full email address<br />
                        Ports: IMAP 143/993, SMTP 25/465/587
                    </p>
                </div>
            </div>
        </div>
    );
}
