import { useI18n } from '../i18n';
import { applyOptionFile, parseOptionFile } from '../lib/dbConfigText';
import { ConfigSettingsEditor, type ConfigSettingsFormat } from './ConfigSettingsEditor';

// The options of one MariaDB option file, by group. The file is read with its
// version, shown only once it is known, and saved as the file that was read
// with the changed lines replaced (see ConfigSettingsEditor and
// lib/dbConfigText.ts). MariaDB reads its option files only when it starts, so
// a save says the change waits for the next restart.
//
// Bir MariaDB seçenek dosyasının seçenekleri, gruba göre. MariaDB seçenek
// dosyalarını yalnız başlarken okur; kayıt, değişikliğin bir sonraki yeniden
// başlatmayı beklediğini söyler.
const optionFormat: ConfigSettingsFormat = {
    parse: parseOptionFile,
    apply: applyOptionFile,
    sectionLabel: (title) => `[${title}]`,
    openByDefault: ['mysqld', 'mariadb', 'server'],
};

export function MariaDBSettings({ configPath }: { configPath: string }) {
    const { t } = useI18n();
    const file = configPath.split('/').pop() || configPath;
    return (
        <ConfigSettingsEditor
            path={configPath}
            file={file}
            service="MariaDB"
            title={t('dbconf.settingsOf', { file })}
            format={optionFormat}
        />
    );
}
