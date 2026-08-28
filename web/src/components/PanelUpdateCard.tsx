import { useEffect, useRef, useState } from 'react';
import { AlertTriangle, DownloadCloud, Loader2, RefreshCw } from 'lucide-react';
import { Button } from './ui';
import { useI18n } from '../i18n';
import { apiErrorText, readApiError } from '../lib/apiError';
import {
    createSystemUpdateRequestID,
    systemUpdateResponseHint,
    useSystemUpdateOperation,
    validUpdateTarget,
    validUpdateVersion,
    type UpdateMarker,
    type UpdateTarget,
} from './SystemUpdateOperation';

type Translate = ReturnType<typeof useI18n>['t'];

type UpdateCheck = {
    supported: boolean;
    available: boolean;
    current_version: string;
    current_commit: string;
    target?: UpdateTarget;
};

type PanelBuild = {
    version: string;
    commit: string;
};

const commitPattern = /^[a-f0-9]{40}$/;

function decodeUpdateCheck(payload: unknown): UpdateCheck | null {
    if (!payload || typeof payload !== 'object') return null;
    const value = payload as Record<string, unknown>;
    if (value.supported !== true || typeof value.available !== 'boolean'
        || !validUpdateVersion(value.current_version)
        || typeof value.current_commit !== 'string' || !commitPattern.test(value.current_commit)) return null;
    if (value.available) {
        if (!validUpdateTarget(value.target)) return null;
        return {
            supported: true, available: true,
            current_version: value.current_version, current_commit: value.current_commit,
            target: value.target,
        };
    }
    if (value.target !== undefined) return null;
    return {
        supported: true, available: false,
        current_version: value.current_version, current_commit: value.current_commit,
    };
}

async function codedResponseHint(response: Response, t: Translate): Promise<{ code?: string; message: string }> {
    const apiError = await readApiError(response);
    return {
        ...(apiError.code ? { code: apiError.code } : {}),
        message: apiError.code ? apiErrorText(apiError, t) : systemUpdateResponseHint(response.status, t),
    };
}

export function PanelUpdateCard() {
    const { t } = useI18n();
    const systemUpdate = useSystemUpdateOperation();
    const [currentBuild, setCurrentBuild] = useState<PanelBuild | null>(null);
    const [check, setCheck] = useState<UpdateCheck | null>(null);
    const [checking, setChecking] = useState(false);
    const [starting, setStarting] = useState(false);
    const [message, setMessage] = useState('');
    const actionInFlight = useRef(false);

    useEffect(() => {
        let cancelled = false;
        fetch('/api/v1/panel/version', { cache: 'no-store', credentials: 'same-origin' })
            .then(async (response) => (response.ok ? response.json() as Promise<unknown> : null))
            .then((payload) => {
                if (cancelled || !payload || typeof payload !== 'object') return;
                const value = payload as Record<string, unknown>;
                if (typeof value.version !== 'string' || value.version.length > 80
                    || typeof value.commit !== 'string' || value.commit.length > 80) return;
                setCurrentBuild({ version: value.version, commit: value.commit });
            })
            .catch(() => undefined);
        return () => { cancelled = true; };
    }, []);

    async function checkForUpdate() {
        if (actionInFlight.current || systemUpdate.active) return;
        actionInFlight.current = true;
        setChecking(true);
        setMessage('');
        setCheck(null);
        try {
            const response = await fetch('/api/v1/panel/update/check', { cache: 'no-store', credentials: 'same-origin' });
            if (!response.ok) {
                throw new Error((await codedResponseHint(response, t)).message);
            }
            const payload = decodeUpdateCheck(await response.json());
            if (!payload) {
                throw new Error(t('panelUpdate.unsupported'));
            }
            setCheck(payload);
            setCurrentBuild({ version: payload.current_version, commit: payload.current_commit });
            setMessage(payload.available ? t('panelUpdate.available') : t('panelUpdate.none'));
        } catch (error) {
            setMessage(error instanceof Error ? error.message : t('panelUpdate.checkFailed'));
        } finally {
            setChecking(false);
            actionInFlight.current = false;
        }
    }

    async function startUpdate() {
        const target = check?.target;
        if (actionInFlight.current || systemUpdate.active || !check?.available || !target) return;
        const requestID = createSystemUpdateRequestID();
        if (!requestID) {
            setMessage(t('panelUpdate.randomFailed'));
            return;
        }
        const exactMarker: UpdateMarker = {
            marker_version: 1,
            request_id: requestID,
            current_version: check.current_version,
            current_commit: check.current_commit,
            target,
            created_at: Date.now(),
        };
        // The provider durably stores the exact request before this component
        // is allowed to send the only start POST. Its overlay is mounted above
        // routing, so navigation cannot unmount the tracker.
        actionInFlight.current = true;
        setStarting(true);
        setMessage('');
        try {
            const result = await systemUpdate.start(exactMarker);
            if (result.kind === 'failed') setMessage(result.message);
        } finally {
            setStarting(false);
            actionInFlight.current = false;
        }
    }

    const target = check?.target;
    const active = systemUpdate.active;
    const currentVersion = check?.current_version ?? currentBuild?.version;
    const currentCommit = check?.current_commit ?? currentBuild?.commit;
    return (
        <section className="rounded-xl border border-border bg-surface p-5 shadow-card" aria-labelledby="panel-update-title">
            <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
                <div className="flex min-w-0 gap-3">
                    <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
                        <DownloadCloud className="h-5 w-5" aria-hidden="true" />
                    </span>
                    <div>
                        <h3 id="panel-update-title" className="font-semibold text-fg">{t('panelUpdate.title')}</h3>
                        <p className="mt-1 text-sm text-fg-muted">{t('panelUpdate.description')}</p>
                    </div>
                </div>
                <Button type="button" onClick={() => void checkForUpdate()} disabled={checking || active || starting}>
                    {checking ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
                    {checking ? t('panelUpdate.checking') : t('panelUpdate.check')}
                </Button>
            </div>

            <div className="mt-4 rounded-lg border border-amber-400/40 bg-amber-400/10 p-4 text-sm text-fg" role="note">
                <div className="flex items-start gap-2"><AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-amber-500" aria-hidden="true" />
                    <p>{t('panelUpdate.alphaNotice')}</p>
                </div>
            </div>

            {currentVersion && currentCommit && (
                <dl className="mt-4 grid gap-3 rounded-lg border border-border bg-surface-subtle p-4 text-sm sm:grid-cols-2">
                    <div><dt className="text-fg-muted">{t('panelUpdate.currentVersion')}</dt><dd className="font-mono text-fg">{currentVersion}</dd></div>
                    <div><dt className="text-fg-muted">{t('panelUpdate.currentCommit')}</dt><dd className="break-all font-mono text-fg">{currentCommit}</dd></div>
                    {target && <>
                        <div><dt className="text-fg-muted">{t('panelUpdate.targetVersion')}</dt><dd className="font-mono font-semibold text-fg">{target.version}</dd></div>
                        <div><dt className="text-fg-muted">{t('panelUpdate.targetPlatform')}</dt><dd className="font-mono text-fg">{target.os}/{target.arch}</dd></div>
                        <div><dt className="text-fg-muted">{t('panelUpdate.sequence')}</dt><dd className="font-mono text-fg">{target.sequence}</dd></div>
                        <div><dt className="text-fg-muted">{t('panelUpdate.archiveSize')}</dt><dd className="font-mono text-fg">{t('panelUpdate.bytes', { size: target.archive_size })}</dd></div>
                        <div className="sm:col-span-2"><dt className="text-fg-muted">{t('panelUpdate.sha256')}</dt><dd className="break-all font-mono text-xs text-fg">{target.archive_sha256}</dd></div>
                    </>}
                </dl>
            )}

            {target && check?.available && !active && (
                <div className="mt-4">
                    <Button id="panel-update-start-button" type="button" onClick={() => void startUpdate()} disabled={starting}>
                        {starting ? <Loader2 className="h-4 w-4 animate-spin" /> : <DownloadCloud className="h-4 w-4" />}
                        {starting ? t('panelUpdate.starting') : t('panelUpdate.start', { version: target.version })}
                    </Button>
                </div>
            )}

            {message && (
                <div className="mt-4 flex items-start gap-2 rounded-lg border border-border p-3 text-sm text-fg" role="status" aria-live="polite">
                    <AlertTriangle className="h-5 w-5 shrink-0 text-amber-500" />
                    <p>{message}</p>
                </div>
            )}
        </section>
    );
}
