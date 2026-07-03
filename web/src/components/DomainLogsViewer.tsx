import { useState, useEffect } from 'react';
import { FileText, Download, Trash2, RefreshCw, Filter } from 'lucide-react';
import { showToast } from './Toast';

interface DomainLogsViewerProps {
    domainId: number;
    domainName: string;
}

export function DomainLogsViewer({ domainId, domainName }: DomainLogsViewerProps) {
    const [logType, setLogType] = useState<'access' | 'error' | 'php'>('access');
    const [logs, setLogs] = useState<string[]>([]);
    const [loading, setLoading] = useState(false);
    const [filter, setFilter] = useState('');
    const [lines, setLines] = useState(100);
    const [autoRefresh, setAutoRefresh] = useState(false);

    useEffect(() => {
        loadLogs();
    }, [domainId, logType, lines]);

    useEffect(() => {
        if (!autoRefresh) return;

        const interval = setInterval(() => {
            loadLogs();
        }, 5000); // Refresh every 5 seconds

        return () => clearInterval(interval);
    }, [autoRefresh, domainId, logType, lines]);

    const loadLogs = async () => {
        setLoading(true);
        try {
            const params = new URLSearchParams({
                lines: lines.toString(),
                ...(filter && { filter })
            });

            const res = await fetch(`/api/v1/domains/${domainId}/logs/${logType}?${params}`);
            if (res.ok) {
                const data = await res.json();
                setLogs(data.lines || []);
            } else {
                showToast('error', 'Failed to load logs');
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to load logs');
        } finally {
            setLoading(false);
        }
    };

    const handleClearLogs = async () => {
        if (!confirm(`Clear all ${logType} logs for ${domainName}?\n\nThis action cannot be undone.`)) {
            return;
        }

        try {
            const res = await fetch(`/api/v1/domains/${domainId}/logs/${logType}`, {
                method: 'DELETE'
            });

            if (res.ok) {
                showToast('success', 'Logs cleared successfully');
                loadLogs();
            } else {
                showToast('error', 'Failed to clear logs');
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to clear logs');
        }
    };

    const handleDownloadLogs = () => {
        const blob = new Blob([logs.join('\n')], { type: 'text/plain' });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `${domainName}-${logType}-${new Date().toISOString().split('T')[0]}.log`;
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        URL.revokeObjectURL(url);
        showToast('success', 'Logs downloaded');
    };

    const getLogTypeColor = (type: string) => {
        switch (type) {
            case 'access': return 'text-primary';
            case 'error': return 'text-danger';
            case 'php': return 'text-warning';
            default: return 'text-fg-muted';
        }
    };

    return (
        <div className="space-y-6">
            <div>
                <h3 className="text-lg font-bold text-fg mb-2">Log Viewer</h3>
                <p className="text-sm text-fg-muted">
                    View and manage logs for {domainName}
                </p>
            </div>

            {/* Controls */}
            <div className="bg-surface-2/50 rounded-lg p-4 border border-border">
                <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
                    {/* Log Type Selector */}
                    <div>
                        <label className="block text-sm text-fg-muted mb-2">Log Type</label>
                        <select
                            value={logType}
                            onChange={(e) => setLogType(e.target.value as 'access' | 'error' | 'php')}
                            className="w-full bg-surface border border-border rounded px-4 py-2 text-fg focus:border-primary focus:outline-none"
                        >
                            <option value="access">Access Log</option>
                            <option value="error">Error Log</option>
                            <option value="php">PHP Error Log</option>
                        </select>
                    </div>

                    {/* Lines Selector */}
                    <div>
                        <label className="block text-sm text-fg-muted mb-2">Lines to Show</label>
                        <select
                            value={lines}
                            onChange={(e) => setLines(Number(e.target.value))}
                            className="w-full bg-surface border border-border rounded px-4 py-2 text-fg focus:border-primary focus:outline-none"
                        >
                            <option value="50">50 lines</option>
                            <option value="100">100 lines</option>
                            <option value="200">200 lines</option>
                            <option value="500">500 lines</option>
                            <option value="1000">1000 lines</option>
                        </select>
                    </div>

                    {/* Filter */}
                    <div>
                        <label className="block text-sm text-fg-muted mb-2">Filter</label>
                        <div className="relative">
                            <input
                                type="text"
                                value={filter}
                                onChange={(e) => setFilter(e.target.value)}
                                placeholder="Search logs..."
                                className="w-full bg-surface border border-border rounded px-4 py-2 pr-10 text-fg focus:border-primary focus:outline-none"
                            />
                            <Filter className="absolute right-3 top-2.5 w-4 h-4 text-fg-subtle" />
                        </div>
                    </div>

                    {/* Actions */}
                    <div>
                        <label className="block text-sm text-fg-muted mb-2">Actions</label>
                        <div className="flex gap-2">
                            <button
                                onClick={loadLogs}
                                disabled={loading}
                                className="flex-1 px-3 py-2 bg-primary text-white rounded hover:bg-primary-hover disabled:opacity-50 flex items-center justify-center gap-1"
                                title="Refresh"
                            >
                                <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
                            </button>
                            <button
                                onClick={handleDownloadLogs}
                                disabled={logs.length === 0}
                                className="flex-1 px-3 py-2 bg-success text-white rounded hover:bg-success disabled:opacity-50 flex items-center justify-center gap-1"
                                title="Download"
                            >
                                <Download className="w-4 h-4" />
                            </button>
                            <button
                                onClick={handleClearLogs}
                                className="flex-1 px-3 py-2 bg-danger text-white rounded hover:bg-danger flex items-center justify-center gap-1"
                                title="Clear"
                            >
                                <Trash2 className="w-4 h-4" />
                            </button>
                        </div>
                    </div>
                </div>

                {/* Auto-refresh toggle */}
                <div className="mt-4">
                    <label className="flex items-center gap-2 cursor-pointer">
                        <input
                            type="checkbox"
                            checked={autoRefresh}
                            onChange={(e) => setAutoRefresh(e.target.checked)}
                            className="w-4 h-4 bg-surface border-border rounded focus:ring-primary"
                        />
                        <span className="text-sm text-fg">Auto-refresh every 5 seconds</span>
                    </label>
                </div>
            </div>

            {/* Log Display */}
            <div className="bg-surface-2/50 rounded-lg border border-border">
                <div className="flex items-center justify-between p-4 border-b border-border">
                    <div className="flex items-center gap-2">
                        <FileText className={`w-5 h-5 ${getLogTypeColor(logType)}`} />
                        <h4 className="text-md font-semibold text-fg">
                            {logType.charAt(0).toUpperCase() + logType.slice(1)} Log
                        </h4>
                        <span className="text-sm text-fg-muted">
                            ({logs.length} {logs.length === 1 ? 'line' : 'lines'})
                        </span>
                    </div>
                </div>

                <div className="p-4">
                    {loading ? (
                        <div className="flex items-center justify-center h-64">
                            <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
                        </div>
                    ) : logs.length === 0 ? (
                        <div className="text-center text-fg-subtle py-12">
                            <FileText className="w-12 h-12 mx-auto mb-2 opacity-50" />
                            <p>No logs found</p>
                        </div>
                    ) : (
                        <div className="bg-bg rounded p-4 overflow-x-auto">
                            <pre className="text-xs text-fg-muted font-mono whitespace-pre-wrap">
                                {logs.map((line, index) => (
                                    <div
                                        key={index}
                                        className="hover:bg-surface-2/50 px-2 py-0.5 rounded"
                                    >
                                        <span className="text-fg-subtle select-none mr-4">
                                            {String(index + 1).padStart(4, ' ')}
                                        </span>
                                        {line}
                                    </div>
                                ))}
                            </pre>
                        </div>
                    )}
                </div>
            </div>
        </div>
    );
}
