import { useI18n } from '../i18n';
import { applyPostgresConf, parsePostgresConf } from '../lib/dbConfigText';
import { ConfigSettingsEditor, type ConfigSettingsFormat } from './ConfigSettingsEditor';

// The settings of postgresql.conf. The file is read with its version, shown
// only once it is known, and saved as the file that was read with the changed
// lines replaced (see ConfigSettingsEditor and lib/dbConfigText.ts).
//
// postgresql.conf ayarları. Dosya sürümüyle okunur, yalnız bilindiğinde
// gösterilir ve okunan dosyanın değişen satırları değiştirilerek kaydedilir.
const postgresFormat: ConfigSettingsFormat = { parse: parsePostgresConf, apply: applyPostgresConf };

export function PostgreSQLSettings({ configPath }: { configPath: string }) {
    const { t } = useI18n();
    return (
        <ConfigSettingsEditor
            path={configPath}
            file="postgresql.conf"
            service="PostgreSQL"
            title={t('dbconf.settingsOf', { file: 'postgresql.conf' })}
            format={postgresFormat}
        />
    );
}
