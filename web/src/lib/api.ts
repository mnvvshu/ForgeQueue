const API_BASE = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080/api/v1';

export interface User {
  id: string;
  username: string;
  email: string;
  created_at: string;
}

export interface Limits {
  cpu_limit: string;
  memory_limit_mb: number;
  timeout_seconds: number;
  max_output_bytes: number;
  max_pids: number;
}

export type JobStatus = 'QUEUED' | 'RUNNING' | 'COMPLETED' | 'FAILED' | 'TIMEOUT' | 'CANCELLED';

export interface Job {
  id: string;
  user_id: string;
  language: string;
  source_code: string;
  stdin?: string;
  status: JobStatus;
  current_attempt: number;
  max_attempts: number;
  limits: Limits;
  stdout?: string;
  stderr?: string;
  exit_code?: number | null;
  output_truncated: boolean;
  failure_category?: string;
  failure_reason?: string;
  assigned_worker?: string;
  created_at: string;
  started_at?: string | null;
  finished_at?: string | null;
  updated_at: string;
}

export interface JobAttempt {
  id: string;
  job_id: string;
  attempt_number: number;
  worker_id: string;
  status: JobStatus;
  exit_code?: number | null;
  stdout?: string;
  stderr?: string;
  output_truncated: boolean;
  failure_category?: string;
  failure_reason?: string;
  started_at: string;
  finished_at?: string | null;
  created_at: string;
}

export interface WorkerInfo {
  id: string;
  hostname: string;
  concurrency: number;
  active_jobs: number;
  completed_jobs: number;
  failed_jobs: number;
  status: 'ONLINE' | 'OFFLINE' | 'BUSY';
  last_heartbeat: string;
  seconds_since_heartbeat: number;
  started_at: string;
  version: string;
}

export interface RuntimeDef {
  language: string;
  display_name: string;
  source_file_name: string;
  extension: string;
  image: string;
  monaco_language: string;
  starter_code: string;
  default_timeout: number;
}

export interface ExecutionEvent {
  type: 'status' | 'stdout' | 'stderr' | 'terminal' | 'error';
  job_id: string;
  data?: string;
  status?: JobStatus;
  exit_code?: number;
  truncated?: boolean;
  timestamp: string;
}

export function getStoredToken(): string | null {
  if (typeof window === 'undefined') return null;
  return localStorage.getItem('forgequeue_token');
}

export function setStoredToken(token: string) {
  if (typeof window !== 'undefined') {
    localStorage.setItem('forgequeue_token', token);
  }
}

export function clearStoredToken() {
  if (typeof window !== 'undefined') {
    localStorage.removeItem('forgequeue_token');
  }
}

async function request<T>(endpoint: string, options: RequestInit = {}): Promise<T> {
  const token = getStoredToken();
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(options.headers as Record<string, string>),
  };

  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }

  const url = `${API_BASE}${endpoint}`;
  const res = await fetch(url, { ...options, headers });

  if (!res.ok) {
    let errMessage = `HTTP ${res.status}`;
    try {
      const errBody = await res.json();
      if (errBody?.error?.message) {
        errMessage = errBody.error.message;
      }
    } catch (_) {}
    throw new Error(errMessage);
  }

  return res.json();
}

export const api = {
  // Auth
  async register(username: string, email: string, password: string): Promise<{ user: User; token: string }> {
    const data = await request<{ user: User; token: string }>('/auth/register', {
      method: 'POST',
      body: JSON.stringify({ username, email, password }),
    });
    setStoredToken(data.token);
    return data;
  },

  async login(username: string, password: string): Promise<{ user: User; token: string }> {
    const data = await request<{ user: User; token: string }>('/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    });
    setStoredToken(data.token);
    return data;
  },

  async getMe(): Promise<User> {
    return request<User>('/auth/me');
  },

  logout() {
    clearStoredToken();
  },

  // Runtimes & Workers
  async getRuntimes(): Promise<RuntimeDef[]> {
    return request<RuntimeDef[]>('/runtimes');
  },

  async getWorkers(): Promise<WorkerInfo[]> {
    return request<WorkerInfo[]>('/workers');
  },

  // Jobs
  async createJob(params: {
    language: string;
    source_code: string;
    stdin?: string;
    limits?: Partial<Limits>;
    idempotencyKey?: string;
  }): Promise<Job> {
    const headers: Record<string, string> = {};
    if (params.idempotencyKey) {
      headers['Idempotency-Key'] = params.idempotencyKey;
    }
    return request<Job>('/jobs', {
      method: 'POST',
      headers,
      body: JSON.stringify({
        language: params.language,
        source_code: params.source_code,
        stdin: params.stdin,
        limits: params.limits,
      }),
    });
  },

  async listJobs(limit = 20, offset = 0): Promise<{ items: Job[]; total: number }> {
    return request<{ items: Job[]; total: number }>(`/jobs?limit=${limit}&offset=${offset}`);
  },

  async getJob(id: string): Promise<Job> {
    return request<Job>(`/jobs/${id}`);
  },

  async getJobAttempts(id: string): Promise<JobAttempt[]> {
    return request<JobAttempt[]>(`/jobs/${id}/attempts`);
  },

  async cancelJob(id: string): Promise<Job> {
    return request<Job>(`/jobs/${id}/cancel`, { method: 'POST' });
  },

  // SSE Subscription
  subscribeJobEvents(jobID: string, onEvent: (ev: ExecutionEvent) => void, onError?: (err: any) => void): () => void {
    const token = getStoredToken();
    const url = `${API_BASE}/jobs/${jobID}/events?token=${token || ''}`;
    const es = new EventSource(url);

    es.onmessage = (e) => {
      try {
        const ev = JSON.parse(e.data) as ExecutionEvent;
        onEvent(ev);
        if (ev.type === 'terminal') {
          es.close();
        }
      } catch (err) {
        console.error('Error parsing SSE event:', err);
      }
    };

    es.onerror = (err) => {
      if (onError) onError(err);
      es.close();
    };

    return () => es.close();
  },
};
