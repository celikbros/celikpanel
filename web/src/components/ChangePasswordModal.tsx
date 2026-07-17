import { useState } from 'react';
import { KeyRound, X } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { readApiError, apiErrorText } from '../lib/apiError';
import { Button, inputClass } from './ui';

// Self-service password change for the signed-in user; the current password
// must be proven (the API enforces it too).
// Oturumdaki kullanıcı için self-servis parola değişimi; mevcut parola
// kanıtlanmalıdır (API de bunu zorlar).
export function ChangePasswordModal({ onClose }: { onClose: () => void }) {
    const { t } = useI18n();
    const [current, setCurrent] = useState('');
    const [next, setNext] = useState('');
    const [next2, setNext2] = useState('');
    const [saving, setSaving] = useState(false);

    const submit = async () => {
        if (next !== next2) {
            showToast('error', t('profile.mismatch'));
            return;
        }
        setSaving(true);
        try {
            const res = await fetch('/api/v1/auth/password', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ current_password: current, new_password: next }),
            });
            if (res.status === 403) {
                showToast('error', t('profile.wrongCurrent'));
                return;
            }
            if (!res.ok) {
                showToast('error', apiErrorText(await readApiError(res), t));
                return;
            }
            showToast('success', t('profile.changed'));
            onClose();
        } catch {
            showToast('error', t('common.error'));
        } finally {
            setSaving(false);
        }
    };

    return (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
            <div
                className="w-full max-w-sm rounded-2xl border border-border bg-surface p-5 shadow-lg"
                onClick={(e) => e.stopPropagation()}
            >
                <div className="mb-4 flex items-center justify-between">
                    <h3 className="flex items-center gap-2 text-sm font-semibold text-fg">
                        <KeyRound className="h-4 w-4 text-primary" />
                        {t('profile.changePassword')}
                    </h3>
                    <button onClick={onClose} className="rounded-md p-1 text-fg-muted hover:bg-surface-2 hover:text-fg">
                        <X className="h-4 w-4" />
                    </button>
                </div>

                <div className="space-y-3">
                    <label className="block">
                        <span className="mb-1 block text-xs text-fg-muted">{t('profile.current')}</span>
                        <input type="password" value={current} onChange={(e) => setCurrent(e.target.value)} className={inputClass} autoFocus />
                    </label>
                    <label className="block">
                        <span className="mb-1 block text-xs text-fg-muted">{t('profile.new')}</span>
                        <input type="password" value={next} onChange={(e) => setNext(e.target.value)} className={inputClass} />
                    </label>
                    <label className="block">
                        <span className="mb-1 block text-xs text-fg-muted">{t('profile.new2')}</span>
                        <input
                            type="password"
                            value={next2}
                            onChange={(e) => setNext2(e.target.value)}
                            onKeyDown={(e) => e.key === 'Enter' && submit()}
                            className={inputClass}
                        />
                    </label>
                </div>

                <div className="mt-4 flex justify-end gap-2">
                    <Button onClick={onClose}>{t('users.cancel')}</Button>
                    <Button variant="primary" onClick={submit} disabled={saving || !current || next.length < 8}>
                        {t('profile.changePassword')}
                    </Button>
                </div>
            </div>
        </div>
    );
}
