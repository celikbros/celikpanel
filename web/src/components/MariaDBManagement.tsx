
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
                <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-500"></div>
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
                    className="p-2 hover:bg-slate-800 rounded-lg transition-colors text-slate-400 hover:text-white"
                >
                    <ArrowLeft size={20} />
                </button>
                <div className="flex items-center gap-3">
                    <div className="p-3 bg-blue-500/10 rounded-xl">
                        <Database className="w-8 h-8 text-blue-400" />
                    </div>
                    <div>
                        <h1 className="text-2xl font-bold text-white">MariaDB Server</h1>
                        <p className="text-slate-400 text-sm">High performance SQL database server</p>
                    </div>
                </div>
            </div>

            {/* Status Card */}
            <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
                <div className="lg:col-span-2 bg-slate-900/50 border border-slate-800 rounded-xl p-6">
                    <div className="flex items-center justify-between mb-6">
                        <div className="flex items-center gap-3">
                            <Activity className={`w-5 h-5 ${isRunning ? 'text-green-500' : 'text-slate-500'}`} />
                            <h2 className="text-lg font-bold text-white">Service Status</h2>
                        </div>
                        <div className={`px-3 py-1 rounded-full text-xs font-bold ${isRunning
                            ? 'bg-green-500/10 text-green-400 border border-green-500/20'
                            : 'bg-slate-800 text-slate-400 border border-slate-700'
                            }`}>
                            {isRunning ? 'ACTIVE (RUNNING)' : 'STOPPED'}
                        </div>
                    </div>

                    <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
                        <button
                            onClick={() => handleAction('start')}
                            disabled={!!actionLoading || isRunning}
                            className="flex flex-col items-center gap-2 p-4 bg-slate-900 border border-slate-800 rounded-xl hover:bg-slate-800 disabled:opacity-50 disabled:cursor-not-allowed transition-all group"
                        >
                            <div className="p-3 bg-green-500/10 rounded-lg group-hover:bg-green-500/20 transition-colors">
                                <Play className={`w-6 h-6 ${isRunning ? 'text-slate-600' : 'text-green-500'}`} />
                            </div>
                            <span className="text-sm font-bold text-slate-300">Start Service</span>
                        </button>

                        <button
                            onClick={() => handleAction('restart')}
                            disabled={!!actionLoading}
                            className="flex flex-col items-center gap-2 p-4 bg-slate-900 border border-slate-800 rounded-xl hover:bg-slate-800 disabled:opacity-50 disabled:cursor-not-allowed transition-all group"
                        >
                            <div className="p-3 bg-yellow-500/10 rounded-lg group-hover:bg-yellow-500/20 transition-colors">
                                <RefreshCw className={`w-6 h-6 text-yellow-500 ${actionLoading === 'restart' ? 'animate-spin' : ''}`} />
                            </div>
                            <span className="text-sm font-bold text-slate-300">Restart</span>
                        </button>

                        <button
                            onClick={() => handleAction('stop')}
                            disabled={!!actionLoading || !isRunning}
                            className="flex flex-col items-center gap-2 p-4 bg-slate-900 border border-slate-800 rounded-xl hover:bg-slate-800 disabled:opacity-50 disabled:cursor-not-allowed transition-all group"
                        >
                            <div className="p-3 bg-red-500/10 rounded-lg group-hover:bg-red-500/20 transition-colors">
                                <Square className={`w-6 h-6 ${!isRunning ? 'text-slate-600' : 'text-red-500'}`} />
                            </div>
                            <span className="text-sm font-bold text-slate-300">Stop Service</span>
                        </button>
                    </div>

                    {isRunning && (
                        <div className="mt-6 pt-6 border-t border-slate-800 grid grid-cols-2 gap-4">
                            <div className="flex items-center gap-3 text-slate-400">
                                <Server size={16} />
                                <span className="text-sm">Process ID: <span className="text-white font-mono">{status?.pid}</span></span>
                            </div>
                            <div className="flex items-center gap-3 text-slate-400">
                                <Clock size={16} />
                                <span className="text-sm">Uptime: <span className="text-white">--</span></span>
                            </div>
                        </div>
                    )}
                </div>

                <div className="bg-gradient-to-br from-blue-900/20 to-slate-900/50 border border-blue-500/20 rounded-xl p-6 relative overflow-hidden">
                    <div className="relative z-10">
                        <h3 className="text-lg font-bold text-white mb-2">MariaDB Tips</h3>
                        <ul className="space-y-3 text-sm text-slate-300">
                            <li className="flex items-start gap-2">
                                <span className="text-blue-400">•</span>
                                Use <code>bind-address = 0.0.0.0</code> to allow remote connections.
                            </li>
                            <li className="flex items-start gap-2">
                                <span className="text-blue-400">•</span>
                                Tune <code>innodb_buffer_pool_size</code> for performance (approx 70% RAM).
                            </li>
                            <li className="flex items-start gap-2">
                                <span className="text-blue-400">•</span>
                                Check error logs if service fails to start.
                            </li>
                        </ul>
                    </div>
                    <div className="absolute -bottom-4 -right-4 text-blue-500/10">
                        <Database size={120} />
                    </div>
                </div>
            </div>

            {/* Config Tabs */}
            <div className="pt-4">
                <div className="border-b border-slate-800 mb-6 flex gap-1">
                    <button
                        onClick={() => setActiveTab('config')}
                        className={`px-4 py-2 text-sm font-bold border-b-2 transition-colors flex items-center gap-2 ${activeTab === 'config'
                            ? 'border-blue-500 text-blue-400'
                            : 'border-transparent text-slate-500 hover:text-white'
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
                        <label className="block text-sm font-medium text-slate-400 mb-2">Select Configuration File</label>
                        <select
                            value={selectedConfigFile || ''}
                            onChange={(e) => setSelectedConfigFile(e.target.value)}
                            className="bg-slate-900 border border-slate-700 text-white rounded-lg px-4 py-2 w-full max-w-md focus:outline-none focus:border-blue-500"
                        >
                            {configFiles.map(f => (
                                <option key={f.path} value={f.path}>
                                    {f.path}
                                </option>
                            ))}
                        </select>
                        <p className="mt-2 text-xs text-slate-500">
                            Note: Access rules and other split configurations may be in separate files like <code>50-server.cnf</code>.
                        </p>
                    </div>
                )}

                {/* Tab Content */}
                <div>
                    {selectedConfigFile ? (
                        <MariaDBSettings key={selectedConfigFile} configPath={selectedConfigFile} />
                    ) : (
                        <div className="text-center py-12 bg-slate-900/50 rounded-xl border border-slate-800">
                            <div className="text-slate-500 mb-2">Configuration file not found.</div>
                            <p className="text-xs text-slate-600">No configuration files detected for this service.</p>
                        </div>
                    )}
                </div>
            </div>
        </div>
    );
}
