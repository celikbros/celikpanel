import { StatusDot } from 'celikpanel-web';

// A tiny status indicator — green when up, grey when down. Used next to
// service and domain rows across the panel.

const Row = ({ ok, label }: { ok: boolean; label: string }) => (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, fontSize: 14, color: 'rgb(var(--fg-muted))' }}>
        <StatusDot ok={ok} />
        {label}
    </span>
);

export const Running = () => <Row ok={true} label="Running" />;

export const Stopped = () => <Row ok={false} label="Stopped" />;

export const Both = () => (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
        <Row ok={true} label="Nginx — running" />
        <Row ok={false} label="PostgreSQL — not installed" />
    </div>
);
