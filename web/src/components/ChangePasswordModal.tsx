import { useState } from 'react';
import { KeyRound } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { readApiError, apiErrorText } from '../lib/apiError';
import { Button, Dialog, inputClass } from './ui';

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
        <Dialog
            id="change-password"
            icon={KeyRound}
            width="sm"
            title={t('profile.changePassword')}
            busy={saving}
            onDismiss={onClose}
            actions={
                <>
                    <Button onClick={onClose}>{t('users.cancel')}</Button>
                    <Button variant="primary" onClick={submit} disabled={saving || !current || next.length < 8}>
                        {t('profile.changePassword')}
                    </Button>
                </>
            }
        >
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
        </Dialog>
    );
}
