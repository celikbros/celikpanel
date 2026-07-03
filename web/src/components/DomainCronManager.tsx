import { useState, useEffect } from 'react';
import {
    Clock, Plus, Trash2, Edit2, RefreshCw,
    Play, Pause, Save, X, AlertCircle
} from 'lucide-react';
import { showToast } from './Toast';

interface CronJob {
    id: string;
    schedule: string;
    command: string;
    enabled: boolean;
    comment: string;
}

interface DomainCronManagerProps {
    domainId: number;
    domainName: string;
}

const SCHEDULE_PRESETS = [
    { label: 'Every minute', value: '* * * * *' },
    { label: 'Every 5 minutes', value: '*/5 * * * *' },
    { label: 'Every 15 minutes', value: '*/15 * * * *' },
    { label: 'Every hour', value: '0 * * * *' },
    { label: 'Every 6 hours', value: '0 */6 * * *' },
    { label: 'Daily at midnight', value: '0 0 * * *' },
    { label: 'Daily at noon', value: '0 12 * * *' },
    { label: 'Weekly (Sunday)', value: '0 0 * * 0' },
    { label: 'Monthly (1st)', value: '0 0 1 * *' },
];

export function DomainCronManager({ domainId }: DomainCronManagerProps) {
    const [jobs, setJobs] = useState<CronJob[]>([]);
    const [loading, setLoading] = useState(true);
    const [showAddForm, setShowAddForm] = useState(false);
    const [editingJob, setEditingJob] = useState<CronJob | null>(null);

    // Form state
    const [schedule, setSchedule] = useState('0 * * * *');
    const [command, setCommand] = useState('');
    const [comment, setComment] = useState('');
    const [saving, setSaving] = useState(false);

    useEffect(() => {
        loadJobs();
    }, [domainId]);

    const loadJobs = async () => {
        setLoading(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/cron`);
            if (res.ok) {
                const data = await res.json();
                setJobs(data.jobs || []);
            } else {
                showToast('error', 'Failed to load cron jobs');
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to load cron jobs');
        } finally {
            setLoading(false);
        }
    };

    const resetForm = () => {
        setSchedule('0 * * * *');
        setCommand('');
        setComment('');
        setEditingJob(null);
        setShowAddForm(false);
    };

    const addJob = async () => {
        if (!command.trim()) {
            showToast('error', 'Command is required');
            return;
        }

        setSaving(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/cron`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ schedule, command, comment })
            });

            const data = await res.json();
            if (data.success) {
                showToast('success', 'Cron job added');
                resetForm();
                loadJobs();
            } else {
                showToast('error', data.error || 'Failed to add cron job');
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to add cron job');
        } finally {
            setSaving(false);
        }
    };

    const updateJob = async () => {
        if (!editingJob || !command.trim()) return;

        setSaving(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/cron`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    id: editingJob.id,
                    schedule,
                    command,
                    enabled: editingJob.enabled,
                    comment
                })
            });

            const data = await res.json();
            if (data.success) {
                showToast('success', 'Cron job updated');
                resetForm();
                loadJobs();
            } else {
                showToast('error', data.error || 'Failed to update cron job');
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to update cron job');
        } finally {
            setSaving(false);
        }
    };

    const toggleJob = async (job: CronJob) => {
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/cron`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    id: job.id,
                    schedule: job.schedule,
                    command: job.command,
                    enabled: !job.enabled,
                    comment: job.comment
                })
            });

            const data = await res.json();
            if (data.success) {
                showToast('success', job.enabled ? 'Job disabled' : 'Job enabled');
                loadJobs();
            } else {
                showToast('error', data.error || 'Failed to toggle job');
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to toggle job');
        }
    };

    const deleteJob = async (job: CronJob) => {
        if (!confirm(`Delete cron job?\n${job.command}`)) return;

        try {
            const res = await fetch(`/api/v1/domains/${domainId}/cron?id=${encodeURIComponent(job.id)}`, {
                method: 'DELETE'
            });

            const data = await res.json();
            if (data.success) {
                showToast('success', 'Cron job deleted');
                loadJobs();
            } else {
                showToast('error', data.error || 'Failed to delete job');
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to delete job');
        }
    };

    const startEdit = (job: CronJob) => {
        setEditingJob(job);
        setSchedule(job.schedule);
        setCommand(job.command);
        setComment(job.comment || '');
        setShowAddForm(true);
    };

    const describeSchedule = (schedule: string) => {
        const preset = SCHEDULE_PRESETS.find(p => p.value === schedule);
        if (preset) return preset.label;

        const [min, hour, dom, month, dow] = schedule.split(' ');
        let desc = '';

        if (min === '*' && hour === '*') desc = 'Every minute';
        else if (min.startsWith('*/')) desc = `Every ${min.slice(2)} minutes`;
        else if (hour === '*') desc = `At minute ${min}`;
        else if (dom === '*' && month === '*' && dow === '*') desc = `Daily at ${hour}:${min.padStart(2, '0')}`;
        else desc = schedule;

        return desc;
    };

    return (
        <div className="space-y-6">
            {/* Add/Edit Form */}
            {showAddForm && (
                <div className="bg-surface-2/50 border border-border rounded-xl p-6">
                    <div className="flex items-center justify-between mb-4">
                        <h3 className="text-lg font-semibold text-fg">
                            {editingJob ? 'Edit Cron Job' : 'Add New Cron Job'}
                        </h3>
                        <button onClick={resetForm} className="p-1 hover:bg-surface-3 rounded">
                            <X className="w-5 h-5 text-fg-muted" />
                        </button>
                    </div>

                    <div className="space-y-4">
                        {/* Schedule */}
                        <div>
                            <label className="block text-sm font-medium text-fg-muted mb-2">Schedule</label>
                            <div className="flex gap-2">
                                <select
                                    value={SCHEDULE_PRESETS.find(p => p.value === schedule)?.value || ''}
                                    onChange={(e) => e.target.value && setSchedule(e.target.value)}
                                    className="px-3 py-2 bg-surface border border-border-strong rounded text-fg text-sm"
                                >
                                    <option value="">Custom...</option>
                                    {SCHEDULE_PRESETS.map(p => (
                                        <option key={p.value} value={p.value}>{p.label}</option>
                                    ))}
                                </select>
                                <input
                                    type="text"
                                    value={schedule}
                                    onChange={(e) => setSchedule(e.target.value)}
                                    placeholder="* * * * *"
                                    className="flex-1 px-3 py-2 bg-surface border border-border-strong rounded text-fg font-mono text-sm"
                                />
                            </div>
                            <p className="text-xs text-fg-subtle mt-1">Format: minute hour day-of-month month day-of-week</p>
                        </div>

                        {/* Command */}
                        <div>
                            <label className="block text-sm font-medium text-fg-muted mb-2">Command</label>
                            <input
                                type="text"
                                value={command}
                                onChange={(e) => setCommand(e.target.value)}
                                placeholder="/usr/bin/php /var/www/example.com/cron.php"
                                className="w-full px-3 py-2 bg-surface border border-border-strong rounded text-fg font-mono text-sm"
                            />
                        </div>

                        {/* Comment */}
                        <div>
                            <label className="block text-sm font-medium text-fg-muted mb-2">Description (optional)</label>
                            <input
                                type="text"
                                value={comment}
                                onChange={(e) => setComment(e.target.value)}
                                placeholder="Daily cleanup task"
                                className="w-full px-3 py-2 bg-surface border border-border-strong rounded text-fg text-sm"
                            />
                        </div>

                        {/* Actions */}
                        <div className="flex justify-end gap-2 pt-2">
                            <button
                                onClick={resetForm}
                                className="px-4 py-2 bg-surface-3 hover:bg-surface-3 rounded text-fg text-sm"
                            >
                                Cancel
                            </button>
                            <button
                                onClick={editingJob ? updateJob : addJob}
                                disabled={saving || !command.trim()}
                                className="flex items-center gap-2 px-4 py-2 bg-primary hover:bg-primary-hover rounded text-white text-sm disabled:opacity-50"
                            >
                                <Save className="w-4 h-4" />
                                {saving ? 'Saving...' : (editingJob ? 'Update' : 'Add Job')}
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {/* Jobs List */}
            <div className="bg-surface-2/50 border border-border rounded-xl p-6">
                <div className="flex items-center justify-between mb-4">
                    <h3 className="text-lg font-semibold text-fg">Scheduled Tasks</h3>
                    <div className="flex items-center gap-2">
                        {!showAddForm && (
                            <button
                                onClick={() => setShowAddForm(true)}
                                className="flex items-center gap-2 px-3 py-1.5 bg-primary hover:bg-primary-hover rounded text-white text-sm"
                            >
                                <Plus className="w-4 h-4" />
                                Add Job
                            </button>
                        )}
                        <button
                            onClick={loadJobs}
                            className="p-2 hover:bg-surface-3 rounded text-fg-muted hover:text-fg"
                        >
                            <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
                        </button>
                    </div>
                </div>

                {loading ? (
                    <div className="flex items-center justify-center py-12">
                        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
                    </div>
                ) : jobs.length === 0 ? (
                    <div className="text-center py-12 text-fg-subtle">
                        <Clock className="w-12 h-12 mx-auto mb-3 opacity-50" />
                        <p>No cron jobs configured</p>
                        <p className="text-sm mt-1">Add a scheduled task to automate recurring commands</p>
                    </div>
                ) : (
                    <div className="space-y-3">
                        {jobs.map((job) => (
                            <div
                                key={job.id}
                                className={`p-4 border rounded-lg ${job.enabled
                                        ? 'bg-surface-3/30 border-border'
                                        : 'bg-surface-2/50 border-border/50 opacity-60'
                                    }`}
                            >
                                <div className="flex items-start justify-between">
                                    <div className="flex-1 min-w-0">
                                        {job.comment && (
                                            <p className="text-sm text-fg-muted mb-1">{job.comment}</p>
                                        )}
                                        <p className="text-fg font-mono text-sm truncate">{job.command}</p>
                                        <div className="flex items-center gap-3 mt-2 text-xs text-fg-muted">
                                            <span className="flex items-center gap-1">
                                                <Clock className="w-3 h-3" />
                                                {describeSchedule(job.schedule)}
                                            </span>
                                            <code className="px-1.5 py-0.5 bg-surface-2 rounded">{job.schedule}</code>
                                            {!job.enabled && (
                                                <span className="px-1.5 py-0.5 bg-warning/20 text-warning rounded">Disabled</span>
                                            )}
                                        </div>
                                    </div>

                                    <div className="flex items-center gap-1 ml-4">
                                        <button
                                            onClick={() => toggleJob(job)}
                                            className={`p-2 rounded ${job.enabled
                                                    ? 'hover:bg-surface-3 text-success'
                                                    : 'hover:bg-surface-3 text-fg-muted'
                                                }`}
                                            title={job.enabled ? 'Disable' : 'Enable'}
                                        >
                                            {job.enabled ? <Pause className="w-4 h-4" /> : <Play className="w-4 h-4" />}
                                        </button>
                                        <button
                                            onClick={() => startEdit(job)}
                                            className="p-2 hover:bg-surface-3 rounded text-fg-muted hover:text-fg"
                                            title="Edit"
                                        >
                                            <Edit2 className="w-4 h-4" />
                                        </button>
                                        <button
                                            onClick={() => deleteJob(job)}
                                            className="p-2 hover:bg-surface-3 rounded text-fg-muted hover:text-danger"
                                            title="Delete"
                                        >
                                            <Trash2 className="w-4 h-4" />
                                        </button>
                                    </div>
                                </div>
                            </div>
                        ))}
                    </div>
                )}
            </div>

            {/* Info */}
            <div className="flex items-start gap-3 p-4 bg-primary/10 border border-primary/30 rounded-lg">
                <AlertCircle className="w-5 h-5 text-primary flex-shrink-0 mt-0.5" />
                <div className="text-sm text-fg-muted">
                    <p className="font-medium text-primary">Cron Format</p>
                    <p className="mt-1 font-mono text-xs">minute(0-59) hour(0-23) day(1-31) month(1-12) weekday(0-6)</p>
                    <p className="mt-1">Use <code className="text-primary">*</code> for "every", <code className="text-primary">*/5</code> for "every 5"</p>
                </div>
            </div>
        </div>
    );
}
