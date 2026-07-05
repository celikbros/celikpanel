import { useState } from 'react';
import { Network, Wrench, RotateCw, CheckCircle2 } from 'lucide-react';
import { ServiceShell } from './ServiceShell';
import { showToast } from './Toast';
import { useI18n } from '../i18n';

interface PowerDNSManagementProps {
    onBack: () => void;
}

// PowerDNS on the shared ServiceShell. Status and start/stop come from the
// shell (honest, managed-services sourced). The one real extra here is the
// config-repair action, which reconfigures the backend server-side.
//
// PowerDNS ortak ServiceShell üzerinde. Durum ve başlat/durdur kabuktan gelir
// (dürüst, managed-services kaynaklı). Buradaki tek gerçek ek, arka ucu sunucu
// tarafında yeniden yapılandıran config-onarım eylemidir.
export function PowerDNSManagement({ onBack }: PowerDNSManagementProps) {
    const { t } = useI18n();
    const [repairing, setRepairing] = useState(false);

    const handleRepair = async () => {
        if (!confirm(t('pdns.repairConfirm'))) return;
        setRepairing(true);
        try {
            const res = await fetch('/api/v1/pdns/configure', { method: 'POST' });
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
