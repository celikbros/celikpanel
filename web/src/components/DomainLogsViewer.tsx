import { useState, useEffect } from 'react';
import { FileText, Download, Trash2, RefreshCw, Search } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import { EmptyState, inputClass } from './ui';

interface DomainLogsViewerProps {
    domainId: number;
    domainName: string;
}

type LogType = 'access' | 'error' | 'php';

const logTypes: { value: LogType; labelKey: TranslationKey; tone: string }[] = [
    { value: 'access', labelKey: 'logs.type.access', tone: 'text-primary' },
    { value: 'error', labelKey: 'logs.type.error', tone: 'text-danger' },
    { value: 'php', labelKey: 'logs.type.php', tone: 'text-warning' },
];

// Live log tail for a domain (access/error/php), server-side filtered.
// Download is client-side; clear is destructive and confirmed.
//
// Bir domain için canlı günlük kuyruğu (erişim/hata/php), sunucu tarafında
// filtrelenir. İndirme istemci tarafındadır; temizleme yıkıcıdır ve onay ister.
export function DomainLogsViewer({ domainId, domainName }: DomainLogsViewerProps) {
    const { t } = useI18n();
    const [logType, setLogType] = useState<LogType>('access');
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
        const interval = setInterval(loadLogs, 5000);
        return () => clearInterval(interval);
    }, [autoRefresh, domainId, logType, lines, filter]);

    const loadLogs = async () => {
        setLoading(true);
        try {
            const params = new URLSearchParams({ lines: String(lines), ...(filter && { filter }) });
            const res = await fetch(`/api/v1/domains/${domainId}/logs/${logType}?${params}`);
            if (!res.ok) throw new Error();
            const data = await res.json();
            setLogs(data.lines || []);
        } catch {
            showToast('error', t('common.error'));
        } finally {
            setLoading(false);
        }
    };

    const clearLogs = async () => {
        const typeLabel = t(logTypes.find((l) => l.value === logType)!.labelKey);
        if (!confirm(t('logs.clearConfirm', { type: typeLabel, domain: domainName }))) return;
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/logs/${logType}`, { method: 'DELETE' });
            if (!res.ok) throw new Error();
            showToast('success', t('logs.cleared'));
            loadLogs();
        } catch {
            showToast('error', t('common.error'));
        }
    };

    const downloadLogs = () => {
        const blob = new Blob([logs.join('\n')], { type: 'text/plain' });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `${domainName}-${logType}-${new Date().toISOString().split('T')[0]}.log`;
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        URL.revokeObjectURL(url);
        showToast('success', t('logs.downloaded'));
    };

    const currentType = logTypes.find((l) => l.value === logType)!;

    return (
        <div className="space-y-4">
            {/* Controls */}
            <div className="flex flex-wrap items-end gap-3">
                <label className="block">
                    <span className="mb-1.5 block text-sm font-medium text-fg-muted">{t('logs.type')}</span>
                    <select
                        value={logType}
                        onChange={(e) => setLogType(e.target.value as LogType)}
                        className={`${inputClass} w-auto`}
                    >
                        {logTypes.map((l) => (
                            <option key={l.value} value={l.value}>
                                {t(l.labelKey)}
                            </option>
                        ))}
                    </select>
                </label>

                <label className="block">
                    <span className="mb-1.5 block text-sm font-medium text-fg-muted">{t('logs.lines')}</span>
                    <select
                        value={lines}
                        onChange={(e) => setLines(Number(e.target.value))}
                        className={`${inputClass} w-auto`}
                    >
                        {[50, 100, 200, 500, 1000].map((n) => (
                            <option key={n} value={n}>
                                {t('logs.linesN', { n })}
                            </option>
                        ))}
                    </select>
                </label>

                <label className="block min-w-[200px] flex-1">
                    <span className="mb-1.5 block text-sm font-medium text-fg-muted">{t('logs.filter')}</span>
                    <div className="relative">
                        <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-fg-subtle" />
                        <input
                            type="text"
                            value={filter}
                            onChange={(e) => setFilter(e.target.value)}
                            onKeyDown={(e) => e.key === 'Enter' && loadLogs()}
                            placeholder={t('logs.filterPlaceholder')}
                            className={`${inputClass} pl-9`}
                        />
                    </div>
                </label>

                <div className="flex items-center gap-1">
                    <button
                        onClick={loadLogs}
                        disabled={loading}
                        title={t('logs.refresh')}
                        className="rounded-lg border border-border-strong bg-surface p-2 text-fg-muted transition-colors hover:bg-surface-2 hover:text-fg disabled:opacity-50"
                    >
                        <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
                    </button>
                    <button
                        onClick={downloadLogs}
                        disabled={logs.length === 0}
                        title={t('logs.download')}
                        className="rounded-lg border border-border-strong bg-surface p-2 text-fg-muted transition-colors hover:bg-surface-2 hover:text-fg disabled:opacity-50"
                    >
                        <Download className="h-4 w-4" />
                    </button>
                    <button
                        onClick={clearLogs}
                        title={t('logs.clear')}
                        className="rounded-lg border border-border-strong bg-surface p-2 text-fg-muted transition-colors hover:bg-danger/10 hover:text-danger"
                    >
                        <Trash2 className="h-4 w-4" />
                    </button>
                </div>
            </div>

            <label className="flex w-fit cursor-pointer items-center gap-2 text-sm text-fg-muted">
                <input
                    type="checkbox"
                    checked={autoRefresh}
                    onChange={(e) => setAutoRefresh(e.target.checked)}
                    className="h-4 w-4 rounded border-border accent-[rgb(var(--primary))]"
                />
                {t('logs.autoRefresh')}
            </label>

            {/* Log output */}
            <div className="rounded-xl border border-border">
                <div className="flex items-center gap-2 border-b border-border px-4 py-2.5">
                    <FileText className={`h-4 w-4 ${currentType.tone}`} />
                    <span className="text-sm font-semibold text-fg">{t(currentType.labelKey)}</span>
                    <span className="text-xs text-fg-muted">{t('logs.linesN', { n: logs.length })}</span>
                </div>

                {loading ? (
                    <div className="flex h-64 items-center justify-center">
                        <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary" />
                    </div>
                ) : logs.length === 0 ? (
                    <div className="py-6">
                        <EmptyState icon={FileText} title={t('logs.empty')} />
                    </div>
                ) : (
                    <div className="max-h-[480px] overflow-auto bg-bg p-3">
                        <pre className="font-mono text-xs text-fg-muted">
                            {logs.map((line, index) => (
                                <div key={index} className="flex rounded px-1 py-0.5 hover:bg-surface-2/60">
                                    <span className="mr-3 select-none text-fg-subtle">
                                        {String(index + 1).padStart(4, ' ')}
                                    </span>
                                    <span className="whitespace-pre-wrap break-all">{line}</span>
                                </div>
                            ))}
                        </pre>
                    </div>
                )}
            </div>
        </div>
    );
}
