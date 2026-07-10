import { Card, Button } from 'celikpanel-web';
import { Server } from 'lucide-react';

// Raised container with an optional icon+title header and an action slot.
// The panel groups related settings and stats inside these.

export const Basic = () => (
    <Card>
        <div style={{ padding: 16, fontSize: 14 }}>Plain card body with no header.</div>
    </Card>
);

export const WithHeader = () => (
    <Card title="Server information" icon={Server}>
        <div style={{ padding: 16, fontSize: 14, color: 'rgb(var(--fg-muted))' }}>
            Hostname, uptime and load average appear here.
        </div>
    </Card>
);

export const WithAction = () => (
    <Card title="Databases" icon={Server} action={<Button variant="secondary">Refresh</Button>}>
        <div style={{ padding: 16, fontSize: 14, color: 'rgb(var(--fg-muted))' }}>
            2 databases · 40 MB used
        </div>
    </Card>
);
