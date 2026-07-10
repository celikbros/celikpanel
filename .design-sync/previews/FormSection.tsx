import { FormSection, Field, ToggleRow, inputClass } from 'celikpanel-web';

// A titled form section with a divider — groups related fields on a settings
// screen without card-in-card nesting.

export const WithFields = () => (
    <div style={{ width: 480 }}>
        <FormSection title="General" description="Basic settings for this domain">
            <Field label="Document root" hint="Where this website's files live">
                <input className={inputClass} defaultValue="/var/www/celikpanel" />
            </Field>
            <ToggleRow name="https" label="Force HTTPS" hint="Redirect HTTP requests to HTTPS" defaultChecked />
        </FormSection>
    </div>
);

export const Stacked = () => (
    <div style={{ width: 480 }}>
        <FormSection title="Mail policy" description="Server-wide inbound and outbound rules">
            <Field label="Max message size (MB)">
                <input className={inputClass} defaultValue="25" />
            </Field>
        </FormSection>
        <FormSection title="Outbound rate">
            <ToggleRow name="rate" label="Limit outgoing mail rate" defaultChecked />
        </FormSection>
    </div>
);
