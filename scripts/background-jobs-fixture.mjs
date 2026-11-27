// Isolated supervisor fixture. It does not start Meter or modify its data.
import { BackgroundJobs } from '../server/background-jobs.ts';

const jobs = new BackgroundJobs(notice => {
  if (notice.type === 'output') console.log(`[${notice.job.label}] events:`, notice.lines, notice.dropped ? `(${notice.dropped} dropped)` : '');
  else console.log(`[${notice.job.label}] ${notice.job.status}, exit=${notice.job.exitCode}`);
});
const cwd = process.cwd();
jobs.start({ conversationId: 'fixture', label: 'Unit tests', command: "sleep 1; echo 'unit tests passed'", cwd });
jobs.start({ conversationId: 'fixture', label: 'CI status', command: "echo 'CI queued'; sleep 1; echo 'integration tests failed'; sleep 1", cwd, mode: 'monitor' });
await new Promise(resolve => setTimeout(resolve, 3000));
await jobs.dispose();
