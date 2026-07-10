import { EmptyState, Button } from 'celikpanel-web';
import { Globe, Plus, Database } from 'lucide-react';

// A centred empty-state card with an icon, message and an optional action —
// shown when a list (domains, databases…) has no items yet.

export const WithAction = () => (
    <div style={{ width: 560 }}>
        <EmptyState
            icon={Globe}
            title="No domains yet"
            hint="Get started by adding your first domain"
            action={<Button variant="primary" icon={Plus}>Add domain</Button>}
        />
    </div>
);

export const NoAction = () => (
    <div style={{ width: 560 }}>
        <EmptyState
            icon={Database}
            title="No databases created yet"
            hint="Click Create database to get started"
        />
    </div>
);
