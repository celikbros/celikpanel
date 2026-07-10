import { Button } from 'celikpanel-web';
import { Plus, Trash2, DownloadCloud } from 'lucide-react';

// One filled primary action per view; everything else is outlined secondary
// or danger. Mirrors how the panel's toolbars actually use it.

export const Primary = () => <Button variant="primary" icon={Plus}>Add domain</Button>;

export const Secondary = () => <Button variant="secondary">Cancel</Button>;

export const Danger = () => <Button variant="danger" icon={Trash2}>Uninstall</Button>;

export const Variants = () => (
    <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
        <Button variant="primary" icon={DownloadCloud}>Install</Button>
        <Button variant="secondary">Rescan</Button>
        <Button variant="danger" icon={Trash2}>Delete</Button>
    </div>
);

export const Disabled = () => <Button variant="primary" disabled>Creating…</Button>;
