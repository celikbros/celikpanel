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

    if (loading) return <div className="text-gray-400">Yükleniyor...</div>;
    if (error) return <div className="text-red-400">Hata: {error}</div>;

    return (
        <div className="bg-gray-900 border border-gray-800 rounded-xl overflow-hidden flex flex-col h-[calc(100vh-12rem)]">
            <div className="flex items-center justify-between px-6 py-4 border-b border-gray-800 bg-gray-900/50">
                <div className="flex items-center gap-3">
                    <button onClick={onBack} className="p-2 hover:bg-gray-800 rounded-lg transition-colors">
                        <ArrowLeft size={20} className="text-gray-400" />
                    </button>
                    <div className="flex items-center gap-2 text-gray-200">
                        <FileCode size={20} className="text-blue-400" />
                        <span className="font-mono text-sm">{path}</span>
                    </div>
                </div>

                <button
                    onClick={handleSave}
                    disabled={saving}
                    className="flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg font-medium transition-colors disabled:opacity-50"
                >
                    <Save size={18} />
                    {saving ? 'Kaydediliyor...' : 'Kaydet'}
                </button>
            </div>

            <div className="flex-1 relative">
                <textarea
                    value={content}
                    onChange={e => setContent(e.target.value)}
                    className="w-full h-full bg-gray-950 text-gray-300 font-mono text-sm p-6 resize-none focus:outline-none"
                    spellCheck={false}
                />
            </div>
        </div>
    );
}
