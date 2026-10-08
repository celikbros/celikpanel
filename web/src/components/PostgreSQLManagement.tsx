import { useState } from 'react';
import { Database, Settings, ShieldCheck, FileCode, type LucideIcon } from 'lucide-react';
import { ServiceShell } from './ServiceShell';
import { ComponentPanels } from './ComponentDetail';
import { ConfigEditor } from './ConfigEditor';
import { PostgreSQLSettings } from './PostgreSQLSettings';
import { PostgreSQLAccessRules } from './PostgreSQLAccessRules';
import { useI18n } from '../i18n';
import { useComponentConfigFiles } from '../lib/managedServices';
import { Checking, CouldNotCheck } from './ui';

interface PostgreSQLManagementProps {
    onBack: () => void;
}

// PostgreSQL on ServiceShell. The shell owns status + start/stop; the body is
// real config editing: visual settings (postgresql.conf), access rules
// (pg_hba.conf), and raw file editing. Config files come from managed-services.
//
// Which files PostgreSQL has on this server is read from the server, and it is
// one of three things (9 Oct 2026): being read, could not be read, or known.
// "postgresql.conf not found" is said only for a scan that was read and does
// not name the file; before, it stood on an installed server for as long as
// the read took and for good when it failed.
//
// PostgreSQL, ServiceShell üzerinde. Durum + başlat/durdur kabuğa aittir;
// gövde gerçek yapılandırma düzenlemesidir. PostgreSQL'in bu sunucuda hangi
// dosyaları olduğu sunucudan okunur ve üç şeyden biridir: okunuyor, okunamadı
// ya da biliniyor. "postgresql.conf bulunamadı" yalnız okunmuş ve dosyayı
// adlandırmayan bir tarama için söylenir.
export function PostgreSQLManagement({ onBack }: PostgreSQLManagementProps) {
    const { t } = useI18n();
    const { files, retry, reading } = useComponentConfigFiles('postgresql');
    const [tab, setTab] = useState<'visual' | 'access'>('visual');
    const [rawFile, setRawFile] = useState<string | null>(null);

    if (rawFile) {
        return <ConfigEditor path={rawFile} onBack={() => setRawFile(null)} />;
    }

    const known = files.state === 'known' ? files.value : null;
    const mainConf = known?.find((path) => path.endsWith('postgresql.conf'));
    const hbaConf = known?.find((path) => path.endsWith('pg_hba.conf'));

    return (
        <ServiceShell serviceId="postgresql" name="PostgreSQL" icon={Database} onBack={onBack}>
            <div className="mb-4 flex items-center gap-1 border-b border-border">
                <Tab active={tab === 'visual'} onClick={() => setTab('visual')} icon={Settings} label={t('db.tab.visual')} />
                <Tab active={tab === 'access'} onClick={() => setTab('access')} icon={ShieldCheck} label={t('db.tab.access')} />
            </div>

            <div className="rounded-xl border border-border bg-surface p-5">
                {files.state === 'loading' ? (
                    <Checking label={t('dbconf.files.checking', { service: 'PostgreSQL' })} className="min-h-[2.75rem] py-2" />
                ) : !known ? (
                    <CouldNotCheck text={t('dbconf.files.unknown', { service: 'PostgreSQL' })} onRetry={retry} busy={reading} />
                ) : tab === 'visual' ? (
                    mainConf ? (
                        <PostgreSQLSettings configPath={mainConf} />
                    ) : (
                        <p className="py-8 text-center text-sm text-fg-muted">{t('db.fileNotFound', { file: 'postgresql.conf' })}</p>
                    )
                ) : hbaConf ? (
                    <PostgreSQLAccessRules configPath={hbaConf} />
                ) : (
                    <p className="py-8 text-center text-sm text-fg-muted">{t('db.fileNotFound', { file: 'pg_hba.conf' })}</p>
                )}
            </div>

            {known && known.length > 0 && (
                <div className="mt-6">
                    <h4 className="mb-2 text-xs font-semibold uppercase tracking-wider text-fg-muted">{t('db.rawFiles')}</h4>
                    <div className="grid grid-cols-1 gap-2 md:grid-cols-2">
                        {known.map((path) => (
                            <button
                                key={path}
                                onClick={() => setRawFile(path)}
                                className="flex min-h-[2.75rem] items-center gap-2.5 rounded-lg border border-border bg-surface px-3 py-2 text-left transition-colors hover:bg-surface-2"
                            >
                                <FileCode className="h-4 w-4 shrink-0 text-fg-muted" aria-hidden="true" />
                                <span className="truncate font-mono text-xs text-fg-muted">{path}</span>
                            </button>
                        ))}
                    </div>
                </div>
            )}
            {/* Overview + journal; the config list is skipped — this page
                has a real editor for those files already. / Genel bakış +
                günlük; ayar listesi atlanır — bu sayfada o dosyalar için
                gerçek bir editör zaten var.
                They are drawn from the same read as the file list above, so
                they come with its answer: while that read is on its way or
                has failed, the card above says so once, with one Retry.
                Üstteki dosya listesiyle aynı okumadan çizilirler; o okuma
                sürerken ya da başarısız olduğunda bunu üstteki kart bir kez,
                tek bir Tekrar dene ile söyler. */}
            {known && <ComponentPanels serviceId="postgresql" show={{ configs: false }} />}
        </ServiceShell>
    );
}

function Tab({ active, onClick, icon: Icon, label }: { active: boolean; onClick: () => void; icon: LucideIcon; label: string }) {
    return (
        <button
            onClick={onClick}
            aria-pressed={active}
            className={`-mb-px flex min-h-[2.75rem] items-center gap-2 border-b-2 px-3 py-2.5 text-sm font-medium transition-colors ${
                active ? 'border-primary text-primary' : 'border-transparent text-fg-muted hover:text-fg'
            }`}
        >
            <Icon className="h-4 w-4" aria-hidden="true" />
            {label}
        </button>
    );
}
