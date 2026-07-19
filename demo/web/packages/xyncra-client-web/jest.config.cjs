/** @type {import('jest').Config} */
module.exports = {
  testEnvironment: 'jsdom',
  rootDir: '.',
  roots: ['<rootDir>/src'],
  setupFiles: ['<rootDir>/jest.setup.cjs'],
  transform: {
    '^.+\\.tsx?$': [
      'ts-jest',
      {
        tsconfig: {
          jsx: 'react-jsx',
          module: 'commonjs',
          esModuleInterop: true,
          allowJs: true,
          types: ['jest'],
          lib: ['ES2022', 'DOM', 'DOM.Iterable'],
        },
      },
    ],
  },
  moduleFileExtensions: ['ts', 'tsx', 'js', 'jsx'],
  testMatch: ['**/__tests__/**/*.test.ts', '**/__tests__/**/*.test.tsx'],
  moduleNameMapper: {
    '^@xyncra/protocol$': '<rootDir>/../xyncra-protocol/src',
    '^@xyncra/protocol/(.*)$': '<rootDir>/../xyncra-protocol/src/$1',
    '^@xyncra/client-core$': '<rootDir>/../xyncra-client-core/src',
    '^@xyncra/client-core/(.*)$': '<rootDir>/../xyncra-client-core/src/$1',
  },
  collectCoverageFrom: [
    'src/**/*.{ts,tsx}',
    '!src/**/*.d.ts',
    '!src/**/*.{js,jsx}',
    '!src/index.ts',
    '!src/**/index.ts',
    '!src/**/index.tsx',
    '!src/**/__tests__/**',
  ],
  coverageThreshold: {
    global: {
      lines: 75,
      branches: 65,
      functions: 80,
    },
  },
};
