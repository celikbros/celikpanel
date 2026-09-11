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
