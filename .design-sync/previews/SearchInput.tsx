import { SearchInput } from 'celikpanel-web';

// A search box with a leading magnifier icon. Controlled — value and onChange
// come from the parent. Shown here with a placeholder and with a value.

export const Empty = () => (
    <SearchInput value="" onChange={() => {}} placeholder="Search domains…" />
);

export const WithValue = () => (
    <SearchInput value="celikpanel.cloud" onChange={() => {}} placeholder="Search domains…" />
);
