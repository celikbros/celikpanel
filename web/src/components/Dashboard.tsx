import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Cpu, MemoryStick, HardDrive, Server, Globe, Database, Activity } from 'lucide-react';
import { api, type Service, type SystemStats } from '../lib/api';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import { Card, PageHeader, UsageBar, StatusDot } from './ui';

// Plesk-style dashboard: a dense grid of real-data widgets — CPU/RAM/disk
// gauges from the live system-stats endpoint, server facts, and service
// health. No placeholders; every number is real.
//
// Plesk tarzı pano: gerçek-veri widget'larından yoğun bir ızgara — canlı
// system-stats uç noktasından CPU/RAM/disk göstergeleri, sunucu bilgileri
// ve servis sağlığı. Placeholder yok; her sayı gerçek.
export function Dashboard() {
    const { t } = useI18n();
    const [stats, setStats] = useState<SystemStats | null>(null);
    const [services, setServices] = useState<Service[] | null>(null);

    useEffect(() => {
        const load = () => api.getSystemStats().then(setStats).catch(() => {});
        load();
        api.getServices().then(setServices).catch(() => setServices([]));
        // Refresh CPU/RAM live every 5s for a real-time feel.
        // Gerçek zamanlı his için CPU/RAM'i 5 sn'de bir yenile.
        const timer = setInterval(load, 5000);
        return () => clearInterval(timer);
    }, []);

    const running = services?.filter((s) => s.status?.includes('running')).length ?? 0;
    const total = services?.length ?? 0;

    return (
        <div className="p-6 md:p-8">
            <PageHeader title={t('dashboard.title')} subtitle={stats?.hostname} />

            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
                <GaugeCard
                    icon={Cpu}
                    label={t('dashboard.cpuUsage')}
                    percent={stats?.cpu_percent ?? 0}
                    value={stats ? `${stats.cpu_percent}%` : '—'}
                    hint={stats ? t('dashboard.cores', { n: stats.cpu_cores }) : ''}
                />
                <GaugeCard
                    icon={MemoryStick}
                    label={t('dashboard.memoryUsage')}
                    percent={pct(stats?.mem_used_bytes, stats?.mem_total_bytes)}
                    value={stats ? `${Math.round(pct(stats.mem_used_bytes, stats.mem_total_bytes))}%` : '—'}
                    hint={
                        stats
                            ? t('dashboard.usedOfTotal', {
                                  used: fmtBytes(stats.mem_used_bytes),
                                  total: fmtBytes(stats.mem_total_bytes),
                              })
                            : ''
                    }
                />
                <GaugeCard
                    icon={HardDrive}
                    label={t('dashboard.diskUsage')}
                    percent={pct(stats?.disk_used_bytes, stats?.disk_total_bytes)}
                    value={stats ? `${Math.round(pct(stats.disk_used_bytes, stats.disk_total_bytes))}%` : '—'}
                    hint={
                        stats
                            ? t('dashboard.usedOfTotal', {
                                  used: fmtBytes(stats.disk_used_bytes),
                                  total: fmtBytes(stats.disk_total_bytes),
                              })
                            : ''
                    }
                />
            </div>

            <div className="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-3">
                <Card title={t('dashboard.serverInfo')} icon={Server} className="lg:col-span-1">
                    <dl className="divide-y divide-border text-sm">
                        <InfoRow label={t('dashboard.hostname')} value={stats?.hostname ?? '—'} />
                        <InfoRow label={t('dashboard.os')} value={stats?.os ?? '—'} />
                        <InfoRow label={t('dashboard.uptime')} value={stats ? fmtUptime(stats.uptime_seconds, t) : '—'} />
                        <InfoRow
                            label={t('dashboard.loadAverage')}
                            value={stats ? stats.load_avg.map((n) => n.toFixed(2)).join('  ') : '—'}
                        />
                    </dl>
                </Card>

                <Card
                    title={t('dashboard.services')}
                    icon={Activity}
                    className="lg:col-span-2"
                    action={
                        <span className="text-xs font-medium text-fg-muted">
                            {services ? `${running}/${total} ${t('dashboard.running')}` : ''}
                        </span>
                    }
                >
                    <div className="grid grid-cols-1 gap-x-6 gap-y-1 p-2 sm:grid-cols-2">
                        {services?.map((s) => {
                            const ok = s.status?.includes('running');
                            return (
                                <div
                                    key={s.id}
                                    className="flex items-center justify-between rounded-lg px-2 py-1.5 hover:bg-surface-2"
                                >
                                    <span className="flex items-center gap-2 text-sm text-fg">
                                        <StatusDot ok={!!ok} />
                                        {s.name}
                                    </span>
                                    <span className={`text-xs ${ok ? 'text-success' : 'text-fg-subtle'}`}>
                                        {ok ? t('dashboard.running') : t('dashboard.stopped')}
                                    </span>
                                </div>
                            );
                        })}
                    </div>
                </Card>
            </div>

            <section className="mt-6">
                <h2 className="mb-3 text-sm font-semibold uppercase tracking-wide text-fg-subtle">
                    {t('dashboard.quickActions')}
                </h2>
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
                    <QuickAction icon={Globe} labelKey="dashboard.addDomain" to="/domains" />
                    <QuickAction icon={Server} labelKey="dashboard.manageServices" to="/services" />
                    <QuickAction icon={Database} labelKey="dashboard.viewDatabases" to="/databases" />
                </div>
            </section>
        </div>
    );
}

function GaugeCard({
    icon: Icon,
    label,
    percent,
    value,
    hint,
}: {
    icon: typeof Cpu;
    label: string;
    percent: number;
    value: string;
    hint: string;
}) {
    return (
        <div className="rounded-xl border border-border bg-surface p-5 shadow-card">
            <div className="flex items-center justify-between">
                <span className="text-sm font-medium text-fg-muted">{label}</span>
                <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary/10 text-primary">
                    <Icon className="h-4 w-4" />
                </span>
            </div>
            <p className="mt-2 text-3xl font-bold tracking-tight">{value}</p>
            <div className="mt-3">
                <UsageBar percent={percent} />
            </div>
            {hint && <p className="mt-2 text-xs text-fg-subtle">{hint}</p>}
        </div>
    );
}

function InfoRow({ label, value }: { label: string; value: string }) {
    return (
        <div className="flex items-center justify-between px-4 py-2.5">
            <dt className="text-fg-muted">{label}</dt>
            <dd className="max-w-[60%] truncate text-right font-medium text-fg">{value}</dd>
        </div>
    );
}

function QuickAction({ icon: Icon, labelKey, to }: { icon: typeof Server; labelKey: TranslationKey; to: string }) {
    const navigate = useNavigate();
    const { t } = useI18n();
    return (
        <button
            onClick={() => navigate(to)}
            className="group flex items-center gap-3 rounded-xl border border-border bg-surface p-4 text-left shadow-card transition-colors hover:border-primary/40 hover:bg-surface-2"
        >
            <span className="flex h-9 w-9 items-center justify-center rounded-lg bg-surface-2 text-fg-muted transition-colors group-hover:bg-primary group-hover:text-primary-fg">
                <Icon className="h-5 w-5" />
            </span>
            <span className="flex-1 text-sm font-medium">{t(labelKey)}</span>
        </button>
    );
}

function pct(used?: number, total?: number): number {
    if (!used || !total) return 0;
    return (used / total) * 100;
}

function fmtBytes(bytes: number): string {
    if (bytes >= 1024 ** 3) return `${(bytes / 1024 ** 3).toFixed(1)} GB`;
    if (bytes >= 1024 ** 2) return `${(bytes / 1024 ** 2).toFixed(0)} MB`;
    if (bytes >= 1024) return `${(bytes / 1024).toFixed(0)} KB`;
    return `${bytes} B`;
}

function fmtUptime(seconds: number, t: (k: TranslationKey, v?: Record<string, string | number>) => string): string {
    const d = Math.floor(seconds / 86400);
    const h = Math.floor((seconds % 86400) / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    if (d > 0) return `${d}${t('common.days')} ${h}${t('common.hours')}`;
    if (h > 0) return `${h}${t('common.hours')} ${m}${t('common.minutes')}`;
    return `${m}${t('common.minutes')}`;
}
