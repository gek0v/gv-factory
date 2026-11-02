// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { defineConfig, type ProxyOptions } from 'vite';
import react from '@vitejs/plugin-react';

declare const process: {
  env?: Record<string, string | undefined>;
};

export const DEFAULT_BACKEND_URL = 'http://localhost:8080';

export function getProxyConfig(backendUrl?: string): Record<string, ProxyOptions> {
  const envTarget = typeof process !== 'undefined' ? process.env?.VITE_BACKEND_URL : undefined;
  const target = (backendUrl && backendUrl.trim()) || (envTarget && envTarget.trim()) || DEFAULT_BACKEND_URL;

  return {
    '/events': {
      target,
      changeOrigin: true,
    },
    '/api': {
      target,
      changeOrigin: true,
    },
  };
}

export default defineConfig({
  plugins: [react()],
  server: {
    port: 3000,
    host: true,
    proxy: getProxyConfig(),
  },
});
