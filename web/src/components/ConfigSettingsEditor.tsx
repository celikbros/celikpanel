import { useEffect, useMemo, useState } from 'react';
import { ChevronDown, ChevronRight } from 'lucide-react';
import { useI18n } from '../i18n';
import { useConfigFile, type ConfigFile, type ConfigFileHandle } from '../lib/configFile';
import type { ConfigItem, ConfigSection, ItemEdit } from '../lib/dbConfigText';
import { ConfigFileGate, ConfigSaveBar, ConfigSaveNotices, FieldRefusal, refusalDetail, refusalLine, refusalName } from './ConfigFileNotices';
import { inputClass } from './ui';

// The settings of one configuration file as a searchable list: each setting
// the file holds, with whether it is set or commented out, and its value.
// postgresql.conf and a MariaDB option file both use it; what differs is how
// their text is read and written back (lib/dbConfigText.ts).
//
// Nothing here exists before the file has been read (ConfigFileGate), and a
// save is the file that was read with only the changed lines replaced.
//
// Tek bir yapılandırma dosyasının ayarları, aranabilir bir liste olarak.
// Dosya okunmadan burada hiçbir şey yoktur; kayıt, okunan dosyanın yalnız
// değişen satırları değiştirilmiş hâlidir.

export interface ConfigSettingsFormat {
    parse: (content: string) => ConfigSection[];
    apply: (content: string, edits: ReadonlyMap<number, ItemEdit>) => string;
    /** How a section title is written: "[mysqld]" for an option file. */
    sectionLabel?: (title: string) => string;
    /** Sections that start open, besides the first. */
    openByDefault?: string[];
}

export function ConfigSettingsEditor({
    path,
    file,
    service,
    title,
    format,
}: {
    path: string;
    /** The file's own name: "postgresql.conf". */
    file: string;
    /** The service's own name: "PostgreSQL". */
    service: string;
    title: string;
    format: ConfigSettingsFormat;
}) {
    const handle = useConfigFile(path);
    return (
        <div aria-busy={handle.remote.state === 'loading' || handle.saving}>
            <h3 className="mb-3 text-sm font-semibold text-fg">{title}</h3>
            <ConfigFileGate handle={handle} file={file}>
                {(value) => <SettingsForm key={value.version} handle={handle} value={value} file={file} service={service} format={format} />}
            </ConfigFileGate>
        </div>
    );
}

function SettingsForm({
    handle,
    value,
    file,
    service,
    format,
}: {
    handle: ConfigFileHandle;
    value: ConfigFile;
    file: string;
    service: string;
    format: ConfigSettingsFormat;
}) {
    const { t } = useI18n();
    const sections = useMemo(() => format.parse(value.content), [format, value.content]);
    // What the person changed, by the line of the file it stands on. A setting
    // that is put back to what the file holds leaves the map again.
    // Kişinin değiştirdikleri, dosyadaki satırlarına göre.
    const [edits, setEdits] = useState<ReadonlyMap<number, ItemEdit>>(new Map());
    const [search, setSearch] = useState('');
    const [open, setOpen] = useState<Record<string, boolean>>(() => {
        const initial: Record<string, boolean> = {};
        for (const title of format.openByDefault ?? []) initial[title] = true;
        if (sections.length > 0 && !Object.keys(initial).some((title) => sections.some((section) => section.title === title))) {
            initial[sections[0].title] = true;
        }
        return initial;
    });

    const refusal = handle.refusal;
    const refusedLine = refusalLine(refusal);
    const refusedName = refusalName(refusal);
    const refused = (item: ConfigItem) => !!refusal && !!refusalDetail(refusal)
        && (refusedLine >= 0 ? item.line === refusedLine : !!refusedName && item.key === refusedName && (edits.has(item.line) || item.enabled));
    const placed = sections.some((section) => section.items.some(refused));

    // The section that holds the refused setting opens, so the message next to
    // the field is on screen.
    // Reddedilen ayarı tutan bölüm açılır.
    useEffect(() => {
        if (!refusal) return;
        const holder = sections.find((section) => section.items.some(refused));
        if (holder) setOpen((before) => ({ ...before, [holder.title]: true }));
    }, [refusal]);

    const change = (item: ConfigItem, next: ItemEdit) => {
        handle.clearRefusal();
        setEdits((before) => {
            const after = new Map(before);
            if (next.value === item.value && next.enabled === item.enabled) after.delete(item.line);
            else after.set(item.line, next);
            return after;
        });
    };

    const term = search.trim().toLowerCase();
    const shown = term
        ? sections
            .map((section) => ({
                ...section,
                items: section.items.filter((item) => item.key.toLowerCase().includes(term) || item.description.toLowerCase().includes(term)),
            }))
            .filter((section) => section.items.length > 0)
        : sections;
    const label = (sectionTitle: string) => (sectionTitle ? (format.sectionLabel?.(sectionTitle) ?? sectionTitle) : t('dbconf.beforeFirstSection'));

    if (sections.length === 0) {
        // The file was read and holds nothing this list can show. That is a
        // known answer about this file, not a failed read.
        // Dosya okundu ve bu listenin gösterebileceği bir şey tutmuyor.
        return <p className="py-6 text-sm text-fg-muted">{t('dbconf.noSettings', { file })}</p>;
    }

    return (
        <>
            <ConfigSaveNotices handle={handle} file={file} service={service} placed={placed} />
            <input
                type="search"
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                aria-label={t('dbconf.search')}
                placeholder={t('dbconf.search')}
                className={`${inputClass} mb-3 max-w-sm`}
            />
            {shown.length === 0 ? (
                <p className="py-6 text-sm text-fg-muted">{t('dbconf.noMatch', { term: search.trim() })}</p>
            ) : (
                <div className="border-b border-border">
                    {shown.map((section) => {
                        const expanded = !!term || !!open[section.title];
                        const changed = section.items.filter((item) => edits.has(item.line)).length;
                        return (
                            <section key={section.title} className="border-t border-border">
                                <button
                                    type="button"
                                    aria-expanded={expanded}
                                    onClick={() => setOpen((before) => ({ ...before, [section.title]: !before[section.title] }))}
                                    className="flex min-h-[2.75rem] w-full items-center gap-2 py-2 text-left text-sm font-semibold text-fg hover:text-primary"
                                >
                                    {expanded
                                        ? <ChevronDown className="h-4 w-4 shrink-0 text-fg-muted" aria-hidden="true" />
                                        : <ChevronRight className="h-4 w-4 shrink-0 text-fg-muted" aria-hidden="true" />}
                                    <span className="min-w-0 break-words">{label(section.title)}</span>
                                    <span className="ml-auto shrink-0 text-xs font-normal tabular-nums text-fg-muted">
                                        {changed > 0 ? t('dbconf.sectionChanged', { n: section.items.length, changed }) : section.items.length}
                                    </span>
                                </button>
                                {expanded && (
                                    <ul className="divide-y divide-border border-t border-border">
                                        {section.items.map((item) => (
                                            <SettingRow
                                                key={item.line}
                                                item={item}
                                                edit={edits.get(item.line)}
                                                onChange={(next) => change(item, next)}
                                                disabled={handle.saving || handle.stale}
                                                service={service}
                                                refusal={refused(item) ? refusal : null}
                                            />
                                        ))}
                                    </ul>
                                )}
                            </section>
                        );
                    })}
                </div>
            )}
            <p className="mt-3 max-w-[75ch] text-xs leading-relaxed text-fg-muted">{t('dbconf.note')}</p>
            <ConfigSaveBar
                handle={handle}
                changes={edits.size}
                onSave={() => void handle.save(format.apply(value.content, edits))}
                onDiscard={() => { handle.clearRefusal(); setEdits(new Map()); }}
            />
        </>
    );
}

function SettingRow({
    item,
    edit,
    onChange,
    disabled,
    service,
    refusal,
}: {
    item: ConfigItem;
    edit: ItemEdit | undefined;
    onChange: (next: ItemEdit) => void;
    disabled: boolean;
    service: string;
    refusal: ReturnType<typeof useConfigFile>['refusal'];
}) {
    const { t } = useI18n();
    const current = edit ?? { value: item.value, enabled: item.enabled };
    const id = `setting-${item.line}`;
    return (
        <li className={`grid gap-x-4 gap-y-1.5 py-2.5 sm:grid-cols-[minmax(0,18rem)_minmax(0,1fr)] ${edit ? 'bg-surface-2/60' : ''}`}>
            <label className="flex min-h-[2.75rem] cursor-pointer items-start gap-2.5 py-1 sm:min-h-0">
                <input
                    type="checkbox"
                    checked={current.enabled}
                    disabled={disabled}
                    onChange={(event) => onChange({ ...current, enabled: event.target.checked })}
                    aria-label={t('dbconf.setInFile', { name: item.key })}
                    className="mt-1 h-4 w-4 shrink-0 accent-primary"
                />
                <span className="min-w-0">
                    <span className="block break-all font-mono text-sm text-fg">{item.key}</span>
                    {!current.enabled && <span className="block text-xs text-fg-muted">{t('dbconf.commentedOut')}</span>}
                    {edit && <span className="block text-xs font-medium text-fg">{t('dbconf.changed')}</span>}
                </span>
            </label>
            <div className="min-w-0">
                <input
                    type="text"
                    value={current.value}
                    disabled={disabled || !current.enabled}
                    onChange={(event) => onChange({ ...current, value: event.target.value })}
                    aria-label={t('dbconf.valueOf', { name: item.key })}
                    aria-invalid={refusal ? true : undefined}
                    aria-describedby={refusal ? `${id}-refusal` : undefined}
                    spellCheck={false}
                    autoComplete="off"
                    className={`w-full rounded-lg border bg-surface px-3 py-2 font-mono text-sm text-fg outline-none transition-shadow focus:border-primary disabled:bg-surface-2 disabled:text-fg-muted ${
                        refusal ? 'border-danger' : 'border-border-strong'
                    }`}
                />
                {refusal && <FieldRefusal id={`${id}-refusal`} service={service} refusal={refusal} />}
                {item.description && <p className="mt-1 break-words text-xs text-fg-muted">{item.description}</p>}
            </div>
        </li>
    );
}
