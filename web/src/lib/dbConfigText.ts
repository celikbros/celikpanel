// Reading and changing a database server's own configuration text without
// disturbing what the editor does not manage (9 Oct 2026; D-022).
//
// The three editors (postgresql.conf, a MariaDB option file, pg_hba.conf) show
// part of a file the server owner also edits by hand. So a save is the file
// that was read with ONLY the lines the person changed replaced: every other
// line, comment, include, unknown directive, blank line and line ending comes
// back byte for byte, in place. A line is identified by its position in the
// text that was read, and that text is pinned by the version every save
// carries, so the position cannot point at another line.
//
// Before this the editors rewrote every line they could parse (all 350
// commented defaults of a postgresql.conf, with a new spacing and quoting),
// gave two lines with the same name the same value, and wrote pg_hba.conf from
// scratch without its comments.
//
// Bir veritabanı sunucusunun kendi yapılandırma metnini, düzenleyicinin
// yönetmediğini bozmadan okumak ve değiştirmek. Kayıt, okunan dosyanın YALNIZ
// kişinin değiştirdiği satırları değiştirilmiş hâlidir; diğer her satır, yorum,
// include, bilinmeyen yönerge, boş satır ve satır sonu bayt bayt yerinde kalır.

export interface ConfigItem {
    /** Index of the line in the text that was read. */
    line: number;
    key: string;
    value: string;
    /** false: the line is commented out, so the server's default applies. */
    enabled: boolean;
    description: string;
}

export interface ConfigSection {
    title: string;
    items: ConfigItem[];
}

export interface ItemEdit {
    value: string;
    enabled: boolean;
}

interface ParsedLine {
    indent: string;
    /** '' when the line is active, else the comment mark that disables it. */
    mark: string;
    key: string;
    separator: string;
    /** The value exactly as written, quotes included. */
    rawValue: string;
    value: string;
    quoted: boolean;
    /** Everything after the value: spacing and a trailing comment. */
    tail: string;
    /** A carriage return that ended the line, kept as it was. */
    ending: string;
}

const splitEnding = (line: string): [string, string] => (line.endsWith('\r') ? [line.slice(0, -1), '\r'] : [line, '']);

// --- postgresql.conf ---------------------------------------------------------

// `name = value`, `name value`, with an optional trailing `# comment`. A
// commented-out setting is `#name = value` with nothing between `#` and the
// name: that is how PostgreSQL's own file writes its defaults, and it keeps
// prose such as "#   name = value" from being taken for a setting.
function parsePostgresLine(text: string): ParsedLine | null {
    const [body, ending] = splitEnding(text);
    const head = /^(\s*)(#?)([A-Za-z_][A-Za-z0-9_.]*)(\s*=\s*|\s+)(.*)$/.exec(body);
    if (!head) return null;
    const [, indent, mark, key, separator, rest] = head;
    if (mark && !separator.includes('=')) return null;
    let rawValue: string;
    let value: string;
    let quoted = false;
    if (rest.startsWith("'")) {
        let end = 1;
        value = '';
        for (;;) {
            if (end >= rest.length) return null; // an unclosed quote: not ours to rewrite
            const ch = rest[end];
            if (ch === "'" && rest[end + 1] === "'") { value += "'"; end += 2; continue; }
            if (ch === '\\' && end + 1 < rest.length) { value += rest[end + 1]; end += 2; continue; }
            if (ch === "'") break;
            value += ch;
            end++;
        }
        rawValue = rest.slice(0, end + 1);
        quoted = true;
    } else {
        const bare = /^[^\s#]*/.exec(rest)![0];
        if (!bare) return null;
        rawValue = bare;
        value = bare;
    }
    const tail = rest.slice(rawValue.length);
    if (!/^\s*(#.*)?$/.test(tail)) return null;
    return { indent, mark, key, separator, rawValue, value, quoted, tail, ending };
}

const tailComment = (tail: string) => {
    const at = tail.indexOf('#');
    return at < 0 ? '' : tail.slice(at + 1).trim();
};

export function parsePostgresConf(content: string): ConfigSection[] {
    const sections: ConfigSection[] = [];
    let current: ConfigSection = { title: '', items: [] };
    const lines = content.split('\n');
    const ruled = (index: number) => /^#\s*-{5,}\s*$/.test((lines[index] ?? '').trim());
    lines.forEach((text, line) => {
        const trimmed = text.trim();
        if (trimmed.startsWith('#') && !trimmed.includes('=')) {
            // PostgreSQL's own file titles its parts in capitals between two
            // ruled lines: "# RESOURCE USAGE (except WAL)".
            const title = trimmed.replace(/^#\s*/, '').trim();
            const capitals = title.replace(/\([^)]*\)/g, '');
            const framed = ruled(line - 1) && ruled(line + 1);
            if (title.length > 3 && !/^-+$/.test(title) && /[A-Z]/.test(capitals) && (framed || !/[a-z]/.test(capitals))) {
                if (current.items.length > 0) sections.push(current);
                current = { title, items: [] };
            }
            return;
        }
        const parsed = parsePostgresLine(text);
        if (!parsed) return;
        current.items.push({ line, key: parsed.key, value: parsed.value, enabled: !parsed.mark, description: tailComment(parsed.tail) });
    });
    if (current.items.length > 0) sections.push(current);
    return sections;
}

const postgresBare = /^(-?[0-9]+(\.[0-9]+)?[A-Za-z]*|[A-Za-z_][A-Za-z0-9_]*)$/;

export function applyPostgresConf(content: string, edits: ReadonlyMap<number, ItemEdit>): string {
    if (edits.size === 0) return content;
    const lines = content.split('\n');
    for (const [line, edit] of edits) {
        const parsed = lines[line] === undefined ? null : parsePostgresLine(lines[line]);
        if (!parsed) continue;
        let raw = parsed.rawValue;
        if (edit.value !== parsed.value) {
            raw = parsed.quoted || !postgresBare.test(edit.value) ? `'${edit.value.replace(/'/g, "''")}'` : edit.value;
        }
        lines[line] = `${parsed.indent}${edit.enabled ? '' : '#'}${parsed.key}${parsed.separator}${raw}${parsed.tail}${parsed.ending}`;
    }
    return lines.join('\n');
}

// --- a MariaDB option file ---------------------------------------------------

// `key`, `key = value`, a trailing ` # comment`. A commented-out option is
// `#key = value` or `;key = value` with nothing between the mark and the name,
// or a bare `#some-flag`; "# prose" is prose.
function parseOptionLine(text: string): ParsedLine | null {
    const [body, ending] = splitEnding(text);
    const head = /^(\s*)([#;]?)([A-Za-z0-9][A-Za-z0-9_-]*)(\s*=\s*)?(.*)$/.exec(body);
    if (!head) return null;
    const [, indent, mark, key, separator = '', rest] = head;
    if (!separator) {
        // A flag: nothing may follow but spacing and a comment.
        if (!/^(\s+#.*|\s*)$/.test(rest)) return null;
        if (mark && !/[-_]/.test(key)) return null;
        return { indent, mark, key, separator, rawValue: '', value: '', quoted: false, tail: rest, ending };
    }
    let rawValue: string;
    let quoted = false;
    const quote = rest[0];
    if (quote === '"' || quote === "'") {
        const close = rest.indexOf(quote, 1);
        if (close < 0) return null;
        rawValue = rest.slice(0, close + 1);
        quoted = true;
    } else {
        const comment = /\s+#/.exec(rest);
        rawValue = (comment ? rest.slice(0, comment.index) : rest).replace(/\s+$/, '');
    }
    const tail = rest.slice(rawValue.length);
    if (!/^\s*(#.*)?$/.test(tail)) return null;
    return { indent, mark, key, separator, rawValue, value: quoted ? rawValue.slice(1, -1) : rawValue, quoted, tail, ending };
}

export function parseOptionFile(content: string): ConfigSection[] {
    const sections: ConfigSection[] = [];
    let current: ConfigSection = { title: '', items: [] };
    content.split('\n').forEach((text, line) => {
        const group = /^\s*\[([^\]]+)\]\s*$/.exec(splitEnding(text)[0]);
        if (group) {
            if (current.items.length > 0) sections.push(current);
            current = { title: group[1], items: [] };
            return;
        }
        // An option before any group is not one the server reads.
        if (!current.title) return;
        const parsed = parseOptionLine(text);
        if (!parsed) return;
        current.items.push({ line, key: parsed.key, value: parsed.value, enabled: !parsed.mark, description: tailComment(parsed.tail) });
    });
    if (current.items.length > 0) sections.push(current);
    return sections;
}

export function applyOptionFile(content: string, edits: ReadonlyMap<number, ItemEdit>): string {
    if (edits.size === 0) return content;
    const lines = content.split('\n');
    for (const [line, edit] of edits) {
        const parsed = lines[line] === undefined ? null : parseOptionLine(lines[line]);
        if (!parsed) continue;
        const mark = edit.enabled ? '' : parsed.mark || '#';
        let assignment = `${parsed.separator}${parsed.rawValue}`;
        if (edit.value !== parsed.value) {
            if (edit.value === '') assignment = '';
            else assignment = `${parsed.separator || ' = '}${parsed.quoted ? parsed.rawValue[0] + edit.value + parsed.rawValue[0] : edit.value}`;
        }
        lines[line] = `${parsed.indent}${mark}${parsed.key}${assignment}${parsed.tail}${parsed.ending}`;
    }
    return lines.join('\n');
}

// --- pg_hba.conf -------------------------------------------------------------

export interface HbaFields {
    type: string;
    database: string;
    user: string;
    /** '' for a local rule. */
    address: string;
    method: string;
}

export interface HbaRule extends HbaFields {
    line: number;
    /** The rule exactly as written. */
    text: string;
    /**
     * false: the rule has something the grid cannot hold (options, a quoted
     * name, a separate netmask, an include, a continuation, a comment after
     * it). It is shown as written and is never rewritten or removed here.
     */
    editable: boolean;
}

export const hbaTypes = ['local', 'host', 'hostssl', 'hostnossl', 'hostgssenc', 'hostnogssenc'];
export const hbaMethods = ['scram-sha-256', 'md5', 'peer', 'ident', 'trust', 'reject', 'password', 'cert', 'gss', 'pam', 'ldap', 'radius'];

export function parseHba(content: string): HbaRule[] {
    const rules: HbaRule[] = [];
    const lines = content.split('\n');
    for (let line = 0; line < lines.length; line++) {
        const text = splitEnding(lines[line])[0];
        const trimmed = text.trim();
        if (!trimmed || trimmed.startsWith('#')) continue;
        const first = line;
        // A backslash at the end continues the rule on the next line.
        let whole = text;
        while (whole.endsWith('\\') && line + 1 < lines.length) {
            line++;
            whole += '\n' + splitEnding(lines[line])[0];
        }
        const fields = trimmed.split(/\s+/);
        const local = fields[0] === 'local';
        const simple = whole === text && !/["#,\\]/.test(trimmed) && hbaTypes.includes(fields[0])
            && fields.length === (local ? 4 : 5);
        rules.push({
            line: first,
            text: whole,
            editable: simple,
            type: fields[0] ?? '',
            database: simple ? fields[1] : '',
            user: simple ? fields[2] : '',
            address: simple && !local ? fields[3] : '',
            method: simple ? fields[local ? 3 : 4] : '',
        });
    }
    return rules;
}

export const formatHbaRule = (rule: HbaFields) => (rule.type === 'local'
    ? `${rule.type.padEnd(8)}${rule.database.padEnd(16)}${rule.user.padEnd(40)}${rule.method}`
    : `${rule.type.padEnd(8)}${rule.database.padEnd(16)}${rule.user.padEnd(16)}${rule.address.padEnd(24)}${rule.method}`
).replace(/\s+$/, '');

export const hbaRuleComplete = (rule: HbaFields) => {
    const word = (value: string) => /^\S+$/.test(value) && !/["#,\\]/.test(value);
    return hbaTypes.includes(rule.type) && word(rule.database) && word(rule.user) && word(rule.method)
        && (rule.type === 'local' || word(rule.address));
};

export interface HbaChanges {
    /** Rules of the file whose fields were changed, by their line. */
    edited: ReadonlyMap<number, HbaFields>;
    /** Lines of rules that are removed. */
    removed: ReadonlySet<number>;
    /** New rules, appended in this order at the end of the file. */
    added: readonly HbaFields[];
}

// applyHba returns the new text and, for each added rule, the line it now
// stands on, so a refusal that names a line can be shown next to that rule.
export function applyHba(content: string, changes: HbaChanges): { content: string; addedLines: number[] } {
    const lines = content.split('\n');
    const rules = new Map(parseHba(content).map((rule) => [rule.line, rule]));
    const out: string[] = [];
    lines.forEach((text, line) => {
        const rule = rules.get(line);
        if (rule?.editable && changes.removed.has(line)) return;
        const edit = rule?.editable ? changes.edited.get(line) : undefined;
        if (edit && formatHbaRule(edit) !== formatHbaRule(rule!)) {
            out.push(formatHbaRule(edit) + splitEnding(text)[1]);
            return;
        }
        out.push(text);
    });
    const addedLines: number[] = [];
    if (changes.added.length > 0) {
        // New rules go after the last line; a file that ends with a line break
        // keeps ending with one.
        const endsWithBreak = out.length > 0 && out[out.length - 1] === '';
        if (endsWithBreak) out.pop();
        const ending = lines.some((text) => text.endsWith('\r')) ? '\r' : '';
        for (const rule of changes.added) {
            addedLines.push(out.length);
            out.push(formatHbaRule(rule) + ending);
        }
        if (endsWithBreak) out.push('');
    }
    return { content: out.join('\n'), addedLines };
}
