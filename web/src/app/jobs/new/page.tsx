'use client';

import React, { useState, useEffect } from 'react';
import dynamic from 'next/dynamic';
import { useRouter } from 'next/navigation';
import { useAuth } from '@/context/AuthContext';
import { api, RuntimeDef } from '@/lib/api';
import { Play, Settings2, Terminal, Code2, AlertCircle } from 'lucide-react';

// Dynamically load Monaco Editor to avoid SSR hydration issues
const MonacoEditor = dynamic(() => import('@monaco-editor/react'), { ssr: false });

const DEFAULT_TEMPLATES: Record<string, string> = {
  python: 'print("Hello ForgeQueue")\n',
  javascript: 'console.log("Hello ForgeQueue");\n',
  go: 'package main\n\nimport "fmt"\n\nfunc main() {\n\tfmt.Println("Hello ForgeQueue")\n}\n',
  cpp: '#include <iostream>\n\nint main() {\n    std::cout << "Hello ForgeQueue" << std::endl;\n    return 0;\n}\n',
  java: 'public class Main {\n    public static void main(String[] args) {\n        System.out.println("Hello ForgeQueue");\n    }\n}\n',
};

export default function NewJobPage() {
  const router = useRouter();
  const { user, loading: authLoading } = useAuth();

  const [runtimes, setRuntimes] = useState<RuntimeDef[]>([]);
  const [language, setLanguage] = useState('python');
  const [sourceCode, setSourceCode] = useState(DEFAULT_TEMPLATES['python']);
  const [stdin, setStdin] = useState('');
  const [showStdin, setShowStdin] = useState(false);
  const [timeoutSeconds, setTimeoutSeconds] = useState(10);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!authLoading && !user) {
      router.push('/login');
      return;
    }

    api.getRuntimes()
      .then((data) => setRuntimes(data))
      .catch((err) => console.error('Failed to load runtimes:', err));
  }, [user, authLoading, router]);

  const handleLanguageChange = (newLang: string) => {
    setLanguage(newLang);
    // Replace with starter template if code was default template
    if (DEFAULT_TEMPLATES[newLang]) {
      setSourceCode(DEFAULT_TEMPLATES[newLang]);
    }
  };

  const handleRun = async () => {
    setError('');
    setSubmitting(true);

    try {
      const job = await api.createJob({
        language,
        source_code: sourceCode,
        stdin: stdin || undefined,
        limits: {
          timeout_seconds: timeoutSeconds,
        },
      });

      router.push(`/jobs/${job.id}`);
    } catch (err: any) {
      setError(err.message || 'Execution submission failed');
      setSubmitting(false);
    }
  };

  const currentRuntime = runtimes.find((r) => r.language === language);

  return (
    <div className="container" style={{ maxWidth: '1400px' }}>
      {/* Top Action Bar */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.25rem' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '1rem' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <Code2 size={20} style={{ color: 'var(--accent-cyan)' }} />
            <h1 style={{ fontSize: '1.25rem', fontWeight: 700 }}>Code Execution Workspace</h1>
          </div>

          {/* Language Selector */}
          <select
            value={language}
            onChange={(e) => handleLanguageChange(e.target.value)}
            className="input-field"
            style={{ width: 'auto', padding: '0.45rem 1rem', fontWeight: 600 }}
          >
            <option value="python">Python 3.12</option>
            <option value="javascript">JavaScript (Node 20)</option>
            <option value="go">Go 1.22</option>
            <option value="cpp">C++ 17 (GCC 13)</option>
            <option value="java">Java 21 (Temurin)</option>
          </select>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: '1rem' }}>
          {/* Stdin Toggle */}
          <button
            type="button"
            className="btn-secondary"
            onClick={() => setShowStdin(!showStdin)}
            style={{ padding: '0.5rem 0.9rem', fontSize: '0.85rem' }}
          >
            <Terminal size={15} />
            <span>{showStdin ? 'Hide Stdin' : 'Add Stdin'}</span>
          </button>

          {/* Timeout Selector */}
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: '0.85rem', color: 'var(--text-secondary)' }}>
            <Settings2 size={16} />
            <span>Timeout:</span>
            <select
              value={timeoutSeconds}
              onChange={(e) => setTimeoutSeconds(Number(e.target.value))}
              className="input-field"
              style={{ width: 'auto', padding: '0.35rem 0.6rem', fontSize: '0.85rem' }}
            >
              <option value={5}>5s</option>
              <option value={10}>10s (default)</option>
              <option value={15}>15s</option>
              <option value={30}>30s</option>
              <option value={60}>60s</option>
            </select>
          </div>

          {/* Run Button */}
          <button
            type="button"
            className="btn-primary"
            onClick={handleRun}
            disabled={submitting}
            style={{ padding: '0.55rem 1.5rem', fontWeight: 700 }}
          >
            <Play size={16} fill="currentColor" />
            <span>{submitting ? 'Dispatching...' : 'Run Code'}</span>
          </button>
        </div>
      </div>

      {error && (
        <div style={{
          display: 'flex',
          alignItems: 'center',
          gap: '0.5rem',
          padding: '0.75rem 1rem',
          background: '#fee2e2',
          border: '1px solid #fecaca',
          borderRadius: 'var(--radius-sm)',
          color: '#991b1b',
          fontSize: '0.85rem',
          marginBottom: '1rem',
        }}>
          <AlertCircle size={16} />
          <span>{error}</span>
        </div>
      )}

      {/* Editor Main Canvas */}
      <div className="glass-panel" style={{ overflow: 'hidden', border: '1px solid var(--border-subtle)' }}>
        <div style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          padding: '0.5rem 1rem',
          background: '#252526',
          borderBottom: '1px solid var(--border-subtle)',
          fontSize: '0.8rem',
          color: 'var(--text-muted)',
          fontFamily: 'var(--font-mono)',
        }}>
          <span>{currentRuntime?.source_file_name || `${language}.src`}</span>
          <span>Docker Sandbox • Isolated Execution</span>
        </div>

        <div style={{ height: showStdin ? '460px' : '580px', transition: 'height 0.2s ease' }}>
          <MonacoEditor
            height="100%"
            language={currentRuntime?.monaco_language || language}
            theme="vs-dark"
            value={sourceCode}
            onChange={(val) => setSourceCode(val || '')}
            options={{
              fontSize: 14,
              fontFamily: "'Fira Code', monospace",
              minimap: { enabled: false },
              scrollBeyondLastLine: false,
              automaticLayout: true,
              tabSize: 4,
              lineNumbers: 'on',
              renderLineHighlight: 'all',
              padding: { top: 12 },
            }}
          />
        </div>

        {/* Stdin Panel if toggled */}
        {showStdin && (
          <div style={{ borderTop: '1px solid var(--border-subtle)', background: '#1e1e1e', padding: '0.75rem 1rem' }}>
            <div style={{ fontSize: '0.8rem', color: 'var(--text-secondary)', marginBottom: '0.4rem', fontWeight: 600 }}>
              Standard Input (stdin)
            </div>
            <textarea
              className="input-field"
              value={stdin}
              onChange={(e) => setStdin(e.target.value)}
              placeholder="Paste or type stdin content here..."
              rows={3}
              style={{ fontFamily: 'var(--font-mono)', fontSize: '0.85rem' }}
            />
          </div>
        )}
      </div>
    </div>
  );
}
