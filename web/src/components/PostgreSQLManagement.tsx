import { useState, useEffect } from 'react';
import { Database, Settings, ShieldCheck, FileCode, type LucideIcon } from 'lucide-react';
import { ServiceShell } from './ServiceShell';
import { ComponentPanels } from './ComponentDetail';
import { ConfigEditor } from './ConfigEditor';
import { PostgreSQLSettings } from './PostgreSQLSettings';
import { PostgreSQLAccessRules } from './PostgreSQLAccessRules';
import { useI18n } from '../i18n';

interface PostgreSQLManagementProps {
    onBack: () => void;
}

interface ConfigFile {
    path: string;
}

// PostgreSQL on ServiceShell. The shell owns status + start/stop; the body is
// real config editing: visual settings (postgresql.conf), access rules
// (pg_hba.conf), and raw file editing. Config files come from managed-services.
//
// PostgreSQL, ServiceShell üzerinde. Durum + başlat/durdur kabuğa aittir;
// gövde gerçek yapılandırma düzenlemesidir: görsel ayarlar (postgresql.conf),
// erişim kuralları (pg_hba.conf) ve ham dosya düzenleme. Yapılandırma
// dosyaları managed-services'ten gelir.
export function PostgreSQLManagement({ onBack }: PostgreSQLManagementProps) {
    const { t } = useI18n();
    const [files, setFiles] = useState<ConfigFile[]>([]);
    const [tab, setTab] = useState<'visual' | 'access'>('visual');
    const [rawFile, setRawFile] = useState<string | null>(null);

    useEffect(() => {
        fetch('/api/v1/managed-services')
            .then((r) => (r.ok ? r.json() : { services: [] }))
            .then((data) => setFiles(data?.services?.find((s: { id: string }) => s.id === 'postgresql')?.config_files ?? []))
            .catch(() => {});
    }, []);

    if (rawFile) {
        return <ConfigEditor path={rawFile} onBack={() => setRawFile(null)} />;
    }

    const mainConf = files.find((f) => f.path.endsWith('postgresql.conf'))?.path;
    const hbaConf = files.find((f) => f.path.endsWith('pg_hba.conf'))?.path;

    return (
        <ServiceShell serviceId="postgresql" name="PostgreSQL" icon={Database} onBack={onBack}>
            <div className="mb-4 flex items-center gap-1 border-b border-border">
                <Tab active={tab === 'visual'} onClick={() => setTab('visual')} icon={Settings} label={t('db.tab.visual')} />
                <Tab active={tab === 'access'} onClick={() => setTab('access')} icon={ShieldCheck} label={t('db.tab.access')} />
            </div>

            <div className="rounded-xl border border-border bg-surface p-5 shadow-card">
                {tab === 'visual' ? (
                    mainConf ? (
                        <PostgreSQLSettings configPath={mainConf} />
                    ) : (
                        <p className="py-8 text-center text-sm text-fg-subtle">{t('db.fileNotFound', { file: 'postgresql.conf' })}</p>
                    )
                ) : hbaConf ? (
                    <PostgreSQLAccessRules configPath={hbaConf} />
                ) : (
                    <p className="py-8 text-center text-sm text-fg-subtle">{t('db.fileNotFound', { file: 'pg_hba.conf' })}</p>
                )}
            </div>

            {files.length > 0 && (
                <div className="mt-6">
                    <h4 className="mb-2 text-xs font-semibold uppercase tracking-wider text-fg-subtle">{t('db.rawFiles')}</h4>
                    <div className="grid grid-cols-1 gap-2 md:grid-cols-2">
                        {files.map((f) => (
                            <button
                                key={f.path}
                                onClick={() => setRawFile(f.path)}
                                className="flex items-center gap-2.5 rounded-lg border border-border bg-surface px-3 py-2 text-left transition-colors hover:bg-surface-2"
                            >
                                <FileCode className="h-4 w-4 shrink-0 text-fg-subtle" />
                                <span className="truncate font-mono text-xs text-fg-muted">{f.path}</span>
                            </button>
                        ))}
                    </div>
                </div>
            )}
            {/* Overview + journal; the config list is skipped — this page
                has a real editor for those files already. / Genel bakış +
                günlük; ayar listesi atlanır — bu sayfada o dosyalar için
                gerçek bir editör zaten var. */}
            <ComponentPanels serviceId="postgresql" show={{ configs: false }} />
        </ServiceShell>
    );
}

function Tab({ active, onClick, icon: Icon, label }: { active: boolean; onClick: () => void; icon: LucideIcon; label: string }) {
    return (
        <button
            onClick={onClick}
            className={`-mb-px flex items-center gap-2 border-b-2 px-3 py-2.5 text-sm font-medium transition-colors ${
                active ? 'border-primary text-primary' : 'border-transparent text-fg-muted hover:text-fg'
            }`}
        >
            <Icon className="h-4 w-4" />
            {label}
        </button>
    );
}
