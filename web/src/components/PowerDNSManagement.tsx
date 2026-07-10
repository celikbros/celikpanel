import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Network, Wrench, RotateCw, CheckCircle2, Globe, ArrowRight, Plus } from 'lucide-react';
import { ServiceShell } from './ServiceShell';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { Button, EmptyState } from './ui';

interface PowerDNSManagementProps {
    onBack: () => void;
}

interface ZoneRow {
    id: number;
    domain_name: string;
    parent_id?: number | null;
}

// PowerDNS on the shared ServiceShell. The page shows what this service is
// actually FOR: the authoritative zones it serves (every top-level panel
// domain is one), each linking to the domain's DNS records. The config
// repair is maintenance, so it sits below the real content.
//
// PowerDNS ortak ServiceShell üzerinde. Sayfa bu servisin gerçekte NE İÇİN
// olduğunu gösterir: sunduğu otoriter bölgeler (paneldeki her üst-seviye
// domain bir bölgedir), her biri domainin DNS kayıtlarına bağlanır.
// Yapılandırma onarımı bakımdır; gerçek içeriğin altında durur.
export function PowerDNSManagement({ onBack }: PowerDNSManagementProps) {
    const { t } = useI18n();
    const navigate = useNavigate();
    const [repairing, setRepairing] = useState(false);
    const [zones, setZones] = useState<ZoneRow[]>([]);
    const [zonesLoaded, setZonesLoaded] = useState(false);

    useEffect(() => {
        fetch('/api/v1/domains')
            .then((r) => (r.ok ? r.json() : []))
            // Subdomains live inside the parent zone — only top-level
            // domains ARE zones. / Alt alanlar ana bölgenin içindedir —
            // yalnız üst-seviye domainler bölgedir.
            .then((d: ZoneRow[]) => setZones((d || []).filter((z) => !z.parent_id)))
            .catch(() => {})
            .finally(() => setZonesLoaded(true));
    }, []);

    const handleRepair = async () => {
        if (!confirm(t('pdns.repairConfirm'))) return;
        setRepairing(true);
        try {
            // The panel's real endpoint is /pdns/enable — it reconfigures the
            // gsqlite3 backend AND re-syncs every panel zone into PowerDNS.
            // Panelin gerçek ucu /pdns/enable — gsqlite3 arka ucunu yeniden
            // yapılandırır VE tüm panel bölgelerini PowerDNS'e eşitler.
            const res = await fetch('/api/v1/pdns/enable', { method: 'POST' });
            if (!res.ok) throw new Error();
            showToast('success', t('pdns.repairDone'));
        } catch {
            showToast('error', t('pdns.repairFailed'));
        } finally {
            setRepairing(false);
        }
    };

    const steps = [t('pdns.step.backend'), t('pdns.step.conflict'), t('pdns.step.port'), t('pdns.step.restart')];

    return (
        <ServiceShell serviceId="pdns" name="PowerDNS" icon={Network} onBack={onBack}>
            {/* Served zones — the service's real content / Sunulan bölgeler —
                servisin gerçek içeriği */}
            <section className="mb-5">
                <div className="mb-3 flex items-center gap-3">
                    <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-teal-500/10 text-teal-600 dark:text-teal-400">
                        <Globe className="h-5 w-5" />
                    </span>
                    <div>
                        <h3 className="text-lg font-semibold text-fg">{t('pdns.zones')}</h3>
                        <p className="text-xs text-fg-muted">{t('pdns.zonesHint')}</p>
                    </div>
                    {zones.length > 0 && (
                        <span className="ml-auto text-sm text-fg-subtle">{t('common.itemsTotal', { n: zones.length })}</span>
                    )}
                </div>
                {!zonesLoaded ? null : zones.length === 0 ? (
                    <EmptyState
                        icon={Globe}
                        title={t('pdns.noZones')}
                        hint={t('pdns.noZonesHint')}
                        action={
                            <Button variant="primary" icon={Plus} onClick={() => navigate('/domains')}>
                                {t('dashboard.addDomain')}
                            </Button>
                        }
                    />
                ) : (
                    <ul className="overflow-hidden rounded-xl border border-border bg-surface shadow-card">
                        {zones.map((z) => (
                            <li key={z.id} className="border-b border-border last:border-0">
                                <button
                                    onClick={() => navigate(`/domains/${encodeURIComponent(z.domain_name)}`)}
                                    className="flex w-full flex-wrap items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-surface-2/60"
                                >
                                    <Globe className="h-4 w-4 shrink-0 text-fg-subtle" />
                                    <span className="min-w-0 flex-1 text-base font-medium text-fg">{z.domain_name}</span>
                                    <span className="inline-flex items-center gap-1 text-sm font-medium text-primary">
                                        {t('pdns.manageRecords')} <ArrowRight className="h-3.5 w-3.5" />
                                    </span>
                                </button>
                            </li>
                        ))}
                    </ul>
                )}
            </section>

            {/* Maintenance / Bakım */}
            <div className="grid grid-cols-1 gap-5 lg:grid-cols-3">
                <div className="rounded-xl border border-border bg-surface p-6 shadow-card lg:col-span-2">
                    <div className="mb-3 flex items-center gap-2">
                        <span className="flex h-9 w-9 items-center justify-center rounded-lg bg-primary/10 text-primary">
                            <Wrench className="h-5 w-5" />
                        </span>
                        <h3 className="text-lg font-semibold text-fg">{t('pdns.repairTitle')}</h3>
                    </div>
                    <p className="mb-6 max-w-xl text-sm leading-relaxed text-fg-muted">{t('pdns.repairDesc')}</p>
                    <button
                        onClick={handleRepair}
                        disabled={repairing}
                        className="inline-flex items-center gap-2 rounded-lg bg-primary px-5 py-2.5 font-medium text-primary-fg transition-colors hover:bg-primary-hover disabled:cursor-not-allowed disabled:opacity-60"
                    >
                        <RotateCw className={`h-4 w-4 ${repairing ? 'animate-spin' : ''}`} />
                        {repairing ? t('pdns.repairing') : t('pdns.repairRun')}
                    </button>
                </div>

                <div className="rounded-xl border border-border bg-surface p-6 shadow-card">
                    <h4 className="mb-4 text-sm font-semibold text-fg">{t('pdns.repairSteps')}</h4>
                    <ul className="space-y-3">
                        {steps.map((step) => (
                            <li key={step} className="flex items-center gap-2.5 text-sm text-fg-muted">
                                <CheckCircle2 className="h-4 w-4 shrink-0 text-fg-subtle" />
                                {step}
                            </li>
                        ))}
                    </ul>
                </div>
            </div>
        </ServiceShell>
    );
}
