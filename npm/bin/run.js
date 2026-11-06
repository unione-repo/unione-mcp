#!/usr/bin/env node
// Runs unione-mcp for npx: on first use it downloads the release binary of this
// package's version for this platform, checks it against the release's
// checksums.txt and caches it; then it runs it (stdio mode unless told otherwise).
// Everything it says goes to stderr: stdout belongs to the MCP session.
'use strict';

const { spawn } = require('node:child_process');
const crypto = require('node:crypto');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { version } = require('../package.json');

const server = 'unione-mcp';
const releases = 'https://github.com/unione-repo/unione-mcp/releases/download';

const goos = { linux: 'linux', darwin: 'darwin', win32: 'windows' }[process.platform];
const goarch = { x64: 'amd64', arm64: 'arm64' }[process.arch];

function fail(message) {
  process.stderr.write(`${server}: ${message}\n`);
  process.exit(1);
}

async function download(url) {
  const res = await fetch(url);
  if (!res.ok) throw new Error(`GET ${url}: HTTP ${res.status}`);
  return Buffer.from(await res.arrayBuffer());
}

// A code host whose download URLs cannot be built from the version (GitFlic
// addresses release files by id) gets them in assets.json, written at release time:
// {"<asset>": {"url": "...", "sha256": "..."}}. Without it the GitHub Releases
// layout and its checksums.txt apply.
function releaseAssets() {
  try {
    return require('../assets.json');
  } catch {
    return null;
  }
}

async function binary() {
  if (!goos || !goarch) {
    throw new Error(`no build for ${process.platform}/${process.arch}; use Docker or build from source`);
  }
  const asset = `${server}-${goos}-${goarch}${goos === 'windows' ? '.exe' : ''}`;
  const cacheRoot = process.env.XDG_CACHE_HOME || path.join(os.homedir(), '.cache');
  const file = path.join(cacheRoot, server, version, asset);
  if (fs.existsSync(file)) return file;

  process.stderr.write(`${server}: downloading ${asset} v${version}\n`);
  const assets = releaseAssets();
  let data;
  let want;
  if (assets) {
    const entry = assets[asset];
    if (!entry) throw new Error(`${asset}: not in release v${version}`);
    data = await download(entry.url);
    want = entry.sha256;
  } else {
    const base = `${releases}/v${version}`;
    const [bin, sums] = await Promise.all([download(`${base}/${asset}`), download(`${base}/checksums.txt`)]);
    const line = sums.toString('utf8').split('\n').map((l) => l.trim().split(/\s+/)).find((f) => f[1] === asset);
    data = bin;
    want = line && line[0];
  }
  const got = crypto.createHash('sha256').update(data).digest('hex');
  if (!want || want !== got) throw new Error(`${asset}: checksum does not match the one published for v${version}`);

  fs.mkdirSync(path.dirname(file), { recursive: true });
  const tmp = `${file}.${process.pid}.tmp`;
  fs.writeFileSync(tmp, data, { mode: 0o755 });
  fs.renameSync(tmp, file);
  return file;
}

binary()
  .then((file) => {
    const args = process.argv.slice(2);
    const child = spawn(file, args.length > 0 ? args : ['stdio'], { stdio: 'inherit' });
    for (const signal of ['SIGINT', 'SIGTERM', 'SIGHUP']) {
      process.on(signal, () => child.kill(signal));
    }
    child.on('error', (err) => fail(err.message));
    child.on('exit', (code, signal) => {
      if (signal) process.kill(process.pid, signal);
      else process.exit(code ?? 1);
    });
  })
  .catch((err) => fail(err.message));
