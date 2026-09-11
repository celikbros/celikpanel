import { useCallback, useEffect, useState } from 'react';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import { decodeSetupComponentCatalog, setupComponentClosure, setupComponentGroups, type SetupComponentCatalog } from '../lib/serverSetupComponents';
import { Button } from './ui';

export function useSetupComponentCatalog(enabled: boolean) {
    const [catalog, setCatalog] = useState<SetupComponentCatalog | null>(null);
    const [failed, setFailed] = useState(false);
    const [attempt, setAttempt] = useState(0);
    const reload = useCallback(() => setAttempt(value => value + 1), []);
    useEffect(() => {
        if (!enabled) return;
        let alive = true;
        const controller = new AbortController();
        const timeout = window.setTimeout(() => controller.abort(), 12000);
        setFailed(false);
        setCatalog(null);
        void fetch('/api/v1/setup/components', { cache: 'no-store', signal: controller.signal }).then(async response => {
            if (!response.ok) throw new Error('catalog unavailable');
            const next = decodeSetupComponentCatalog(await response.json());
            if (!next) throw new Error('catalog invalid');
            if (alive) setCatalog(next);
        }).catch(() => { if (alive) setFailed(true); }).finally(() => window.clearTimeout(timeout));
        return () => { alive = false; controller.abort(); window.clearTimeout(timeout); };
    }, [enabled, attempt]);
    return { catalog, failed, reload };
}

const categories: Record<string, TranslationKey> = {
    web: 'setup.components.category.web', database: 'setup.components.category.database',
    email: 'setup.components.category.email', cache: 'setup.components.category.cache',
    security: 'setup.components.category.security',
};

export function ServerSetupComponents({ catalog, selected, onChange, disabled }: {
    catalog: SetupComponentCatalog;
    selected: string[];
    onChange: (selected: string[]) => void;
    disabled: boolean;
}) {
    const { t } = useI18n();
    const effective = setupComponentClosure(selected, catalog);
    const required = new Set(catalog.required_components);
    const groups = setupComponentGroups(catalog).filter(group => !group.some(row => required.has(row.id)));
    const lookup = new Map(catalog.components.map(row => [row.id, row]));
    const grouped = [...new Set(groups.map(group => group[0].category))];
    const unknown = selected.filter(id => !lookup.has(id));
    return <fieldset disabled={disabled} className="space-y-4">
        <legend className="text-xl font-semibold">{t('setup.components.title')}</legend>
        <p className="pt-2 text-sm leading-6 text-fg-muted">{t('setup.components.help')}</p>
        <p className="text-sm leading-6 text-fg-muted">{t('setup.components.preserve')}</p>
        {catalog.inventory_state === 'unknown' && <p role="alert" className="text-sm text-danger">{t('setup.components.inventoryUnknown')}</p>}
        {unknown.length > 0 && <div role="alert" className="space-y-2 text-sm text-danger"><p>{t('setup.components.savedUnavailable')}</p><Button type="button" onClick={() => onChange(selected.filter(id => lookup.has(id)))}>{t('setup.components.removeUnavailable')}</Button></div>}
        <div className="grid items-start gap-5 xl:grid-cols-2">
        {grouped.map(category => <fieldset key={category} className="rounded-lg border border-border bg-surface">
            <legend className="ml-4 px-1 text-base font-semibold">{t(categories[category] || 'setup.components.category.other')}</legend>
            <div className="divide-y divide-border">
                {groups.filter(group => group[0].category === category).map(group => {
                    const groupIDs = new Set(group.map(row => row.id));
                    const checked = group.some(row => effective.has(row.id));
                    const neededBy = selected.filter(id => !groupIDs.has(id) && group.some(row => setupComponentClosure([id], catalog).has(row.id)));
                    const conflicts = catalog.components.filter(row => effective.has(row.id) && !groupIDs.has(row.id)
                        && group.some(member => row.conflicts.includes(member.id) || member.conflicts.includes(row.id)));
                    const unavailable = catalog.inventory_state !== 'ready' || group.some(row => !row.supported);
                    const locked = neededBy.length > 0 || (!checked && (unavailable || conflicts.length > 0));
                    const installed = catalog.inventory_state === 'ready' && group.every(row => row.installed);
                    const name = group.map(row => row.name).join(' + ');
                    const rowID = `setup-component-${group[0].id}`;
                    return <div key={rowID} className="px-4 py-3">
                        <label htmlFor={rowID} className={`flex items-start gap-3 ${locked ? 'cursor-default' : 'cursor-pointer'}`}>
                            <input id={rowID} type="checkbox" checked={checked} disabled={locked} aria-describedby={`${rowID}-detail`}
                                onChange={() => onChange(checked ? selected.filter(id => !groupIDs.has(id)) : [...selected, group[0].id])}
                                className="mt-1 h-4 w-4 shrink-0 accent-primary" />
                            <span className="min-w-0 flex-1"><span className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1"><span className="font-medium">{name}</span>{installed && <span className="text-xs text-fg-muted">{t('setup.components.installed')}</span>}</span>
                                <span id={`${rowID}-detail`} className="mt-1 block text-sm leading-6 text-fg-muted">
                                    {neededBy.length > 0 ? t('setup.components.requiredBy', { names: neededBy.map(id => lookup.get(id)?.name || id).join(', ') })
                                        : unavailable ? t('setup.components.unavailable')
                                            : conflicts.length > 0 ? t('setup.components.conflicts', { names: conflicts.map(row => row.name).join(', ') })
                                                : group.length > 1 ? t('setup.components.together')
                                                    : group[0].dependencies.length > 0 ? t('setup.components.includes', { names: group[0].dependencies.map(id => lookup.get(id)?.name || id).join(', ') })
                                                        : installed ? t('setup.components.keep') : t('setup.components.optional')}
                                </span>
                            </span>
                        </label>
                    </div>;
                })}
            </div>
        </fieldset>)}
        </div>
        <div className="border-t border-border pt-5 text-sm leading-6 text-fg-muted"><p className="font-semibold text-fg">{t('setup.components.accessTitle')}</p><p className="mt-2">{t('setup.components.accessHelp')}</p></div>
    </fieldset>;
}
