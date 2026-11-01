// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { describe, it, expect } from 'vitest';
import packageJson from '../package.json';

describe('Web Package Configuration', () => {
  it('should declare pnpm.onlyBuiltDependencies with esbuild for container supply chain security', () => {
    expect(packageJson).toHaveProperty('pnpm');
    const pnpmConfig = (packageJson as { pnpm?: { onlyBuiltDependencies?: string[] } }).pnpm;
    expect(pnpmConfig).toBeDefined();
    expect(pnpmConfig?.onlyBuiltDependencies).toBeInstanceOf(Array);
    expect(pnpmConfig?.onlyBuiltDependencies).toContain('esbuild');
  });
});
