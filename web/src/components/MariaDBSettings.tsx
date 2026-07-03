import { useState, useEffect, useMemo } from 'react';
import { api } from '../lib/api';
import { Save, Search, ChevronDown, ChevronRight, AlertCircle, Database } from 'lucide-react';

interface MariaDBSettingsProps {
    configPath: string;
}

interface ConfigItem {
    key: string;
    value: string;
    description?: string;
    enabled: boolean;
    originalLine: string;
}

interface ConfigSection {
    title: string;
    items: ConfigItem[];
}

export function MariaDBSettings({ configPath }: MariaDBSettingsProps) {
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [sections, setSections] = useState<ConfigSection[]>([]);
    const [originalContent, setOriginalContent] = useState('');
    const [searchTerm, setSearchTerm] = useState('');
    const [expandedSections, setExpandedSections] = useState<Record<string, boolean>>({});

    useEffect(() => {
        loadConfig();
    }, [configPath]);

    const loadConfig = async () => {
        setLoading(true);
        try {
            const res = await api.getConfig(configPath);
            setOriginalContent(res.Content);
            parseConfig(res.Content);
        } catch (err) {
            console.error(err);
        } finally {
            setLoading(false);
        }
    };

    const parseConfig = (content: string) => {
        const lines = content.split('\n');
        const parsedSections: ConfigSection[] = [];

        // Default section for items at top without [section]
        let currentSection: ConfigSection = { title: "Global / Uncategorized", items: [] };

        // Regex for Section: [section_name]
        const sectionRegex = /^\s*\[(.*)\]\s*$/;

        // Regex for Setting: key = value OR key (boolean flag)
        // INI allows key=val or key = val.
        // Comments start with # or ;
        const settingRegex = /^(\s*[#;]\s*)?([a-zA-Z0-9_-]+)\s*(?:=\s*(.*))?$/;

        lines.forEach((line) => {
            const trimmed = line.trim();
            if (!trimmed) return;

            // Check for Section
            const secMatch = line.match(sectionRegex);
            if (secMatch) {
                if (currentSection.items.length > 0) {
                    parsedSections.push(currentSection);
                }
                currentSection = { title: secMatch[1], items: [] };
                return;
            }

            // Check for Settings
            const match = line.match(settingRegex);

            if (match) {
                // match[1] is comment prefix (# or ;)
                // match[2] is key
                // match[3] is value (optional)

                const isCommented = !!match[1];
                const key = match[2];
                let value = match[3] || ''; // If no equals, it might be a flag.

                if (isCommented && !line.includes('=') && key.length > 30) return;

                // Handle trailing comments in value
                let comment = '';
                const cIdx = value.search(/[#;]/);
                if (cIdx !== -1) {
                    comment = value.substring(cIdx + 1).trim();
                    value = value.substring(0, cIdx).trim();
                }

                currentSection.items.push({
                    key,
                    value,
                    enabled: !isCommented,
                    description: comment,
                    originalLine: line
                });
            }
        });

        if (currentSection.items.length > 0) {
            parsedSections.push(currentSection);
        }

        setSections(parsedSections);

        // Expand [mysqld] by default as it's the main server config
        setExpandedSections({ 'mysqld': true });
    };

    const handleSave = async () => {
        setSaving(true);
        try {
            const lines = originalContent.split('\n');
            const newLines: string[] = [];

            // Map for fast lookups: Section -> Key -> Item
            const changes = new Map<string, Map<string, ConfigItem>>();
            sections.forEach(sec => {
                const itemMap = new Map<string, ConfigItem>();
                sec.items.forEach(item => itemMap.set(item.key, item));
                changes.set(sec.title, itemMap);
            });

            let currentSectionTitle = "Global / Uncategorized";

            lines.forEach(line => {
                // const trimmed = line.trim(); // unused
                const secMatch = line.match(/^\s*\[(.*)\]\s*$/);
                if (secMatch) {
                    currentSectionTitle = secMatch[1];
                    newLines.push(line);
                    return;
                }

                // Try to match setting
                const match = line.match(/^(\s*[#;]\s*)?([a-zA-Z0-9_-]+)\s*(?:=\s*(.*))?$/);
                if (match) {
                    const key = match[2];
                    const sectionMap = changes.get(currentSectionTitle);

                    if (sectionMap && sectionMap.has(key)) {
                        const newItem = sectionMap.get(key)!;
                        const val = newItem.value;

                        if (newItem.enabled) {
                            if (val === '') {
                                newLines.push(`${key}`);
                            } else {
                                newLines.push(`${key} = ${val}`);
                            }
                        } else {
                            if (val === '') {
                                newLines.push(`# ${key}`);
                            } else {
                                newLines.push(`# ${key} = ${val}`);
                            }
                        }
                    } else {
                        newLines.push(line);
                    }
                } else {
                    newLines.push(line);
                }
            });

            const newContent = newLines.join('\n');
            await api.saveConfig(configPath, newContent);
            setOriginalContent(newContent);
            alert('MariaDB configuration saved. Please restart the service.');
        } catch (err: any) {
            alert('Error saving config: ' + err.message);
        } finally {
            setSaving(false);
        }
    };

    const toggleSection = (title: string) => {
        setExpandedSections(prev => ({ ...prev, [title]: !prev[title] }));
    };

    const updateItem = (sectionIndex: number, itemIndex: number, field: keyof ConfigItem, val: any) => {
        const newSections = [...sections];
        newSections[sectionIndex].items[itemIndex] = {
            ...newSections[sectionIndex].items[itemIndex],
            [field]: val
        };
        setSections(newSections);
    };

    // Filter logic
    const filteredSections = useMemo(() => {
        if (!searchTerm) return sections;
        const lower = searchTerm.toLowerCase();

        return sections.map(sec => ({
            ...sec,
            items: sec.items.filter(item =>
                item.key.toLowerCase().includes(lower) ||
                (item.description && item.description.toLowerCase().includes(lower))
            )
        })).filter(sec => sec.items.length > 0);
    }, [sections, searchTerm]);

    if (loading) return <div className="p-8 text-center text-fg-subtle">Loading MariaDB configuration...</div>;

    return (
        <div className="bg-surface/50 border border-border rounded-xl p-6">
            <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 mb-6">
                <h3 className="text-lg font-bold text-fg flex items-center gap-2">
                    <Database className="w-5 h-5 text-primary" />
                    MariaDB Configuration (my.cnf)
                </h3>

                <div className="relative w-full md:w-64">
                    <Search className="absolute left-3 top-2.5 w-4 h-4 text-fg-subtle" />
                    <input
                        type="text"
                        placeholder="Search settings..."
                        value={searchTerm}
                        onChange={e => setSearchTerm(e.target.value)}
                        className="w-full bg-surface border border-border rounded-lg py-2 pl-10 pr-4 text-fg focus:outline-none focus:border-primary text-sm"
                    />
                </div>
            </div>

            <div className="space-y-4">
                {filteredSections.length === 0 ? (
                    <div className="text-center py-8 text-fg-subtle">
                        No settings found matching "{searchTerm}"
                    </div>
                ) : (
                    filteredSections.map((section) => (
                        <div key={section.title} className="border border-border rounded-lg overflow-hidden bg-surface/20">
                            <button
                                onClick={() => toggleSection(section.title)}
                                className="w-full flex items-center justify-between p-4 hover:bg-surface-2/50 transition-colors"
                            >
                                <span className="font-bold text-sm text-fg-muted uppercase tracking-wide">
                                    [{section.title}]
                                    <span className="text-fg-subtle text-xs ml-2">({section.items.length})</span>
                                </span>
                                {expandedSections[section.title] || searchTerm ? <ChevronDown size={18} className="text-fg-subtle" /> : <ChevronRight size={18} className="text-fg-subtle" />}
                            </button>

                            {(expandedSections[section.title] || searchTerm) && (
                                <div className="p-4 border-t border-border grid grid-cols-1 gap-4">
                                    {section.items.map((item, iIdx) => (
                                        <div key={item.key + iIdx} className={`group flex flex-col sm:flex-row sm:items-start gap-4 p-3 rounded-lg border ${item.enabled ? 'border-border bg-surface/40' : 'border-dashed border-border/50 opacity-60'}`}>
                                            <div className="pt-2">
                                                <input
                                                    type="checkbox"
                                                    checked={item.enabled}
                                                    onChange={e => updateItem(sections.findIndex(s => s.title === section.title), sections[sections.findIndex(s => s.title === section.title)].items.indexOf(item), 'enabled', e.target.checked)}
                                                    className="rounded border-border-strong bg-surface-2 text-primary focus:ring-0"
                                                />
                                            </div>
                                            <div className="flex-1 space-y-1">
                                                <div className="flex flex-col sm:flex-row sm:items-center gap-2">
                                                    <label className="font-mono text-sm text-primary font-bold">{item.key}</label>
                                                    {!item.enabled && <span className="text-[10px] bg-surface-2 text-fg-subtle px-1.5 py-0.5 rounded">Disabled</span>}
                                                </div>
                                                <input
                                                    type="text"
                                                    value={item.value}
                                                    onChange={e => updateItem(sections.findIndex(s => s.title === section.title), sections[sections.findIndex(s => s.title === section.title)].items.indexOf(item), 'value', e.target.value)}
                                                    className="w-full bg-bg border border-border rounded px-2 py-1 text-sm text-fg-muted focus:border-primary focus:outline-none"
                                                    disabled={!item.enabled}
                                                />
                                                {item.description && <p className="text-xs text-fg-subtle">{item.description}</p>}
                                            </div>
                                        </div>
                                    ))}
                                </div>
                            )}
                        </div>
                    ))
                )}
            </div>

            <div className="mt-8 flex justify-end sticky bottom-4">
                <div className="bg-surface/90 backdrop-blur border border-border p-2 rounded-xl shadow-2xl">
                    <button
                        onClick={handleSave}
                        disabled={saving}
                        className="flex items-center gap-2 px-6 py-2 bg-primary hover:bg-primary text-white rounded-lg font-bold transition-all disabled:opacity-50"
                    >
                        <Save size={18} />
                        {saving ? 'Saving...' : 'Save All Changes'}
                    </button>
                </div>
            </div>

            <div className="mt-4 text-xs text-fg-subtle flex items-center gap-2">
                <AlertCircle size={14} />
                <p>Advanced Mode: Parsing INI-style configuration (my.cnf). Sections are grouped by [brackets].</p>
            </div>
        </div>
    );
}
