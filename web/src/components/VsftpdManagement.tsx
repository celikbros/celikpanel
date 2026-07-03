import { useState, useEffect } from 'react';
import { ServiceManagementLayout } from './ServiceManagementLayout';
import { Activity, Play, Square, RotateCw } from 'lucide-react';

interface VsftpdManagementProps {
    initialVersion: string;
    onBack: () => void;
}

export function VsftpdManagement({ initialVersion, onBack }: VsftpdManagementProps) {
    const [activeVersion, setActiveVersion] = useState(initialVersion || 'default');
    const [status, setStatus] = useState<{ active: boolean; pid?: string } | null>(null);
    const [loading, setLoading] = useState(false);
    const [activeTab, setActiveTab] = useState('config');

    const fetchData = async () => {
        setLoading(true);
        try {
            const statusRes = await fetch('/api/v1/service/status?name=vsftpd');
            if (statusRes.ok) setStatus(await statusRes.json());
            else setStatus({ active: false });
        } catch (err) {
            console.error("Failed to fetch vsftpd stats:", err);
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        fetchData();
    }, [activeVersion]);

    const handleServiceAction = async (action: 'start' | 'stop' | 'restart') => {
        const actionText = action === 'start' ? 'start' : action === 'stop' ? 'stop' : 'restart';
        if (!confirm(`Are you sure you want to ${actionText} vsftpd?`)) return;

        try {
            const response = await fetch('/api/v1/service/action', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name: 'vsftpd', action }),
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

    const tabs = [
        { id: 'config', label: 'Configuration', icon: <Activity className="w-4 h-4" /> },
    ];

    return (
        <ServiceManagementLayout
            serviceName="FTP Server (vsftpd)"
            serviceIcon="📂"
            versions={['default']}
            activeVersion={activeVersion}
            onVersionChange={setActiveVersion}
            onBack={onBack}
            hideSidebar={true}
        >
            {loading && !status ? (
                <div className="flex items-center justify-center py-20">
                    <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
                </div>
            ) : (
                <div className="w-full space-y-8">
                    {/* Service Status & Control Card */}
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
                                    <h2 className="text-xl font-bold text-fg mb-1">Very Secure FTP Daemon</h2>
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
                                    disabled={status?.active}
                                    className="px-6 py-2.5 bg-success/10 hover:bg-success/20 text-success border border-success/20 hover:border-success/30 rounded-xl font-medium transition-all duration-200 flex items-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed group"
                                >
                                    <Play className="w-4 h-4 group-hover:fill-green-400/20" /> Start
                                </button>
                                <button
                                    onClick={() => handleServiceAction('stop')}
                                    disabled={!status?.active}
                                    className="px-6 py-2.5 bg-danger/10 hover:bg-danger/20 text-danger border border-danger/20 hover:border-danger/30 rounded-xl font-medium transition-all duration-200 flex items-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed group"
                                >
                                    <Square className="w-4 h-4 group-hover:fill-red-400/20" /> Stop
                                </button>
                                <button
                                    onClick={() => handleServiceAction('restart')}
                                    className="px-6 py-2.5 bg-warning/10 hover:bg-warning/20 text-warning border border-warning/20 hover:border-warning/30 rounded-xl font-medium transition-all duration-200 flex items-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed group"
                                >
                                    <RotateCw className="w-4 h-4 group-hover:rotate-180 transition-transform duration-700" /> Restart
                                </button>
                            </div>
                        </div>
                    </div>

                    {/* Navigation Tabs */}
                    <div className="flex items-center gap-4 border-b border-border pb-1">
                        {tabs.map(tab => (
                            <button
                                key={tab.id}
                                onClick={() => setActiveTab(tab.id)}
                                className={`px-4 py-2 text-sm font-medium rounded-t-lg border-b-2 transition-colors flex items-center gap-2 ${activeTab === tab.id
                                        ? 'border-primary text-primary bg-primary/5'
                                        : 'border-transparent text-fg-muted hover:text-fg hover:bg-surface-2/50'
                                    }`}
                            >
                                {tab.icon}
                                {tab.label}
                            </button>
                        ))}
                    </div>

                    {/* Content Area */}
                    <div className="min-h-[500px]">
                        {activeTab === 'config' && (
                            <div className="bg-surface/50 border border-border rounded-xl p-12 text-center">
                                <Activity className="w-16 h-16 mx-auto mb-6 text-fg-subtle" />
                                <h3 className="text-xl font-bold text-fg mb-2">FTP Configuration</h3>
                                <p className="text-fg-subtle">Advanced FTP server configuration settings are coming soon.</p>
                            </div>
                        )}
                    </div>
                </div>
            )}
        </ServiceManagementLayout>
    );
}
