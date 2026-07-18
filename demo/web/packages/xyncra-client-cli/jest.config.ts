import type { Config } from 'jest';

const config: Config = {
  displayName: 'xyncra-client-cli',
  testEnvironment: 'node',
  testMatch: ['<rootDir>/src/__tests__/**/*.test.ts'],
  transform: { '^.+\\.tsx?$': 'ts-jest' },
  moduleNameMapper: {
    '^@xyncra/protocol$': '<rootDir>/../xyncra-protocol/src',
    '^@xyncra/client-core$': '<rootDir>/../xyncra-client-core/src',
    '^(\\.{1,2}/.*)\\.js$': '$1',
  },
};

export default config;
