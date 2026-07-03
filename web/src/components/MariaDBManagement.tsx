
import { useState, useEffect } from 'react';
import { api, ConfigFile } from '../lib/api';
import { Play, Square, RefreshCw, ArrowLeft, Activity, Server, Clock, Database, Settings } from 'lucide-react';
import { MariaDBSettings } from './MariaDBSettings';

interface MariaDBManagementProps {
    initialVersion: string;
    onBack: () => void;
}

interface ServiceStatus {
    name: string;
    active: boolean;
    pid: string;
}

export function MariaDBManagement({ initialVersion: _initialVersion, onBack }: MariaDBManagementProps) {
    const [status, setStatus] = useState<ServiceStatus | null>(null);
    const [loading, setLoading] = useState(true);
    const [actionLoading, setActionLoading] = useState<string | null>(null);
    const [configFiles, setConfigFiles] = useState<ConfigFile[]>([]);
    const [selectedConfigFile, setSelectedConfigFile] = useState<string | null>(null);
    const [activeTab, setActiveTab] = useState<'status' | 'config'>('status');

    useEffect(() => {
        fetchStatus();
        fetchConfigFiles();
        const interval = setInterval(fetchStatus, 5000);
        return () => clearInterval(interval);
    }, []);

    const fetchStatus = async () => {
        try {
            const res = await api.getServiceStatus('mariadb');
            setStatus(res);
            setLoading(false);
        } catch (err) {
            console.error('Failed to fetch status:', err);
        }
    };

    const fetchConfigFiles = async () => {
        try {
            // We fetch the service definition to get config files
            const services = await api.getServices();
            const mariadb = services.find(s => s.id === 'mariadb');

            if (mariadb && mariadb.config_files && mariadb.config_files.length > 0) {
                setConfigFiles(mariadb.config_files);
                // Prefer my.cnf or server.cnf, otherwise first one
                const preferred = mariadb.config_files.find(f => f.path.endsWith('50-server.cnf')) ||
                    mariadb.config_files.find(f => f.path.endsWith('my.cnf')) ||
                    mariadb.config_files[0];
                setSelectedConfigFile(preferred.path);
            } else {
                console.log('MariaDB ConfigFiles not found or empty');
            }
        } catch (err) {
            console.error('Failed to fetch config files:', err);
        }
    };

    const handleAction = async (action: 'start' | 'stop' | 'restart') => {
        setActionLoading(action);
        try {
            await api.serviceAction('mariadb', action);
            // Polling will update status, but let's wait a bit
            setTimeout(fetchStatus, 1000);
            setTimeout(fetchStatus, 3000);
        } catch (err: any) {
            alert(`Failed to ${action} service: ${err.message}`);
        } finally {
            setActionLoading(null);
        }
    };

    if (loading) {
        return (
            <div className="flex items-center justify-center p-12">
                <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
            </div>
        );
    }

    const isRunning = status?.active;

    return (
        <div className="space-y-6">
            {/* Header */}
            <div className="flex items-center gap-4">
                <button
                    onClick={onBack}
                    className="p-2 hover:bg-surface-2 rounded-lg transition-colors text-fg-muted hover:text-fg"
                >
                    <ArrowLeft size={20} />
                </button>
                <div className="flex items-center gap-3">
                    <div className="p-3 bg-primary/10 rounded-xl">
                        <Database className="w-8 h-8 text-primary" />
                    </div>
                    <div>
                        <h1 className="text-2xl font-bold text-fg">MariaDB Server</h1>
                        <p className="text-fg-muted text-sm">High performance SQL database server</p>
                    </div>
                </div>
            </div>

            {/* Status Card */}
            <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
                <div className="lg:col-span-2 bg-surface/50 border border-border rounded-xl p-6">
                    <div className="flex items-center justify-between mb-6">
                        <div className="flex items-center gap-3">
                            <Activity className={`w-5 h-5 ${isRunning ? 'text-success' : 'text-fg-subtle'}`} />
                            <h2 className="text-lg font-bold text-fg">Service Status</h2>
                        </div>
                        <div className={`px-3 py-1 rounded-full text-xs font-bold ${isRunning
                            ? 'bg-success/10 text-success border border-success/20'
                            : 'bg-surface-2 text-fg-muted border border-border'
                            }`}>
                            {isRunning ? 'ACTIVE (RUNNING)' : 'STOPPED'}
                        </div>
                    </div>

                    <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
                        <button
                            onClick={() => handleAction('start')}
                            disabled={!!actionLoading || isRunning}
                            className="flex flex-col items-center gap-2 p-4 bg-surface border border-border rounded-xl hover:bg-surface-2 disabled:opacity-50 disabled:cursor-not-allowed transition-all group"
                        >
                            <div className="p-3 bg-success/10 rounded-lg group-hover:bg-success/20 transition-colors">
                                <Play className={`w-6 h-6 ${isRunning ? 'text-fg-subtle' : 'text-success'}`} />
                            </div>
                            <span className="text-sm font-bold text-fg-muted">Start Service</span>
                        </button>

                        <button
                            onClick={() => handleAction('restart')}
                            disabled={!!actionLoading}
                            className="flex flex-col items-center gap-2 p-4 bg-surface border border-border rounded-xl hover:bg-surface-2 disabled:opacity-50 disabled:cursor-not-allowed transition-all group"
                        >
                            <div className="p-3 bg-warning/10 rounded-lg group-hover:bg-warning/20 transition-colors">
                                <RefreshCw className={`w-6 h-6 text-warning ${actionLoading === 'restart' ? 'animate-spin' : ''}`} />
                            </div>
                            <span className="text-sm font-bold text-fg-muted">Restart</span>
                        </button>

                        <button
                            onClick={() => handleAction('stop')}
                            disabled={!!actionLoading || !isRunning}
                            className="flex flex-col items-center gap-2 p-4 bg-surface border border-border rounded-xl hover:bg-surface-2 disabled:opacity-50 disabled:cursor-not-allowed transition-all group"
                        >
                            <div className="p-3 bg-danger/10 rounded-lg group-hover:bg-danger/20 transition-colors">
                                <Square className={`w-6 h-6 ${!isRunning ? 'text-fg-subtle' : 'text-danger'}`} />
                            </div>
                            <span className="text-sm font-bold text-fg-muted">Stop Service</span>
                        </button>
                    </div>

                    {isRunning && (
                        <div className="mt-6 pt-6 border-t border-border grid grid-cols-2 gap-4">
                            <div className="flex items-center gap-3 text-fg-muted">
                                <Server size={16} />
                                <span className="text-sm">Process ID: <span className="text-fg font-mono">{status?.pid}</span></span>
                            </div>
                            <div className="flex items-center gap-3 text-fg-muted">
                                <Clock size={16} />
                                <span className="text-sm">Uptime: <span className="text-fg">--</span></span>
                            </div>
                        </div>
                    )}
                </div>

                <div className="bg-gradient-to-br from-primary/20 to-surface/50 border border-primary/20 rounded-xl p-6 relative overflow-hidden">
                    <div className="relative z-10">
                        <h3 className="text-lg font-bold text-fg mb-2">MariaDB Tips</h3>
                        <ul className="space-y-3 text-sm text-fg-muted">
                            <li className="flex items-start gap-2">
                                <span className="text-primary">•</span>
                                Use <code>bind-address = 0.0.0.0</code> to allow remote connections.
                            </li>
                            <li className="flex items-start gap-2">
                                <span className="text-primary">•</span>
                                Tune <code>innodb_buffer_pool_size</code> for performance (approx 70% RAM).
                            </li>
                            <li className="flex items-start gap-2">
                                <span className="text-primary">•</span>
                                Check error logs if service fails to start.
                            </li>
                        </ul>
                    </div>
                    <div className="absolute -bottom-4 -right-4 text-primary/10">
                        <Database size={120} />
                    </div>
                </div>
            </div>

            {/* Config Tabs */}
            <div className="pt-4">
                <div className="border-b border-border mb-6 flex gap-1">
                    <button
                        onClick={() => setActiveTab('config')}
                        className={`px-4 py-2 text-sm font-bold border-b-2 transition-colors flex items-center gap-2 ${activeTab === 'config'
                            ? 'border-primary text-primary'
                            : 'border-transparent text-fg-subtle hover:text-fg'
                            }`}
                    >
                        <Settings size={16} />
                        Visual Configuration ({status && configFiles.length > 0 ? 'my.cnf' : 'Searching...'})
                    </button>
                    {/* Access Rules typically SQL grant based, so skipping strictly file-based tab unless we want hosts.allow wrapper */}
                </div>

                {/* Config File Selector */}
                {configFiles.length > 0 && (
                    <div className="mb-6">
                        <label className="block text-sm font-medium text-fg-muted mb-2">Select Configuration File</label>
                        <select
                            value={selectedConfigFile || ''}
                            onChange={(e) => setSelectedConfigFile(e.target.value)}
                            className="bg-surface border border-border text-fg rounded-lg px-4 py-2 w-full max-w-md focus:outline-none focus:border-primary"
                        >
                            {configFiles.map(f => (
                                <option key={f.path} value={f.path}>
                                    {f.path}
                                </option>
                            ))}
                        </select>
                        <p className="mt-2 text-xs text-fg-subtle">
                            Note: Access rules and other split configurations may be in separate files like <code>50-server.cnf</code>.
                        </p>
                    </div>
                )}

                {/* Tab Content */}
                <div>
                    {selectedConfigFile ? (
                        <MariaDBSettings key={selectedConfigFile} configPath={selectedConfigFile} />
                    ) : (
                        <div className="text-center py-12 bg-surface/50 rounded-xl border border-border">
                            <div className="text-fg-subtle mb-2">Configuration file not found.</div>
                            <p className="text-xs text-fg-subtle">No configuration files detected for this service.</p>
                        </div>
                    )}
                </div>
            </div>
        </div>
    );
}
