import { useState, useEffect, useMemo } from 'react';
import { api } from '../lib/api';
import { Save, Search, ChevronDown, ChevronRight, Settings, AlertCircle } from 'lucide-react';

interface PostgreSQLSettingsProps {
    configPath: string;
}

interface ConfigItem {
    key: string;
    value: string;
    description?: string;
    enabled: boolean; // if true, line is active "key = val". if false, commented out "#key = val"
    originalLine: string;
}

interface ConfigSection {
    title: string;
    items: ConfigItem[];
}

export function PostgreSQLSettings({ configPath }: PostgreSQLSettingsProps) {
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

        // Default section if no header found
        let currentSection: ConfigSection = { title: "General Settings", items: [] };

        // Regex helpers
        // Header looks like: #------------------------------------------------------------------------------
        // followed by: # SECTION NAME
        // OR just: # SECTION NAME

        // Setting looks like: key = value # comment
        // Or commented: #key = value # comment
        const settingRegex = /^(\s*#\s*)?([a-z_][a-z0-9_]*)\s*=\s*(.*)$/;
        // Note: active line regex: ^\s*([a-z_]+)\s*=\s*(.*)$
        // Commented active line: ^\s*#\s*([a-z_]+)\s*=\s*(.*)$

        lines.forEach((line) => {
            const trimmed = line.trim();
            if (!trimmed) return;

            // Check for Section Headers
            // Heuristic: Comment line, uppercase letters, length > 3, no equals sign
            if (trimmed.startsWith('#') && !trimmed.includes('=')) {
                const text = trimmed.replace(/^#\s*/, '').trim();
                // Check if it's a separator line
                if (text.match(/^-+$/)) return;

                // If it looks like a title (Uppercase or Title Case with no lowercase mostly?)
                // PostgreSQL conf headers are often: CONNECTIONS AND AUTHENTICATION
                if (text.length > 3 && /[A-Z]/.test(text) && !/[a-z]/.test(text)) {
                    // Push old section if not empty
                    if (currentSection.items.length > 0) {
                        parsedSections.push(currentSection);
                    }
                    currentSection = { title: text, items: [] };
                    // Auto-expand mostly used sections?
                    return;
                }
            }

            // Check for Settings
            const match = line.match(settingRegex);
            if (match) {
                // match[1] is comment prefix (if exists)
                // match[2] is key
                // match[3] is value + comment

                let isCommented = !!match[1];
                const key = match[2];
                let rawValue = match[3];
                let comment = '';

                // Extract trailing comment
                const commentIndex = rawValue.indexOf('#');
                if (commentIndex !== -1) {
                    comment = rawValue.substring(commentIndex + 1).trim();
                    rawValue = rawValue.substring(0, commentIndex).trim();
                }

                // Remove quotes from value if present
                let value = rawValue.trim();
                if ((value.startsWith("'") && value.endsWith("'")) || (value.startsWith('"') && value.endsWith('"'))) {
                    value = value.slice(1, -1);
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

        // Push last section
        if (currentSection.items.length > 0) {
            parsedSections.push(currentSection);
        }

        // If only General section found and title is default, try to group?
        // No, stick to file structure.

        setSections(parsedSections);

        // Expand first section by default
        if (parsedSections.length > 0) {
            setExpandedSections({ [parsedSections[0].title]: true });
        }
    };

    const handleSave = async () => {
        setSaving(true);
        try {
            // Reconstruct config
            // We need to iterate over sections and find changed items.
            // But preserving file structure is tricky if we just use sections array.
            // "originalContent" is our template. We need to replace lines.

            // Better: Iterate originalContent lines, identify parsed items, and replace if changed.
            // But we have parsed items in a structure.

            // Map keys to their new values/status
            const changeMap = new Map<string, ConfigItem>();
            sections.forEach(sec => sec.items.forEach(item => changeMap.set(item.key, item)));

            const lines = originalContent.split('\n');
            const newLines = lines.map(line => {
                // const trimmed = line.trim(); // unused
                const match = line.match(/^(\s*#\s*)?([a-z_][a-z0-9_]*)\s*=\s*(.*)$/);

                if (match) {
                    const key = match[2];
                    if (changeMap.has(key)) {
                        const newItem = changeMap.get(key)!;
                        // Determine quote need
                        const isNum = /^[0-9]+(\.[0-9]+)?(MB|GB|kB|ms|s|min)?$/.test(newItem.value);
                        const isBool = newItem.value === 'on' || newItem.value === 'off';
                        const valStr = (isNum || isBool) ? newItem.value : `'${newItem.value}'`;

                        if (newItem.enabled) {
                            return `${key} = ${valStr}\t# ${newItem.description || ''}`;
                        } else {
                            // commented out
                            return `#${key} = ${valStr}\t# ${newItem.description || ''}`;
                        }
                    }
                }
                return line;
            });

            const newContent = newLines.join('\n');

            await api.saveConfig(configPath, newContent);
            setOriginalContent(newContent); // Update original content to match saved
            alert('Configuration saved successfully. Please restart PostgreSQL.');
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
                item.key.includes(lower) ||
                (item.description && item.description.toLowerCase().includes(lower))
            )
        })).filter(sec => sec.items.length > 0);
    }, [sections, searchTerm]);

    if (loading) return <div className="p-8 text-center text-slate-500">Loading full configuration...</div>;

    return (
        <div className="bg-slate-900/50 border border-slate-800 rounded-xl p-6">
            <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 mb-6">
                <h3 className="text-lg font-bold text-white flex items-center gap-2">
                    <Settings className="w-5 h-5 text-blue-400" />
                    Full Configuration
                </h3>

                <div className="relative w-full md:w-64">
                    <Search className="absolute left-3 top-2.5 w-4 h-4 text-slate-500" />
                    <input
                        type="text"
                        placeholder="Search settings..."
                        value={searchTerm}
                        onChange={e => setSearchTerm(e.target.value)}
                        className="w-full bg-slate-900 border border-slate-700 rounded-lg py-2 pl-10 pr-4 text-white focus:outline-none focus:border-blue-500 text-sm"
                    />
                </div>
            </div>

            <div className="space-y-4">
                {filteredSections.length === 0 ? (
                    <div className="text-center py-8 text-slate-500">
                        No settings found matching "{searchTerm}"
                    </div>
                ) : (
                    filteredSections.map((section) => (
                        <div key={section.title} className="border border-slate-800 rounded-lg overflow-hidden bg-slate-900/20">
                            <button
                                onClick={() => toggleSection(section.title)}
                                className="w-full flex items-center justify-between p-4 hover:bg-slate-800/50 transition-colors"
                            >
                                <span className="font-bold text-sm text-slate-300 uppercase tracking-wide">{section.title} <span className="text-slate-600 text-xs ml-2">({section.items.length})</span></span>
                                {expandedSections[section.title] || searchTerm ? <ChevronDown size={18} className="text-slate-500" /> : <ChevronRight size={18} className="text-slate-500" />}
                            </button>

                            {(expandedSections[section.title] || searchTerm) && (
                                <div className="p-4 border-t border-slate-800 grid grid-cols-1 gap-4">
                                    {section.items.map((item) => (
                                        <div key={item.key} className={`group flex flex-col sm:flex-row sm:items-start gap-4 p-3 rounded-lg border ${item.enabled ? 'border-slate-800 bg-slate-900/40' : 'border-dashed border-slate-800/50 opacity-60'}`}>
                                            <div className="pt-2">
                                                <input
                                                    type="checkbox"
                                                    checked={item.enabled}
                                                    onChange={e => updateItem(sections.findIndex(s => s.title === section.title), sections[sections.findIndex(s => s.title === section.title)].items.indexOf(item), 'enabled', e.target.checked)}
                                                    className="rounded border-slate-600 bg-slate-800 text-blue-500 focus:ring-0"
                                                />
                                            </div>
                                            <div className="flex-1 space-y-1">
                                                <div className="flex flex-col sm:flex-row sm:items-center gap-2">
                                                    <label className="font-mono text-sm text-blue-400 font-bold">{item.key}</label>
                                                    {!item.enabled && <span className="text-[10px] bg-slate-800 text-slate-500 px-1.5 py-0.5 rounded">Disabled</span>}
                                                </div>
                                                <input
                                                    type="text"
                                                    value={item.value}
                                                    onChange={e => updateItem(sections.findIndex(s => s.title === section.title), sections[sections.findIndex(s => s.title === section.title)].items.indexOf(item), 'value', e.target.value)}
                                                    className="w-full bg-slate-950 border border-slate-700 rounded px-2 py-1 text-sm text-gray-300 focus:border-blue-500 focus:outline-none"
                                                    disabled={!item.enabled}
                                                />
                                                {item.description && <p className="text-xs text-slate-500">{item.description}</p>}
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
                <div className="bg-slate-900/90 backdrop-blur border border-slate-700 p-2 rounded-xl shadow-2xl">
                    <button
                        onClick={handleSave}
                        disabled={saving}
                        className="flex items-center gap-2 px-6 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg font-bold transition-all disabled:opacity-50"
                    >
                        <Save size={18} />
                        {saving ? 'Saving...' : 'Save All Changes'}
                    </button>
                </div>
            </div>

            <div className="mt-4 text-xs text-slate-500 flex items-center gap-2">
                <AlertCircle size={14} />
                <p>Advanced Mode: Showing all parsed settings from configuration file.</p>
            </div>
        </div>
    );
}
