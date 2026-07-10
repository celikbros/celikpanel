import { PageHeader, Button } from 'celikpanel-web';
import { Plus } from 'lucide-react';

// Page title with optional breadcrumb, subtitle and an actions slot. Every
// panel screen opens with one so navigation and the primary action are
// always in the same place.

export const Full = () => (
    <div style={{ width: 720 }}>
        <PageHeader
            title="Domains"
            subtitle="Manage the domains hosted on this server"
            breadcrumb={['Home', 'Domains']}
            actions={<Button variant="primary" icon={Plus}>Add domain</Button>}
        />
    </div>
);

export const TitleOnly = () => (
    <div style={{ width: 720 }}>
        <PageHeader title="Settings" />
    </div>
);

export const WithSubtitle = () => (
    <div style={{ width: 720 }}>
        <PageHeader
            title="Services"
            subtitle="Install and manage core system services"
            breadcrumb={['Home', 'Services']}
        />
    </div>
);
