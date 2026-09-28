'use client';

import React, { useEffect, useState, useRef } from 'react';
import { useParams, useRouter } from 'next/navigation';
import { useAuth } from '@/context/AuthContext';
import { api, Job, JobAttempt, ExecutionEvent } from '@/lib/api';
import {
  Play,
  RotateCcw,
  StopCircle,
  Terminal,
  Cpu,
  Clock,
  AlertTriangle,
  FileCode,
  Layers,
  CheckCircle,
  XCircle,
} from 'lucide-react';

export default function JobDetailPage() {
  const params = useParams();
  const router = useRouter();
  const { user, loading: authLoading } = useAuth();
  const jobID = params.id as string;

  const [job, setJob] = useState<Job | null>(null);
  const [attempts, setAttempts] = useState<JobAttempt[]>([]);
  const [stdout, setStdout] = useState('');
  const [stderr, setStderr] = useState('');
  const [status, setStatus] = useState<string>('QUEUED');
  const [exitCode, setExitCode] = useState<number | null>(null);
  const [truncated, setTruncated] = useState(false);
  const [loading, setLoading] = useState(true);
  const [cancelling, setCancelling] = useState(false);
  const [activeTab, setActiveTab] = useState<'console' | 'source' | 'attempts'>('console');

  const terminalEndRef = useRef<HTMLDivElement>(null);

  // Auto-scroll terminal on new output chunks
  useEffect(() => {
    if (activeTab === 'console') {
      terminalEndRef.current?.scrollIntoView({ behavior: 'smooth' });
    }
  }, [stdout, stderr, activeTab]);

  useEffect(() => {
    if (!authLoading && !user) {
      router.push('/login');
      return;
    }

    if (!jobID) return;

    // 1. Initial snapshot fetch
    api.getJob(jobID)
      .then((data) => {
        setJob(data);
        setStatus(data.status);
        if (data.stdout) setStdout(data.stdout);
        if (data.stderr) setStderr(data.stderr);
        if (data.exit_code !== undefined) setExitCode(data.exit_code);
        setTruncated(data.output_truncated);
      })
      .catch((err) => console.error('Failed to load job:', err))
      .finally(() => setLoading(false));

    api.getJobAttempts(jobID)
      .then((atts) => setAttempts(atts || []))
      .catch((err) => console.error('Failed to load attempts:', err));

    // 2. Subscribe to real-time Server-Sent Events
    const unsubscribe = api.subscribeJobEvents(
      jobID,
      (ev: ExecutionEvent) => {
        if (ev.type === 'status' && ev.status) {
          setStatus(ev.status);
        } else if (ev.type === 'stdout' && ev.data) {
          setStdout((prev) => prev + ev.data);
        } else if (ev.type === 'stderr' && ev.data) {
          setStderr((prev) => prev + ev.data);
        } else if (ev.type === 'terminal') {
          if (ev.status) setStatus(ev.status);
          if (ev.exit_code !== undefined) setExitCode(ev.exit_code);
          if (ev.truncated !== undefined) setTruncated(ev.truncated);
          // Refresh job details & attempts on terminal transition
          api.getJob(jobID).then(setJob).catch(console.error);
          api.getJobAttempts(jobID).then(setAttempts).catch(console.error);
        }
      },
      (err) => {
        console.warn('SSE subscription error or closed:', err);
      }
    );

    return () => {
      unsubscribe();
    };
  }, [jobID, user, authLoading, router]);

  const handleCancel = async () => {
    setCancelling(true);
    try {
      const updated = await api.cancelJob(jobID);
      setJob(updated);
      setStatus('CANCELLED');
    } catch (err: any) {
      alert(`Cancellation failed: ${err.message}`);
    } finally {
      setCancelling(false);
    }
  };

  if (authLoading || loading) {
    return (
      <div className="container" style={{ textAlign: 'center', paddingTop: '4rem', color: 'var(--text-secondary)' }}>
        Loading job execution data...
      </div>
    );
  }

  if (!job) {
    return (
      <div className="container" style={{ textAlign: 'center', paddingTop: '4rem', color: 'var(--accent-rose)' }}>
        Job not found or access denied.
      </div>
    );
  }

  const isTerminal = status === 'COMPLETED' || status === 'FAILED' || status === 'TIMEOUT' || status === 'CANCELLED';

  return (
    <div className="container" style={{ maxWidth: '1400px' }}>
      {/* Top Header Card */}
      <div className="glass-panel" style={{ padding: '1.25rem 1.75rem', marginBottom: '1.5rem' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '1rem' }}>
          <div>
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', marginBottom: '0.4rem' }}>
              <span style={{ fontSize: '1.2rem', fontWeight: 700, fontFamily: 'var(--font-mono)' }}>
                Job #{job.id.slice(0, 8)}
              </span>
              <span className={`badge badge-${status.toLowerCase()}`}>{status}</span>
              {truncated && (
                <span className="badge" style={{ background: 'rgba(245, 158, 11, 0.2)', color: '#fbbf24' }}>
                  <AlertTriangle size={12} /> Output Truncated
                </span>
              )}
            </div>

            <div style={{ fontSize: '0.85rem', color: 'var(--text-secondary)', display: 'flex', gap: '1.5rem', flexWrap: 'wrap' }}>
              <span>Language: <strong style={{ color: 'var(--text-primary)', textTransform: 'uppercase' }}>{job.language}</strong></span>
              <span>Worker: <strong style={{ color: 'var(--text-primary)', fontFamily: 'var(--font-mono)' }}>{job.assigned_worker || 'Assigning...'}</strong></span>
              <span>Attempt: <strong style={{ color: 'var(--text-primary)' }}>{job.current_attempt} / {job.max_attempts}</strong></span>
              {exitCode !== null && (
                <span>Exit Code: <strong style={{ color: exitCode === 0 ? 'var(--accent-green)' : 'var(--accent-rose)' }}>{exitCode}</strong></span>
              )}
            </div>
          </div>

          <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
            {!isTerminal && (
              <button
                type="button"
                className="btn-secondary"
                onClick={handleCancel}
                disabled={cancelling}
                style={{ borderColor: 'var(--accent-rose)', color: '#fb7185' }}
              >
                <StopCircle size={16} />
                <span>{cancelling ? 'Cancelling...' : 'Cancel Job'}</span>
              </button>
            )}

            <button
              type="button"
              className="btn-primary"
              onClick={() => router.push('/jobs/new')}
            >
              <RotateCcw size={15} />
              <span>Submit Another</span>
            </button>
          </div>
        </div>
      </div>

      {/* Tabs */}
      <div style={{ display: 'flex', gap: '0.5rem', marginBottom: '1rem' }}>
        <button
          onClick={() => setActiveTab('console')}
          className={`btn-secondary ${activeTab === 'console' ? 'btn-primary' : ''}`}
          style={{ padding: '0.45rem 1rem', fontSize: '0.85rem' }}
        >
          <Terminal size={15} />
          <span>Live Console</span>
        </button>
        <button
          onClick={() => setActiveTab('source')}
          className={`btn-secondary ${activeTab === 'source' ? 'btn-primary' : ''}`}
          style={{ padding: '0.45rem 1rem', fontSize: '0.85rem' }}
        >
          <FileCode size={15} />
          <span>Source Code</span>
        </button>
        <button
          onClick={() => setActiveTab('attempts')}
          className={`btn-secondary ${activeTab === 'attempts' ? 'btn-primary' : ''}`}
          style={{ padding: '0.45rem 1rem', fontSize: '0.85rem' }}
        >
          <Layers size={15} />
          <span>Attempts History ({attempts.length})</span>
        </button>
      </div>

      {/* Tab: Console */}
      {activeTab === 'console' && (
        <div className="terminal-window">
          <div className="terminal-header">
            <div className="terminal-dots">
              <div className="terminal-dot terminal-dot-red" />
              <div className="terminal-dot terminal-dot-yellow" />
              <div className="terminal-dot terminal-dot-green" />
            </div>
            <span>
              {status === 'RUNNING' ? '● Streaming Output...' : `Process Finished (Exit ${exitCode ?? '?'})`}
            </span>
            <span>stdout / stderr</span>
          </div>

          <div className="terminal-body" style={{ minHeight: '380px' }}>
            {stdout === '' && stderr === '' ? (
              <div style={{ color: 'var(--text-muted)', fontStyle: 'italic' }}>
                {status === 'QUEUED' && 'Job is queued in Redis Streams, waiting for available worker slot...'}
                {status === 'RUNNING' && 'Container starting and program executing...'}
                {status === 'COMPLETED' && 'Program produced no console output.'}
                {status === 'CANCELLED' && 'Execution was cancelled.'}
              </div>
            ) : (
              <>
                {stdout && <div style={{ color: '#e2e8f0' }}>{stdout}</div>}
                {stderr && <div style={{ color: '#fb7185', marginTop: stdout ? '0.5rem' : 0 }}>{stderr}</div>}
              </>
            )}

            {job.failure_reason && (
              <div style={{
                marginTop: '1rem',
                padding: '0.5rem 0.75rem',
                background: 'rgba(244, 63, 94, 0.15)',
                border: '1px solid rgba(244, 63, 94, 0.3)',
                borderRadius: '4px',
                color: '#fb7185',
                fontSize: '0.8rem',
              }}>
                <strong>Failure Details ({job.failure_category}):</strong> {job.failure_reason}
              </div>
            )}
            <div ref={terminalEndRef} />
          </div>
        </div>
      )}

      {/* Tab: Source Code */}
      {activeTab === 'source' && (
        <div className="glass-panel" style={{ padding: '1.25rem' }}>
          <div style={{ marginBottom: '0.75rem', fontSize: '0.85rem', color: 'var(--text-secondary)' }}>
            Submitted Source Code
          </div>
          <pre style={{
            background: '#06090e',
            padding: '1.25rem',
            borderRadius: 'var(--radius-sm)',
            fontFamily: 'var(--font-mono)',
            fontSize: '0.85rem',
            overflowX: 'auto',
            border: '1px solid var(--border-subtle)',
          }}>
            {job.source_code}
          </pre>

          {job.stdin && (
            <div style={{ marginTop: '1.5rem' }}>
              <div style={{ marginBottom: '0.75rem', fontSize: '0.85rem', color: 'var(--text-secondary)' }}>
                Standard Input (stdin)
              </div>
              <pre style={{
                background: '#06090e',
                padding: '1rem',
                borderRadius: 'var(--radius-sm)',
                fontFamily: 'var(--font-mono)',
                fontSize: '0.85rem',
                border: '1px solid var(--border-subtle)',
              }}>
                {job.stdin}
              </pre>
            </div>
          )}
        </div>
      )}

      {/* Tab: Attempts History */}
      {activeTab === 'attempts' && (
        <div className="glass-panel" style={{ padding: '1.25rem' }}>
          <div style={{ marginBottom: '1rem', fontSize: '0.9rem', fontWeight: 600 }}>
            Execution Attempt Trail
          </div>
          {attempts.length === 0 ? (
            <div style={{ color: 'var(--text-muted)', fontSize: '0.85rem' }}>No attempt records yet.</div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
              {attempts.map((att) => (
                <div
                  key={att.id}
                  style={{
                    background: 'var(--bg-tertiary)',
                    border: '1px solid var(--border-subtle)',
                    borderRadius: 'var(--radius-sm)',
                    padding: '1rem',
                  }}
                >
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.5rem' }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                      <strong style={{ fontSize: '0.9rem' }}>Attempt #{att.attempt_number}</strong>
                      <span className={`badge badge-${att.status.toLowerCase()}`}>{att.status}</span>
                    </div>
                    <span style={{ fontSize: '0.8rem', color: 'var(--text-muted)', fontFamily: 'var(--font-mono)' }}>
                      Worker: {att.worker_id}
                    </span>
                  </div>

                  <div style={{ fontSize: '0.8rem', color: 'var(--text-secondary)', display: 'flex', gap: '1rem' }}>
                    <span>Started: {new Date(att.started_at).toLocaleTimeString()}</span>
                    {att.finished_at && <span>Finished: {new Date(att.finished_at).toLocaleTimeString()}</span>}
                    {att.exit_code !== null && <span>Exit: {att.exit_code}</span>}
                  </div>

                  {att.failure_reason && (
                    <div style={{ marginTop: '0.5rem', fontSize: '0.8rem', color: '#fb7185' }}>
                      {att.failure_category}: {att.failure_reason}
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Resource Boundaries Details Footer */}
      <div className="glass-panel" style={{ marginTop: '1.5rem', padding: '1rem 1.5rem' }}>
        <div style={{ fontSize: '0.8rem', color: 'var(--text-muted)', display: 'flex', gap: '2rem', flexWrap: 'wrap' }}>
          <span>CPU Limit: <strong>{job.limits.cpu_limit} Core</strong></span>
          <span>RAM Limit: <strong>{job.limits.memory_limit_mb} MB</strong></span>
          <span>Timeout Limit: <strong>{job.limits.timeout_seconds}s</strong></span>
          <span>Max Output: <strong>{Math.round(job.limits.max_output_bytes / 1024)} KB</strong></span>
          <span>Created: <strong>{new Date(job.created_at).toLocaleString()}</strong></span>
        </div>
      </div>
    </div>
  );
}
