import { useState } from 'react';
import { Database, Lightbulb } from 'lucide-react';
import { ServiceShell } from './ServiceShell';
import { ComponentPanels } from './ComponentDetail';
import { MariaDBSettings } from './MariaDBSettings';
import { useI18n } from '../i18n';
import { useComponentConfigFiles } from '../lib/managedServices';
import { Checking, CouldNotCheck, inputClass } from './ui';
import { editorCardHeight } from './ConfigFileNotices';

interface MariaDBManagementProps {
    onBack: () => void;
}

// MariaDB on ServiceShell. Status + start/stop come from the shell; the body
// is the real visual config editor over the server's own .cnf files (fetched
// from managed-services), plus a small tips card.
//
// Which option files MariaDB has on this server is read from the server, and
// it is one of three things (9 Oct 2026): being read, could not be read, or
// known. "my.cnf not found" is said only for a scan that was read and names no
// file; before, it stood on an installed server for as long as the read took
// and for good when it failed.
//
// MariaDB, ServiceShell üzerinde. Durum + başlat/durdur kabuktan gelir; gövde,
// sunucunun kendi .cnf dosyaları üzerinde gerçek görsel yapılandırma
// düzenleyicisidir. MariaDB'nin bu sunucuda hangi seçenek dosyaları olduğu
// sunucudan okunur ve üç şeyden biridir: okunuyor, okunamadı ya da biliniyor.
export function MariaDBManagement({ onBack }: MariaDBManagementProps) {
    const { t } = useI18n();
    const { files, retry, reading } = useComponentConfigFiles('mariadb');
    // The file the person chose; until they choose, the server file that holds
    // the daemon's own options.
    // Kişinin seçtiği dosya; seçene kadar, sunucunun kendi seçeneklerini tutan dosya.
    const [chosen, setChosen] = useState<string | null>(null);

    const known = files.state === 'known' ? files.value : null;
    const preferred = known
        ? known.find((path) => path.endsWith('50-server.cnf')) ?? known.find((path) => path.endsWith('my.cnf')) ?? known[0]
        : undefined;
    const selected = chosen && known?.includes(chosen) ? chosen : preferred;

    const tips = [t('mariadb.tip.bind'), t('mariadb.tip.buffer'), t('mariadb.tip.logs')];

    return (
        <ServiceShell serviceId="mariadb" name="MariaDB" icon={Database} onBack={onBack}>
            <div className="grid grid-cols-1 gap-5 lg:grid-cols-3">
                <div className="min-w-0 lg:col-span-2">
                    {/* The chooser's place is kept while the files are read,
                        so the card under it does not move down when they
                        arrive. It is a placeholder of the same height, not a
                        field: nothing can be chosen before the files are
                        known. / Seçicinin yeri dosyalar okunurken ayrılır;
                        böylece altındaki kart dosyalar gelince aşağı kaymaz.
                        Aynı yükseklikte bir yer tutucudur, alan değildir. */}
                    {files.state === 'loading' && (
                        <div className="mb-4" aria-hidden="true">
                            <span className="mb-1.5 block text-sm font-medium text-fg-muted">{t('db.selectConfigFile')}</span>
                            <div className={`${inputClass} border-dashed bg-transparent font-mono`}>&nbsp;</div>
                        </div>
                    )}
                    {known && known.length > 0 && (
                        <div className="mb-4">
                            <label htmlFor="mariadb-config-file" className="mb-1.5 block text-sm font-medium text-fg-muted">{t('db.selectConfigFile')}</label>
                            <select
                                id="mariadb-config-file"
                                value={selected ?? ''}
                                onChange={(e) => setChosen(e.target.value)}
                                className={`${inputClass} font-mono`}
                            >
                                {known.map((path) => (
                                    <option key={path} value={path}>
                                        {path}
                                    </option>
                                ))}
                            </select>
                        </div>
                    )}

                    <div className={`rounded-xl border border-border bg-surface p-5 ${editorCardHeight}`}>
                        {files.state === 'loading' ? (
                            <Checking label={t('dbconf.files.checking', { service: 'MariaDB' })} className="min-h-[2.75rem] py-2" />
                        ) : !known ? (
                            <CouldNotCheck text={t('dbconf.files.unknown', { service: 'MariaDB' })} onRetry={retry} busy={reading} />
                        ) : selected ? (
                            <MariaDBSettings key={selected} configPath={selected} />
                        ) : (
                            <p className="py-8 text-center text-sm text-fg-muted">{t('db.fileNotFound', { file: 'my.cnf' })}</p>
                        )}
                    </div>
                </div>

                <div className="self-start rounded-xl border border-border bg-surface p-6">
                    <div className="mb-4 flex items-center gap-2">
                        <Lightbulb className="h-5 w-5 text-warning" aria-hidden="true" />
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
                gerçek bir editör zaten var.
                They are drawn from the same read as the file list above, so
                they come with its answer: while that read is on its way or
                has failed, the card above says so once, with one Retry.
                Üstteki dosya listesiyle aynı okumadan çizilirler; o okuma
                sürerken ya da başarısız olduğunda bunu üstteki kart bir kez,
                tek bir Tekrar dene ile söyler. */}
            {known && <ComponentPanels serviceId="mariadb" show={{ configs: false }} />}
        </ServiceShell>
    );
}
