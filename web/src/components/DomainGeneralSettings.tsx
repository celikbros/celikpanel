import { useState, useEffect } from 'react';
import { Save, Plus, Trash2, Globe } from 'lucide-react';
import { showToast } from './Toast';

interface DomainGeneralSettingsProps {
    domainId: number;
    domainName: string;
}

interface GeneralSettings {
    domain_id: number;
    domain_name: string;
    document_root: string;
    web_server: string;
    redirect_www: boolean;
    redirect_https: boolean;
    aliases: string[];
}

export function DomainGeneralSettings({ domainId, domainName }: DomainGeneralSettingsProps) {
    const [settings, setSettings] = useState<GeneralSettings | null>(null);
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [newAlias, setNewAlias] = useState('');
    const [availableWebServers, setAvailableWebServers] = useState<string[]>(['nginx']); // Default to nginx

    useEffect(() => {
        loadSettings();
        loadAvailableWebServers();
    }, [domainId]);

    const loadAvailableWebServers = async () => {
        try {
            // Check which web servers are installed
            const res = await fetch('/api/v1/system/check');
            if (res.ok) {
                const data = await res.json();
                const servers: string[] = [];
                if (data.nginx) servers.push('nginx');
                if (data.apache) servers.push('apache');
                setAvailableWebServers(servers.length > 0 ? servers : ['nginx']); // Fallback to nginx
            }
        } catch (err) {
            console.error('Failed to check installed services:', err);
            // Keep default nginx
        }
    };

    const loadSettings = async () => {
        setLoading(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/general`);
            if (res.ok) {
                const data = await res.json();
                setSettings(data);
            } else {
                showToast('error', 'Failed to load settings');
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to load settings');
        } finally {
            setLoading(false);
        }
    };

    const handleSaveSettings = async (e: React.FormEvent<HTMLFormElement>) => {
        e.preventDefault();
        if (!settings) return;

        const formData = new FormData(e.currentTarget);
        const updates = {
            document_root: formData.get('document_root') as string,
            web_server: formData.get('web_server') as string,
            redirect_www: formData.get('redirect_www') === 'on',
            redirect_https: formData.get('redirect_https') === 'on',
        };

        setSaving(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/general`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(updates)
            });

            if (res.ok) {
                showToast('success', 'Settings updated successfully');
                loadSettings();
            } else {
                showToast('error', 'Failed to update settings');
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to update settings');
        } finally {
            setSaving(false);
        }
    };

    const handleAddAlias = async () => {
        if (!newAlias.trim()) {
            showToast('error', 'Please enter an alias');
            return;
        }

        try {
            const res = await fetch(`/api/v1/domains/${domainId}/aliases`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ alias: newAlias })
            });

            if (res.ok) {
                showToast('success', `Alias "${newAlias}" added`);
                setNewAlias('');
                loadSettings();
            } else {
                const error = await res.text();
                showToast('error', error || 'Failed to add alias');
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to add alias');
        }
    };

    const handleDeleteAlias = async (alias: string) => {
        if (!confirm(`Delete alias "${alias}"?`)) return;

        try {
            const res = await fetch(`/api/v1/domains/${domainId}/aliases/${alias}`, {
                method: 'DELETE'
            });

            if (res.ok) {
                showToast('success', `Alias "${alias}" deleted`);
                loadSettings();
            } else {
                showToast('error', 'Failed to delete alias');
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to delete alias');
        }
    };

    if (loading) {
        return (
            <div className="flex items-center justify-center h-64">
                <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-500"></div>
            </div>
        );
    }

    if (!settings) {
        return <div className="text-red-400">Failed to load settings</div>;
    }

    return (
        <div className="space-y-6">
            <div>
                <h3 className="text-lg font-bold text-gray-100 mb-2">General Settings</h3>
                <p className="text-sm text-gray-400">
                    Configure basic domain settings for {domainName}
                </p>
            </div>

            {/* Main Settings Form */}
            <form onSubmit={handleSaveSettings} className="space-y-6">
                {/* Document Root */}
                <div className="bg-gray-800/50 rounded-lg p-6 border border-gray-700">
                    <h4 className="text-md font-semibold text-gray-200 mb-4">Document Root</h4>
                    <input
                        type="text"
                        name="document_root"
                        defaultValue={settings.document_root}
                        className="w-full bg-gray-900 border border-gray-700 rounded px-4 py-2 text-white focus:border-blue-500 focus:outline-none font-mono text-sm"
                        placeholder="/var/www/html"
                    />
                    <p className="text-xs text-gray-400 mt-2">
                        The directory where your website files are located
                    </p>
                </div>

                {/* Web Server */}
                <div className="bg-gray-800/50 rounded-lg p-6 border border-gray-700">
                    <h4 className="text-md font-semibold text-gray-200 mb-4">Web Server</h4>
                    {availableWebServers.length > 1 ? (
                        <select
                            name="web_server"
                            defaultValue={settings.web_server}
                            className="w-full bg-gray-900 border border-gray-700 rounded px-4 py-2 text-white focus:border-blue-500 focus:outline-none"
                        >
                            {availableWebServers.map(server => (
                                <option key={server} value={server}>
                                    {server.charAt(0).toUpperCase() + server.slice(1)}
                                </option>
                            ))}
                        </select>
                    ) : (
                        <div className="text-white">
                            {availableWebServers[0]?.charAt(0).toUpperCase() + availableWebServers[0]?.slice(1) || 'nginx'}
                            <input type="hidden" name="web_server" value={availableWebServers[0] || 'nginx'} />
                        </div>
                    )}
                    <p className="text-xs text-gray-400 mt-2">
                        {availableWebServers.length > 1
                            ? 'Select the web server for this domain'
                            : 'Only one web server is installed'}
                    </p>
                </div>

                {/* Redirects */}
                <div className="bg-gray-800/50 rounded-lg p-6 border border-gray-700">
                    <h4 className="text-md font-semibold text-gray-200 mb-4">Redirects</h4>
                    <div className="space-y-3">
                        <label className="flex items-center gap-3 cursor-pointer">
                            <input
                                type="checkbox"
                                name="redirect_www"
                                defaultChecked={settings.redirect_www}
                                className="w-4 h-4 bg-gray-900 border-gray-700 rounded focus:ring-blue-500"
                            />
                            <div>
                                <div className="text-white text-sm">Redirect to www</div>
                                <div className="text-xs text-gray-400">
                                    Redirect {domainName} → www.{domainName}
                                </div>
                            </div>
                        </label>
                        <label className="flex items-center gap-3 cursor-pointer">
                            <input
                                type="checkbox"
                                name="redirect_https"
                                defaultChecked={settings.redirect_https}
                                className="w-4 h-4 bg-gray-900 border-gray-700 rounded focus:ring-blue-500"
                            />
                            <div>
                                <div className="text-white text-sm">Force HTTPS</div>
                                <div className="text-xs text-gray-400">
                                    Redirect HTTP → HTTPS
                                </div>
                            </div>
                        </label>
                    </div>
                </div>

                {/* Save Button */}
                <div className="flex justify-end">
                    <button
                        type="submit"
                        disabled={saving}
                        className="px-6 py-2 bg-blue-600 text-white rounded hover:bg-blue-700 disabled:opacity-50 flex items-center gap-2"
                    >
                        <Save className="w-4 h-4" />
                        {saving ? 'Saving...' : 'Save Settings'}
                    </button>
                </div>
            </form>

            {/* Domain Aliases */}
            <div className="bg-gray-800/50 rounded-lg p-6 border border-gray-700">
                <h4 className="text-md font-semibold text-gray-200 mb-4">Domain Aliases</h4>
                <p className="text-sm text-gray-400 mb-4">
                    Additional domains that point to this website
                </p>

                {/* Add Alias */}
                <div className="flex gap-2 mb-4">
                    <input
                        type="text"
                        value={newAlias}
                        onChange={(e) => setNewAlias(e.target.value)}
                        onKeyPress={(e) => e.key === 'Enter' && handleAddAlias()}
                        placeholder="alias.example.com"
                        className="flex-1 bg-gray-900 border border-gray-700 rounded px-4 py-2 text-white focus:border-blue-500 focus:outline-none"
                    />
                    <button
                        onClick={handleAddAlias}
                        className="px-4 py-2 bg-green-600 text-white rounded hover:bg-green-700 flex items-center gap-2"
                    >
                        <Plus className="w-4 h-4" />
                        Add
                    </button>
                </div>

                {/* Aliases List */}
                {settings.aliases && settings.aliases.length > 0 ? (
                    <div className="space-y-2">
                        {settings.aliases.map((alias) => (
                            <div
                                key={alias}
                                className="flex items-center justify-between bg-gray-900 border border-gray-700 rounded px-4 py-3"
                            >
                                <div className="flex items-center gap-2">
                                    <Globe className="w-4 h-4 text-blue-400" />
                                    <span className="text-white font-mono text-sm">{alias}</span>
                                </div>
                                <button
                                    onClick={() => handleDeleteAlias(alias)}
                                    className="p-2 text-red-400 hover:bg-red-900/30 rounded transition-colors"
                                >
                                    <Trash2 className="w-4 h-4" />
                                </button>
                            </div>
                        ))}
                    </div>
                ) : (
                    <p className="text-sm text-gray-500 text-center py-4">
                        No aliases configured
                    </p>
                )}
            </div>
        </div>
    );
}
