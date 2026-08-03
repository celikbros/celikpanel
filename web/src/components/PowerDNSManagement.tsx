import { useEffect, useState } from 'react';
import { useNavigate } from '../router';
import { Network, Wrench, RotateCw, CheckCircle2, Globe, FileText, ChevronDown, ChevronRight, Info } from 'lucide-react';
import { ServiceShell } from './ServiceShell';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { api } from '../lib/api';
import { Button } from './ui';

interface PowerDNSManagementProps {
    onBack: () => void;
}

// PowerDNS management, honest version: this service has NO hand-edited
// settings by design — the panel owns pdns.conf (SQLite backend, port 53,
// DNSSEC) and the real DNS work (records) lives per domain. So the page
// says exactly that, shows the actual config file read-only for
// transparency, and keeps the one real action: config repair.
//
// PowerDNS yönetimi, dürüst sürüm: bu serviste elle düzenlenecek ayar
// bilinçli olarak yok — pdns.conf'un sahibi panel (SQLite arka ucu, port
// 53, DNSSEC) ve asıl DNS işi (kayıtlar) domain başına yaşar. Sayfa da tam
// bunu söyler, şeffaflık için gerçek config dosyasını salt-okur gösterir
// ve tek gerçek eylemi tutar: yapılandırma onarımı.
export function PowerDNSManagement({ onBack }: PowerDNSManagementProps) {
    const { t } = useI18n();
    const navigate = useNavigate();
    const [repairing, setRepairing] = useState(false);
    const [configFiles, setConfigFiles] = useState<string[]>([]);
    const [openFile, setOpenFile] = useState<string | null>(null);
    const [fileContent, setFileContent] = useState<string>('');

    useEffect(() => {
        fetch('/api/v1/managed-services')
            .then((r) => (r.ok ? r.json() : null))
            .then((d) => {
                const svc = (d?.services || []).find((s: { id: string }) => s.id === 'pdns');
                setConfigFiles((svc?.config_files || []).map((f: { path: string }) => f.path));
            })
            .catch(() => {});
    }, []);

    const toggleFile = async (path: string) => {
        if (openFile === path) {
            setOpenFile(null);
            return;
        }
        try {
            const cfg = await api.getConfig(path);
            setFileContent(cfg.Content || '');
            setOpenFile(path);
        } catch {
            showToast('error', t('common.error'));
        }
    };

    const handleRepair = async () => {
        if (!confirm(t('pdns.repairConfirm'))) return;
        setRepairing(true);
        try {
            // /pdns/enable reconfigures the gsqlite3 backend AND re-syncs
            // every panel zone into PowerDNS.
            // /pdns/enable gsqlite3 arka ucunu yeniden yapılandırır VE tüm
            // panel bölgelerini PowerDNS'e eşitler.
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
            {/* The honest answer to 'where are the settings?' / 'Ayarlar
                nerede?' sorusunun dürüst cevabı */}
            <section className="mb-5 rounded-xl border border-border bg-surface p-5 shadow-card">
                <div className="flex flex-wrap items-start gap-3">
                    <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
                        <Info className="h-5 w-5" />
                    </span>
                    <div className="min-w-0 flex-1">
                        <h3 className="text-base font-semibold text-fg">{t('pdns.managed')}</h3>
                        <p className="mt-1 max-w-3xl text-sm leading-relaxed text-fg-muted">{t('pdns.managedDesc')}</p>
                    </div>
                    <Button variant="secondary" icon={Globe} onClick={() => navigate('/domains')}>
                        {t('pdns.goDomainRecords')}
                    </Button>
                </div>
            </section>

            {/* Actual configuration, read-only / Gerçek yapılandırma, salt-okur */}
            {configFiles.length > 0 && (
                <section className="mb-5 overflow-hidden rounded-xl border border-border bg-surface shadow-card">
                    {configFiles.map((path) => (
                        <div key={path} className="border-b border-border last:border-0">
                            <button
                                onClick={() => toggleFile(path)}
                                className="flex w-full items-center gap-2.5 px-4 py-3 text-left transition-colors hover:bg-surface-2/60"
                            >
                                <FileText className="h-4 w-4 shrink-0 text-fg-subtle" />
                                <span className="min-w-0 flex-1 font-mono text-sm text-fg">{path}</span>
                                <span className="text-xs text-fg-subtle">{t('pdns.readOnly')}</span>
                                {openFile === path ? (
                                    <ChevronDown className="h-4 w-4 text-fg-subtle" />
                                ) : (
                                    <ChevronRight className="h-4 w-4 text-fg-subtle" />
                                )}
                            </button>
                            {openFile === path && (
                                <div className="border-t border-border bg-surface-2/40 px-4 py-3">
                                    <p className="mb-2 text-xs text-fg-subtle">{t('pdns.readOnlyNote')}</p>
                                    <pre className="max-h-80 overflow-auto rounded-lg bg-surface-2 p-3 font-mono text-xs leading-relaxed text-fg">
                                        {fileContent || '—'}
                                    </pre>
                                </div>
                            )}
                        </div>
                    ))}
                </section>
            )}

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
