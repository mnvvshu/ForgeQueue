import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  scenarios: {
    constant_request_rate: {
      executor: 'constant-arrival-rate',
      rate: 10, // 10 jobs per second
      timeUnit: '1s',
      duration: '10s', // 100 total jobs
      preAllocatedVUs: 10,
      maxVUs: 50,
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'], // <1% errors
    http_req_duration: ['p(95)<500'], // 95% of requests < 500ms
  },
};

const BASE_URL = __ENV.API_URL || 'http://localhost:8080/api/v1';

export function setup() {
  const uniqueId = Date.now();
  const registerPayload = JSON.stringify({
    username: `loaduser_${uniqueId}`,
    email: `load_${uniqueId}@example.com`,
    password: 'Password123!',
  });

  const res = http.post(`${BASE_URL}/auth/register`, registerPayload, {
    headers: { 'Content-Type': 'application/json' },
  });

  check(res, {
    'user registered': (r) => r.status === 201,
  });

  return { token: res.json('token') };
}

export default function (data) {
  const headers = {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${data.token}`,
  };

  const jobPayload = JSON.stringify({
    language: 'python',
    source_code: `print("Load test execution #${__ITER}")`,
  });

  const res = http.post(`${BASE_URL}/jobs`, jobPayload, { headers });

  check(res, {
    'job queued (201)': (r) => r.status === 201,
    'job has id': (r) => r.json('id') !== undefined,
  });

  sleep(0.1);
}
