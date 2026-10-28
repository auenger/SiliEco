// Applied only to Chromium started by this pinned Remotion renderer.
// Workaround: https://github.com/remotion-dev/remotion/issues/11428
const childProcess = require('node:child_process');
const originalSpawn = childProcess.spawn;
childProcess.spawn = function (file, args, options) {
  const isRemotionBrowser = Array.isArray(args) && args.includes('--remotion-shared-memory-capture');
  if (isRemotionBrowser && !args.includes('--disable-gpu-rasterization')) {
    args = ['--disable-gpu-rasterization', ...args];
    console.error('[Silieco] Chrome Headless Shell: GPU rasterization disabled.');
  }
  return originalSpawn.call(this, file, args, options);
};
require('node:module').syncBuiltinESMExports();
