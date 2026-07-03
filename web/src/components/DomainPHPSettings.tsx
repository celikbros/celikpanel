import { useState, useEffect } from 'react';
import { Save } from 'lucide-react';
import { showToast } from './Toast';

interface DomainPHPSettingsProps {
    domainId: number;
    domainName: string;
    currentVersion: string;
    onVersionChange: (version: string) => void;
}

interface PHPSettings {
    domain_id: number;
    domain_name: string;
    php_version: string;
    pool_name: string;
    pool_config?: any;
}

export function DomainPHPSettings({ domainId, domainName, currentVersion, onVersionChange }: DomainPHPSettingsProps) {
    const [settings, setSettings] = useState<PHPSettings | null>(null);
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [selectedVersion, setSelectedVersion] = useState(currentVersion);
    const [availableVersions, setAvailableVersions] = useState<string[]>(['8.3']);

    useEffect(() => {
        loadSettings();
        loadAvailableVersions();
    }, [domainId]);

    const loadAvailableVersions = async () => {
        try {
            const res = await fetch('/api/v1/managed-services');
            if (res.ok) {
                const services = await res.json();
                const phpServices = services.filter((s: any) => s.id === 'php-fpm');
                if (phpServices.length > 0 && phpServices[0].versions) {
                    setAvailableVersions(phpServices[0].versions);
                }
            }
        } catch (err) {
            console.error('Failed to load PHP versions:', err);
        }
    };

    const loadSettings = async () => {
        setLoading(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/php`);
            if (res.ok) {
                const data = await res.json();
                setSettings(data);
                setSelectedVersion(data.php_version);
            } else {
                showToast('error', 'Failed to load PHP settings');
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to load PHP settings');
        } finally {
            setLoading(false);
        }
    };

    const handleVersionChange = async () => {
        if (selectedVersion === currentVersion) {
            showToast('info', 'No changes to save');
            return;
        }

        if (!confirm(`Change PHP version from ${currentVersion} to ${selectedVersion}?\n\nThis will reload PHP-FPM and may cause brief downtime.`)) {
            return;
        }

        setSaving(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/php`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ php_version: selectedVersion })
            });

            if (res.ok) {
                showToast('success', `PHP version changed to ${selectedVersion}`);
                onVersionChange(selectedVersion);

                // Reload settings to get updated pool info and current version
                await loadSettings();
            } else {
                const error = await res.text();
                showToast('error', `Failed to change PHP version: ${error}`);
                // Reset selected version on error
                setSelectedVersion(currentVersion);
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to change PHP version');
            // Reset selected version on error
            setSelectedVersion(currentVersion);
        } finally {
            setSaving(false);
        }
    };

    if (loading) {
        return (
            <div className="flex items-center justify-center h-64">
                <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
            </div>
        );
    }

    if (!settings) {
        return <div className="text-danger">Failed to load PHP settings</div>;
    }

    return (
        <div className="space-y-6">
            <div>
                <h3 className="text-lg font-bold text-fg mb-4">PHP Configuration</h3>
                <p className="text-sm text-fg-muted mb-6">
                    Manage PHP version and FPM pool settings for {domainName}
                </p>
            </div>

            {/* PHP Version Selector */}
            <div className="bg-surface-2/50 rounded-lg p-6 border border-border">
                <h4 className="text-md font-semibold text-fg mb-4">PHP Version</h4>
                <div className="flex items-center gap-4">
                    <select
                        value={selectedVersion}
                        onChange={(e) => setSelectedVersion(e.target.value)}
                        className="flex-1 bg-surface border border-border rounded px-4 py-2 text-fg focus:border-primary focus:outline-none"
                    >
                        {availableVersions.map(version => (
                            <option key={version} value={version}>PHP {version}</option>
                        ))}
                    </select>
                    <button
                        onClick={handleVersionChange}
                        disabled={saving || selectedVersion === currentVersion}
                        className="px-4 py-2 bg-primary text-white rounded hover:bg-primary-hover disabled:opacity-50 disabled:cursor-not-allowed flex items-center gap-2"
                    >
                        <Save className="w-4 h-4" />
                        {saving ? 'Saving...' : 'Apply'}
                    </button>
                </div>
                {selectedVersion !== currentVersion && (
                    <p className="text-sm text-warning mt-2">
                        ⚠️ Changing PHP version will reload PHP-FPM service
                    </p>
                )}
            </div>

            {/* Pool Configuration */}
            <div className="bg-surface-2/50 rounded-lg p-6 border border-border">
                <div className="mb-4">
                    <h4 className="text-md font-semibold text-fg">FPM Pool Configuration</h4>
                    <p className="text-sm text-fg-muted mt-1">
                        Pool: <span className="font-mono text-primary">{settings.pool_name}</span>
                    </p>
                </div>

                {/* Show warning when version change is pending */}
                {selectedVersion !== currentVersion ? (
                    <div className="bg-warning/15/20 border border-warning/50 rounded-lg p-4 text-center">
                        <p className="text-warning text-sm">
                            ⚠️ PHP version değişikliğini önce uygulayın
                        </p>
                        <p className="text-fg-muted text-xs mt-1">
                            Pool configuration, version değişikliği uygulandıktan sonra görünecek
                        </p>
                    </div>
                ) : settings.pool_config ? (
                    <form onSubmit={async (e) => {
                        e.preventDefault();
                        const formData = new FormData(e.currentTarget);
                        const poolConfig = {
                            pm: formData.get('pm') as string,
                            pm_max_children: parseInt(formData.get('pm_max_children') as string),
                            pm_start_servers: parseInt(formData.get('pm_start_servers') as string),
                            pm_min_spare_servers: parseInt(formData.get('pm_min_spare_servers') as string),
                            pm_max_spare_servers: parseInt(formData.get('pm_max_spare_servers') as string),
                            user: formData.get('user') as string,
                            group: formData.get('group') as string,
                        };

                        try {
                            const res = await fetch(`/api/v1/domains/${domainId}/php/pool`, {
                                method: 'POST',
                                headers: { 'Content-Type': 'application/json' },
                                body: JSON.stringify({
                                    version: currentVersion,
                                    pool_config: poolConfig
                                })
                            });

                            if (res.ok) {
                                showToast('success', 'Pool configuration updated');
                                loadSettings();
                            } else {
                                showToast('error', 'Failed to update pool configuration');
                            }
                        } catch (err) {
                            console.error(err);
                            showToast('error', 'Failed to update pool configuration');
                        }
                    }} className="space-y-4">
                        <div className="grid grid-cols-2 gap-4">
                            <div>
                                <label className="block text-xs text-fg-muted mb-1">PM Mode</label>
                                <select
                                    name="pm"
                                    defaultValue={settings.pool_config.pm || 'dynamic'}
                                    className="w-full bg-surface border border-border rounded px-3 py-2 text-fg text-sm focus:border-primary focus:outline-none"
                                >
                                    <option value="dynamic">dynamic</option>
                                    <option value="static">static</option>
                                    <option value="ondemand">ondemand</option>
                                </select>
                            </div>
                            <div>
                                <label className="block text-xs text-fg-muted mb-1">Max Children</label>
                                <input
                                    type="number"
                                    name="pm_max_children"
                                    defaultValue={settings.pool_config.pm_max_children || 5}
                                    className="w-full bg-surface border border-border rounded px-3 py-2 text-fg text-sm focus:border-primary focus:outline-none"
                                />
                            </div>
                            <div>
                                <label className="block text-xs text-fg-muted mb-1">Start Servers</label>
                                <input
                                    type="number"
                                    name="pm_start_servers"
                                    defaultValue={settings.pool_config.pm_start_servers || 2}
                                    className="w-full bg-surface border border-border rounded px-3 py-2 text-fg text-sm focus:border-primary focus:outline-none"
                                />
                            </div>
                            <div>
                                <label className="block text-xs text-fg-muted mb-1">Min Spare Servers</label>
                                <input
                                    type="number"
                                    name="pm_min_spare_servers"
                                    defaultValue={settings.pool_config.pm_min_spare_servers || 1}
                                    className="w-full bg-surface border border-border rounded px-3 py-2 text-fg text-sm focus:border-primary focus:outline-none"
                                />
                            </div>
                            <div>
                                <label className="block text-xs text-fg-muted mb-1">Max Spare Servers</label>
                                <input
                                    type="number"
                                    name="pm_max_spare_servers"
                                    defaultValue={settings.pool_config.pm_max_spare_servers || 3}
                                    className="w-full bg-surface border border-border rounded px-3 py-2 text-fg text-sm focus:border-primary focus:outline-none"
                                />
                            </div>
                            <div>
                                <label className="block text-xs text-fg-muted mb-1">User</label>
                                <input
                                    type="text"
                                    name="user"
                                    defaultValue={settings.pool_config.user || 'www-data'}
                                    className="w-full bg-surface border border-border rounded px-3 py-2 text-fg text-sm focus:border-primary focus:outline-none"
                                />
                            </div>
                            <div>
                                <label className="block text-xs text-fg-muted mb-1">Group</label>
                                <input
                                    type="text"
                                    name="group"
                                    defaultValue={settings.pool_config.group || 'www-data'}
                                    className="w-full bg-surface border border-border rounded px-3 py-2 text-fg text-sm focus:border-primary focus:outline-none"
                                />
                            </div>
                        </div>
                        <div className="flex justify-end pt-4 border-t border-border">
                            <button
                                type="submit"
                                className="px-4 py-2 bg-primary text-white rounded hover:bg-primary-hover flex items-center gap-2"
                            >
                                <Save className="w-4 h-4" />
                                Save Pool Configuration
                            </button>
                        </div>
                    </form>
                ) : (
                    <p className="text-sm text-fg-muted">
                        Pool configuration not available.
                    </p>
                )}
            </div>
        </div>
    );
}
