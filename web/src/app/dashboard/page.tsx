'use client';

import React, { useEffect, useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { useAuth } from '@/context/AuthContext';
import { api, Job, WorkerInfo } from '@/lib/api';
import { Play, CheckCircle2, XCircle, Clock, Cpu, Server, ArrowRight } from 'lucide-react';

export default function DashboardPage() {
  const router = useRouter();
  const { user, loading: authLoading } = useAuth();

  const [jobs, setJobs] = useState<Job[]>([]);
  const [workers, setWorkers] = useState<WorkerInfo[]>([]);
  const [totalJobs, setTotalJobs] = useState(0);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!authLoading && !user) {
      router.push('/login');
      return;
    }

    if (user) {
      loadData();
      const interval = setInterval(loadData, 5000);
      return () => clearInterval(interval);
    }
  }, [user, authLoading, router]);

  const loadData = async () => {
    try {
      const [jobsData, workersData] = await Promise.all([
        api.listJobs(10, 0),
        api.getWorkers(),
      ]);
      setJobs(jobsData.items || []);
      setTotalJobs(jobsData.total || 0);
      setWorkers(workersData || []);
    } catch (err) {
      console.error('Failed to load dashboard data:', err);
    } finally {
      setLoading(false);
    }
  };

  const completedCount = jobs.filter((j) => j.status === 'COMPLETED').length;
  const runningCount = jobs.filter((j) => j.status === 'RUNNING').length;
  const failedCount = jobs.filter((j) => j.status === 'FAILED' || j.status === 'TIMEOUT').length;
  const onlineWorkers = workers.filter((w) => w.status === 'ONLINE').length;

  if (authLoading || loading) {
    return (
      <div className="container" style={{ textAlign: 'center', paddingTop: '4rem', color: 'var(--text-secondary)' }}>
        Loading dashboard metrics...
      </div>
    );
  }

  return (
    <div className="container">
      {/* Header */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '2rem' }}>
        <div>
          <h1 style={{ fontSize: '1.75rem', fontWeight: 800, marginBottom: '0.25rem' }}>Cluster Overview</h1>
          <p style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
            Real-time status of distributed execution cluster
          </p>
        </div>
        <Link href="/jobs/new" className="btn-primary">
          <Play size={16} />
          <span>New Execution</span>
        </Link>
      </div>

      {/* Metrics Row */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: '1rem', marginBottom: '2rem' }}>
        <div className="glass-panel" style={{ padding: '1.25rem' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', color: 'var(--text-secondary)', fontSize: '0.85rem' }}>
            <span>Total Submissions</span>
            <Server size={18} style={{ color: 'var(--accent-cyan)' }} />
          </div>
          <div style={{ fontSize: '2rem', fontWeight: 700, marginTop: '0.5rem', color: 'var(--text-primary)' }}>
            {totalJobs}
          </div>
        </div>

        <div className="glass-panel" style={{ padding: '1.25rem' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', color: 'var(--text-secondary)', fontSize: '0.85rem' }}>
            <span>Active / Running</span>
            <Clock size={18} style={{ color: '#2563eb' }} />
          </div>
          <div style={{ fontSize: '2rem', fontWeight: 700, marginTop: '0.5rem', color: '#2563eb' }}>
            {runningCount}
          </div>
        </div>

        <div className="glass-panel" style={{ padding: '1.25rem' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', color: 'var(--text-secondary)', fontSize: '0.85rem' }}>
            <span>Completed (Page)</span>
            <CheckCircle2 size={18} style={{ color: 'var(--accent-green)' }} />
          </div>
          <div style={{ fontSize: '2rem', fontWeight: 700, marginTop: '0.5rem', color: 'var(--accent-green)' }}>
            {completedCount}
          </div>
        </div>

        <div className="glass-panel" style={{ padding: '1.25rem' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', color: 'var(--text-secondary)', fontSize: '0.85rem' }}>
            <span>Online Workers</span>
            <Cpu size={18} style={{ color: '#7c3aed' }} />
          </div>
          <div style={{ fontSize: '2rem', fontWeight: 700, marginTop: '0.5rem', color: '#7c3aed' }}>
            {onlineWorkers} <span style={{ fontSize: '1rem', color: 'var(--text-muted)' }}>/ {workers.length}</span>
          </div>
        </div>
      </div>

      {/* Main Grid: Recent Jobs + Workers Snapshot */}
      <div className="grid-2">
        {/* Recent Jobs */}
        <div className="glass-panel" style={{ padding: '1.5rem' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.25rem' }}>
            <h2 style={{ fontSize: '1.1rem', fontWeight: 700 }}>Recent Submissions</h2>
            <Link href="/jobs" style={{ display: 'flex', alignItems: 'center', gap: '0.25rem', fontSize: '0.85rem', color: 'var(--accent-cyan)', textDecoration: 'none' }}>
              <span>View All</span>
              <ArrowRight size={14} />
            </Link>
          </div>

          {jobs.length === 0 ? (
            <div style={{ textAlign: 'center', padding: '2.5rem 1rem', color: 'var(--text-muted)' }}>
              No jobs submitted yet. Start your first execution!
            </div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
              {jobs.slice(0, 6).map((job) => (
                <Link
                  key={job.id}
                  href={`/jobs/${job.id}`}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    padding: '0.85rem 1rem',
                    background: 'var(--bg-tertiary)',
                    borderRadius: 'var(--radius-sm)',
                    textDecoration: 'none',
                    border: '1px solid var(--border-subtle)',
                    transition: 'border-color 0.2s ease',
                  }}
                >
                  <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
                    <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.8rem', color: 'var(--text-secondary)' }}>
                      {job.id.slice(0, 8)}...
                    </span>
                    <span style={{ fontSize: '0.8rem', textTransform: 'uppercase', fontWeight: 600, color: 'var(--text-muted)' }}>
                      {job.language}
                    </span>
                  </div>

                  <div style={{ display: 'flex', alignItems: 'center', gap: '1rem' }}>
                    <span className={`badge badge-${job.status.toLowerCase()}`}>{job.status}</span>
                    <span style={{ fontSize: '0.75rem', color: 'var(--text-muted)' }}>
                      {new Date(job.created_at).toLocaleTimeString()}
                    </span>
                  </div>
                </Link>
              ))}
            </div>
          )}
        </div>

        {/* Worker Cluster Status */}
        <div className="glass-panel" style={{ padding: '1.5rem' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.25rem' }}>
            <h2 style={{ fontSize: '1.1rem', fontWeight: 700 }}>Execution Workers</h2>
            <Link href="/workers" style={{ display: 'flex', alignItems: 'center', gap: '0.25rem', fontSize: '0.85rem', color: 'var(--accent-cyan)', textDecoration: 'none' }}>
              <span>Cluster Details</span>
              <ArrowRight size={14} />
            </Link>
          </div>

          {workers.length === 0 ? (
            <div style={{ textAlign: 'center', padding: '2.5rem 1rem', color: 'var(--text-muted)' }}>
              No active worker heartbeats received yet.
            </div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
              {workers.map((worker) => (
                <div
                  key={worker.id}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    padding: '0.85rem 1rem',
                    background: 'var(--bg-tertiary)',
                    borderRadius: 'var(--radius-sm)',
                    border: '1px solid var(--border-subtle)',
                  }}
                >
                  <div>
                    <div style={{ fontWeight: 600, fontSize: '0.9rem', color: 'var(--text-primary)' }}>
                      {worker.id}
                    </div>
                    <div style={{ fontSize: '0.75rem', color: 'var(--text-muted)' }}>
                      Host: {worker.hostname} • {worker.active_jobs} / {worker.concurrency} slots active
                    </div>
                  </div>

                  <div style={{ textAlign: 'right' }}>
                    <span className={`badge badge-${worker.status.toLowerCase()}`}>
                      {worker.status}
                    </span>
                    <div style={{ fontSize: '0.75rem', color: 'var(--text-muted)', marginTop: '0.25rem' }}>
                      HB: {Math.round(worker.seconds_since_heartbeat)}s ago
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
