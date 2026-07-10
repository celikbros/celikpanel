import { Field, inputClass } from 'celikpanel-web';

// A labelled form field with an optional hint under it. Wrap any input/select
// styled with the shared inputClass.

export const WithHint = () => (
    <div style={{ width: 360 }}>
        <Field label="Domain name" hint="Enter the domain name without www">
            <input className={inputClass} defaultValue="example.com" />
        </Field>
    </div>
);

export const Select = () => (
    <div style={{ width: 360 }}>
        <Field label="PHP version">
            <select className={inputClass} defaultValue="8.4">
                <option>8.4</option>
                <option>8.3</option>
            </select>
        </Field>
    </div>
);
