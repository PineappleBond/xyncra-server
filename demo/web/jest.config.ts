import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { configUmiAlias, createConfig } from '@umijs/max/test.js';

const readPkgVersion = (pkg: string) =>
  JSON.parse(readFileSync(join('node_modules', pkg, 'package.json'), 'utf-8'))
    .version;

export default async (): Promise<any> => {
  const config = await configUmiAlias({
    ...createConfig({
      target: 'browser',
    }),
  });

  // Base config for the main web app tests (jsdom environment)
  const baseConfig = {
    ...config,
    testPathIgnorePatterns: ['/node_modules/', '/.worktrees/', '/packages/xyncra-client-core/', '/packages/xyncra-client-cli/', '/dist/'],
    moduleNameMapper: {
      '\\.md$': '<rootDir>/tests/__mocks__/raw.js',
      ...(config.moduleNameMapper || {}),
      '^mermaid$': '<rootDir>/tests/__mocks__/mermaid.js',
    },
    testEnvironmentOptions: {
      ...(config?.testEnvironmentOptions || {}),
      url: 'http://localhost:8000',
    },
    // Use babel-jest instead of the umi esbuild transformer: the esbuild path
    // crashes on same-module mixed type+value imports
    // (`import type { XyncraContextValue }` + `import { XyncraContext }`) with
    // "Cannot transform the imported binding". babel-jest with
    // @babel/preset-typescript strips type-only bindings correctly.
    transform: {
      '^.+\\.(ts|tsx|js|jsx)$': ['babel-jest', {
        presets: [
          ['@babel/preset-env', { targets: { node: 'current' } }],
          ['@babel/preset-react', { runtime: 'automatic' }],
          ['@babel/preset-typescript', { isTSX: true, allExtensions: true, onlyRemoveTypeImports: false }],
        ],
      }],
    },
    transformIgnorePatterns: [
      'node_modules/(?!(antd|@ant-design|rc-[^/]+|@rc-component|lodash-es|@babel/runtime)/)',
    ],
    setupFiles: [...(config.setupFiles || []), './tests/setupTests.jsx'],
    globals: {
      ...config.globals,
      localStorage: null,
      __APP_VERSION__: 'test',
      __UMI_VERSION__: readPkgVersion('@umijs/max'),
      __UTOO_VERSION__: readPkgVersion('@utoo/pack'),
    },
  };

  // Config for @xyncra/client-core tests (node environment, no jsdom)
  const coreConfig = {
    ...config,
    displayName: 'xyncra-client-core',
    testMatch: ['<rootDir>/packages/xyncra-client-core/src/__tests__/**/*.test.ts'],
    testPathIgnorePatterns: ['/node_modules/', '/.worktrees/'],
    testEnvironment: 'node',
    moduleNameMapper: {
      ...(config.moduleNameMapper || {}),
      '^@xyncra/protocol$': '<rootDir>/packages/xyncra-protocol/src',
    },
    setupFiles: [
      '<rootDir>/packages/xyncra-client-core/src/__tests__/setup.ts',
    ],
    transform: {
      '^.+\\.tsx?$': 'ts-jest',
    },
  };

  // Config for @xyncra/client-cli tests (node environment, no jsdom)
  const cliConfig = {
    ...config,
    displayName: 'xyncra-client-cli',
    testMatch: ['<rootDir>/packages/xyncra-client-cli/src/__tests__/**/*.test.ts'],
    testPathIgnorePatterns: ['/node_modules/', '/.worktrees/'],
    testEnvironment: 'node',
    moduleNameMapper: {
      ...(config.moduleNameMapper || {}),
      '^@xyncra/protocol$': '<rootDir>/packages/xyncra-protocol/src',
      '^@xyncra/client-core$': '<rootDir>/packages/xyncra-client-core/src',
      '^(\\.{1,2}/.*)\\.js$': '$1',
    },
    transform: {
      '^.+\\.tsx?$': 'ts-jest',
    },
  };

  return {
    projects: [baseConfig, coreConfig, cliConfig],
  };
};
