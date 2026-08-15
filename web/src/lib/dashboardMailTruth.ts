export type DashboardMailProfileStatus = 'unknown' | 'available' | 'partial' | 'complete' | 'blocked';
export type DashboardMailAttemptStatus = 'none' | 'in_progress' | 'succeeded' | 'failed';

export interface DashboardMailTruthProfile {
    status: DashboardMailProfileStatus;
    verified: boolean;
    latest_attempt_status: DashboardMailAttemptStatus;
    warning?: string;
    blocked_reason?: string;
}

export function summarizeDashboardMailTruth<T extends DashboardMailTruthProfile>(
    profiles: T[],
    scanFresh: boolean,
) {
    const complete = profiles.some((profile) => (
        profile.status === 'complete' && profile.verified && !profile.warning
    ));
    const needsReconciliation = (profile: T) => (
        profile.status === 'unknown'
        || profile.status === 'partial'
        || (profile.status === 'complete' && (!profile.verified || Boolean(profile.warning)))
    );
    const attemptedProblem = profiles.find((profile) => (
        needsReconciliation(profile) && profile.latest_attempt_status === 'failed'
    )) ?? profiles.find((profile) => (
        needsReconciliation(profile) && profile.latest_attempt_status === 'in_progress'
    )) ?? profiles.find((profile) => (
        needsReconciliation(profile)
        && profile.latest_attempt_status === 'succeeded'
        && !profile.verified
    ));
    const observedProblem = profiles.find((profile) => profile.status === 'unknown')
        ?? profiles.find((profile) => profile.status === 'partial')
        ?? profiles.find((profile) => (
            profile.status === 'complete' && (!profile.verified || Boolean(profile.warning))
        ))
        ?? profiles.find((profile) => profile.status === 'blocked');

    // A verified selected profile wins over unattempted alternatives that are
    // only "partial" because mail profiles share Postfix/Dovecot. A real latest
    // failed/in-progress/unproven attempt remains actionable and is never hidden.
    const problem = attemptedProblem ?? (complete ? undefined : observedProblem);
    const partial = scanFresh
        && problem?.status === 'partial'
        && attemptedProblem === undefined;
    const needsAttention = scanFresh && Boolean(problem && !partial);
    const availableOnly = profiles.every((profile) => profile.status === 'available');

    return { complete, problem, partial, needsAttention, availableOnly };
}
