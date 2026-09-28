'use client';

import React, { useEffect, useState } from 'react';
import { api, WorkerInfo } from '@/lib/api';
import { Cpu, RefreshCw, Activity, CheckCircle, XCircle } from 'lucide-react';

export default function WorkersPage() {
  const [workers, setWorkers] = useState<WorkerInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);

  const fetchWorkers = async () => {
    try {
      const data = await api.getWorkers();
      setWorkers(data || []);
    } catch (err) {
      console.error('Failed to fetch worker list:', err);
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  };

  useEffect(() => {
    fetchWorkers();
    const timer = setInterval(fetchWorkers, 3000);
    return () => clearInterval(timer);
  }, []);

  const totalConcurrency = workers.reduce((acc, w) => acc + w.concurrency, 0);
  const totalActive = workers.reduce((acc, w) => acc + w.active_jobs, 0);
  const onlineCount = workers.filter((w) => w.status === 'ONLINE').length;

  return (
    <div className="container">
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.5rem' }}>
        <div>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <Cpu size={22} style={{ color: 'var(--accent-cyan)' }} />
            <h1 style={{ fontSize: '1.5rem', fontWeight: 800 }}>Execution Worker Nodes</h1>
          </div>
          <p style={{ color: 'var(--text-secondary)', fontSize: '0.85rem' }}>
            Active nodes pulling execution tasks from Redis Streams
          </p>
        </div>

        <button
          onClick={() => { setRefreshing(true); fetchWorkers(); }}
          className="btn-secondary"
          style={{ padding: '0.45rem 0.9rem', fontSize: '0.85rem' }}
        >
          <RefreshCw size={14} className={refreshing ? 'spin' : ''} />
          <span>Refresh</span>
        </button>
      </div>

      {/* Cluster Capacity Summary */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: '1rem', marginBottom: '1.5rem' }}>
        <div className="glass-panel" style={{ padding: '1rem 1.25rem' }}>
          <div style={{ fontSize: '0.8rem', color: 'var(--text-secondary)' }}>Online Nodes</div>
          <div style={{ fontSize: '1.75rem', fontWeight: 700, color: 'var(--accent-cyan)' }}>
            {onlineCount} <span style={{ fontSize: '0.9rem', color: 'var(--text-muted)' }}>/ {workers.length}</span>
          </div>
        </div>

        <div className="glass-panel" style={{ padding: '1rem 1.25rem' }}>
          <div style={{ fontSize: '0.8rem', color: 'var(--text-secondary)' }}>Total Concurrency Slots</div>
          <div style={{ fontSize: '1.75rem', fontWeight: 700, color: 'var(--text-primary)' }}>
            {totalConcurrency}
          </div>
        </div>

        <div className="glass-panel" style={{ padding: '1rem 1.25rem' }}>
          <div style={{ fontSize: '0.8rem', color: 'var(--text-secondary)' }}>Active Slot Utilization</div>
          <div style={{ fontSize: '1.75rem', fontWeight: 700, color: totalActive > 0 ? '#fbbf24' : 'var(--text-muted)' }}>
            {totalActive} <span style={{ fontSize: '0.9rem', color: 'var(--text-muted)' }}>slots occupied</span>
          </div>
        </div>
      </div>

      {/* Workers Table */}
      <div className="glass-panel" style={{ overflow: 'hidden' }}>
        <div className="table-container">
          <table className="data-table">
            <thead>
              <tr>
                <th>Node ID</th>
                <th>Hostname</th>
                <th>Status</th>
                <th>Slots In Use</th>
                <th>Completed</th>
                <th>Failed</th>
                <th>Heartbeat Recency</th>
                <th>Version</th>
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <tr>
                  <td colSpan={8} style={{ textAlign: 'center', padding: '3rem', color: 'var(--text-muted)' }}>
                    Scanning worker cluster...
                  </td>
                </tr>
              ) : workers.length === 0 ? (
                <tr>
                  <td colSpan={8} style={{ textAlign: 'center', padding: '3rem', color: 'var(--text-muted)' }}>
                    No worker nodes registered. Start worker instances to accept tasks.
                  </td>
                </tr>
              ) : (
                workers.map((w) => {
                  const isOnline = w.status === 'ONLINE';
                  return (
                    <tr key={w.id}>
                      <td>
                        <span style={{ fontFamily: 'var(--font-mono)', fontWeight: 600, color: 'var(--accent-cyan)', fontSize: '0.85rem' }}>
                          {w.id}
                        </span>
                      </td>
                      <td style={{ fontSize: '0.85rem', color: 'var(--text-secondary)' }}>
                        {w.hostname}
                      </td>
                      <td>
                        <span className={`badge badge-${w.status.toLowerCase()}`}>
                          {w.status}
                        </span>
                      </td>
                      <td style={{ fontSize: '0.85rem' }}>
                        <span style={{ fontWeight: 600, color: w.active_jobs > 0 ? '#38bdf8' : 'var(--text-primary)' }}>
                          {w.active_jobs}
                        </span>
                        <span style={{ color: 'var(--text-muted)' }}> / {w.concurrency}</span>
                      </td>
                      <td style={{ fontSize: '0.85rem', color: 'var(--accent-green)' }}>
                        {w.completed_jobs}
                      </td>
                      <td style={{ fontSize: '0.85rem', color: w.failed_jobs > 0 ? '#fb7185' : 'var(--text-muted)' }}>
                        {w.failed_jobs}
                      </td>
                      <td style={{ fontSize: '0.85rem' }}>
                        <span style={{ color: w.seconds_since_heartbeat < 10 ? 'var(--text-primary)' : '#fbbf24' }}>
                          {Math.round(w.seconds_since_heartbeat)}s ago
                        </span>
                      </td>
                      <td style={{ fontSize: '0.8rem', fontFamily: 'var(--font-mono)', color: 'var(--text-muted)' }}>
                        v{w.version}
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}
