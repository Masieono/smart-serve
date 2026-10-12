import test from 'node:test';
import assert from 'node:assert/strict';

import { describeCron, buildDailyCron, parseDailyCron, cronFieldError } from '../assets/js/cron.js';

test('describes common schedules in plain English', () => {
  assert.equal(describeCron('0 3 * * *'), 'Every day at 03:00');
  assert.equal(describeCron('30 2 * * 0'), 'Every Sunday at 02:30');
  assert.equal(describeCron('0 */6 * * *'), 'Every 6 hours');
  assert.equal(describeCron('*/15 * * * *'), 'Every 15 minutes');
  assert.equal(describeCron('0 3 * * 1,3,5'), 'Every Monday, Wednesday and Friday at 03:00');
  assert.equal(describeCron('0 0 1 * *'), 'On the 1st of the month at 00:00');
  assert.equal(describeCron('0 9 * * 1-5'), 'Every weekday at 09:00');
  assert.equal(describeCron('0 3 * 3 *'), 'Every day at 03:00 in March');
});

test('returns null for expressions it cannot describe', () => {
  assert.equal(describeCron('0 3 * *'), null);
  assert.equal(describeCron('0 3 * * MON'), null);
  assert.equal(describeCron(''), null);
});

test('round-trips the guided daily form', () => {
  const daily = buildDailyCron('03:00', []);
  assert.equal(daily, '0 3 * * *');
  assert.deepEqual(parseDailyCron(daily), { time: '03:00', weekdays: [] });

  const weekly = buildDailyCron('22:30', [1, 4]);
  assert.equal(weekly, '30 22 * * 1,4');
  assert.deepEqual(parseDailyCron(weekly), { time: '22:30', weekdays: [1, 4] });

  // An interval cron is not a guided daily schedule.
  assert.equal(parseDailyCron('0 */6 * * *'), null);
});

test('field validation counts the five fields', () => {
  assert.equal(cronFieldError('0 3 * * *'), null);
  assert.match(cronFieldError('0 3 * *'), /five fields/);
  assert.match(cronFieldError(''), /Enter a cron/);
});
