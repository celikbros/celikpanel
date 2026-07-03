import { useState, useEffect } from 'react';
import { ServiceManagementLayout } from './ServiceManagementLayout';
import { Settings, Shield, Activity, Play, Square, RotateCw } from 'lucide-react';

interface NginxManagementProps {
    initialVersion: string; // Nginx usually has one version, but we keep the prop for consistency
    onBack: () => void;
}

interface NginxGlobalConfig {
    worker_processes: string;
    worker_connections: string;
    keepalive_timeout: string;
    client_max_body_size: string;
    server_tokens: string;
    gzip: string;
}

interface NginxSSLConfig {
    ssl_ciphers: string;
    ssl_protocols: string;
    ssl_prefer_server_ciphers: string;
}

interface NginxRateLimit {
    name: string;
    zone: string;
    size: string;
    rate: string;
}

export function NginxManagement({ initialVersion, onBack }: NginxManagementProps) {
    const [activeVersion, setActiveVersion] = useState(initialVersion || 'default');
    const [activeTab, setActiveTab] = useState('global');
    const [loading, setLoading] = useState(false);
    const [serviceStatus, setServiceStatus] = useState<{ active: boolean } | null>(null);

    const [globalConfig, setGlobalConfig] = useState<NginxGlobalConfig | null>(null);
    const [sslConfig, setSSLConfig] = useState<NginxSSLConfig | null>(null);
    const [rateLimits, setRateLimits] = useState<NginxRateLimit[]>([]);

    useEffect(() => {
        const fetchData = async () => {
            setLoading(true);
            try {
                // Fetch service status
                const statusRes = await fetch('/api/v1/service/status?name=nginx');
                if (statusRes.ok) setServiceStatus(await statusRes.json());

                // Fetch Global Config
                const globalRes = await fetch('/api/v1/nginx/global');
                if (globalRes.ok) setGlobalConfig(await globalRes.json());

                // Fetch SSL Config
                const sslRes = await fetch('/api/v1/nginx/ssl');
                if (sslRes.ok) setSSLConfig(await sslRes.json());

                // Fetch Rate Limits
                const rateRes = await fetch('/api/v1/nginx/ratelimits');
                if (rateRes.ok) setRateLimits(await rateRes.json());

            } catch (err) {
                console.error("Failed to fetch Nginx data:", err);
                alert("Failed to load Nginx configuration");
            } finally {
                setLoading(false);
            }
        };

        fetchData();
    }, []);

    const handleServiceAction = async (action: 'start' | 'stop' | 'restart') => {
        if (!confirm(`Are you sure you want to ${action} Nginx?`)) return;

        try {
            await fetch('/api/v1/service/action', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name: 'nginx', action }),
            });

            // Wait and refresh status
            await new Promise(resolve => setTimeout(resolve, 1000));
            const statusRes = await fetch('/api/v1/service/status?name=nginx');
            if (statusRes.ok) {
                setServiceStatus(await statusRes.json());
            }
        } catch (err) {
            alert('Failed to execute action');
        }
    };

    const handleSaveGlobalConfig = async () => {
        if (!globalConfig) return;
        if (!confirm("Apply changes to Nginx global configuration?")) return;

        try {
            await fetch('/api/v1/nginx/global', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ config: globalConfig }),
            });
            alert("Global configuration saved successfully");
        } catch (err) {
            alert("Failed to save configuration");
        }
    };

    const handleSaveSSLConfig = async () => {
        if (!sslConfig) return;
        if (!confirm("Apply changes to Nginx SSL configuration?")) return;

        try {
            await fetch('/api/v1/nginx/ssl', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ config: sslConfig }),
            });
            alert("SSL configuration saved successfully");
        } catch (err) {
            alert("Failed to save configuration");
        }
    };

    const tabs = [
        { id: 'global', label: 'Global Settings', icon: <Settings className="w-4 h-4" /> },
        { id: 'ssl', label: 'SSL Configuration', icon: <Shield className="w-4 h-4" /> },
        { id: 'ratelimits', label: 'Rate Limiting', icon: <Activity className="w-4 h-4" /> },
    ];

    return (
        <ServiceManagementLayout
            serviceName="Nginx"
            serviceIcon="🔄"
            versions={['default']}
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
                                <div className={`w-16 h-16 rounded-2xl flex items-center justify-center shadow-lg transition-all duration-500 ${serviceStatus?.active
                                        ? 'bg-green-500/10 text-green-400 shadow-green-500/20 border border-green-500/20'
                                        : 'bg-red-500/10 text-red-500 shadow-red-500/20 border border-red-500/20'
                                    }`}>
                                    <Activity className={`w-8 h-8 ${serviceStatus?.active ? 'animate-pulse' : ''}`} />
                                </div>
                                <div>
                                    <h2 className="text-xl font-bold text-white mb-1">Nginx Web Server</h2>
                                    <div className="flex items-center gap-2">
                                        <div className={`w-2 h-2 rounded-full ${serviceStatus?.active ? 'bg-green-400' : 'bg-red-500'}`}></div>
                                        <span className={`text-sm font-medium ${serviceStatus?.active ? 'text-green-400' : 'text-red-400'}`}>
                                            {serviceStatus?.active ? 'Active & Running' : 'Stopped'}
                                        </span>
                                    </div>
                                </div>
                            </div>

                            <div className="flex flex-wrap items-center gap-3">
                                <button onClick={() => handleServiceAction('start')} className="px-6 py-2.5 bg-green-600/10 hover:bg-green-600/20 text-green-400 border border-green-600/20 hover:border-green-600/30 rounded-xl font-medium transition-all duration-200 flex items-center gap-2 group">
                                    <Play className="w-4 h-4 group-hover:fill-green-400/20" /> Start
                                </button>
                                <button onClick={() => handleServiceAction('stop')} className="px-6 py-2.5 bg-red-600/10 hover:bg-red-600/20 text-red-400 border border-red-600/20 hover:border-red-600/30 rounded-xl font-medium transition-all duration-200 flex items-center gap-2 group">
                                    <Square className="w-4 h-4 group-hover:fill-red-400/20" /> Stop
                                </button>
                                <button onClick={() => handleServiceAction('restart')} className="px-6 py-2.5 bg-yellow-600/10 hover:bg-yellow-600/20 text-yellow-400 border border-yellow-600/20 hover:border-yellow-600/30 rounded-xl font-medium transition-all duration-200 flex items-center gap-2 group">
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
                        {activeTab === 'global' && globalConfig && (
                            <div className="bg-slate-900/50 border border-slate-800 rounded-xl p-6">
                                <h3 className="text-lg font-semibold text-white mb-6 flex items-center gap-2">
                                    <Settings className="w-5 h-5 text-blue-400" />
                                    Global Configuration
                                </h3>
                                <div className="grid grid-cols-1 md:grid-cols-2 gap-6 max-w-4xl">
                                    <div>
                                        <label className="block text-sm font-medium text-gray-400 mb-2">Worker Processes</label>
                                        <input
                                            type="text"
                                            value={globalConfig.worker_processes}
                                            onChange={(e) => setGlobalConfig({ ...globalConfig, worker_processes: e.target.value })}
                                            className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500/50 transition-all"
                                        />
                                    </div>
                                    <div>
                                        <label className="block text-sm font-medium text-gray-400 mb-2">Worker Connections</label>
                                        <input
                                            type="text"
                                            value={globalConfig.worker_connections}
                                            onChange={(e) => setGlobalConfig({ ...globalConfig, worker_connections: e.target.value })}
                                            className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500/50 transition-all"
                                        />
                                    </div>
                                    <div>
                                        <label className="block text-sm font-medium text-gray-400 mb-2">Keepalive Timeout</label>
                                        <input
                                            type="text"
                                            value={globalConfig.keepalive_timeout}
                                            onChange={(e) => setGlobalConfig({ ...globalConfig, keepalive_timeout: e.target.value })}
                                            className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500/50 transition-all"
                                        />
                                    </div>
                                    <div>
                                        <label className="block text-sm font-medium text-gray-400 mb-2">Client Max Body Size</label>
                                        <input
                                            type="text"
                                            value={globalConfig.client_max_body_size}
                                            onChange={(e) => setGlobalConfig({ ...globalConfig, client_max_body_size: e.target.value })}
                                            className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500/50 transition-all"
                                        />
                                    </div>
                                    <div>
                                        <label className="block text-sm font-medium text-gray-400 mb-2">Server Tokens</label>
                                        <select
                                            value={globalConfig.server_tokens}
                                            onChange={(e) => setGlobalConfig({ ...globalConfig, server_tokens: e.target.value })}
                                            className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500/50 transition-all appearance-none cursor-pointer"
                                        >
                                            <option value="on">On</option>
                                            <option value="off">Off</option>
                                        </select>
                                    </div>
                                    <div>
                                        <label className="block text-sm font-medium text-gray-400 mb-2">Gzip Compression</label>
                                        <select
                                            value={globalConfig.gzip}
                                            onChange={(e) => setGlobalConfig({ ...globalConfig, gzip: e.target.value })}
                                            className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500/50 transition-all appearance-none cursor-pointer"
                                        >
                                            <option value="on">On</option>
                                            <option value="off">Off</option>
                                        </select>
                                    </div>
                                </div>
                                <div className="mt-8 pt-6 border-t border-slate-800 flex justify-end">
                                    <button onClick={handleSaveGlobalConfig} className="px-6 py-2.5 bg-blue-600 hover:bg-blue-700 text-white rounded-xl font-medium transition-colors shadow-lg shadow-blue-600/20">
                                        Save Changes
                                    </button>
                                </div>
                            </div>
                        )}

                        {activeTab === 'ssl' && sslConfig && (
                            <div className="bg-slate-900/50 border border-slate-800 rounded-xl p-6">
                                <h3 className="text-lg font-semibold text-white mb-6 flex items-center gap-2">
                                    <Shield className="w-5 h-5 text-blue-400" />
                                    SSL Configuration
                                </h3>
                                <div className="space-y-6 max-w-4xl">
                                    <div>
                                        <label className="block text-sm font-medium text-gray-400 mb-2">SSL Protocols</label>
                                        <input
                                            type="text"
                                            value={sslConfig.ssl_protocols}
                                            onChange={(e) => setSSLConfig({ ...sslConfig, ssl_protocols: e.target.value })}
                                            className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:border-blue-500 focus:outline-none font-mono text-sm shadow-sm"
                                        />
                                        <p className="mt-1 text-xs text-gray-500">Supported protocols (e.g. TLSv1.2 TLSv1.3)</p>
                                    </div>
                                    <div>
                                        <label className="block text-sm font-medium text-gray-400 mb-2">SSL Ciphers</label>
                                        <textarea
                                            value={sslConfig.ssl_ciphers}
                                            onChange={(e) => setSSLConfig({ ...sslConfig, ssl_ciphers: e.target.value })}
                                            rows={4}
                                            className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:border-blue-500 focus:outline-none font-mono text-sm shadow-sm"
                                        />
                                    </div>
                                    <div>
                                        <label className="block text-sm font-medium text-gray-400 mb-2">Prefer Server Ciphers</label>
                                        <div className="flex items-center gap-4">
                                            <label className="flex items-center gap-2 cursor-pointer">
                                                <input
                                                    type="radio"
                                                    checked={sslConfig.ssl_prefer_server_ciphers === 'on'}
                                                    onChange={() => setSSLConfig({ ...sslConfig, ssl_prefer_server_ciphers: 'on' })}
                                                    className="text-blue-600 focus:ring-blue-500 bg-slate-900 border-slate-700"
                                                />
                                                <span className="text-white">On</span>
                                            </label>
                                            <label className="flex items-center gap-2 cursor-pointer">
                                                <input
                                                    type="radio"
                                                    checked={sslConfig.ssl_prefer_server_ciphers === 'off'}
                                                    onChange={() => setSSLConfig({ ...sslConfig, ssl_prefer_server_ciphers: 'off' })}
                                                    className="text-blue-600 focus:ring-blue-500 bg-slate-900 border-slate-700"
                                                />
                                                <span className="text-white">Off</span>
                                            </label>
                                        </div>
                                    </div>
                                </div>
                                <div className="mt-8 pt-6 border-t border-slate-800 flex justify-end">
                                    <button onClick={handleSaveSSLConfig} className="px-6 py-2.5 bg-blue-600 hover:bg-blue-700 text-white rounded-xl font-medium transition-colors shadow-lg shadow-blue-600/20">
                                        Save SSL Settings
                                    </button>
                                </div>
                            </div>
                        )}

                        {activeTab === 'ratelimits' && (
                            <div className="bg-slate-900/50 border border-slate-800 rounded-xl p-6">
                                <h3 className="text-lg font-semibold text-white mb-6 flex items-center gap-2">
                                    <Activity className="w-5 h-5 text-blue-400" />
                                    Rate Limiting Zones
                                </h3>
                                <div className="grid gap-4">
                                    {rateLimits.map((limit, idx) => (
                                        <div key={idx} className="bg-slate-950 p-5 rounded-xl border border-slate-800/50 flex justify-between items-center hover:border-blue-500/30 transition-all group">
                                            <div>
                                                <div className="flex items-center gap-3 mb-2">
                                                    <h4 className="font-bold text-white font-mono text-lg">{limit.name}</h4>
                                                    <span className="px-2 py-0.5 rounded text-xs bg-slate-800 text-gray-400">Zone: {limit.zone}</span>
                                                </div>
                                                <div className="flex items-center gap-6 text-sm text-gray-400">
                                                    <div>
                                                        Size: <span className="text-blue-400 font-mono">{limit.size}</span>
                                                    </div>
                                                    <div>
                                                        Rate: <span className="text-green-400 font-mono">{limit.rate || 'N/A'}</span>
                                                    </div>
                                                </div>
                                            </div>
                                            <button className="px-4 py-2 rounded-lg bg-slate-900 text-blue-400 hover:text-white hover:bg-blue-600/20 border border-slate-800 hover:border-blue-500/50 transition-all text-sm font-medium">Edit Zone</button>
                                        </div>
                                    ))}
                                    {rateLimits.length === 0 && (
                                        <div className="text-center py-12 text-gray-500">
                                            No rate limits configured.
                                        </div>
                                    )}
                                </div>
                            </div>
                        )}
                    </div>
                </div>
            )}
        </ServiceManagementLayout>
    );
}
