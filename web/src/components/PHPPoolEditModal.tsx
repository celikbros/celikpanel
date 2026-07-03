import { useState, useEffect } from 'react';
import { X, Save, Trash2 } from 'lucide-react';

interface PHPPoolConfig {
    name: string;
    user: string;
    group: string;
    listen: string;
    listen_owner: string;
    listen_group: string;
    listen_mode: string;
    pm: string;
    pm_max_children: number;
    pm_start_servers: number;
    pm_min_spare_servers: number;
    pm_max_spare_servers: number;
    pm_max_requests: number;
}

interface PHPPoolEditModalProps {
    version: string;
    poolName: string;
    onClose: () => void;
    onSave: () => void;
}

export function PHPPoolEditModal({ version, poolName, onClose, onSave }: PHPPoolEditModalProps) {
    const [config, setConfig] = useState<PHPPoolConfig | null>(null);
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);

    useEffect(() => {
        const fetchConfig = async () => {
            try {
                const res = await fetch(`/api/v1/php/pool-config?version=${version}&pool=${poolName}`);
                if (res.ok) {
                    setConfig(await res.json());
                } else {
                    alert('Failed to load pool configuration');
                    onClose();
                }
            } catch (err) {
                console.error(err);
                alert('Failed to load pool configuration');
                onClose();
            } finally {
                setLoading(false);
            }
        };
        fetchConfig();
    }, [version, poolName]);

    const handleSave = async () => {
        if (!config) return;
        setSaving(true);
        try {
            const res = await fetch('/api/v1/php/pool-config', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    version,
                    pool_config: config
                })
            });

            if (res.ok) {
                onSave();
                onClose();
            } else {
                alert('Failed to save pool configuration');
            }
        } catch (err) {
            console.error(err);
            alert('Failed to save pool configuration');
        } finally {
            setSaving(false);
        }
    };

    const handleDelete = async () => {
        if (!confirm(`Are you sure you want to delete pool "${poolName}"? This action cannot be undone.`)) return;

        try {
            const res = await fetch(`/api/v1/php/pool-config?version=${version}&pool=${poolName}`, {
                method: 'DELETE'
            });

            if (res.ok) {
                onSave(); // Refresh list
                onClose();
            } else {
                alert('Failed to delete pool');
            }
        } catch (err) {
            console.error(err);
            alert('Failed to delete pool');
        }
    };

    if (loading) {
        return (
            <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
                <div className="bg-surface-2 p-6 rounded-lg border border-border">
                    <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
                </div>
            </div>
        );
    }

    if (!config) return null;

    return (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 overflow-y-auto py-10">
            <div className="bg-surface-2 rounded-lg border border-border w-full max-w-2xl m-4 flex flex-col max-h-[90vh]">
                <div className="flex justify-between items-center p-4 border-b border-border">
                    <h3 className="text-lg font-bold text-fg">Edit Pool: {poolName}</h3>
                    <button onClick={onClose} className="text-fg-muted hover:text-fg">
                        <X className="w-5 h-5" />
                    </button>
                </div>

                <div className="p-6 space-y-6 overflow-y-auto">
                    {/* General Settings */}
                    <div className="space-y-4">
                        <h4 className="text-sm font-bold text-primary uppercase tracking-wider">General Settings</h4>
                        <div className="grid grid-cols-2 gap-4">
                            <div>
                                <label className="block text-xs text-fg-muted mb-1">User</label>
                                <input
                                    type="text"
                                    value={config.user}
                                    onChange={e => setConfig({ ...config, user: e.target.value })}
                                    className="w-full bg-surface border border-border rounded px-3 py-2 text-fg text-sm"
                                />
                            </div>
                            <div>
                                <label className="block text-xs text-fg-muted mb-1">Group</label>
                                <input
                                    type="text"
                                    value={config.group}
                                    onChange={e => setConfig({ ...config, group: e.target.value })}
                                    className="w-full bg-surface border border-border rounded px-3 py-2 text-fg text-sm"
                                />
                            </div>
                            <div className="col-span-2">
                                <label className="block text-xs text-fg-muted mb-1">Listen Address (Socket or Port)</label>
                                <input
                                    type="text"
                                    value={config.listen}
                                    onChange={e => setConfig({ ...config, listen: e.target.value })}
                                    className="w-full bg-surface border border-border rounded px-3 py-2 text-fg text-sm font-mono"
                                />
                            </div>
                        </div>
                    </div>

                    {/* Process Manager */}
                    <div className="space-y-4">
                        <h4 className="text-sm font-bold text-primary uppercase tracking-wider">Process Manager</h4>
                        <div>
                            <label className="block text-xs text-fg-muted mb-1">Mode</label>
                            <select
                                value={config.pm}
                                onChange={e => setConfig({ ...config, pm: e.target.value })}
                                className="w-full bg-surface border border-border rounded px-3 py-2 text-fg text-sm"
                            >
                                <option value="dynamic">Dynamic</option>
                                <option value="ondemand">On Demand</option>
                                <option value="static">Static</option>
                            </select>
                        </div>

                        <div className="grid grid-cols-2 gap-4">
                            <div>
                                <label className="block text-xs text-fg-muted mb-1">Max Children</label>
                                <input
                                    type="number"
                                    value={config.pm_max_children}
                                    onChange={e => setConfig({ ...config, pm_max_children: parseInt(e.target.value) })}
                                    className="w-full bg-surface border border-border rounded px-3 py-2 text-fg text-sm"
                                />
                            </div>
                            <div>
                                <label className="block text-xs text-fg-muted mb-1">Max Requests</label>
                                <input
                                    type="number"
                                    value={config.pm_max_requests}
                                    onChange={e => setConfig({ ...config, pm_max_requests: parseInt(e.target.value) })}
                                    className="w-full bg-surface border border-border rounded px-3 py-2 text-fg text-sm"
                                />
                            </div>

                            {config.pm === 'dynamic' && (
                                <>
                                    <div>
                                        <label className="block text-xs text-fg-muted mb-1">Start Servers</label>
                                        <input
                                            type="number"
                                            value={config.pm_start_servers}
                                            onChange={e => setConfig({ ...config, pm_start_servers: parseInt(e.target.value) })}
                                            className="w-full bg-surface border border-border rounded px-3 py-2 text-fg text-sm"
                                        />
                                    </div>
                                    <div>
                                        <label className="block text-xs text-fg-muted mb-1">Min Spare Servers</label>
                                        <input
                                            type="number"
                                            value={config.pm_min_spare_servers}
                                            onChange={e => setConfig({ ...config, pm_min_spare_servers: parseInt(e.target.value) })}
                                            className="w-full bg-surface border border-border rounded px-3 py-2 text-fg text-sm"
                                        />
                                    </div>
                                    <div>
                                        <label className="block text-xs text-fg-muted mb-1">Max Spare Servers</label>
                                        <input
                                            type="number"
                                            value={config.pm_max_spare_servers}
                                            onChange={e => setConfig({ ...config, pm_max_spare_servers: parseInt(e.target.value) })}
                                            className="w-full bg-surface border border-border rounded px-3 py-2 text-fg text-sm"
                                        />
                                    </div>
                                </>
                            )}
                        </div>
                    </div>
                </div>

                <div className="p-4 border-t border-border flex justify-between bg-surface-2/50">
                    <button
                        onClick={handleDelete}
                        className="px-4 py-2 bg-danger/15/30 text-danger rounded hover:bg-danger/15/50 text-sm font-medium flex items-center gap-2"
                    >
                        <Trash2 className="w-4 h-4" /> Delete Pool
                    </button>
                    <div className="flex gap-2">
                        <button
                            onClick={onClose}
                            className="px-4 py-2 text-fg-muted hover:text-fg text-sm"
                        >
                            Cancel
                        </button>
                        <button
                            onClick={handleSave}
                            disabled={saving}
                            className="px-4 py-2 bg-primary text-white rounded hover:bg-primary-hover text-sm font-medium flex items-center gap-2 disabled:opacity-50"
                        >
                            <Save className="w-4 h-4" />
                            {saving ? 'Saving...' : 'Save Changes'}
                        </button>
                    </div>
                </div>
            </div>
        </div>
    );
}
