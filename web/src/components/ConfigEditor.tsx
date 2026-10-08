import { useEffect, useState } from 'react';
import { ArrowLeft, FileCode } from 'lucide-react';
import { useI18n } from '../i18n';
import { useConfigFile, type ConfigFile, type ConfigFileHandle } from '../lib/configFile';
import { ConfigFileGate, ConfigSaveNotices } from './ConfigFileNotices';
import { Button } from './ui';

interface ConfigEditorProps {
    path: string;
    onBack: () => void;
}

// The whole text of one managed configuration file. It follows the same rule
// as the visual editors (9 Oct 2026): the text area exists only for a file
// that was read, the save carries the version of that read, and a refused save
// keeps what was typed with the reason beside it.
//
// Yönetilen bir yapılandırma dosyasının bütün metni. Görsel düzenleyicilerle
// aynı kurala uyar: metin alanı yalnız okunmuş bir dosya için vardır, kayıt o
// okumanın sürümünü taşır ve reddedilen kayıt, yazılanı gerekçesiyle birlikte
// ekranda tutar.
export function ConfigEditor({ path, onBack }: ConfigEditorProps) {
    const { t } = useI18n();
    const handle = useConfigFile(path);
    const file = path.split('/').pop() || path;
    return (
        <div className="rounded-xl border border-border bg-surface" aria-busy={handle.remote.state === 'loading' || handle.saving}>
            <div className="flex flex-wrap items-center gap-3 border-b border-border px-5 py-3">
                <button
                    type="button"
                    onClick={onBack}
                    aria-label={t('common.back')}
                    title={t('common.back')}
                    className="inline-flex h-11 w-11 shrink-0 items-center justify-center rounded-lg text-fg-muted transition-colors hover:bg-surface-2 hover:text-fg"
                >
                    <ArrowLeft className="h-5 w-5" aria-hidden="true" />
                </button>
                <FileCode className="h-5 w-5 shrink-0 text-fg-muted" aria-hidden="true" />
                <h3 className="min-w-0 break-all font-mono text-sm text-fg">{path}</h3>
            </div>
            <div className="p-5">
                <ConfigFileGate handle={handle} file={file}>
                    {(value) => <RawText key={value.version} handle={handle} value={value} file={file} />}
                </ConfigFileGate>
            </div>
        </div>
    );
}

function RawText({ handle, value, file }: { handle: ConfigFileHandle; value: ConfigFile; file: string }) {
    const { t } = useI18n();
    const [text, setText] = useState(value.content);
    useEffect(() => setText(value.content), [value.content]);
    const changed = text !== value.content;
    return (
        <>
            <ConfigSaveNotices handle={handle} file={file} service={t('dbconf.theService')} placed={false} />
            <textarea
                value={text}
                onChange={(event) => { handle.clearRefusal(); setText(event.target.value); }}
                disabled={handle.saving || handle.stale}
                aria-label={t('dbconf.raw.text', { file })}
                spellCheck={false}
                className="block h-[calc(100dvh-24rem)] min-h-[18rem] w-full resize-y rounded-lg border border-border-strong bg-bg p-4 font-mono text-sm leading-relaxed text-fg outline-none focus:border-primary disabled:text-fg-muted"
            />
            <div className="mt-4 flex flex-wrap items-center justify-end gap-x-3 gap-y-2">
                <p className="min-w-0 basis-full text-xs text-fg-muted sm:mr-auto sm:basis-auto" aria-live="polite">
                    {changed ? t('dbconf.raw.changed') : t('dbconf.noChanges')}
                </p>
                <Button type="button" onClick={() => { handle.clearRefusal(); setText(value.content); }} disabled={!changed || handle.saving}>
                    {t('dbconf.discard')}
                </Button>
                <Button type="button" variant="primary" onClick={() => void handle.save(text)} disabled={!changed || handle.stale} loading={handle.saving}>
                    {handle.saving ? t('dbconf.saving') : t('dbconf.save')}
                </Button>
            </div>
        </>
    );
}
