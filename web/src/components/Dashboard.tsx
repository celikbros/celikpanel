import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Server, Globe, Database, Plus, ArrowRight } from 'lucide-react';
import { api, type Service } from '../lib/api';
import { useAuth } from '../auth/AuthContext';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';

// The landing view. Plain, at-a-glance status plus a few quick actions —
// no dashboard clutter, in keeping with the simplicity principle.
// Açılış görünümü. Sade, tek bakışta durum ve birkaç hızlı işlem — sadelik
// ilkesine uygun olarak pano kalabalığı yok.
export function Dashboard() {
    const { user } = useAuth();
    const { t } = useI18n();
    const [services, setServices] = useState<Service[] | null>(null);
    const [domainCount, setDomainCount] = useState<number | null>(null);

    useEffect(() => {
        api.getServices().then(setServices).catch(() => setServices([]));
        fetch('/api/v1/domains')
            .then((r) => (r.ok ? r.json() : []))
            .then((d) => setDomainCount(Array.isArray(d) ? d.length : 0))
            .catch(() => setDomainCount(0));
    }, []);

    const running = services?.filter((s) => s.status?.includes('running')).length ?? 0;
    const total = services?.length ?? 0;

    return (
        <div className="mx-auto max-w-6xl p-6 md:p-8">
            <header className="mb-8">
                <h1 className="text-2xl font-bold tracking-tight">{t('dashboard.title')}</h1>
                <p className="mt-1 text-fg-muted">{t('dashboard.welcome', { name: user.username })}</p>
            </header>

            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
                <StatCard
                    icon={Server}
                    labelKey="dashboard.services"
                    value={services === null ? '—' : `${running}/${total}`}
                    hint={services === null ? t('common.loading') : t('dashboard.servicesRunning', { running, total })}
                />
                <StatCard
                    icon={Globe}
                    labelKey="dashboard.domains"
                    value={domainCount === null ? '—' : String(domainCount)}
                />
                <StatCard icon={Database} labelKey="dashboard.databases" value="—" />
            </div>

            <section className="mt-8">
                <h2 className="mb-3 text-sm font-semibold uppercase tracking-wide text-fg-subtle">
                    {t('dashboard.quickActions')}
                </h2>
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
                    <QuickAction icon={Plus} labelKey="dashboard.addDomain" to="/domains" />
                    <QuickAction icon={Server} labelKey="dashboard.manageServices" to="/services" />
                    <QuickAction icon={Database} labelKey="dashboard.viewDatabases" to="/databases" />
                </div>
            </section>
        </div>
    );
}

function StatCard({
    icon: Icon,
    labelKey,
    value,
    hint,
}: {
    icon: typeof Server;
    labelKey: TranslationKey;
    value: string;
    hint?: string;
}) {
    const { t } = useI18n();
    return (
        <div className="rounded-xl border border-border bg-surface p-5 shadow-card">
            <div className="flex items-center justify-between">
                <span className="text-sm font-medium text-fg-muted">{t(labelKey)}</span>
                <span className="flex h-9 w-9 items-center justify-center rounded-lg bg-primary/10 text-primary">
                    <Icon className="h-5 w-5" />
                </span>
            </div>
            <p className="mt-3 text-3xl font-bold tracking-tight">{value}</p>
            {hint && <p className="mt-1 text-xs text-fg-subtle">{hint}</p>}
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
            <ArrowRight className="h-4 w-4 text-fg-subtle transition-transform group-hover:translate-x-0.5" />
        </button>
    );
}
