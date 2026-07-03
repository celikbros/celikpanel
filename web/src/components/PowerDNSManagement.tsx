import { useState, useEffect } from 'react';
import { ServiceManagementLayout } from './ServiceManagementLayout';
import { Settings, Activity, RotateCw, Play, Square, CheckCircle } from 'lucide-react';
import { showToast } from './Toast';

interface PowerDNSManagementProps {
    initialVersion: string;
    onBack: () => void;
}

export function PowerDNSManagement({ initialVersion, onBack }: PowerDNSManagementProps) {
    const [activeVersion, setActiveVersion] = useState(initialVersion || 'default');
    const [serviceStatus, setServiceStatus] = useState<{ active: boolean } | null>(null);
    const [actionLoading, setActionLoading] = useState(false);
    const [repairLoading, setRepairLoading] = useState(false);

    useEffect(() => {
        fetchStatus();
    }, []);

    const fetchStatus = async () => {
        try {
            const statusRes = await fetch('/api/v1/service/status?name=pdns');
            if (statusRes.ok) setServiceStatus(await statusRes.json());
        } catch (err) {
            console.error("Failed to fetch status:", err);
        }
    };

    const handleServiceAction = async (action: 'start' | 'stop' | 'restart') => {
        setActionLoading(true);
        try {
            const res = await fetch('/api/v1/service/action', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name: 'pdns', action }),
            });
            const data = await res.json().catch(() => ({}));

            if (!res.ok) throw new Error(data.error || "API call failed");

            // Wait and refresh status
            await new Promise(resolve => setTimeout(resolve, 1500));
            const newStatus = await fetch('/api/v1/service/status?name=pdns').then(r => r.json());
            setServiceStatus(newStatus);

            if (action === 'start' && !newStatus.active) {
                showToast('error', 'PowerDNS failed to start. Try Auto-Repair.');
            } else {
                showToast('success', `PowerDNS ${action} successful`);
            }
        } catch (err: any) {
            showToast('error', err.message || 'Failed to execute action');
        } finally {
            setActionLoading(false);
        }
    };

    const handleRepairConfig = async () => {
        if (!confirm("This will overwrite PowerDNS configuration. Continue?")) return;
        setRepairLoading(true);
        try {
            const res = await fetch('/api/v1/pdns/configure', { method: 'POST' });
            const data = await res.json().catch(() => ({}));

            if (res.ok) {
                showToast('success', "PowerDNS configuration repaired successfully");
                setTimeout(fetchStatus, 3000);
            } else {
                throw new Error(data.error || "Configuration failed");
            }
        } catch (err: any) {
            showToast('error', err.message || "Failed to repair configuration");
        } finally {
            setRepairLoading(false);
        }
    };

    return (
        <ServiceManagementLayout
            serviceName="PowerDNS"
            serviceIcon="⚡"
            versions={['default']}
            activeVersion={activeVersion}
            onVersionChange={setActiveVersion}
            onBack={onBack}
            activeTab="status"
            onTabChange={() => { }}
            tabs={[]}
            hideSidebar={true}
        >
            <div className="w-full space-y-8">
                {/* Header Section */}
                <div>
                    <h2 className="text-2xl font-bold text-white mb-2 flex items-center gap-2">
                        <Activity className="w-6 h-6 text-blue-400" />
                        Service Status & Control
                    </h2>
                    <p className="text-gray-400">Manage the PowerDNS service state and configuration.</p>
                </div>

                {/* Status Card - Full Width */}
                <div className="bg-gray-800/40 border border-gray-700 rounded-xl p-8 flex flex-col md:flex-row items-center justify-between gap-6">
                    <div className="flex items-center gap-6">
                        <div className={`w-16 h-16 rounded-full flex items-center justify-center shadow-lg transition-all duration-500 ${serviceStatus?.active ? 'bg-green-500/10 text-green-500 shadow-green-500/20' : 'bg-red-500/10 text-red-500 shadow-red-500/20'}`}>
                            <Activity className={`w-8 h-8 ${actionLoading ? 'animate-pulse' : ''}`} />
                        </div>
                        <div>
                            <h3 className="text-lg font-bold text-gray-100">PowerDNS Service</h3>
                            <div className="flex items-center gap-2 mt-1">
                                <div className={`w-2.5 h-2.5 rounded-full transition-colors duration-300 ${serviceStatus?.active ? 'bg-green-500 animate-pulse' : 'bg-red-500'}`} />
                                <span className={`text-sm font-medium transition-colors duration-300 ${serviceStatus?.active ? 'text-green-400' : 'text-red-400'}`}>
                                    {actionLoading ? 'Processing...' : (serviceStatus?.active ? 'Active & Running' : 'Stopped / Failed')}
                                </span>
                            </div>
                        </div>
                    </div>

                    <div className="flex gap-3 w-full md:w-auto">
                        <button
                            onClick={() => handleServiceAction('start')}
                            disabled={actionLoading || repairLoading || serviceStatus?.active}
                            className="flex-1 md:flex-none px-6 py-3 bg-gray-800 hover:bg-gray-700 text-white rounded-lg border border-gray-600 transition-all font-medium flex items-center justify-center gap-2 active:scale-95 disabled:opacity-50 disabled:cursor-not-allowed"
                        >
                            <Play className="w-4 h-4 text-green-400" /> Start
                        </button>
                        <button
                            onClick={() => handleServiceAction('stop')}
                            disabled={actionLoading || repairLoading || !serviceStatus?.active}
                            className="flex-1 md:flex-none px-6 py-3 bg-gray-800 hover:bg-gray-700 text-white rounded-lg border border-gray-600 transition-all font-medium flex items-center justify-center gap-2 active:scale-95 disabled:opacity-50 disabled:cursor-not-allowed"
                        >
                            <Square className="w-4 h-4 text-red-400" /> Stop
                        </button>
                        <button
                            onClick={() => handleServiceAction('restart')}
                            disabled={actionLoading || repairLoading}
                            className="flex-1 md:flex-none px-6 py-3 bg-gray-800 hover:bg-gray-700 text-white rounded-lg border border-gray-600 transition-all font-medium flex items-center justify-center gap-2 active:scale-95 disabled:opacity-50 disabled:cursor-not-allowed"
                        >
                            <RotateCw className={`w-4 h-4 text-yellow-400 ${actionLoading ? 'animate-spin' : ''}`} /> Restart
                        </button>
                    </div>
                </div>

                {/* Repair Section - Distinct UI */}
                <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
                    <div className="lg:col-span-2 bg-gradient-to-br from-blue-900/10 to-transparent border border-blue-900/30 rounded-xl p-8 relative overflow-hidden group">
                        <div className="absolute top-0 right-0 p-32 bg-blue-500/5 blur-[100px] rounded-full group-hover:bg-blue-500/10 transition-all duration-700"></div>

                        <div className="relative z-10">
                            <div className="flex items-center gap-3 mb-6">
                                <div className="p-2 bg-blue-500/10 rounded-lg">
                                    <Settings className="w-6 h-6 text-blue-400" />
                                </div>
                                <h3 className="text-xl font-bold text-white">One-Click Configuration Repair</h3>
                            </div>

                            <p className="text-gray-300 leading-relaxed mb-8 max-w-2xl">
                                Automatically resolve common issues by reconfiguring the database connection,
                                fixing port conflicts (systemd-resolved), and removing conflicting backends.
                            </p>

                            <button
                                onClick={handleRepairConfig}
                                disabled={repairLoading || actionLoading}
                                className="px-8 py-4 bg-blue-600 hover:bg-blue-500 text-white rounded-xl font-bold shadow-lg shadow-blue-900/20 transition-all flex items-center gap-3 active:scale-95 disabled:opacity-50 disabled:cursor-not-allowed w-full sm:w-auto justify-center"
                            >
                                {repairLoading ? <RotateCw className="w-5 h-5 animate-spin" /> : <div className="bg-white/20 p-1 rounded-full"><RotateCw className="w-4 h-4" /></div>}
                                {repairLoading ? 'Repairing System...' : 'Run Auto-Repair Tool'}
                            </button>
                        </div>
                    </div>

                    <div className="bg-gray-900 border border-gray-800 rounded-xl p-6">
                        <h4 className="font-bold text-gray-200 mb-4 flex items-center gap-2">
                            Repair Actions
                        </h4>
                        <ul className="space-y-4">
                            {[
                                "Configure PostgreSQL Backend",
                                "Remove Bind Backend Conflict",
                                "Fix Port 53 (Stub Listener)",
                                "Restart Service"
                            ].map((item, i) => (
                                <li key={i} className="flex items-center gap-3 text-sm text-gray-400">
                                    <CheckCircle className="w-4 h-4 text-gray-600 group-hover:text-green-500 transition-colors" />
                                    {item}
                                </li>
                            ))}
                        </ul>
                    </div>
                </div>
            </div>
        </ServiceManagementLayout>
    );
}
