import { useState, useEffect } from 'react';
import { ServiceManagementLayout } from './ServiceManagementLayout';
import { Mail, Activity, Play, Square, RotateCw, Trash2, RefreshCw } from 'lucide-react';

interface PostfixManagementProps {
    initialVersion: string;
    onBack: () => void;
}

interface PostfixQueueItem {
    id: string;
    size: string;
    sender: string;
    arrival: string;
    status: string;
}

interface PostfixSummary {
    active: number;
    deferred: number;
    hold: number;
    corrupt: number;
}

export function PostfixManagement({ initialVersion, onBack }: PostfixManagementProps) {
    const [activeVersion, setActiveVersion] = useState(initialVersion || 'default');
    const [activeTab, setActiveTab] = useState('queue');
    const [loading, setLoading] = useState(false);

    const [queue, setQueue] = useState<PostfixQueueItem[]>([]);
    const [summary, setSummary] = useState<PostfixSummary | null>(null);

    const [status, setStatus] = useState<{ active: boolean; pid?: string } | null>(null);

    const fetchData = async () => {
        setLoading(true);
        try {
            // Fetch Status
            const statusRes = await fetch('/api/v1/service/status?name=postfix');
            if (statusRes.ok) setStatus(await statusRes.json());
            else setStatus({ active: false });

            // Fetch Queue
            const queueRes = await fetch('/api/v1/postfix/queue');
            if (queueRes.ok) setQueue(await queueRes.json());

            // Fetch Summary
            const summaryRes = await fetch('/api/v1/postfix/summary');
            if (summaryRes.ok) setSummary(await summaryRes.json());

        } catch (err) {
            console.error("Failed to fetch Postfix data:", err);
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        fetchData();
    }, [activeVersion]);

    const handleServiceAction = async (action: 'start' | 'stop' | 'restart') => {
        const actionText = action === 'start' ? 'start' : action === 'stop' ? 'stop' : 'restart';
        if (!confirm(`Are you sure you want to ${actionText} Postfix?`)) return;

        try {
            const response = await fetch('/api/v1/service/action', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name: 'postfix', action }),
            });

            if (!response.ok) throw new Error('Action failed');

            // Wait for service to react
            await new Promise(resolve => setTimeout(resolve, 1500));

            // Refresh status
            const statusRes = await fetch('/api/v1/service/status?name=postfix');
            if (statusRes.ok) setStatus(await statusRes.json());

        } catch (err) {
            console.error('Service action error:', err);
            alert('Failed to execute action');
        }
    };

    const handleQueueAction = async (action: string, id?: string) => {
        if (!confirm(`Are you sure you want to ${action} ${id ? 'message ' + id : 'all messages'}?`)) return;

        try {
            await fetch('/api/v1/postfix/queue', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ action, id }),
            });

            // Refresh Queue
            const queueRes = await fetch('/api/v1/postfix/queue');
            if (queueRes.ok) setQueue(await queueRes.json());

            const summaryRes = await fetch('/api/v1/postfix/summary');
            if (summaryRes.ok) setSummary(await summaryRes.json());

        } catch (err) {
            alert("Failed to execute queue action");
        }
    };

    const tabs = [
        { id: 'queue', label: 'Mail Queue', icon: <Mail className="w-4 h-4" /> },
        { id: 'logs', label: 'Mail Logs', icon: <Activity className="w-4 h-4" /> },
    ];

    return (
        <ServiceManagementLayout
            serviceName="Postfix"
            serviceIcon="📧"
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
                                    <h2 className="text-xl font-bold text-fg mb-1">Postfix Mail Transfer Agent</h2>
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
                        {activeTab === 'queue' && (
                            <div className="space-y-6">
                                {/* Summary Cards */}
                                {summary && (
                                    <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
                                        <div className="bg-surface/50 p-6 rounded-xl border border-border text-center relative overflow-hidden group">
                                            <div className="absolute top-0 left-0 w-full h-1 bg-primary/50"></div>
                                            <h4 className="text-fg-muted text-xs uppercase font-bold tracking-wider mb-2">Active</h4>
                                            <p className="text-3xl font-bold text-fg group-hover:text-primary transition-colors">{summary.active}</p>
                                        </div>
                                        <div className="bg-surface/50 p-6 rounded-xl border border-border text-center relative overflow-hidden group">
                                            <div className="absolute top-0 left-0 w-full h-1 bg-warning/50"></div>
                                            <h4 className="text-fg-muted text-xs uppercase font-bold tracking-wider mb-2">Deferred</h4>
                                            <p className="text-3xl font-bold text-fg group-hover:text-warning transition-colors">{summary.deferred}</p>
                                        </div>
                                        <div className="bg-surface/50 p-6 rounded-xl border border-border text-center relative overflow-hidden group">
                                            <div className="absolute top-0 left-0 w-full h-1 bg-purple-500/50"></div>
                                            <h4 className="text-fg-muted text-xs uppercase font-bold tracking-wider mb-2">Hold</h4>
                                            <p className="text-3xl font-bold text-fg group-hover:text-purple-400 transition-colors">{summary.hold}</p>
                                        </div>
                                        <div className="bg-surface/50 p-6 rounded-xl border border-border text-center relative overflow-hidden group">
                                            <div className="absolute top-0 left-0 w-full h-1 bg-danger/50"></div>
                                            <h4 className="text-fg-muted text-xs uppercase font-bold tracking-wider mb-2">Corrupt</h4>
                                            <p className="text-3xl font-bold text-fg group-hover:text-danger transition-colors">{summary.corrupt}</p>
                                        </div>
                                    </div>
                                )}

                                {/* Queue Actions */}
                                <div className="flex justify-end gap-3">
                                    <button onClick={() => fetchData()} className="px-4 py-2 bg-surface-2 hover:bg-surface-3 text-fg rounded-lg text-sm flex items-center gap-2 border border-border transition-all">
                                        <RefreshCw className="w-3.5 h-3.5" /> Refresh
                                    </button>
                                    <button onClick={() => handleQueueAction('flush')} className="px-4 py-2 bg-primary/10 hover:bg-primary/20 text-primary border border-primary/20 rounded-lg text-sm flex items-center gap-2 transition-all">
                                        <RotateCw className="w-3.5 h-3.5" /> Flush Queue
                                    </button>
                                    <button onClick={() => handleQueueAction('delete_all')} className="px-4 py-2 bg-danger/10 hover:bg-danger/20 text-danger border border-danger/20 rounded-lg text-sm flex items-center gap-2 transition-all">
                                        <Trash2 className="w-3.5 h-3.5" /> Delete All
                                    </button>
                                </div>

                                {/* Queue List */}
                                <div className="bg-surface/50 rounded-xl border border-border overflow-hidden">
                                    <table className="w-full text-left text-sm text-fg-muted">
                                        <thead className="bg-bg text-fg-muted uppercase text-xs font-bold tracking-wider">
                                            <tr>
                                                <th className="px-6 py-4">ID</th>
                                                <th className="px-6 py-4">Sender</th>
                                                <th className="px-6 py-4">Size</th>
                                                <th className="px-6 py-4">Arrival</th>
                                                <th className="px-6 py-4">Status</th>
                                                <th className="px-6 py-4 text-right">Action</th>
                                            </tr>
                                        </thead>
                                        <tbody className="divide-y divide-border">
                                            {queue.map((item) => (
                                                <tr key={item.id} className="hover:bg-surface-2/50 transition-colors">
                                                    <td className="px-6 py-4 font-mono text-fg font-medium">{item.id}</td>
                                                    <td className="px-6 py-4">{item.sender}</td>
                                                    <td className="px-6 py-4">{item.size} bytes</td>
                                                    <td className="px-6 py-4 text-fg-subtle">{item.arrival}</td>
                                                    <td className="px-6 py-4">
                                                        <span className={`px-2.5 py-1 rounded-full text-xs font-bold ${item.status === 'active' ? 'bg-primary/10 text-primary border border-primary/20' :
                                                                item.status === 'deferred' ? 'bg-warning/10 text-warning border border-warning/20' :
                                                                    'bg-surface-3 text-fg-muted'
                                                            }`}>
                                                            {item.status}
                                                        </span>
                                                    </td>
                                                    <td className="px-6 py-4 text-right">
                                                        <button
                                                            onClick={() => handleQueueAction('delete_id', item.id)}
                                                            className="p-2 text-fg-muted hover:text-danger hover:bg-danger/10 rounded-lg transition-all"
                                                            title="Delete Message"
                                                        >
                                                            <Trash2 className="w-4 h-4" />
                                                        </button>
                                                    </td>
                                                </tr>
                                            ))}
                                            {queue.length === 0 && (
                                                <tr>
                                                    <td colSpan={6} className="px-6 py-12 text-center text-fg-subtle">
                                                        <div className="flex flex-col items-center gap-3">
                                                            <Mail className="w-12 h-12 text-fg-subtle" />
                                                            <p>Mail queue is empty.</p>
                                                        </div>
                                                    </td>
                                                </tr>
                                            )}
                                        </tbody>
                                    </table>
                                </div>
                            </div>
                        )}

                        {activeTab === 'logs' && (
                            <div className="bg-surface/50 border border-border rounded-xl p-8 text-center text-fg-subtle">
                                <Activity className="w-12 h-12 mx-auto mb-4 opacity-20" />
                                <p>Mail logs viewer coming soon.</p>
                            </div>
                        )}
                    </div>
                </div>
            )}
        </ServiceManagementLayout>
    );
}
