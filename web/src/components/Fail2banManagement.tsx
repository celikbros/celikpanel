import { useState, useEffect } from 'react';
import { ServiceManagementLayout } from './ServiceManagementLayout';
import { Shield, Play, Square, RotateCw, Lock, AlertTriangle } from 'lucide-react';

interface Fail2banManagementProps {
    initialVersion: string;
    onBack: () => void;
}

interface Fail2banJail {
    name: string;
    enabled: boolean;
    active: boolean;
    banned: number;
}

interface Fail2banBannedIP {
    ip: string;
    jail: string;
    time: string;
    country: string;
}

interface Fail2banConfig {
    ban_time: string;
    find_time: string;
    max_retry: number;
    ignore_ip: string[];
}

export function Fail2banManagement({ initialVersion, onBack }: Fail2banManagementProps) {
    const [activeVersion, setActiveVersion] = useState(initialVersion || 'default');
    const [activeTab, setActiveTab] = useState('jails');
    const [loading, setLoading] = useState(false);
    const [status, setStatus] = useState<{ active: boolean; pid?: string } | null>(null);

    const [jails, setJails] = useState<Fail2banJail[]>([]);
    const [bannedIPs, setBannedIPs] = useState<Fail2banBannedIP[]>([]);
    const [config, setConfig] = useState<Fail2banConfig | null>(null);

    const fetchData = async () => {
        setLoading(true);
        try {
            // Fetch Status
            const statusRes = await fetch('/api/v1/service/status?name=fail2ban');
            if (statusRes.ok) setStatus(await statusRes.json());
            else setStatus({ active: false });

            // Fetch Jails
            const jailsRes = await fetch('/api/v1/fail2ban/jails');
            if (jailsRes.ok) setJails(await jailsRes.json());

            // Fetch Banned IPs
            const bannedRes = await fetch('/api/v1/fail2ban/banned');
            if (bannedRes.ok) setBannedIPs(await bannedRes.json());

            // Fetch Config
            const configRes = await fetch('/api/v1/fail2ban/config');
            if (configRes.ok) setConfig(await configRes.json());

        } catch (err) {
            console.error("Failed to fetch Fail2ban data:", err);
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        fetchData();
    }, [activeVersion]);

    const handleServiceAction = async (action: 'start' | 'stop' | 'restart') => {
        const actionText = action === 'start' ? 'start' : action === 'stop' ? 'stop' : 'restart';
        if (!confirm(`Are you sure you want to ${actionText} Fail2ban?`)) return;

        try {
            const response = await fetch('/api/v1/service/action', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name: 'fail2ban', action }),
            });

            if (!response.ok) throw new Error('Action failed');

            // Wait for service to react
            await new Promise(resolve => setTimeout(resolve, 1500));

            // Refresh status
            const statusRes = await fetch('/api/v1/service/status?name=fail2ban');
            if (statusRes.ok) setStatus(await statusRes.json());
        } catch (err) {
            console.error('Service action error:', err);
            alert('Failed to execute action');
        }
    };

    const handleUnbanIP = async (ip: string, jail: string) => {
        if (!confirm(`Unban IP ${ip} from jail ${jail}?`)) return;

        try {
            await fetch('/api/v1/fail2ban/banned', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ ip, jail }),
            });

            // Refresh list
            const bannedRes = await fetch('/api/v1/fail2ban/banned');
            if (bannedRes.ok) setBannedIPs(await bannedRes.json());
        } catch (err) {
            alert('Failed to unban IP');
        }
    };

    const handleToggleJail = async (name: string, enabled: boolean) => {
        if (!confirm(`${enabled ? 'Enable' : 'Disable'} jail ${name}?`)) return;

        try {
            await fetch('/api/v1/fail2ban/jails', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name, enabled }),
            });

            // Refresh list
            const jailsRes = await fetch('/api/v1/fail2ban/jails');
            if (jailsRes.ok) setJails(await jailsRes.json());
        } catch (err) {
            alert('Failed to update jail');
        }
    };

    const tabs = [
        { id: 'jails', label: 'Jails', icon: <Lock className="w-4 h-4" /> },
        { id: 'banned', label: 'Banned IPs', icon: <AlertTriangle className="w-4 h-4" /> },
        { id: 'config', label: 'Configuration', icon: <Shield className="w-4 h-4" /> },
    ];

    return (
        <ServiceManagementLayout
            serviceName="Fail2ban"
            serviceIcon="🚫"
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
                                    <Shield className={`w-8 h-8 ${status?.active ? 'animate-pulse' : ''}`} />
                                </div>
                                <div>
                                    <h2 className="text-xl font-bold text-white mb-1">Fail2ban Intrusion Prevention</h2>
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
                        {activeTab === 'jails' && (
                            <div className="grid gap-4 md:grid-cols-2">
                                {jails.map(jail => (
                                    <div key={jail.name} className="bg-slate-900/50 p-6 rounded-xl border border-slate-800 flex justify-between items-center group hover:border-blue-500/30 transition-all">
                                        <div className="flex items-center gap-4">
                                            <div className={`p-3 rounded-xl ${jail.active ? 'bg-green-500/20 text-green-400' : 'bg-slate-700/30 text-slate-500'}`}>
                                                <Lock className="w-6 h-6" />
                                            </div>
                                            <div>
                                                <h4 className="font-bold text-white text-lg">{jail.name}</h4>
                                                <p className="text-sm text-slate-400 flex items-center gap-2">
                                                    Status: <span className={`font-medium ${jail.active ? 'text-green-400' : 'text-slate-500'}`}>{jail.active ? 'Active' : 'Inactive'}</span>
                                                    <span className="w-1 h-1 bg-slate-600 rounded-full"></span>
                                                    Banned: <span className="text-red-400 font-bold">{jail.banned}</span>
                                                </p>
                                            </div>
                                        </div>
                                        <div className="flex items-center gap-3">
                                            <label className="relative inline-flex items-center cursor-pointer">
                                                <input
                                                    type="checkbox"
                                                    className="sr-only peer"
                                                    checked={jail.enabled}
                                                    onChange={(e) => handleToggleJail(jail.name, e.target.checked)}
                                                />
                                                <div className="w-11 h-6 bg-slate-700 peer-focus:outline-none rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all peer-checked:bg-blue-600 border border-slate-600"></div>
                                            </label>
                                        </div>
                                    </div>
                                ))}
                            </div>
                        )}

                        {activeTab === 'banned' && (
                            <div className="bg-slate-900/50 rounded-xl border border-slate-800 overflow-hidden">
                                <table className="w-full text-left text-sm text-gray-400">
                                    <thead className="bg-slate-950 text-slate-300 uppercase text-xs font-bold tracking-wider">
                                        <tr>
                                            <th className="px-6 py-4">IP Address</th>
                                            <th className="px-6 py-4">Jail</th>
                                            <th className="px-6 py-4">Ban Time</th>
                                            <th className="px-6 py-4">Country</th>
                                            <th className="px-6 py-4 text-right">Action</th>
                                        </tr>
                                    </thead>
                                    <tbody className="divide-y divide-slate-800">
                                        {bannedIPs.map((ip, idx) => (
                                            <tr key={idx} className="hover:bg-slate-800/50 transition-colors">
                                                <td className="px-6 py-4 font-mono text-white font-medium">{ip.ip}</td>
                                                <td className="px-6 py-4 text-blue-400 bg-blue-500/5 rounded lg:bg-transparent">{ip.jail}</td>
                                                <td className="px-6 py-4">{ip.time}</td>
                                                <td className="px-6 py-4">{ip.country}</td>
                                                <td className="px-6 py-4 text-right">
                                                    <button
                                                        onClick={() => handleUnbanIP(ip.ip, ip.jail)}
                                                        className="text-red-400 hover:text-red-300 font-medium text-xs border border-red-500/20 bg-red-500/10 px-3 py-1.5 rounded-lg hover:bg-red-500/20 transition-all shadow-sm shadow-red-500/5"
                                                    >
                                                        Unban
                                                    </button>
                                                </td>
                                            </tr>
                                        ))}
                                        {bannedIPs.length === 0 && (
                                            <tr>
                                                <td colSpan={5} className="px-6 py-12 text-center text-slate-500">
                                                    <div className="flex flex-col items-center gap-3">
                                                        <Shield className="w-12 h-12 text-slate-700" />
                                                        <p>No banned IPs found. System is clean.</p>
                                                    </div>
                                                </td>
                                            </tr>
                                        )}
                                    </tbody>
                                </table>
                            </div>
                        )}

                        {activeTab === 'config' && config && (
                            <div className="space-y-8 max-w-4xl mx-auto">
                                <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
                                    <div className="bg-slate-900/50 p-6 rounded-xl border border-slate-800">
                                        <label className="block text-sm font-medium text-slate-400 mb-2">Ban Time</label>
                                        <input
                                            type="text"
                                            value={config.ban_time}
                                            readOnly
                                            className="w-full bg-slate-950 border border-slate-800 rounded-lg px-4 py-3 text-white font-mono focus:border-blue-500 focus:outline-none"
                                        />
                                        <p className="text-xs text-slate-500 mt-2">Duration of ban (e.g. 10m, 1h)</p>
                                    </div>
                                    <div className="bg-slate-900/50 p-6 rounded-xl border border-slate-800">
                                        <label className="block text-sm font-medium text-slate-400 mb-2">Find Time</label>
                                        <input
                                            type="text"
                                            value={config.find_time}
                                            readOnly
                                            className="w-full bg-slate-950 border border-slate-800 rounded-lg px-4 py-3 text-white font-mono focus:border-blue-500 focus:outline-none"
                                        />
                                        <p className="text-xs text-slate-500 mt-2">Window to count failures</p>
                                    </div>
                                    <div className="bg-slate-900/50 p-6 rounded-xl border border-slate-800">
                                        <label className="block text-sm font-medium text-slate-400 mb-2">Max Retry</label>
                                        <input
                                            type="number"
                                            value={config.max_retry}
                                            readOnly
                                            className="w-full bg-slate-950 border border-slate-800 rounded-lg px-4 py-3 text-white font-mono focus:border-blue-500 focus:outline-none"
                                        />
                                        <p className="text-xs text-slate-500 mt-2">Failures before ban</p>
                                    </div>
                                </div>
                                <div className="bg-slate-900/50 p-6 rounded-xl border border-slate-800">
                                    <label className="block text-sm font-medium text-slate-400 mb-2">Whitelisted IPs (Ignore IP)</label>
                                    <textarea
                                        value={config.ignore_ip.join('\n')}
                                        readOnly
                                        rows={6}
                                        className="w-full bg-slate-950 border border-slate-800 rounded-lg px-4 py-3 text-white focus:border-blue-500 focus:outline-none font-mono text-sm leading-relaxed"
                                    />
                                    <p className="text-xs text-slate-500 mt-2">One IP or CIDR per line. These IPs will never be banned.</p>
                                </div>
                                <div className="flex justify-end pt-4">
                                    <button className="px-6 py-3 bg-blue-600 text-white rounded-xl hover:bg-blue-700 font-medium shadow-lg shadow-blue-500/20 transition-all hover:scale-105">Save Configuration</button>
                                </div>
                            </div>
                        )}
                    </div>
                </div>
            )}
        </ServiceManagementLayout>
    );
}
