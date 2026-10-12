// Plain-English descriptions for common cron expressions, and the reverse:
// building a cron expression from a friendly "daily at HH:MM" / weekday choice.
// (docs/UI.md §7.3 — the user should not need to know cron syntax.)

const DAYS = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];
const MONTHS = ['January', 'February', 'March', 'April', 'May', 'June',
  'July', 'August', 'September', 'October', 'November', 'December'];

const isNum = (s) => /^\d+$/.test(s);
const pad = (s) => String(s).padStart(2, '0');

function capitalize(text) {
  return text.charAt(0).toUpperCase() + text.slice(1);
}

function ordinal(n) {
  const mod100 = n % 100;
  if (mod100 >= 11 && mod100 <= 13) return `${n}th`;
  switch (n % 10) {
    case 1: return `${n}st`;
    case 2: return `${n}nd`;
    case 3: return `${n}rd`;
    default: return `${n}th`;
  }
}

// Cron months are 1-12, so the names array is offset by one.
function describeMonths(field) {
  const parts = field.split(',');
  if (!parts.every(isNum)) return null;
  const names = parts.map((p) => MONTHS[Number(p) - 1]).filter(Boolean);
  if (names.length !== parts.length) return null;
  return names.join(' and ');
}

function describeTime(minute, hour) {
  if (minute === '*') return 'every minute';
  if (/^\*\/\d+$/.test(minute)) return `every ${minute.slice(2)} minutes`;

  if (hour === '*') {
    return isNum(minute) ? `at minute ${minute} of every hour` : null;
  }
  if (/^\*\/\d+$/.test(hour)) {
    if (minute === '0') return `every ${hour.slice(2)} hours`;
    return isNum(minute) ? `every ${hour.slice(2)} hours at minute ${minute}` : null;
  }

  const hours = hour.split(',');
  const minutes = minute.split(',');
  if (!hours.every(isNum) || !minutes.every(isNum)) return null;

  const times = [];
  for (const h of hours) for (const m of minutes) times.push(`${pad(h)}:${pad(m)}`);
  return `at ${times.join(' and ')}`;
}

function describeWeekdays(field) {
  if (field === '1-5') return 'weekday';
  if (field === '0,6' || field === '6,0') return 'weekend day';

  const parts = field.split(',');
  if (!parts.every(isNum)) return null;
  const names = parts.map((p) => DAYS[Number(p)]).filter(Boolean);
  if (!names.length) return null;
  if (names.length === 1) return names[0];
  return `${names.slice(0, -1).join(', ')} and ${names[names.length - 1]}`;
}

function describeDay(dom, dow) {
  const domAll = dom === '*';
  const dowAll = dow === '*' || dow === '0-6' || dow === '0-7';

  if (domAll && dowAll) return 'every day';
  if (!dowAll) {
    const names = describeWeekdays(dow);
    return names ? `every ${names}` : null;
  }
  if (/^\*\/\d+$/.test(dom)) return `every ${dom.slice(2)} days`;
  if (isNum(dom)) return `on the ${ordinal(Number(dom))} of the month`;
  if (dom.split(',').every(isNum)) return `on days ${dom.split(',').join(', ')}`;
  return null;
}

/**
 * Describe a five-field cron expression in plain English.
 * Returns null when the expression cannot be understood.
 */
export function describeCron(expression) {
  const fields = String(expression || '').trim().split(/\s+/).filter(Boolean);
  if (fields.length !== 5) return null;

  const [minute, hour, dom, month, dow] = fields;
  const time = describeTime(minute, hour);
  const day = describeDay(dom, dow);
  if (!time || !day) return null;

  const monthPart = month === '*' ? '' : (() => {
    const names = describeMonths(month);
    return names ? ` in ${names}` : null;
  })();
  if (monthPart === null) return null;

  if (day === 'every day') {
    return capitalize(time.startsWith('every') ? time : `${day} ${time}`) + monthPart;
  }
  return capitalize(`${day} ${time}${monthPart}`);
}

/** Blocking validation: a cron expression must have exactly five fields. */
export function cronFieldError(expression) {
  const count = String(expression || '').trim().split(/\s+/).filter(Boolean).length;
  if (count === 0) return 'Enter a cron expression.';
  if (count !== 5) return `Cron needs five fields (minute hour day month weekday) — got ${count}.`;
  return null;
}

/** Build "minute hour * * dayOfWeek" from a time and a set of weekday numbers. */
export function buildDailyCron(time, weekdays) {
  const [hh, mm] = String(time || '03:00').split(':');
  const dow = weekdays && weekdays.length ? [...weekdays].sort().join(',') : '*';
  return `${Number(mm)} ${Number(hh)} * * ${dow}`;
}

/** Split a guided daily cron back into { time, weekdays }, or null. */
export function parseDailyCron(expression) {
  const match = /^(\d{1,2}) (\d{1,2}) \* \* (\*|[0-6](,[0-6])*)$/.exec(String(expression || '').trim());
  if (!match) return null;
  const [, minute, hour, dow] = match;
  return {
    time: `${pad(hour)}:${pad(minute)}`,
    weekdays: dow === '*' ? [] : dow.split(',').map(Number),
  };
}

export const WEEKDAY_NAMES = DAYS;
