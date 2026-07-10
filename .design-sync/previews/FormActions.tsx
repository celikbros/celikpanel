import { FormActions, Button } from 'celikpanel-web';

// A right-aligned action bar that closes a form — primary action on the
// right, secondary (cancel) to its left.

export const SaveCancel = () => (
    <div style={{ width: 480 }}>
        <FormActions>
            <Button variant="secondary">Cancel</Button>
            <Button variant="primary">Save changes</Button>
        </FormActions>
    </div>
);

export const SingleAction = () => (
    <div style={{ width: 480 }}>
        <FormActions>
            <Button variant="primary">Apply</Button>
        </FormActions>
    </div>
);
