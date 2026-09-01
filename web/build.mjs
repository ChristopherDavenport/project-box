// Build: bundle the app, carry the JH themes alongside. Run from web/:
//   npm run build
// The output in dist/ is committed and embedded into boxd (go:embed), so the
// daemon serves its own control room and CI needs no node toolchain.
import { build } from 'esbuild';
import { cpSync } from 'node:fs';

await build({
  entryPoints: ['src/app.js'],
  bundle: true,
  format: 'esm',
  minify: true,
  sourcemap: false,
  outfile: 'dist/app.js',
  logLevel: 'info',
});
cpSync('index.html', 'dist/index.html');
cpSync('node_modules/@jack-henry/jh-core/platforms/web/css/jh-theme-light.css', 'dist/jh-theme-light.css');
cpSync('node_modules/@jack-henry/jh-core/platforms/web/css/jh-theme-dark.css', 'dist/jh-theme-dark.css');
