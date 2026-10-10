import { useMemo, useState, type ReactNode } from 'react';
import { Plus, Trash2 } from 'lucide-react';
import { useI18n } from '../i18n';
import { useConfigFile, type ConfigFile, type ConfigFileHandle } from '../lib/configFile';
import { applyHba, hbaMethods, hbaRuleComplete, hbaTypes, parseHba, type HbaFields, type HbaRule } from '../lib/dbConfigText';
import { ConfigFileGate, ConfigSaveBar, ConfigSaveNotices, FieldRefusal, refusalDetail, refusalLine } from './ConfigFileNotices';
import { Button } from './ui';

// The access rules of pg_hba.conf, in the order PostgreSQL reads them.
//
// The file is read with its version and shown only once it is known. A save is
// the file that was read with the changed rules replaced, the removed rules
// gone and new rules added at the end; comments, includes and every rule this
// grid cannot hold stay exactly as written (lib/dbConfigText.ts). The server
// refuses a save that would take away the local administrator access, and its
// answer is shown here.
//
// Before 9 Oct 2026 this screen wrote the file from scratch, without its
// comments, and after a failed read it showed "No access rules" with Save on:
// a file of comments only, which refuses every connection.
//
// pg_hba.conf erişim kuralları, PostgreSQL'in okuduğu sırayla. Dosya sürümüyle
// okunur ve yalnız bilindiğinde gösterilir. Kayıt, okunan dosyanın değişen
// kuralları değiştirilmiş, kaldırılanları çıkarılmış ve yenileri sona eklenmiş
// hâlidir; yorumlar, include'lar ve bu tablonun tutamadığı her kural yazıldığı
// gibi kalır.
export function PostgreSQLAccessRules({ configPath }: { configPath: string }) {
    const { t } = useI18n();
    const handle = useConfigFile(configPath);
    return (
        <div aria-busy={handle.remote.state === 'loading' || handle.saving}>
            <h3 className="mb-3 text-sm font-semibold text-fg">{t('dbconf.hba.title')}</h3>
            <ConfigFileGate handle={handle} file="pg_hba.conf">
                {(value) => <RulesForm key={value.version} handle={handle} value={value} />}
            </ConfigFileGate>
        </div>
    );
}

const blankRule: HbaFields = { type: 'host', database: '', user: '', address: '', method: 'scram-sha-256' };

function RulesForm({ handle, value }: { handle: ConfigFileHandle; value: ConfigFile }) {
    const { t } = useI18n();
    const rules = useMemo(() => parseHba(value.content), [value.content]);
    const [edited, setEdited] = useState<ReadonlyMap<number, HbaFields>>(new Map());
    const [removed, setRemoved] = useState<ReadonlySet<number>>(new Set());
    const [added, setAdded] = useState<readonly HbaFields[]>([]);

    const fieldsOf = (rule: HbaRule): HbaFields => edited.get(rule.line) ?? rule;
    const same = (a: HbaFields, b: HbaFields) => a.type === b.type && a.database === b.database && a.user === b.user
        && (a.type === 'local' || a.address === b.address) && a.method === b.method;
    const editRule = (rule: HbaRule, next: HbaFields) => {
        handle.clearRefusal();
        setEdited((before) => {
            const after = new Map(before);
            if (same(next, rule)) after.delete(rule.line);
            else after.set(rule.line, next);
            return after;
        });
    };
    const toggleRemoved = (line: number) => {
        handle.clearRefusal();
        setRemoved((before) => {
            const after = new Set(before);
            if (!after.delete(line)) after.add(line);
            return after;
        });
    };

    const changes = edited.size + removed.size + added.length;
    const incomplete = [...edited.values(), ...added].some((rule) => !hbaRuleComplete(rule));
    const next = useMemo(() => applyHba(value.content, { edited, removed, added }), [value.content, edited, removed, added]);

    // A refusal that names a line is shown next to the rule on that line of
    // the file that was sent.
    // Bir satırı adlandıran ret, gönderilen dosyanın o satırındaki kuralın
    // yanında gösterilir.
    const refusal = handle.refusal;
    const refusedLine = refusal && refusalDetail(refusal) ? refusalLine(refusal) : -1;
    const sentLineOf = (rule: HbaRule) => rule.line - [...removed].filter((line) => line < rule.line).length;
    const refusedAdded = next.addedLines.indexOf(refusedLine);
    const refusedRule = refusedLine < 0 ? undefined : rules.find((rule) => !removed.has(rule.line) && sentLineOf(rule) === refusedLine);
    const placed = refusedAdded >= 0 || !!refusedRule;
    const locked = handle.saving || handle.stale;

    const discard = () => {
        handle.clearRefusal();
        setEdited(new Map());
        setRemoved(new Set());
        setAdded([]);
    };

    return (
        <>
            <ConfigSaveNotices handle={handle} file="pg_hba.conf" service="PostgreSQL" placed={placed} />
            {rules.length === 0 && added.length === 0 ? (
                // The file was read and holds no rule: a known answer.
                // Dosya okundu ve hiç kural tutmuyor: bilinen bir yanıt.
                <p className="py-4 text-sm text-fg-muted">{t('dbconf.hba.none')}</p>
            ) : (
                <ol className="border-b border-border">
                    {rules.map((rule, index) => (
                        <li key={rule.line} className={`border-t border-border py-2.5 ${edited.has(rule.line) ? 'bg-surface-2/60' : ''}`}>
                            {rule.editable ? (
                                <RuleFields
                                    number={index + 1}
                                    first={index === 0}
                                    fields={fieldsOf(rule)}
                                    onChange={(fields) => editRule(rule, fields)}
                                    disabled={locked || removed.has(rule.line)}
                                    note={removed.has(rule.line) ? t('dbconf.hba.willBeRemoved') : edited.has(rule.line) ? t('dbconf.changed') : ''}
                                    action={
                                        <button
                                            type="button"
                                            disabled={locked}
                                            onClick={() => toggleRemoved(rule.line)}
                                            aria-label={t(removed.has(rule.line) ? 'dbconf.hba.keep' : 'dbconf.hba.remove', { n: index + 1 })}
                                            title={t(removed.has(rule.line) ? 'dbconf.hba.keep' : 'dbconf.hba.remove', { n: index + 1 })}
                                            className="inline-flex h-11 w-11 shrink-0 items-center justify-center rounded-md text-fg-muted hover:bg-surface-2 hover:text-danger disabled:pointer-events-none disabled:opacity-50"
                                        >
                                            {removed.has(rule.line)
                                                ? <span className="text-xs font-medium text-fg">{t('dbconf.hba.keepShort')}</span>
                                                : <Trash2 className="h-4 w-4" aria-hidden="true" />}
                                        </button>
                                    }
                                />
                            ) : (
                                <div className="grid gap-x-3 gap-y-1 sm:grid-cols-[2rem_minmax(0,1fr)_2.75rem]">
                                    <span className="pt-0.5 text-xs tabular-nums text-fg-muted">{index + 1}</span>
                                    <div className="min-w-0">
                                        <pre className="overflow-x-auto whitespace-pre-wrap break-all font-mono text-sm text-fg">{rule.text}</pre>
                                        <p className="mt-1 text-xs text-fg-muted">{t('dbconf.hba.asWritten')}</p>
                                    </div>
                                </div>
                            )}
                            {refusal && refusedRule === rule && <FieldRefusal id={`hba-${rule.line}-refusal`} service="PostgreSQL" refusal={refusal} />}
                        </li>
                    ))}
                    {added.map((rule, index) => (
                        <li key={`new-${index}`} className="border-t border-border bg-surface-2/60 py-2.5">
                            <RuleFields
                                number={rules.length + index + 1}
                                first={rules.length + index === 0}
                                fields={rule}
                                onChange={(fields) => {
                                    handle.clearRefusal();
                                    setAdded((before) => before.map((item, at) => (at === index ? fields : item)));
                                }}
                                disabled={locked}
                                note={t('dbconf.hba.new')}
                                action={
                                    <button
                                        type="button"
                                        disabled={locked}
                                        onClick={() => { handle.clearRefusal(); setAdded((before) => before.filter((_, at) => at !== index)); }}
                                        aria-label={t('dbconf.hba.remove', { n: rules.length + index + 1 })}
                                        title={t('dbconf.hba.remove', { n: rules.length + index + 1 })}
                                        className="inline-flex h-11 w-11 shrink-0 items-center justify-center rounded-md text-fg-muted hover:bg-surface-2 hover:text-danger disabled:pointer-events-none disabled:opacity-50"
                                    >
                                        <Trash2 className="h-4 w-4" aria-hidden="true" />
                                    </button>
                                }
                            />
                            {refusal && refusedAdded === index && <FieldRefusal id={`hba-new-${index}-refusal`} service="PostgreSQL" refusal={refusal} />}
                        </li>
                    ))}
                </ol>
            )}
            <div className="mt-3 flex flex-wrap items-start justify-between gap-3">
                <p className="max-w-[75ch] text-xs leading-relaxed text-fg-muted">{t('dbconf.hba.order')}</p>
                <Button type="button" icon={Plus} disabled={locked} onClick={() => { handle.clearRefusal(); setAdded((before) => [...before, blankRule]); }}>
                    {t('dbconf.hba.add')}
                </Button>
            </div>
            <ConfigSaveBar
                handle={handle}
                changes={changes}
                blockedBy={changes > 0 && incomplete ? t('dbconf.hba.incomplete') : undefined}
                onSave={() => void handle.save(next.content)}
                onDiscard={discard}
            />
        </>
    );
}

const cell = 'w-full rounded-lg border border-border-strong bg-surface px-2.5 py-2 font-mono text-sm text-fg outline-none focus:border-primary disabled:bg-surface-2 disabled:text-fg-muted';

function RuleFields({
    number,
    first,
    fields,
    onChange,
    disabled,
    note,
    action,
}: {
    number: number;
    /** The first row of the list carries the column names on a wide screen. */
    first: boolean;
    fields: HbaFields;
    onChange: (fields: HbaFields) => void;
    disabled: boolean;
    note: string;
    action: ReactNode;
}) {
    const { t } = useI18n();
    const local = fields.type === 'local';
    // A method or type the file holds that this list does not offer stays
    // selectable, so opening a rule never changes it.
    // Dosyanın tuttuğu, listede olmayan bir yöntem ya da tür seçilebilir kalır.
    const types = hbaTypes.includes(fields.type) ? hbaTypes : [fields.type, ...hbaTypes];
    const methods = hbaMethods.includes(fields.method) ? hbaMethods : [fields.method, ...hbaMethods];
    // On a wide screen the rules read as a table: the first row names the
    // columns, and the rows under it keep their names for assistive readers.
    // Geniş ekranda kurallar tablo gibi okunur: sütun adlarını ilk satır taşır.
    const name = `mb-1 block text-xs text-fg-muted ${first ? '' : 'xl:sr-only'}`;
    const text = (field: 'database' | 'user' | 'address', label: string) => (
        <label className="min-w-0">
            <span className={name}>{label}</span>
            <input
                type="text"
                value={fields[field]}
                disabled={disabled}
                onChange={(event) => onChange({ ...fields, [field]: event.target.value })}
                spellCheck={false}
                autoComplete="off"
                className={cell}
            />
        </label>
    );
    return (
        <div className="grid grid-cols-[minmax(0,1fr)_2.75rem] items-start gap-x-3 gap-y-1 sm:grid-cols-[2rem_minmax(0,1fr)_2.75rem] sm:gap-y-2">
            <span className={`self-center text-xs tabular-nums text-fg-muted sm:self-start sm:pt-7 ${first ? '' : 'xl:pt-2.5'}`}>{number}</span>
            <div className="col-span-2 row-start-2 grid min-w-0 grid-cols-2 gap-2 sm:col-span-1 sm:col-start-2 sm:row-start-1 xl:grid-cols-[8rem_minmax(0,1fr)_minmax(0,1fr)_minmax(0,1.2fr)_10rem]">
                <label className="min-w-0">
                    <span className={name}>{t('dbconf.hba.type')}</span>
                    <select
                        value={fields.type}
                        disabled={disabled}
                        onChange={(event) => onChange({ ...fields, type: event.target.value, address: event.target.value === 'local' ? '' : fields.address })}
                        className={cell}
                    >
                        {types.map((type) => <option key={type} value={type}>{type}</option>)}
                    </select>
                </label>
                {text('database', t('dbconf.hba.database'))}
                {text('user', t('dbconf.hba.user'))}
                {local ? (
                    <div className="min-w-0">
                        <span className={name}>{t('dbconf.hba.address')}</span>
                        <p className="py-2 text-sm text-fg-muted">{t('dbconf.hba.localSocket')}</p>
                    </div>
                ) : text('address', t('dbconf.hba.address'))}
                <label className="col-span-2 min-w-0 xl:col-span-1">
                    <span className={name}>{t('dbconf.hba.method')}</span>
                    <select
                        value={fields.method}
                        disabled={disabled}
                        onChange={(event) => onChange({ ...fields, method: event.target.value })}
                        className={cell}
                    >
                        {methods.map((method) => <option key={method} value={method}>{method}</option>)}
                    </select>
                </label>
                {note && <p className="col-span-full text-xs font-medium text-fg">{note}</p>}
            </div>
            <div className={`col-start-2 row-start-1 flex justify-end sm:col-start-3 sm:pt-5 ${first ? '' : 'xl:pt-0'}`}>{action}</div>
        </div>
    );
}
