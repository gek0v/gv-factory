// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import viteConfig, { getProxyConfig, DEFAULT_BACKEND_URL } from '../vite.config';

declare const process: {
  env: Record<string, string | undefined>;
};

describe('Vite Dev Proxy Configuration (Issue #104)', () => {
  const originalEnv = process.env.VITE_BACKEND_URL;

  beforeEach(() => {
    delete process.env.VITE_BACKEND_URL;
  });

  afterEach(() => {
    if (originalEnv !== undefined) {
      process.env.VITE_BACKEND_URL = originalEnv;
    } else {
      delete process.env.VITE_BACKEND_URL;
    }
  });

  it('uses default backend URL http://localhost:8080 when VITE_BACKEND_URL is not set', () => {
    const proxy = getProxyConfig();
    expect(proxy['/events']).toBeDefined();
    expect(proxy['/events'].target).toBe(DEFAULT_BACKEND_URL);
    expect(proxy['/events'].changeOrigin).toBe(true);

    expect(proxy['/api']).toBeDefined();
    expect(proxy['/api'].target).toBe(DEFAULT_BACKEND_URL);
    expect(proxy['/api'].changeOrigin).toBe(true);
  });

  it('respects VITE_BACKEND_URL environment variable when present', () => {
    process.env.VITE_BACKEND_URL = 'http://backend-service:8080';
    const proxy = getProxyConfig();

    expect(proxy['/events'].target).toBe('http://backend-service:8080');
    expect(proxy['/events'].changeOrigin).toBe(true);

    expect(proxy['/api'].target).toBe('http://backend-service:8080');
    expect(proxy['/api'].changeOrigin).toBe(true);
  });

  it('prioritizes explicit backendUrl parameter over environment variable', () => {
    process.env.VITE_BACKEND_URL = 'http://env-host:8080';
    const proxy = getProxyConfig('http://explicit-host:9090');

    expect(proxy['/events'].target).toBe('http://explicit-host:9090');
    expect(proxy['/api'].target).toBe('http://explicit-host:9090');
  });

  it('falls back to default when VITE_BACKEND_URL is empty or whitespace', () => {
    process.env.VITE_BACKEND_URL = '   ';
    const proxy = getProxyConfig();

    expect(proxy['/events'].target).toBe(DEFAULT_BACKEND_URL);
    expect(proxy['/api'].target).toBe(DEFAULT_BACKEND_URL);
  });

  it('contains exactly the required proxy endpoints with changeOrigin true', () => {
    const proxy = getProxyConfig();
    const keys = Object.keys(proxy);

    expect(keys).toEqual(['/events', '/api']);
    for (const key of keys) {
      expect(proxy[key].changeOrigin).toBe(true);
    }
  });

  it('validates default exported vite configuration includes resolved proxy rules', () => {
    expect(viteConfig.server).toBeDefined();
    expect(viteConfig.server?.proxy).toBeDefined();

    const proxy = viteConfig.server?.proxy as Record<string, { target: string; changeOrigin: boolean }>;
    expect(proxy['/events']).toBeDefined();
    expect(proxy['/api']).toBeDefined();
    expect(proxy['/events'].changeOrigin).toBe(true);
    expect(proxy['/api'].changeOrigin).toBe(true);
  });
});
