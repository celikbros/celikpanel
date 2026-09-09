import { useEffect } from 'react';
import { useAuth } from '../auth/AuthContext';
import { useNavigate } from '../router';

/** Ask the server owner once per authenticated mount; never ask a tenant. */
export function LicenseOnboarding() {
    const { user, role } = useAuth();
    const navigate = useNavigate();

    useEffect(() => {
        if (role !== 'admin') return;
        const controller = new AbortController();
        void fetch('/api/v1/panel/license', { signal: controller.signal, cache: 'no-store' })
            .then(async response => {
                if (!response.ok) return;
                const status = await response.json();
                if (!controller.signal.aborted && status?.state === 'missing' && status.can_provision === false) {
                    navigate('/settings?section=license&setup=1', { replace: true });
                }
            })
            .catch(() => { /* Existing license notices and server-side restrictions remain available. */ });
        return () => controller.abort();
    }, [user.username, role, navigate]);

    return null;
}
