#!/usr/bin/env node
// CLI entry point — delegates to the root command.
import { createRootCommand } from '../src/commands/root.js';

const program = createRootCommand();
program.parseAsync(process.argv).catch((err: Error) => {
  console.error(`Error: ${err.message}`);
  process.exit(1);
});
