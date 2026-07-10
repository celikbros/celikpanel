import { ToggleRow } from 'celikpanel-web';

// A checkbox row with a label and optional hint — the panel's standard
// on/off setting control.

export const On = () => (
    <div style={{ width: 420 }}>
        <ToggleRow name="ssl" label="Enable SSL (Let's Encrypt)" hint="Automatically provision and renew the certificate." defaultChecked />
    </div>
);

export const Off = () => (
    <div style={{ width: 420 }}>
        <ToggleRow name="www" label="Redirect to www" hint="celikpanel.cloud → www.celikpanel.cloud" />
    </div>
);

export const List = () => (
    <div style={{ width: 420, display: 'flex', flexDirection: 'column', gap: 12 }}>
        <ToggleRow name="ssl" label="Enable SSL (Let's Encrypt)" hint="Automatically provision and renew." defaultChecked />
        <ToggleRow name="https" label="Force HTTPS" hint="Redirect HTTP requests to HTTPS" defaultChecked />
        <ToggleRow name="www" label="Redirect to www" />
    </div>
);
