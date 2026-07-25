import { useState, useEffect } from 'react';
import { Database, Lightbulb } from 'lucide-react';
import { ServiceShell } from './ServiceShell';
import { ComponentPanels } from './ComponentDetail';
import { MariaDBSettings } from './MariaDBSettings';
import { useI18n } from '../i18n';

interface MariaDBManagementProps {
    onBack: () => void;
}

interface ConfigFile {
    path: string;
}

// MariaDB on ServiceShell. Status + start/stop come from the shell; the body
// is the real visual config editor over the server's own .cnf files (fetched
// from managed-services), plus a small tips card.
//
// MariaDB, ServiceShell üzerinde. Durum + başlat/durdur kabuktan gelir; gövde,
// sunucunun kendi .cnf dosyaları (managed-services'ten alınır) üzerinde gerçek
// görsel yapılandırma düzenleyicisi ve küçük bir ipuçları kartıdır.
export function MariaDBManagement({ onBack }: MariaDBManagementProps) {
    const { t } = useI18n();
    const [files, setFiles] = useState<ConfigFile[]>([]);
    const [selected, setSelected] = useState<string | null>(null);

    useEffect(() => {
        fetch('/api/v1/managed-services')
            .then((r) => (r.ok ? r.json() : { services: [] }))
            .then((data) => {
                const cfgs: ConfigFile[] = data?.services?.find((s: { id: string }) => s.id === 'mariadb')?.config_files ?? [];
                setFiles(cfgs);
                const preferred =
                    cfgs.find((f) => f.path.endsWith('50-server.cnf')) ??
                    cfgs.find((f) => f.path.endsWith('my.cnf')) ??
                    cfgs[0];
                if (preferred) setSelected(preferred.path);
            })
            .catch(() => {});
    }, []);

    const tips = [t('mariadb.tip.bind'), t('mariadb.tip.buffer'), t('mariadb.tip.logs')];

    return (
        <ServiceShell serviceId="mariadb" name="MariaDB" icon={Database} onBack={onBack}>
            <div className="grid grid-cols-1 gap-5 lg:grid-cols-3">
                <div className="lg:col-span-2">
                    {files.length > 0 && (
                        <div className="mb-4">
                            <label className="mb-1.5 block text-sm font-medium text-fg-muted">{t('db.selectConfigFile')}</label>
                            <select
                                value={selected ?? ''}
                                onChange={(e) => setSelected(e.target.value)}
                                className="w-full rounded-lg border border-border bg-surface-2 px-3 py-2 text-sm text-fg outline-none focus:border-primary focus:ring-2 focus:ring-primary/30"
                            >
                                {files.map((f) => (
                                    <option key={f.path} value={f.path}>
                                        {f.path}
                                    </option>
                                ))}
                            </select>
                        </div>
                    )}

                    <div className="rounded-xl border border-border bg-surface p-5 shadow-card">
                        {selected ? (
                            <MariaDBSettings key={selected} configPath={selected} />
                        ) : (
                            <p className="py-8 text-center text-sm text-fg-subtle">{t('db.fileNotFound', { file: 'my.cnf' })}</p>
                        )}
                    </div>
                </div>

                <div className="rounded-xl border border-border bg-surface p-6 shadow-card">
                    <div className="mb-4 flex items-center gap-2">
                        <Lightbulb className="h-5 w-5 text-warning" />
                        <h4 className="text-sm font-semibold text-fg">{t('mariadb.tips')}</h4>
                    </div>
                    <ul className="space-y-3 text-sm text-fg-muted">
                        {tips.map((tip) => (
                            <li key={tip} className="flex items-start gap-2">
                                <span className="mt-1.5 h-1 w-1 shrink-0 rounded-full bg-primary" />
                                {tip}
                            </li>
                        ))}
                    </ul>
                </div>
            </div>
            {/* Overview + journal; the config list is skipped — this page
                has a real editor for those files already. / Genel bakış +
                günlük; ayar listesi atlanır — bu sayfada o dosyalar için
                gerçek bir editör zaten var. */}
            <ComponentPanels serviceId="mariadb" show={{ configs: false }} />
        </ServiceShell>
    );
}
