import { decodeList } from './remote';

// The databases of one domain: GET /api/v1/domains/{id}/databases. One
// address, one decoder, for every screen that reads it (the Databases tab and
// the Backups tab of a domain).
// Bir alan adının veritabanları. Okuyan her ekran için (alan adının
// Veritabanları ve Yedekler sekmeleri) tek adres, tek çözücü.
export type DatabaseType = 'mysql' | 'postgresql';
export type DatabaseEngine = { value: DatabaseType; label: string };

export interface DatabaseInfo {
    id: number;
    name: string;
    type: string;
    user: string;
    created_at: string;
}

export function parseAvailableDatabaseTypes(value: unknown): DatabaseEngine[] {
    if (!Array.isArray(value)) return [];

    const parsed: DatabaseEngine[] = [];
    const seen = new Set<DatabaseType>();
    for (const item of value) {
        if (item !== 'mysql' && item !== 'postgresql') return [];
        if (seen.has(item)) continue;
        seen.add(item);
        parsed.push({
            value: item,
            label: item === 'mysql' ? 'MySQL / MariaDB' : 'PostgreSQL',
        });
    }
    return parsed;
}

// One answer carries this domain's databases and, for a team member, the
// engines that member may create on. A body that is not this shape is unknown.
// Tek yanıt bu alan adının veritabanlarını ve ekip üyesi için oluşturabileceği
// motorları taşır. Bu biçimde olmayan gövde bilinmeyendir.
export interface DomainDatabases {
    databases: DatabaseInfo[];
    availableTypes: DatabaseEngine[];
}

export function decodeDomainDatabases(raw: unknown): DomainDatabases {
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) throw new Error('shape');
    const payload = raw as Record<string, unknown>;
    return {
        databases: decodeList<DatabaseInfo>(payload.databases),
        availableTypes: parseAvailableDatabaseTypes(payload.available_types),
    };
}
