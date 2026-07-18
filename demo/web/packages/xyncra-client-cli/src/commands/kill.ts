/**
 * `kill` command — terminate the running listen daemon.
 *
 * Mirrors Go newKillCommand in internal/cli/kill.go.
 * Reads lock file, checks if process alive, sends signal, cleans up files.
 * @module
 */

import type { Command } from 'commander';
import { CLIContext } from '../cli-context.js';
import { readLockInfo, isProcessAlive, cleanupDaemonFiles } from '../lock.js';

/**
 * Register the `kill` command on the given Commander program.
 *
 * Usage: xyncra-client kill [--force] [--timeout <duration>]
 */
export function registerKillCommand(program: Command): void {
  program
    .command('kill')
    .description('Terminate the running listen daemon')
    .option('--force', 'Force kill with SIGKILL instead of SIGTERM', false)
    .option('--timeout <ms>', 'Timeout in milliseconds to wait for process to exit', '5000')
    .action(async (options: { force: boolean; timeout: string }, cmd: Command) => {
      const cliCtx = CLIContext.fromCommand(cmd);
      const force = options.force;
      const timeout = parseInt(options.timeout, 10);

      // 1. Read lock file.
      const info = readLockInfo(cliCtx.getLockPath());
      if (!info) {
        // Lock file does not exist or cannot be parsed — no daemon running.
        console.error('No running daemon found.');
        return; // D-039: exit 0 when daemon is already stopped.
      }

      // 2. Check if process is alive.
      if (!isProcessAlive(info.pid)) {
        console.error(
          `Daemon process (PID: ${info.pid}) is not running. Cleaning up stale files.`,
        );
        cleanupDaemonFiles(cliCtx.userDir);
        return;
      }

      // 3. Determine signal.
      const signal: NodeJS.Signals = force ? 'SIGKILL' : 'SIGTERM';

      // 4. Send signal and wait for exit.
      const exited = await terminateProcess(info.pid, signal, timeout);
      if (!exited) {
        if (!force) {
          console.error(
            `Error: process did not respond to SIGTERM within ${timeout}ms. Use --force to force kill`,
          );
          process.exit(3);
        }
        // SIGKILL timeout — unusual but non-fatal.
        console.error(`Warning: process ${info.pid} did not exit after SIGKILL`);
      }

      // 5. Cleanup files.
      cleanupDaemonFiles(cliCtx.userDir);
      console.error(`Daemon terminated (PID: ${info.pid}). Files cleaned up.`);
    });
}

/**
 * Send a signal to a process and poll until it exits or timeout is reached.
 * Returns true if the process exited, false on timeout.
 */
async function terminateProcess(
  pid: number,
  signal: NodeJS.Signals,
  timeoutMs: number,
): Promise<boolean> {
  try {
    process.kill(pid, signal);
  } catch (err) {
    throw new Error(`signal process ${pid}: ${(err as Error).message}`);
  }

  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (!isProcessAlive(pid)) {
      return true;
    }
    await sleep(200);
  }

  return false;
}

/** Sleep for the given number of milliseconds. */
function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
