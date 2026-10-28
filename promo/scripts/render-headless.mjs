import {spawn} from 'node:child_process';
import {createRequire} from 'node:module';
import {fileURLToPath} from 'node:url';
import path from 'node:path';
const require = createRequire(import.meta.url);
const cli = path.join(path.dirname(require.resolve('@remotion/cli/package.json')), 'remotion-cli.js');
const fix = fileURLToPath(new URL('./chrome-raster-fix.cjs', import.meta.url));
const args = process.argv.slice(2);
const child = spawn(process.execPath, ['--require', fix, cli, ...args], {
  stdio: 'inherit',
  shell: false,
});
child.on('error', (error) => {console.error(error);process.exitCode = 1;});
child.on('exit', (code) => {process.exitCode = code ?? 1;});
for (const signal of ['SIGINT', 'SIGTERM']) {
  process.on(signal, () => child.kill(signal));
}
