import { useState, useEffect } from 'react';
import { Inbox, Clock, Users } from 'lucide-react';
import { ServiceShell } from './ServiceShell';
import { ComponentPanels } from './ComponentDetail';
import { useI18n } from '../i18n';

interface DovecotManagementProps {
    onBack: () => void;
    onSelectConfig?: (path: string) => void;
}

interface DovecotStats {
    uptime: string;
    connections: number;
    logins: number;
    auth_success: number;
    auth_fail: number;
}

export function DovecotManagement({ onBack, onSelectConfig }: DovecotManagementProps) {
    const { t } = useI18n();
    const [stats, setStats] = useState<DovecotStats | null>(null);

    useEffect(() => {
        fetch('/api/v1/dovecot/stats')
            .then((r) => (r.ok ? r.json() : null))
            .then(setStats)
            .catch(() => {});
    }, []);

    // Only surface what is genuinely measured (uptime, live connections).
    // Login/auth counters need the stats plugin, so we don't show fabricated
    // zeros for them.
    // Yalnızca gerçekten ölçüleni göster (uptime, canlı bağlantı). Giriş/
    // kimlik sayaçları stats eklentisi gerektirir; onlar için uydurma sıfır
    // göstermeyiz.
    return (
        <ServiceShell serviceId="dovecot" name="Dovecot" icon={Inbox} onBack={onBack}>
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <StatCard icon={Clock} label={t('dovecot.uptime')} value={stats?.uptime ?? '—'} />
                <StatCard icon={Users} label={t('dovecot.connections')} value={stats ? String(stats.connections) : '—'} />
            </div>
            <p className="mt-4 text-xs text-fg-subtle">{t('dovecot.statsNote')}</p>
            {/* The panel already knows Dovecot's unit, ports, packages, config
                files and journal — show them instead of ending the page here
                (operator, 25 Jul). / Panel, Dovecot'un birimini, portlarını,
                paketlerini, ayar dosyalarını ve günlüğünü zaten biliyor —
                sayfayı burada bitirmek yerine onları göster (operatör, 25 Tem). */}
            <ComponentPanels serviceId="dovecot" onSelectConfig={onSelectConfig} />
        </ServiceShell>
    );
}

function StatCard({ icon: Icon, label, value }: { icon: typeof Clock; label: string; value: string }) {
    return (
        <div className="rounded-xl border border-border bg-surface p-5 shadow-card">
            <div className="flex items-center justify-between">
                <span className="text-sm font-medium text-fg-muted">{label}</span>
                <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary/10 text-primary">
                    <Icon className="h-4 w-4" />
                </span>
            </div>
            <p className="mt-2 text-3xl font-bold tracking-tight">{value}</p>
        </div>
    );
}
