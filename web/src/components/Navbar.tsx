'use client';

import React from 'react';
import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { useAuth } from '@/context/AuthContext';
import { useTheme } from '@/context/ThemeContext';
import { Terminal, Cpu, Play, List, LayoutDashboard, LogIn, LogOut, UserPlus, Sun, Moon } from 'lucide-react';

export default function Navbar() {
  const pathname = usePathname();
  const { user, logout } = useAuth();
  const { theme, toggleTheme } = useTheme();

  const isActive = (path: string) => pathname === path;

  return (
    <nav className="navbar">
      <Link href="/dashboard" className="brand">
        <Terminal size={22} style={{ color: 'var(--accent-blue)' }} />
        <span>ForgeQueue</span>
      </Link>

      <div className="nav-links">
        <Link href="/dashboard" className={`nav-link ${isActive('/dashboard') ? 'active' : ''}`}>
          <LayoutDashboard size={16} />
          <span>Dashboard</span>
        </Link>
        <Link href="/jobs/new" className={`nav-link ${isActive('/jobs/new') ? 'active' : ''}`}>
          <Play size={16} />
          <span>Submit Job</span>
        </Link>
        <Link href="/jobs" className={`nav-link ${isActive('/jobs') ? 'active' : ''}`}>
          <List size={16} />
          <span>History</span>
        </Link>
        <Link href="/workers" className={`nav-link ${isActive('/workers') ? 'active' : ''}`}>
          <Cpu size={16} />
          <span>Workers</span>
        </Link>
      </div>

      <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
        <button onClick={toggleTheme} className="theme-toggle" title={`Switch to ${theme === 'light' ? 'dark' : 'light'} mode`}>
          {theme === 'light' ? <Moon size={16} /> : <Sun size={16} />}
        </button>

        {user ? (
          <>
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: '0.85rem' }}>
              <span style={{ color: 'var(--text-muted)' }}>Signed in as</span>
              <span style={{ color: 'var(--accent-blue)', fontWeight: 600 }}>{user.username}</span>
            </div>
            <button
              onClick={logout}
              className="btn-secondary"
              style={{ padding: '0.4rem 0.8rem', fontSize: '0.8rem' }}
            >
              <LogOut size={14} />
              <span>Sign Out</span>
            </button>
          </>
        ) : (
          <div style={{ display: 'flex', gap: '0.6rem' }}>
            <Link href="/login" className="btn-secondary" style={{ padding: '0.4rem 0.85rem', fontSize: '0.85rem' }}>
              <LogIn size={14} />
              <span>Login</span>
            </Link>
            <Link href="/register" className="btn-primary" style={{ padding: '0.4rem 0.85rem', fontSize: '0.85rem' }}>
              <UserPlus size={14} />
              <span>Register</span>
            </Link>
          </div>
        )}
      </div>
    </nav>
  );
}
