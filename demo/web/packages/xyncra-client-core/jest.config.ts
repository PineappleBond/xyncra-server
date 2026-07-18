import type { Config } from 'jest';

const config: Config = {
  preset: 'ts-jest',
  testEnvironment: 'node',
  setupFiles: ['<rootDir>/src/__tests__/setup.ts'],
  moduleNameMapper: {
    '^@xyncra/protocol$': '<rootDir>/../xyncra-protocol/src',
  },
  testMatch: ['**/__tests__/**/*.test.ts'],
};

export default config;
