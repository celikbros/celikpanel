import { useState, useEffect } from 'react';
import { ServiceManagementLayout } from './ServiceManagementLayout';
import { Puzzle, FileText, RotateCw, Play, Square, Activity } from 'lucide-react';
import { PHPExtendedConfig } from './PHPExtendedConfig';

interface PHPManagementProps {
    initialVersion: string;
    availableVersions: string[];
    onBack: () => void;
}

interface PHPExtension {
    name: string;
    enabled: boolean;
}

export function PHPManagement({ initialVersion, availableVersions, onBack }: PHPManagementProps) {
    const [activeVersion, setActiveVersion] = useState(initialVersion);
    const [activeTab, setActiveTab] = useState<'extensions' | 'config'>('extensions');
    const [loading, setLoading] = useState(true);

    const [extensions, setExtensions] = useState<PHPExtension[]>([]);

    const [status, setStatus] = useState<{ active: boolean; pid?: string } | null>(null);

    const fetchData = async () => {
        setLoading(true);
        try {
            const statusRes = await fetch(`/api/v1/service/status?name=php${activeVersion}-fpm`);
            if (statusRes.ok) {
                setStatus(await statusRes.json());
            } else {
                setStatus({ active: false });
            }

            const extRes = await fetch(`/api/v1/php/extensions?version=${activeVersion}`);
            if (extRes.ok) setExtensions(await extRes.json());

        } catch (err) {
            console.error("Failed to fetch PHP data:", err);
            alert("Failed to load PHP data");
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        fetchData();
    }, [activeVersion]);

    const handleServiceAction = async (action: 'start' | 'stop' | 'restart') => {
        const actionText = action === 'start' ? 'start' : action === 'stop' ? 'stop' : 'restart';
        if (!confirm(`Are you sure you want to ${actionText} PHP ${activeVersion}?`)) return;

        try {
            const serviceName = `php${activeVersion}-fpm`;
            const response = await fetch('/api/v1/service/action', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name: serviceName, action }),
            });

            if (!response.ok) {
                throw new Error('Service action failed');
            }

            await new Promise(resolve => setTimeout(resolve, 1000));

            const statusRes = await fetch(`/api/v1/service/status?name=${serviceName}`);
            if (statusRes.ok) {
                const newStatus = await statusRes.json();
                setStatus(newStatus);
            }
        } catch (err) {
            console.error('Service action error:', err);
            alert('Failed to execute action');
        }
    };

    const tabs = [
        { id: 'extensions', label: 'Extensions', icon: <Puzzle className="w-4 h-4" /> },
        { id: 'config', label: 'Configuration', icon: <FileText className="w-4 h-4" /> },
    ];

    return (
        <ServiceManagementLayout
            serviceName="PHP-FPM"
            serviceIcon="🐘"
            versions={availableVersions}
            activeVersion={activeVersion}
            onVersionChange={setActiveVersion}
            onBack={onBack}
            hideSidebar={true}
        >
            {loading ? (
                <div className="flex items-center justify-center py-20">
                    <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-500"></div>
                </div>
            ) : (
                <div className="w-full space-y-8">
                    {/* Service Status & Control Card */}
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
                                    <h2 className="text-xl font-bold text-white mb-1">PHP-FPM Service</h2>
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
                                {/* Version Selector */}
                                {/* Version Selector */}
                                <div className="relative">
                                    <select
                                        value={activeVersion}
                                        onChange={(e) => setActiveVersion(e.target.value)}
                                        className="appearance-none bg-slate-800 border border-slate-700 text-white text-sm font-bold rounded-xl pl-4 pr-10 py-2.5 hover:border-blue-500/50 focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500/50 transition-all cursor-pointer"
                                    >
                                        {availableVersions.map(v => (
                                            <option key={v} value={v} className="bg-slate-900 border-b border-slate-800 py-2">PHP {v}</option>
                                        ))}
                                    </select>
                                    <div className="absolute inset-y-0 right-0 flex items-center pr-3 pointer-events-none text-slate-400">
                                        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
                                        </svg>
                                    </div>
                                    <div className="absolute -top-2 left-2 px-1 bg-slate-900 text-[10px] font-bold text-slate-400">VER</div>
                                </div>

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

                    {/* Navigation Tabs */}
                    <div className="flex items-center gap-4 border-b border-slate-800 pb-1">
                        {tabs.map(tab => (
                            <button
                                key={tab.id}
                                onClick={() => setActiveTab(tab.id as 'extensions' | 'config')}
                                className={`px-4 py-2 text-sm font-medium rounded-t-lg border-b-2 transition-colors flex items-center gap-2 ${activeTab === tab.id
                                    ? 'border-blue-500 text-blue-400 bg-blue-500/5'
                                    : 'border-transparent text-gray-400 hover:text-white hover:bg-slate-800/50'
                                    }`}
                            >
                                {tab.icon}
                                {tab.label}
                            </button>
                        ))}
                    </div>

                    {/* Content Area */}
                    <div className="min-h-[500px]">
                        {activeTab === 'extensions' && (
                            <div className="bg-slate-900/50 border border-slate-800 rounded-xl p-6">
                                <h3 className="text-lg font-semibold text-white mb-6 flex items-center gap-2">
                                    <Puzzle className="w-5 h-5 text-blue-400" />
                                    Installed Extensions
                                </h3>
                                <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
                                    {extensions.map(ext => (
                                        <div key={ext.name} className="group bg-slate-950 p-4 rounded-xl border border-slate-800/50 hover:border-blue-500/30 transition-all duration-300 flex items-center justify-between">
                                            <span className="text-sm font-mono text-gray-300 font-medium group-hover:text-blue-200 transition-colors">{ext.name}</span>
                                            <div
                                                onClick={async () => {
                                                    try {
                                                        const newEnabled = !ext.enabled;
                                                        await fetch(`/api/v1/php/extensions`, {
                                                            method: 'POST',
                                                            headers: { 'Content-Type': 'application/json' },
                                                            body: JSON.stringify({
                                                                version: activeVersion,
                                                                extension: ext.name,
                                                                enabled: newEnabled
                                                            })
                                                        });

                                                        setExtensions(extensions.map(e =>
                                                            e.name === ext.name ? { ...e, enabled: newEnabled } : e
                                                        ));
                                                    } catch (err) {
                                                        console.error('Failed to toggle extension:', err);
                                                        alert('Failed to toggle extension');
                                                    }
                                                }}
                                                className={`w-11 h-6 rounded-full relative cursor-pointer transition-colors duration-300 ${ext.enabled ? 'bg-blue-600' : 'bg-slate-700'}`}
                                            >
                                                <div
                                                    className={`absolute top-1 w-4 h-4 rounded-full transition-all duration-300 shadow-sm ${ext.enabled ? 'bg-white left-[24px]' : 'bg-gray-400 left-[4px]'}`}
                                                ></div>
                                            </div>
                                        </div>
                                    ))}
                                </div>
                            </div>
                        )}

                        {activeTab === 'config' && (
                            <div className="bg-slate-900/50 border border-slate-800 rounded-xl overflow-hidden">
                                <PHPExtendedConfig version={activeVersion} />
                            </div>
                        )}
                    </div>
                </div>
            )}
        </ServiceManagementLayout>
    );
}
