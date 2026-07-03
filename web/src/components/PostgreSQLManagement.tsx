import { useState, useEffect } from 'react';
import { ServiceManagementLayout } from './ServiceManagementLayout';
import { Activity, Play, Square, RotateCw, FileCode, Settings } from 'lucide-react';
import { ConfigEditor } from './ConfigEditor';
import { PostgreSQLSettings } from './PostgreSQLSettings';
import { PostgreSQLAccessRules } from './PostgreSQLAccessRules';

interface PostgreSQLManagementProps {
    initialVersion: string;
    onBack: () => void;
}

export function PostgreSQLManagement({ initialVersion, onBack }: PostgreSQLManagementProps) {
    const [activeVersion, setActiveVersion] = useState(initialVersion || 'default');
    const [loading, setLoading] = useState(false);
    const [status, setStatus] = useState<{ active: boolean; pid?: string } | null>(null);
    const [configFiles, setConfigFiles] = useState<any[]>([]);
    const [selectedConfigFile, setSelectedConfigFile] = useState<string | null>(null);
    const [configMode, setConfigMode] = useState<'visual' | 'access'>('visual');

    const fetchData = async () => {
        setLoading(true);
        try {
            const statusRes = await fetch('/api/v1/service/status?name=postgresql');
            if (statusRes.ok) {
                setStatus(await statusRes.json());
            } else {
                setStatus({ active: false });
            }
        } catch (err) {
            console.error("Failed to fetch PostgreSQL status:", err);
        } finally {
            setLoading(false);
        }
    };

    const fetchConfigFiles = async () => {
        try {
            const res = await fetch('/api/v1/managed-services');
            if (res.ok) {
                const services = await res.json();
                const pg = services.find((s: any) => s.id === 'postgresql');
                if (pg && pg.config_files) {
                    setConfigFiles(pg.config_files);
                }
            }
        } catch (err) {
            console.error("Failed to fetch config files:", err);
        }
    };

    useEffect(() => {
        fetchData();
        fetchConfigFiles();
    }, [activeVersion]);

    const handleServiceAction = async (action: 'start' | 'stop' | 'restart') => {
        const actionText = action === 'start' ? 'start' : action === 'stop' ? 'stop' : 'restart';
        if (!confirm(`Are you sure you want to ${actionText} PostgreSQL?`)) return;

        try {
            const response = await fetch('/api/v1/service/action', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name: 'postgresql', action }),
            });

            if (!response.ok) throw new Error('Action failed');

            // Wait for service to react
            await new Promise(resolve => setTimeout(resolve, 1500));
            fetchData();
        } catch (err) {
            console.error('Service action error:', err);
            alert('Failed to execute action');
        }
    };

    // If a config file is selected, show editor
    if (selectedConfigFile) {
        return <ConfigEditor path={selectedConfigFile} onBack={() => setSelectedConfigFile(null)} />;
    }

    return (
        <ServiceManagementLayout
            serviceName="PostgreSQL"
            serviceIcon="🐘"
            versions={['default']}
            activeVersion={activeVersion}
            onVersionChange={setActiveVersion}
            onBack={onBack}
            hideSidebar={true}
        >
            <div className="w-full space-y-8">
                {/* Status Card */}
                <div className="bg-gradient-to-br from-slate-900 to-slate-900/50 rounded-2xl border border-slate-800 p-8 relative overflow-hidden">
                    <div className="absolute top-0 right-0 w-64 h-64 bg-blue-500/5 rounded-full blur-3xl -mr-32 -mt-32 pointer-events-none"></div>

                    <div className="flex flex-col md:flex-row items-center justify-between gap-6 relative z-10">
                        <div className="flex items-center gap-6">
                            <div className={`w-16 h-16 rounded-2xl flex items-center justify-center shadow-lg transition-all duration-500 ${status?.active
                                ? 'bg-green-500/10 text-green-400 shadow-green-500/20 border border-green-500/20'
                                : 'bg-red-500/10 text-red-500 shadow-red-500/20 border border-red-500/20'
                                }`}>
                                <Activity className={`w-8 h-8 ${status?.active ? 'animate-pulse' : ''}`} />
                            </div>
                            <div>
                                <h2 className="text-xl font-bold text-white mb-1">PostgreSQL Service</h2>
                                <div className="flex items-center gap-2">
                                    <div className={`w-2 h-2 rounded-full ${status?.active ? 'bg-green-400' : 'bg-red-500'}`}></div>
                                    <span className={`text-sm font-medium ${status?.active ? 'text-green-400' : 'text-red-400'}`}>
                                        {status?.active ? 'Active & Running' : 'Stopped'}
                                    </span>
                                    {status?.pid && <span className="text-xs text-slate-500 bg-slate-800 px-2 py-0.5 rounded-full ml-2">PID: {status.pid}</span>}
                                </div>
                            </div>
                        </div>

                        <div className="flex flex-wrap items-center gap-3">
                            <button
                                onClick={() => handleServiceAction('start')}
                                disabled={loading || status?.active}
                                className="px-6 py-2.5 bg-green-600/10 hover:bg-green-600/20 text-green-400 border border-green-600/20 hover:border-green-600/30 rounded-xl font-medium transition-all duration-200 flex items-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed group"
                            >
                                <Play className="w-4 h-4 group-hover:fill-green-400/20" /> Start
                            </button>
                            <button
                                onClick={() => handleServiceAction('stop')}
                                disabled={loading || !status?.active}
                                className="px-6 py-2.5 bg-red-600/10 hover:bg-red-600/20 text-red-400 border border-red-600/20 hover:border-red-600/30 rounded-xl font-medium transition-all duration-200 flex items-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed group"
                            >
                                <Square className="w-4 h-4 group-hover:fill-red-400/20" /> Stop
                            </button>
                            <button
                                onClick={() => handleServiceAction('restart')}
                                disabled={loading}
                                className="px-6 py-2.5 bg-yellow-600/10 hover:bg-yellow-600/20 text-yellow-400 border border-yellow-600/20 hover:border-yellow-600/30 rounded-xl font-medium transition-all duration-200 flex items-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed group"
                            >
                                <RotateCw className="w-4 h-4 group-hover:rotate-180 transition-transform duration-700" /> Restart
                            </button>
                        </div>
                    </div>
                </div>

                {/* Configuration Section */}
                <div>
                    <h3 className="text-lg font-bold text-white mb-4 flex items-center gap-2">
                        <Settings className="w-5 h-5 text-gray-400" />
                        Configuration
                    </h3>

                    <div className="bg-slate-900 border border-slate-800 rounded-xl overflow-hidden mb-6">
                        {/* Tab Switcher for Config Mode */}
                        <div className="flex border-b border-slate-800">
                            <button
                                onClick={() => setConfigMode('visual')}
                                className={`px-6 py-3 text-sm font-medium transition-colors ${configMode === 'visual' ? 'text-blue-400 border-b-2 border-blue-500 bg-slate-800/50' : 'text-slate-400 hover:text-white hover:bg-slate-800'}`}
                            >
                                Visual Settings
                            </button>
                            <button
                                onClick={() => setConfigMode('access')}
                                className={`px-6 py-3 text-sm font-medium transition-colors ${configMode === 'access' ? 'text-blue-400 border-b-2 border-blue-500 bg-slate-800/50' : 'text-slate-400 hover:text-white hover:bg-slate-800'}`}
                            >
                                Access Rules (pg_hba)
                            </button>
                        </div>

                        <div className="p-6">
                            {configMode === 'visual' ? (
                                configFiles.find(f => f.path.endsWith('postgresql.conf')) ? (
                                    <PostgreSQLSettings configPath={configFiles.find(f => f.path.endsWith('postgresql.conf')).path} />
                                ) : (
                                    <div className="text-center text-slate-500 py-8">
                                        postgresql.conf not found.
                                    </div>
                                )
                            ) : (
                                configFiles.find(f => f.path.endsWith('pg_hba.conf')) ? (
                                    <PostgreSQLAccessRules configPath={configFiles.find(f => f.path.endsWith('pg_hba.conf')).path} />
                                ) : (
                                    <div className="text-center text-slate-500 py-8">
                                        pg_hba.conf not found.
                                    </div>
                                )
                            )}
                        </div>
                    </div>

                    <div className="opacity-50 hover:opacity-100 transition-opacity">
                        <h4 className="text-xs font-bold text-slate-500 mb-2 uppercase tracking-wider">Advanced: Raw Files</h4>
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                            {configFiles.map((file) => (
                                <button
                                    key={file.path}
                                    onClick={() => setSelectedConfigFile(file.path)}
                                    className="flex items-center gap-3 p-3 bg-slate-900/50 border border-slate-800 rounded-lg hover:border-slate-600 transition-all text-left"
                                >
                                    <FileCode className="w-4 h-4 text-slate-500" />
                                    <span className="font-mono text-xs text-slate-400">{file.path.split('/').pop()}</span>
                                </button>
                            ))}
                        </div>
                    </div>
                </div>
            </div>
        </ServiceManagementLayout>
    );
}
