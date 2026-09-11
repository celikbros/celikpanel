import { useRef, useState } from 'react';
import { useI18n } from '../i18n';
import { Link } from '../router';
import { decodeServerSetup, type ServerSetupSnapshot } from '../lib/serverSetup';
import { useServerSetup } from './ServerSetupGate';
import { Button } from './ui';

type ChoiceProps = { snapshot: ServerSetupSnapshot; onChosen: (next: ServerSetupSnapshot) => void };

function GuidanceActions({ snapshot, onChosen, manualOnly = false, compact = false }: ChoiceProps & { manualOnly?: boolean; compact?: boolean }) {
    const { t } = useI18n();
    const setup = useServerSetup();
    const [busy, setBusy] = useState(false);
    const [failed, setFailed] = useState(false);
    const pending = useRef(false);
    async function choose(guidance: 'guided' | 'manual') {
        if (pending.current) return;
        pending.current = true; setBusy(true); setFailed(false);
        const controller = new AbortController();
        const timeout = window.setTimeout(() => controller.abort(), 15000);
        try {
            const response = await fetch('/api/v1/setup/guidance', {
                method: 'PUT', headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ revision: setup?.snapshot?.revision ?? snapshot.revision, guidance }), signal: controller.signal,
            });
            if (!response.ok) throw new Error('guidance');
            const next = decodeServerSetup(await response.json());
            if (!next || next.guidance !== guidance) throw new Error('guidance contract');
            onChosen(next);
        } catch {
            // A lost response may have saved the preference. Re-read it; never
            // navigate on an unconfirmed write or replay a stale revision.
            setFailed(true);
            await setup?.reload().catch(() => {});
        } finally { window.clearTimeout(timeout); pending.current = false; setBusy(false); }
    }
    return <div className={compact ? "mt-4 flex flex-wrap items-start gap-x-6 gap-y-3" : "mt-6"}>
        <div className="flex flex-wrap gap-3">
            {!manualOnly && <Button variant="primary" disabled={busy} onClick={() => void choose('guided')}>{t('setup.useWizard')}</Button>}
            {compact ? <button type="button" disabled={busy} onClick={() => void choose('manual')} className="min-h-11 py-2 text-sm font-medium text-primary underline underline-offset-4 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary disabled:cursor-not-allowed disabled:opacity-60">{t('setup.manual')}</button> : <Button variant="secondary" disabled={busy} onClick={() => void choose('manual')}>{t('setup.manual')}</Button>}
        </div>
        <p className={compact ? "min-w-0 flex-1 basis-80 py-2 text-sm leading-6 text-fg-muted" : "mt-4 max-w-2xl text-sm text-fg-muted"}>{t('setup.manualHelp')}</p>
        {failed && <p role="alert" className="mt-4 max-w-2xl text-sm text-danger">{t('setup.choiceFailed')}</p>}
    </div>;
}

export function ServerSetupChoice(props: ChoiceProps) {
    const { t } = useI18n();
    return <section aria-labelledby="setup-choice-title" className="max-w-3xl">
        <h1 id="setup-choice-title" className="text-2xl font-semibold leading-tight sm:text-3xl">{t('setup.choiceTitle')}</h1>
        <p className="mt-4 max-w-2xl text-fg-muted">{t(props.snapshot.origin === 'legacy' ? 'setup.choiceLegacy' : 'setup.choiceIntro')}</p>
        {props.snapshot.origin === 'legacy' && <p className="mt-3 max-w-2xl text-fg-muted">{t('setup.choiceIntro')}</p>}
        <GuidanceActions {...props} />
    </section>;
}

export function ServerSetupManualAction(props: ChoiceProps & { compact?: boolean }) {
    return <GuidanceActions {...props} manualOnly />;
}

export function ServerSetupSettings() {
    const setup = useServerSetup();
    const { t } = useI18n();
    if (!setup?.snapshot) return <div className="space-y-4" role="status"><p>{t('setup.loading')}</p><Button onClick={() => void setup?.reload().catch(() => {})}>{t('common.retry')}</Button></div>;
    const snapshot = setup.snapshot;
    const active = ['running', 'waiting'].includes(snapshot.status);
    return <section className="rounded-xl border border-border bg-surface p-6" aria-labelledby="setup-settings-title">
        <h2 id="setup-settings-title" className="text-lg font-semibold">{t('setup.settingsTitle')}</h2>
        <p className="mt-3 max-w-2xl text-sm text-fg-muted">{t(snapshot.guidance === 'manual' ? 'setup.manualSelected' : snapshot.status === 'ready' ? 'setup.completeHelp' : 'setup.choiceIntro')}</p>
        <Link to="/setup" className="mt-5 inline-flex rounded-lg border border-border-strong px-4 py-2 text-sm font-semibold text-primary hover:bg-surface-2 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary">{t(active ? 'setup.resume' : 'setup.openWizard')}</Link>
        {!active && snapshot.guidance !== 'manual' && snapshot.status !== 'ready' && <ServerSetupManualAction snapshot={snapshot} onChosen={setup.accept} />}
    </section>;
}
