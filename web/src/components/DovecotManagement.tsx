import { useState, useEffect } from 'react';
import { ServiceManagementLayout } from './ServiceManagementLayout';
import { Activity, Play, Square, RotateCw, Users, Shield } from 'lucide-react';

interface DovecotManagementProps {
    initialVersion: string;
    onBack: () => void;
}

interface DovecotStats {
    uptime: string;
    connections: number;
    logins: number;
    auth_success: number;
    auth_fail: number;
}

export function DovecotManagement({ initialVersion, onBack }: DovecotManagementProps) {
    const [activeVersion, setActiveVersion] = useState(initialVersion || 'default');
    const [activeTab, setActiveTab] = useState('stats');
    const [loading, setLoading] = useState(false);

    const [status, setStatus] = useState<{ active: boolean; pid?: string } | null>(null);
    const [stats, setStats] = useState<DovecotStats | null>(null);

    const fetchData = async () => {
        setLoading(true);
        try {
            // Fetch Status
            const statusRes = await fetch('/api/v1/service/status?name=dovecot');
            if (statusRes.ok) setStatus(await statusRes.json());
            else setStatus({ active: false });

            // Fetch Stats
            const statsRes = await fetch('/api/v1/dovecot/stats');
            if (statsRes.ok) setStats(await statsRes.json());

        } catch (err) {
            console.error("Failed to fetch Dovecot data:", err);
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        fetchData();
    }, [activeVersion]);

    const handleServiceAction = async (action: 'start' | 'stop' | 'restart') => {
        const actionText = action === 'start' ? 'start' : action === 'stop' ? 'stop' : 'restart';
        if (!confirm(`Are you sure you want to ${actionText} Dovecot?`)) return;

        try {
            const response = await fetch('/api/v1/service/action', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name: 'dovecot', action }),
            });

            if (!response.ok) throw new Error('Action failed');

            // Wait for service to react
            await new Promise(resolve => setTimeout(resolve, 1500));

            // Refresh status
            const statusRes = await fetch('/api/v1/service/status?name=dovecot');
            if (statusRes.ok) setStatus(await statusRes.json());

        } catch (err) {
            console.error('Service action error:', err);
            alert('Failed to execute action');
        }
    };

    const tabs = [
        { id: 'stats', label: 'Statistics', icon: <Activity className="w-4 h-4" /> },
        { id: 'config', label: 'Configuration', icon: <Shield className="w-4 h-4" /> },
    ];

    return (
        <ServiceManagementLayout
            serviceName="Dovecot"
            serviceIcon="📬"
            versions={['default']}
            activeVersion={activeVersion}
            onVersionChange={setActiveVersion}
            onBack={onBack}
            hideSidebar={true}
        >
            {loading && !status ? (
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
                                    <h2 className="text-xl font-bold text-white mb-1">Dovecot POP3/IMAP Server</h2>
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
                                    disabled={status?.active}
                                    className="px-6 py-2.5 bg-green-600/10 hover:bg-green-600/20 text-green-400 border border-green-600/20 hover:border-green-600/30 rounded-xl font-medium transition-all duration-200 flex items-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed group"
                                >
                                    <Play className="w-4 h-4 group-hover:fill-green-400/20" /> Start
                                </button>
                                <button
                                    onClick={() => handleServiceAction('stop')}
                                    disabled={!status?.active}
                                    className="px-6 py-2.5 bg-red-600/10 hover:bg-red-600/20 text-red-400 border border-red-600/20 hover:border-red-600/30 rounded-xl font-medium transition-all duration-200 flex items-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed group"
                                >
                                    <Square className="w-4 h-4 group-hover:fill-red-400/20" /> Stop
                                </button>
                                <button
                                    onClick={() => handleServiceAction('restart')}
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
                                onClick={() => setActiveTab(tab.id)}
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
                        {activeTab === 'stats' && stats && (
                            <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                                <div className="bg-slate-900/50 p-8 rounded-2xl border border-slate-800">
                                    <h3 className="text-xl font-bold text-white mb-6 flex items-center gap-3">
                                        <div className="p-2 bg-blue-500/10 rounded-lg text-blue-400">
                                            <Activity className="w-5 h-5" />
                                        </div>
                                        General Stats
                                    </h3>
                                    <div className="space-y-6">
                                        <div className="flex justify-between items-center pb-4 border-b border-slate-800">
                                            <span className="text-slate-400 text-sm font-medium">System Uptime</span>
                                            <span className="text-white font-mono bg-slate-800/50 px-3 py-1 rounded-lg">{stats.uptime}</span>
                                        </div>
                                        <div className="flex justify-between items-center pb-4 border-b border-slate-800">
                                            <span className="text-slate-400 text-sm font-medium">Active Connections</span>
                                            <span className="text-blue-400 font-bold font-mono text-lg">{stats.connections}</span>
                                        </div>
                                        <div className="flex justify-between items-center">
                                            <span className="text-slate-400 text-sm font-medium">Total Logins</span>
                                            <span className="text-white font-mono font-bold">{stats.logins}</span>
                                        </div>
                                    </div>
                                </div>

                                <div className="bg-slate-900/50 p-8 rounded-2xl border border-slate-800">
                                    <h3 className="text-xl font-bold text-white mb-6 flex items-center gap-3">
                                        <div className="p-2 bg-green-500/10 rounded-lg text-green-400">
                                            <Users className="w-5 h-5 effect-shine" />
                                        </div>
                                        Authentication
                                    </h3>
                                    <div className="space-y-6">
                                        <div className="flex justify-between items-center pb-4 border-b border-slate-800">
                                            <span className="text-slate-400 text-sm font-medium">Auth Success</span>
                                            <span className="text-green-400 font-bold font-mono text-2xl">{stats.auth_success}</span>
                                        </div>
                                        <div className="flex justify-between items-center pb-4 border-b border-slate-800">
                                            <span className="text-slate-400 text-sm font-medium">Auth Failures</span>
                                            <span className="text-red-400 font-bold font-mono text-2xl">{stats.auth_fail}</span>
                                        </div>
                                        <div className="mt-4 pt-4">
                                            <div className="w-full bg-slate-800 rounded-full h-2 overflow-hidden">
                                                <div
                                                    className="bg-green-500 h-2 rounded-full"
                                                    style={{ width: `${(stats.auth_success / (stats.auth_success + stats.auth_fail || 1)) * 100}%` }}
                                                ></div>
                                            </div>
                                            <p className="text-xs text-slate-500 mt-2 text-right">Success Rate</p>
                                        </div>
                                    </div>
                                </div>
                            </div>
                        )}

                        {activeTab === 'config' && (
                            <div className="bg-slate-900/50 border border-slate-800 rounded-xl p-8 text-center text-slate-500">
                                <Shield className="w-12 h-12 mx-auto mb-4 opacity-20" />
                                <p>Advanced Dovecot configuration viewer coming soon.</p>
                            </div>
                        )}
                    </div>
                </div>
            )}
        </ServiceManagementLayout>
    );
}
