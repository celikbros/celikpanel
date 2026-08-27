import { useEffect, useRef } from 'react';
import { Loader2 as LoaderCircle, WifiOff, X } from 'lucide-react';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import type { ApiError } from '../lib/apiError';
import type { ComponentOperation, InteractionBlockView } from './ComponentOperation';
import { ErrorBanner } from './ui';

type OperationOverlayProps = {
    view: InteractionBlockView | null;
    operation: ComponentOperation | null;
    label: string;
    submitting: boolean;
    recovering: boolean;
    refreshing: boolean;
    interrupted: boolean;
};

type FailureOverlayProps = {
    failure: ApiError;
    dismissLabel: string;
    onDismiss: () => void;
};

export default function OperationOverlay(props: OperationOverlayProps | FailureOverlayProps) {
    const { t } = useI18n();
    const dialogRef = useRef<HTMLDivElement>(null);

    useEffect(() => {
        dialogRef.current?.focus();
    }, []);

    if ('failure' in props) {
        return (
            <div
                role="alert"
                className="fixed inset-x-4 top-4 z-[90] mx-auto max-w-2xl rounded-xl bg-surface shadow-2xl"
            >
                <div className="relative">
                    <ErrorBanner error={props.failure} className="pr-12" />
                    <button
                        type="button"
                        onClick={props.onDismiss}
                        aria-label={props.dismissLabel}
                        title={props.dismissLabel}
                        className="absolute right-2 top-2 rounded-md p-1.5 text-danger transition-colors hover:bg-danger/10"
                    >
                        <X className="h-4 w-4" />
                    </button>
                </div>
            </div>
        );
    }
    const { view, operation, label, submitting, recovering, refreshing, interrupted } = props;
    const disconnected = view?.interrupted ?? interrupted;
    let statusText = view?.status ?? (disconnected
        ? t('services.operation.reconnecting')
        : t('services.operation.starting'));
    if (!view && !disconnected && recovering) {
        statusText = t('services.operation.recoveringRequest');
    } else if (!view && !disconnected && refreshing) {
        statusText = t('services.operation.refreshing');
    } else if (!view && !disconnected && !submitting && operation) {
        const normalizedPhase = operation.phase.trim().toLowerCase().replace(/[^a-z0-9]+/g, '_');
        const phaseKey = `services.operation.phase.${normalizedPhase}` as TranslationKey;
        const translated = t(phaseKey);
        statusText = translated === phaseKey
            ? operation.phase || t('services.operation.running')
            : translated;
    }
    const hint = view?.hint ?? t('services.operation.backgroundHint');
    const operationID = view?.operationID || operation?.id;

    return (
        <div
            ref={dialogRef}
            role="dialog"
            aria-modal="true"
            aria-labelledby="component-operation-title"
            tabIndex={-1}
            className="fixed inset-0 z-[100] flex items-center justify-center bg-slate-950/75 p-4 backdrop-blur-sm outline-none"
        >
            <div className="w-full max-w-lg rounded-2xl border border-border-strong bg-surface p-6 text-center shadow-2xl">
                <span className={`mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-2xl ${
                    disconnected ? 'bg-warning/10 text-warning' : 'bg-primary/10 text-primary'
                }`}>
                    {disconnected
                        ? <WifiOff className="h-7 w-7" />
                        : <LoaderCircle className="h-7 w-7 animate-spin" />}
                </span>
                <h2 id="component-operation-title" className="text-xl font-semibold text-fg">
                    {view?.title ?? t('services.operation.title', { name: label || t('services.install') })}
                </h2>
                <p role="status" aria-live="polite" className="mt-2 text-sm font-medium text-fg-muted">
                    {statusText}
                </p>
                {hint && (
                    <p className="mt-4 rounded-lg border border-border bg-surface-2 px-4 py-3 text-xs leading-5 text-fg-subtle">
                        {hint}
                    </p>
                )}
                {operationID && (
                    <p className="mt-3 font-mono text-[11px] text-fg-subtle">
                        {view?.operationID ?? t('services.operation.id', { id: operationID })}
                    </p>
                )}
            </div>
        </div>
    );
}
