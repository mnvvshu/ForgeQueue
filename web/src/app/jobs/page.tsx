'use client';

import React, { useEffect, useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { useAuth } from '@/context/AuthContext';
import { api, Job } from '@/lib/api';
import { List, Play, ChevronLeft, ChevronRight, ExternalLink } from 'lucide-react';

export default function JobsHistoryPage() {
  const router = useRouter();
  const { user, loading: authLoading } = useAuth();

  const [jobs, setJobs] = useState<Job[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const limit = 15;

  useEffect(() => {
    if (!authLoading && !user) {
      router.push('/login');
      return;
    }

    if (user) {
      fetchJobs(page);
    }
  }, [user, authLoading, page, router]);

  const fetchJobs = async (currentPage: number) => {
    setLoading(true);
    try {
      const offset = (currentPage - 1) * limit;
      const data = await api.listJobs(limit, offset);
      setJobs(data.items || []);
      setTotal(data.total || 0);
    } catch (err) {
      console.error('Failed to fetch jobs:', err);
    } finally {
      setLoading(false);
    }
  };

  const totalPages = Math.ceil(total / limit) || 1;

  if (authLoading) {
    return <div className="container" style={{ textAlign: 'center', paddingTop: '4rem' }}>Loading...</div>;
  }

  return (
    <div className="container">
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.5rem' }}>
        <div>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <List size={22} style={{ color: 'var(--accent-cyan)' }} />
            <h1 style={{ fontSize: '1.5rem', fontWeight: 800 }}>Execution History</h1>
          </div>
          <p style={{ color: 'var(--text-secondary)', fontSize: '0.85rem' }}>
            All code execution requests submitted by your account
          </p>
        </div>

        <Link href="/jobs/new" className="btn-primary">
          <Play size={15} />
          <span>New Execution</span>
        </Link>
      </div>

      <div className="glass-panel" style={{ overflow: 'hidden' }}>
        <div className="table-container">
          <table className="data-table">
            <thead>
              <tr>
                <th>Job ID</th>
                <th>Language</th>
                <th>Status</th>
                <th>Attempt</th>
                <th>Worker</th>
                <th>Created At</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <tr>
                  <td colSpan={7} style={{ textAlign: 'center', padding: '3rem', color: 'var(--text-muted)' }}>
                    Fetching execution logs...
                  </td>
                </tr>
              ) : jobs.length === 0 ? (
                <tr>
                  <td colSpan={7} style={{ textAlign: 'center', padding: '3rem', color: 'var(--text-muted)' }}>
                    No executions recorded yet.
                  </td>
                </tr>
              ) : (
                jobs.map((job) => (
                  <tr key={job.id}>
                    <td>
                      <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.8rem', color: 'var(--accent-cyan)' }}>
                        {job.id.slice(0, 8)}...
                      </span>
                    </td>
                    <td>
                      <span style={{ textTransform: 'uppercase', fontWeight: 600, fontSize: '0.8rem' }}>
                        {job.language}
                      </span>
                    </td>
                    <td>
                      <span className={`badge badge-${job.status.toLowerCase()}`}>{job.status}</span>
                    </td>
                    <td style={{ fontSize: '0.85rem' }}>
                      {job.current_attempt} / {job.max_attempts}
                    </td>
                    <td>
                      <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.8rem', color: 'var(--text-secondary)' }}>
                        {job.assigned_worker ? job.assigned_worker.slice(0, 16) : '—'}
                      </span>
                    </td>
                    <td style={{ fontSize: '0.8rem', color: 'var(--text-secondary)' }}>
                      {new Date(job.created_at).toLocaleString()}
                    </td>
                    <td>
                      <Link
                        href={`/jobs/${job.id}`}
                        style={{
                          display: 'inline-flex',
                          alignItems: 'center',
                          gap: '0.25rem',
                          color: 'var(--accent-cyan)',
                          textDecoration: 'none',
                          fontSize: '0.8rem',
                          fontWeight: 600,
                        }}
                      >
                        <span>Details</span>
                        <ExternalLink size={13} />
                      </Link>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>

        {/* Pagination Bar */}
        <div style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          padding: '1rem 1.5rem',
          background: 'var(--bg-tertiary)',
          borderTop: '1px solid var(--border-subtle)',
          fontSize: '0.85rem',
          color: 'var(--text-secondary)',
        }}>
          <div>
            Showing Page <strong>{page}</strong> of <strong>{totalPages}</strong> ({total} total records)
          </div>

          <div style={{ display: 'flex', gap: '0.5rem' }}>
            <button
              onClick={() => setPage((p) => Math.max(1, p - 1))}
              disabled={page <= 1 || loading}
              className="btn-secondary"
              style={{ padding: '0.35rem 0.75rem', fontSize: '0.8rem' }}
            >
              <ChevronLeft size={14} />
              <span>Previous</span>
            </button>
            <button
              onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
              disabled={page >= totalPages || loading}
              className="btn-secondary"
              style={{ padding: '0.35rem 0.75rem', fontSize: '0.8rem' }}
            >
              <span>Next</span>
              <ChevronRight size={14} />
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
