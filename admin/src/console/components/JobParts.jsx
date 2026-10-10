import React from 'react';
import { Status } from './States';

const STATUS = {
  PENDING: { label: 'Queued', tone: 'neutral' },
  RUNNING: { label: 'Running', tone: 'accent', pulse: true },
  COMPLETED: { label: 'Completed', tone: 'ok' },
  FAILED: { label: 'Failed', tone: 'crit' },
  CANCELLED: { label: 'Stopped', tone: 'neutral' },
};

export const jobStatusLabel = status => STATUS[status]?.label || status;

export function JobStatus({ status }) {
  const s = STATUS[status] || { label: status, tone: 'neutral' };
  return <Status tone={s.tone} pulse={s.pulse}>{s.label}</Status>;
}
