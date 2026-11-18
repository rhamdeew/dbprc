// npm audit with an explicit ignore list (npm has no native one).
// Fails on any high/critical advisory that is not listed below.
import { execFileSync } from 'node:child_process';

const IGNORED = {
  // picomatch <= 4.0.7: no patched release exists; reachable only via dev tooling
  // (vite, vitest, eslint, postcss-mixins). Remove once a fixed picomatch ships.
  'GHSA-fc5q-39xc-vjpf': 'picomatch ReDoS, no fix available',
};

let out;
try {
  out = execFileSync('npm', ['audit', '--json'], { encoding: 'utf8', maxBuffer: 64 << 20 });
} catch (e) {
  out = e.stdout; // npm audit exits non-zero when it finds issues
}
const report = JSON.parse(out);
if (report.error) {
  console.error('npm audit failed:', report.error.summary ?? report.error);
  process.exit(1);
}

const advisories = new Map();
for (const v of Object.values(report.vulnerabilities ?? {})) {
  for (const via of v.via) {
    if (typeof via === 'object') advisories.set(via.url, via);
  }
}

let failed = false;
for (const [url, a] of advisories) {
  const id = url.split('/').pop();
  const blocking = a.severity === 'high' || a.severity === 'critical';
  if (id in IGNORED) {
    console.log(`ignored  ${id} (${a.severity}) ${a.name}: ${IGNORED[id]}`);
  } else if (blocking) {
    console.error(`FAIL     ${id} (${a.severity}) ${a.name}: ${a.title}`);
    failed = true;
  }
}
console.log(failed ? 'audit failed' : 'audit ok');
process.exit(failed ? 1 : 0);
