import type { ServerSetupDraft, SetupPurpose } from './serverSetup';

export interface SetupComponent {
    id: string;
    name: string;
    category: string;
    dependencies: string[];
    conflicts: string[];
    supported: boolean;
    installed: boolean;
    reason?: string;
}
export interface SetupComponentCatalog {
    version: 1;
    inventory_state: 'ready' | 'unknown';
    presets: Record<SetupPurpose, string[]>;
    required_components: string[];
    components: SetupComponent[];
}
const isRecord = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value);
const ids = (value: unknown): value is string[] => Array.isArray(value) && value.length <= 80
    && value.every(id => typeof id === 'string' && /^[a-z][a-z0-9-]{0,63}$/.test(id)) && new Set(value).size === value.length;

export function decodeSetupComponentCatalog(value: unknown): SetupComponentCatalog | null {
    if (!isRecord(value) || value.version !== 1 || !['ready', 'unknown'].includes(String(value.inventory_state))
        || !isRecord(value.presets) || !ids(value.required_components) || !Array.isArray(value.components)
        || value.components.length > 80 || value.components.length === 0) return null;
    for (const item of value.components) {
        if (!isRecord(item) || !ids([item.id]) || typeof item.name !== 'string' || !item.name.trim() || item.name.length > 120
            || typeof item.category !== 'string' || item.category.length > 40 || !ids(item.dependencies) || !ids(item.conflicts)
            || typeof item.supported !== 'boolean' || typeof item.installed !== 'boolean'
            || (item.reason !== undefined && (typeof item.reason !== 'string' || item.reason.length > 300))) return null;
    }
    const rows = value.components as SetupComponent[];
    const known = new Set(rows.map(row => row.id));
    if (known.size !== rows.length || value.required_components.some(id => !known.has(id))) return null;
    if (rows.some(row => row.dependencies.some(id => !known.has(id)))) return null;
    for (const purpose of ['web', 'web_mail', 'application', 'dns', 'custom']) {
        const preset = value.presets[purpose];
        if (!ids(preset) || preset.some(id => !known.has(id))) return null;
    }
    return value as unknown as SetupComponentCatalog;
}

export function setupComponentClosure(requested: string[], catalog: SetupComponentCatalog): Set<string> {
    const rows = new Map(catalog.components.map(row => [row.id, row]));
    const found = new Set<string>();
    const visit = (id: string) => {
        if (found.has(id)) return;
        found.add(id);
        rows.get(id)?.dependencies.forEach(visit);
    };
    requested.forEach(visit);
    return found;
}

// Mutually dependent services form one removable choice. For example, the
// supported mail lifecycle prepares Postfix and Dovecot together; treating
// each as a disabled dependency of the other would trap the user's selection.
export function setupComponentGroups(catalog: SetupComponentCatalog): SetupComponent[][] {
    const reach = new Map(catalog.components.map(row => [row.id, setupComponentClosure([row.id], catalog)]));
    const grouped = new Set<string>();
    return catalog.components.flatMap(row => {
        if (grouped.has(row.id)) return [];
        const group = catalog.components.filter(other => reach.get(row.id)!.has(other.id) && reach.get(other.id)!.has(row.id));
        group.forEach(other => grouped.add(other.id));
        return [group];
    });
}

export function setupPresetComponents(draft: ServerSetupDraft, catalog: SetupComponentCatalog): string[] {
    let selected = [...catalog.presets[draft.purpose]];
    if (draft.purpose === 'application') {
        selected = selected.filter(id => !['mariadb', 'postgresql'].includes(id));
        if (draft.database) selected.push(draft.database);
    }
    return selected;
}

export function setupEffectiveComponents(draft: ServerSetupDraft, catalog: SetupComponentCatalog | null): Set<string> {
    if (draft.customization) return catalog ? setupComponentClosure(draft.customization.components, catalog) : new Set(draft.customization.components);
    if (catalog) return setupComponentClosure(setupPresetComponents(draft, catalog), catalog);
    if (draft.purpose === 'web_mail') return new Set(['nginx', 'php-fpm', 'mariadb', 'postfix', 'dovecot', 'roundcube', 'rspamd']);
    if (draft.purpose === 'application') return new Set(['nginx', 'node', ...(draft.database ? [draft.database] : [])]);
    return new Set(draft.purpose === 'web' ? ['nginx', 'php-fpm', 'mariadb'] : []);
}
