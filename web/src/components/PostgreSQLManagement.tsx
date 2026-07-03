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
                <div className="bg-gradient-to-br from-surface to-surface/50 rounded-2xl border border-border p-8 relative overflow-hidden">
                    <div className="absolute top-0 right-0 w-64 h-64 bg-primary/5 rounded-full blur-3xl -mr-32 -mt-32 pointer-events-none"></div>

                    <div className="flex flex-col md:flex-row items-center justify-between gap-6 relative z-10">
                        <div className="flex items-center gap-6">
                            <div className={`w-16 h-16 rounded-2xl flex items-center justify-center shadow-lg transition-all duration-500 ${status?.active
                                ? 'bg-success/10 text-success shadow-green-500/20 border border-success/20'
                                : 'bg-danger/10 text-danger shadow-red-500/20 border border-danger/20'
                                }`}>
                                <Activity className={`w-8 h-8 ${status?.active ? 'animate-pulse' : ''}`} />
                            </div>
                            <div>
                                <h2 className="text-xl font-bold text-fg mb-1">PostgreSQL Service</h2>
                                <div className="flex items-center gap-2">
                                    <div className={`w-2 h-2 rounded-full ${status?.active ? 'bg-success' : 'bg-danger'}`}></div>
                                    <span className={`text-sm font-medium ${status?.active ? 'text-success' : 'text-danger'}`}>
                                        {status?.active ? 'Active & Running' : 'Stopped'}
                                    </span>
                                    {status?.pid && <span className="text-xs text-fg-subtle bg-surface-2 px-2 py-0.5 rounded-full ml-2">PID: {status.pid}</span>}
                                </div>
                            </div>
                        </div>

                        <div className="flex flex-wrap items-center gap-3">
                            <button
                                onClick={() => handleServiceAction('start')}
                                disabled={loading || status?.active}
                                className="px-6 py-2.5 bg-success/10 hover:bg-success/20 text-success border border-success/20 hover:border-success/30 rounded-xl font-medium transition-all duration-200 flex items-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed group"
                            >
                                <Play className="w-4 h-4 group-hover:fill-green-400/20" /> Start
                            </button>
                            <button
                                onClick={() => handleServiceAction('stop')}
                                disabled={loading || !status?.active}
                                className="px-6 py-2.5 bg-danger/10 hover:bg-danger/20 text-danger border border-danger/20 hover:border-danger/30 rounded-xl font-medium transition-all duration-200 flex items-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed group"
                            >
                                <Square className="w-4 h-4 group-hover:fill-red-400/20" /> Stop
                            </button>
                            <button
                                onClick={() => handleServiceAction('restart')}
                                disabled={loading}
                                className="px-6 py-2.5 bg-warning/10 hover:bg-warning/20 text-warning border border-warning/20 hover:border-warning/30 rounded-xl font-medium transition-all duration-200 flex items-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed group"
                            >
                                <RotateCw className="w-4 h-4 group-hover:rotate-180 transition-transform duration-700" /> Restart
                            </button>
                        </div>
                    </div>
                </div>

                {/* Configuration Section */}
                <div>
                    <h3 className="text-lg font-bold text-fg mb-4 flex items-center gap-2">
                        <Settings className="w-5 h-5 text-fg-muted" />
                        Configuration
                    </h3>

                    <div className="bg-surface border border-border rounded-xl overflow-hidden mb-6">
                        {/* Tab Switcher for Config Mode */}
                        <div className="flex border-b border-border">
                            <button
                                onClick={() => setConfigMode('visual')}
                                className={`px-6 py-3 text-sm font-medium transition-colors ${configMode === 'visual' ? 'text-primary border-b-2 border-primary bg-surface-2/50' : 'text-fg-muted hover:text-fg hover:bg-surface-2'}`}
                            >
                                Visual Settings
                            </button>
                            <button
                                onClick={() => setConfigMode('access')}
                                className={`px-6 py-3 text-sm font-medium transition-colors ${configMode === 'access' ? 'text-primary border-b-2 border-primary bg-surface-2/50' : 'text-fg-muted hover:text-fg hover:bg-surface-2'}`}
                            >
                                Access Rules (pg_hba)
                            </button>
                        </div>

                        <div className="p-6">
                            {configMode === 'visual' ? (
                                configFiles.find(f => f.path.endsWith('postgresql.conf')) ? (
                                    <PostgreSQLSettings configPath={configFiles.find(f => f.path.endsWith('postgresql.conf')).path} />
                                ) : (
                                    <div className="text-center text-fg-subtle py-8">
                                        postgresql.conf not found.
                                    </div>
                                )
                            ) : (
                                configFiles.find(f => f.path.endsWith('pg_hba.conf')) ? (
                                    <PostgreSQLAccessRules configPath={configFiles.find(f => f.path.endsWith('pg_hba.conf')).path} />
                                ) : (
                                    <div className="text-center text-fg-subtle py-8">
                                        pg_hba.conf not found.
                                    </div>
                                )
                            )}
                        </div>
                    </div>

                    <div className="opacity-50 hover:opacity-100 transition-opacity">
                        <h4 className="text-xs font-bold text-fg-subtle mb-2 uppercase tracking-wider">Advanced: Raw Files</h4>
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                            {configFiles.map((file) => (
                                <button
                                    key={file.path}
                                    onClick={() => setSelectedConfigFile(file.path)}
                                    className="flex items-center gap-3 p-3 bg-surface/50 border border-border rounded-lg hover:border-border-strong transition-all text-left"
                                >
                                    <FileCode className="w-4 h-4 text-fg-subtle" />
                                    <span className="font-mono text-xs text-fg-muted">{file.path.split('/').pop()}</span>
                                </button>
                            ))}
                        </div>
                    </div>
                </div>
            </div>
        </ServiceManagementLayout>
    );
}
