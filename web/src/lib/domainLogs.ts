export interface DomainLogTimeFilterResult {
    applied: boolean;
    exact: boolean;
    start_time?: string;
    end_time?: string;
    parsed_lines: number;
    unparsed_lines: number;
    assumed_timezone?: string;
    warning?: string;
}

export interface DomainLogsResponse {
    success: boolean;
    lines: string[];
    total: number;
    log_path: string;
    truncated: boolean;
    warning?: string;
    time_filter?: DomainLogTimeFilterResult;
}

export type LogTimeRangeError = 'invalid' | 'reversed';

export interface LogTimeRangeQuery {
    startTime?: string;
    endTime?: string;
    error?: LogTimeRangeError;
}

const localDateTimePattern =
    /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})(?::(\d{2})(?:\.(\d{1,3}))?)?$/;

// datetime-local deliberately has no timezone. Construct and round-trip its
// fields in the browser's local zone, then serialize the instant as RFC3339
// with an explicit Z offset. The round trip rejects normalized invalid dates
// and local times that do not exist during a daylight-saving transition.
export function localDateTimeToRFC3339(value: string): string | null {
    const match = localDateTimePattern.exec(value);
    if (!match) return null;

    const [, yearText, monthText, dayText, hourText, minuteText, secondText = '0', fractionText = '0'] = match;
    const year = Number(yearText);
    const month = Number(monthText) - 1;
    const day = Number(dayText);
    const hour = Number(hourText);
    const minute = Number(minuteText);
    const second = Number(secondText);
    const millisecond = Number(fractionText.padEnd(3, '0'));

    const local = new Date(0);
    local.setFullYear(year, month, day);
    local.setHours(hour, minute, second, millisecond);

    if (
        Number.isNaN(local.getTime()) ||
        local.getFullYear() !== year ||
        local.getMonth() !== month ||
        local.getDate() !== day ||
        local.getHours() !== hour ||
        local.getMinutes() !== minute ||
        local.getSeconds() !== second ||
        local.getMilliseconds() !== millisecond
    ) {
        return null;
    }

    return local.toISOString();
}

export function buildLogTimeRangeQuery(startLocal: string, endLocal: string): LogTimeRangeQuery {
    const startTime = startLocal ? localDateTimeToRFC3339(startLocal) : undefined;
    const endTime = endLocal ? localDateTimeToRFC3339(endLocal) : undefined;
    if (startTime === null || endTime === null) return { error: 'invalid' };
    if (startTime && endTime && Date.parse(startTime) > Date.parse(endTime)) return { error: 'reversed' };
    return { startTime, endTime };
}

function isRecord(value: unknown): value is Record<string, unknown> {
    return typeof value === 'object' && value !== null;
}

function optionalString(value: unknown): string | undefined {
    return typeof value === 'string' && value ? value : undefined;
}

function readTimeFilter(value: unknown): DomainLogTimeFilterResult | undefined {
    if (!isRecord(value) || typeof value.applied !== 'boolean' || typeof value.exact !== 'boolean') return undefined;
    return {
        applied: value.applied,
        exact: value.exact,
        start_time: optionalString(value.start_time),
        end_time: optionalString(value.end_time),
        parsed_lines: typeof value.parsed_lines === 'number' ? value.parsed_lines : 0,
        unparsed_lines: typeof value.unparsed_lines === 'number' ? value.unparsed_lines : 0,
        assumed_timezone: optionalString(value.assumed_timezone),
        warning: optionalString(value.warning),
    };
}

// Treat the HTTP response as untrusted data. A malformed line collection must
// not crash React or silently turn an API contract error into an empty log.
export function parseDomainLogsResponse(value: unknown): DomainLogsResponse | null {
    if (!isRecord(value) || value.success !== true || !Array.isArray(value.lines) || !value.lines.every((line) => typeof line === 'string')) {
        return null;
    }
    return {
        success: true,
        lines: value.lines,
        total: typeof value.total === 'number' ? value.total : value.lines.length,
        log_path: typeof value.log_path === 'string' ? value.log_path : '',
        truncated: value.truncated === true,
        warning: optionalString(value.warning),
        time_filter: readTimeFilter(value.time_filter),
    };
}
