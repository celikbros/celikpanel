// Only a reviewed updater's explicit pre-mutation outcome supports retry
// guidance. Historical generic failures and recovery-required states retain
// their diagnostic text; never infer safety from a package-manager substring.
export function systemUpdateFailureMessage(
    diagnostic: string,
    t: (key: 'panelUpdate.packageManagerBusy') => string,
): string {
    const summary = /(?:^|: )!! CELIKPANEL_UPDATE_FAILURE code=package_manager_busy state=unchanged reason=/;
    return summary.test(diagnostic)
        ? t('panelUpdate.packageManagerBusy')
        : diagnostic;
}

/** The reviewed updater's terminal preflight stop, parsed from its summary line. */
export type SystemUpdatePreflightStop = { step: string; diagnostic: string };

// The updater emits: code=recovery_runtime_preflight_failed state=unchanged
// reason=recovery runtime preflight step=<step>: <diagnostic> detail=
// Only this exact unchanged outcome is recognised; nothing is inferred.
export function systemUpdatePreflightStop(diagnostic: string): SystemUpdatePreflightStop | undefined {
    const match = /(?:^|: )!! CELIKPANEL_UPDATE_FAILURE code=recovery_runtime_preflight_failed state=unchanged reason=recovery runtime preflight step=([a-z_]{1,40}): (.*) detail=/.exec(diagnostic);
    if (!match) return undefined;
    return { step: match[1], diagnostic: match[2].trim() };
}

/**
 * The server's summary without the updater's internal tokens (the marker,
 * code= and state= values, and the reason=/detail= labels). Empty when nothing
 * readable remains, so the caller omits the line.
 */
export function withoutInternalTokens(message: string): string {
    const cleaned = message
        .replace(/!! CELIKPANEL_UPDATE_FAILURE/g, ' ')
        .replace(/\b(?:code|state)=\S*/g, ' ')
        .replace(/\b(?:reason|detail)=/g, ' ')
        .replace(/\s+/g, ' ')
        .replace(/[\s:;,]+$/, '')
        .trim();
    return /[A-Za-z0-9]/.test(cleaned) ? cleaned : '';
}
