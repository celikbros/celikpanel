import { useI18n } from '../i18n';
import type { SetupEditorStep } from '../lib/serverSetup';

export function ServerSetupSteps({ steps, current, mobile = false }: { steps: (SetupEditorStep | 'progress')[]; current: SetupEditorStep | 'progress'; mobile?: boolean }) {
    const { t } = useI18n();
    return <nav aria-label={t('setup.steps')}>
        {mobile && <p className="mb-3 flex items-center justify-between gap-4 text-sm font-semibold"><span>{t(`setup.step.${current}`)}</span><span className="tabular-nums text-fg-muted">{steps.indexOf(current) + 1} / {steps.length}</span></p>}
        <ol className={mobile ? 'flex gap-2' : 'space-y-1'}>
            {steps.map((item, index) => <li key={item} aria-current={current === item ? 'step' : undefined}
                className={`flex items-center gap-3 rounded-lg px-3 py-3 text-sm ${mobile ? 'flex-1 justify-center px-1' : ''} ${current === item ? mobile ? 'bg-surface-2 font-semibold text-primary' : 'bg-sidebar-active font-semibold text-sidebar-active-fg' : mobile ? 'text-fg-muted' : 'text-sidebar-muted'}`}>
                <span className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-full tabular-nums ${current === item ? 'bg-primary text-primary-fg' : mobile ? 'border border-border-strong' : 'border border-sidebar-border'}`}>{index + 1}</span>
                <span className={mobile ? 'sr-only' : ''}>{t(`setup.step.${item}`)}</span>
            </li>)}
        </ol>
    </nav>;
}
