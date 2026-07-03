import { useState, useEffect } from 'react';
import { Settings, Save, X } from 'lucide-react';

interface PHPConfig {
    memory_limit: string;
    max_execution_time: number;
    upload_max_filesize: string;
    post_max_size: string;
    max_input_vars: number;
}

interface MySQLConfig {
    max_connections: number;
    innodb_buffer_pool_size: string;
    query_cache_size: string;
    max_allowed_packet: string;
}

interface ConfigModalProps {
    serviceName: string;
    serviceType: 'php' | 'mysql';
    phpVersion?: string;
    onClose: () => void;
}

const API_BASE = '/api/v1';

export function ServiceConfigModal({ serviceName, serviceType, phpVersion, onClose }: ConfigModalProps) {
    const [phpConfig, setPHPConfig] = useState<PHPConfig | null>(null);
    const [mysqlConfig, setMySQLConfig] = useState<MySQLConfig | null>(null);
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState<string | null>(null);

    useEffect(() => {
        loadConfig();
    }, []);

    const loadConfig = async () => {
        setLoading(true);
        setError(null);
        try {
            if (serviceType === 'php' && phpVersion) {
                const res = await fetch(`${API_BASE}/config/php?version=${phpVersion}`);
                if (!res.ok) throw new Error('Failed to load PHP config');
                const data = await res.json();
                setPHPConfig(data);
            } else if (serviceType === 'mysql') {
                const res = await fetch(`${API_BASE}/config/mysql`);
                if (!res.ok) throw new Error('Failed to load MySQL config');
                const data = await res.json();
                setMySQLConfig(data);
            }
        } catch (err: any) {
            setError(err.message);
        } finally {
            setLoading(false);
        }
    };

    const saveConfig = async () => {
        setSaving(true);
        setError(null);
        try {
            if (serviceType === 'php' && phpConfig && phpVersion) {
                const res = await fetch(`${API_BASE}/config/php`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ ...phpConfig, php_version: phpVersion }),
                });
                if (!res.ok) throw new Error('Failed to save PHP config');
                alert('PHP configuration saved successfully!');
                onClose();
            } else if (serviceType === 'mysql' && mysqlConfig) {
                const res = await fetch(`${API_BASE}/config/mysql`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(mysqlConfig),
                });
                if (!res.ok) throw new Error('Failed to save MySQL config');
                alert('MySQL configuration saved successfully!');
                onClose();
            }
        } catch (err: any) {
            setError(err.message);
        } finally {
            setSaving(false);
        }
    };

    if (loading) {
        return (
            <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
                <div className="bg-surface border border-border rounded-xl p-8 max-w-2xl w-full mx-4">
                    <div className="text-center text-fg-muted">Loading configuration...</div>
                </div>
            </div>
        );
    }

    return (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
            <div className="bg-surface border border-border rounded-xl p-8 max-w-2xl w-full mx-4 max-h-[90vh] overflow-y-auto">
                <div className="flex justify-between items-center mb-6">
                    <div className="flex items-center gap-3">
                        <div className="p-2 bg-primary/10 rounded-lg">
                            <Settings className="w-6 h-6 text-primary" />
                        </div>
                        <div>
                            <h2 className="text-xl font-bold text-fg">{serviceName} Configuration</h2>
                            <p className="text-sm text-fg-subtle">Adjust common settings</p>
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

                {serviceType === 'php' && phpConfig && (
                    <div className="space-y-4">
                        <div>
                            <label className="block text-sm font-medium text-fg-muted mb-2">Memory Limit</label>
                            <input
                                type="text"
                                value={phpConfig.memory_limit}
                                onChange={(e) => setPHPConfig({ ...phpConfig, memory_limit: e.target.value })}
                                className="w-full bg-surface-2 border border-border rounded-lg px-4 py-2 text-fg focus:outline-none focus:border-primary"
                                placeholder="256M"
                            />
                            <p className="text-xs text-fg-subtle mt-1">e.g., 128M, 256M, 512M</p>
                        </div>

                        <div>
                            <label className="block text-sm font-medium text-fg-muted mb-2">Max Execution Time (seconds)</label>
                            <input
                                type="number"
                                value={phpConfig.max_execution_time}
                                onChange={(e) => setPHPConfig({ ...phpConfig, max_execution_time: parseInt(e.target.value) })}
                                className="w-full bg-surface-2 border border-border rounded-lg px-4 py-2 text-fg focus:outline-none focus:border-primary"
                            />
                        </div>

                        <div>
                            <label className="block text-sm font-medium text-fg-muted mb-2">Upload Max Filesize</label>
                            <input
                                type="text"
                                value={phpConfig.upload_max_filesize}
                                onChange={(e) => setPHPConfig({ ...phpConfig, upload_max_filesize: e.target.value })}
                                className="w-full bg-surface-2 border border-border rounded-lg px-4 py-2 text-fg focus:outline-none focus:border-primary"
                                placeholder="64M"
                            />
                        </div>

                        <div>
                            <label className="block text-sm font-medium text-fg-muted mb-2">Post Max Size</label>
                            <input
                                type="text"
                                value={phpConfig.post_max_size}
                                onChange={(e) => setPHPConfig({ ...phpConfig, post_max_size: e.target.value })}
                                className="w-full bg-surface-2 border border-border rounded-lg px-4 py-2 text-fg focus:outline-none focus:border-primary"
                                placeholder="64M"
                            />
                        </div>

                        <div>
                            <label className="block text-sm font-medium text-fg-muted mb-2">Max Input Vars</label>
                            <input
                                type="number"
                                value={phpConfig.max_input_vars}
                                onChange={(e) => setPHPConfig({ ...phpConfig, max_input_vars: parseInt(e.target.value) })}
                                className="w-full bg-surface-2 border border-border rounded-lg px-4 py-2 text-fg focus:outline-none focus:border-primary"
                            />
                        </div>
                    </div>
                )}

                {serviceType === 'mysql' && mysqlConfig && (
                    <div className="space-y-4">
                        <div>
                            <label className="block text-sm font-medium text-fg-muted mb-2">Max Connections</label>
                            <input
                                type="number"
                                value={mysqlConfig.max_connections}
                                onChange={(e) => setMySQLConfig({ ...mysqlConfig, max_connections: parseInt(e.target.value) })}
                                className="w-full bg-surface-2 border border-border rounded-lg px-4 py-2 text-fg focus:outline-none focus:border-primary"
                            />
                        </div>

                        <div>
                            <label className="block text-sm font-medium text-fg-muted mb-2">InnoDB Buffer Pool Size</label>
                            <input
                                type="text"
                                value={mysqlConfig.innodb_buffer_pool_size}
                                onChange={(e) => setMySQLConfig({ ...mysqlConfig, innodb_buffer_pool_size: e.target.value })}
                                className="w-full bg-surface-2 border border-border rounded-lg px-4 py-2 text-fg focus:outline-none focus:border-primary"
                                placeholder="1G"
                            />
                            <p className="text-xs text-fg-subtle mt-1">e.g., 128M, 1G, 2G</p>
                        </div>

                        <div>
                            <label className="block text-sm font-medium text-fg-muted mb-2">Query Cache Size</label>
                            <input
                                type="text"
                                value={mysqlConfig.query_cache_size}
                                onChange={(e) => setMySQLConfig({ ...mysqlConfig, query_cache_size: e.target.value })}
                                className="w-full bg-surface-2 border border-border rounded-lg px-4 py-2 text-fg focus:outline-none focus:border-primary"
                                placeholder="64M"
                            />
                        </div>

                        <div>
                            <label className="block text-sm font-medium text-fg-muted mb-2">Max Allowed Packet</label>
                            <input
                                type="text"
                                value={mysqlConfig.max_allowed_packet}
                                onChange={(e) => setMySQLConfig({ ...mysqlConfig, max_allowed_packet: e.target.value })}
                                className="w-full bg-surface-2 border border-border rounded-lg px-4 py-2 text-fg focus:outline-none focus:border-primary"
                                placeholder="64M"
                            />
                        </div>
                    </div>
                )}

                <div className="flex gap-3 mt-6">
                    <button
                        onClick={saveConfig}
                        disabled={saving}
                        className="flex-1 flex items-center justify-center gap-2 bg-primary hover:bg-primary-hover disabled:bg-surface-3 text-white px-6 py-3 rounded-lg transition-colors font-medium"
                    >
                        <Save className="w-5 h-5" />
                        {saving ? 'Saving...' : 'Save Configuration'}
                    </button>
                    <button
                        onClick={onClose}
                        className="px-6 py-3 bg-surface-2 hover:bg-surface-3 text-fg-muted rounded-lg transition-colors"
                    >
                        Cancel
                    </button>
                </div>
            </div>
        </div>
    );
}
