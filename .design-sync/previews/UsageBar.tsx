import { UsageBar } from 'celikpanel-web';

// Labelled progress bar that turns amber past 75% and red past 90%, so a
// full disk or maxed CPU reads at a glance. Each cell shows one threshold.

const Row = ({ label, percent }: { label: string; percent: number }) => (
    <div style={{ width: 260 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 12, marginBottom: 4, color: 'rgb(var(--fg-muted))' }}>
            <span>{label}</span>
            <span>{percent}%</span>
        </div>
        <UsageBar percent={percent} />
    </div>
);

export const Normal = () => <Row label="Disk (/)" percent={42} />;

export const Warning = () => <Row label="Memory" percent={82} />;

export const Critical = () => <Row label="CPU" percent={96} />;

export const Thresholds = () => (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        <Row label="Disk" percent={42} />
        <Row label="Memory" percent={82} />
        <Row label="CPU" percent={96} />
    </div>
);
