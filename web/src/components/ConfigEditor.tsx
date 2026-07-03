import { useEffect, useState } from 'react';
import { api } from '../lib/api';
import { Save, ArrowLeft, FileCode } from 'lucide-react';

interface ConfigEditorProps {
    path: string;
    onBack: () => void;
}

export function ConfigEditor({ path, onBack }: ConfigEditorProps) {
    const [content, setContent] = useState('');
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState<string | null>(null);

    useEffect(() => {
        api.getConfig(path)
            .then(res => setContent(res.Content))
            .catch(err => setError(err.message))
            .finally(() => setLoading(false));
    }, [path]);

    const handleSave = async () => {
        setSaving(true);
        try {
            await api.saveConfig(path, content);
            alert('Kaydedildi!');
        } catch (err: any) {
            alert('Hata: ' + err.message);
        } finally {
            setSaving(false);
        }
    };

    if (loading) return <div className="text-fg-muted">Yükleniyor...</div>;
    if (error) return <div className="text-danger">Hata: {error}</div>;

    return (
        <div className="bg-surface border border-border rounded-xl overflow-hidden flex flex-col h-[calc(100vh-12rem)]">
            <div className="flex items-center justify-between px-6 py-4 border-b border-border bg-surface/50">
                <div className="flex items-center gap-3">
                    <button onClick={onBack} className="p-2 hover:bg-surface-2 rounded-lg transition-colors">
                        <ArrowLeft size={20} className="text-fg-muted" />
                    </button>
                    <div className="flex items-center gap-2 text-fg">
                        <FileCode size={20} className="text-primary" />
                        <span className="font-mono text-sm">{path}</span>
                    </div>
                </div>

                <button
                    onClick={handleSave}
                    disabled={saving}
                    className="flex items-center gap-2 px-4 py-2 bg-primary hover:bg-primary text-white rounded-lg font-medium transition-colors disabled:opacity-50"
                >
                    <Save size={18} />
                    {saving ? 'Kaydediliyor...' : 'Kaydet'}
                </button>
            </div>

            <div className="flex-1 relative">
                <textarea
                    value={content}
                    onChange={e => setContent(e.target.value)}
                    className="w-full h-full bg-bg text-fg-muted font-mono text-sm p-6 resize-none focus:outline-none"
                    spellCheck={false}
                />
            </div>
        </div>
    );
}
