// Finding an existing cmnet folder, so nobody has to type a path.
//
// A previous cmnet (or advisor.exe) install keeps cmnet.db, bundle.json and the
// image cache in whatever folder it was run from. This scans the places people
// actually put such folders — Desktop, Downloads, Documents, the home folder,
// and the top two levels of every drive — and reports each folder holding a
// cmnet.db, newest first. Bounded in depth and time so it never stalls startup.

const fs = require('node:fs/promises');
const path = require('node:path');
const os = require('node:os');

const SKIP = new Set([
  'windows', 'program files', 'program files (x86)', 'programdata', '$recycle.bin',
  'system volume information', 'appdata', 'node_modules', '.git', 'steamapps',
  'recovery', 'perflogs', 'msocache', '$windows.~bt', '$windows.~ws',
]);

async function exists(p) {
  try { await fs.access(p); return true; } catch { return false; }
}

async function describe(dir) {
  const db = path.join(dir, 'cmnet.db');
  try {
    const st = await fs.stat(db);
    return {
      dir,
      dbBytes: st.size,
      modified: st.mtimeMs,
      hasBundle: await exists(path.join(dir, 'bundle.json')),
    };
  } catch {
    return null;
  }
}

async function walk(root, depth, found, deadline) {
  if (Date.now() > deadline || depth < 0) return;
  let entries;
  try {
    entries = await fs.readdir(root, { withFileTypes: true });
  } catch {
    return;
  }
  if (entries.some((e) => e.isFile() && e.name.toLowerCase() === 'cmnet.db')) {
    const d = await describe(root);
    if (d) found.set(path.resolve(root).toLowerCase(), d);
  }
  for (const e of entries) {
    if (!e.isDirectory() || e.name.startsWith('.') || SKIP.has(e.name.toLowerCase())) continue;
    await walk(path.join(root, e.name), depth - 1, found, deadline);
    if (Date.now() > deadline) return;
  }
}

async function scan({ extra = [], budgetMs = 4000 } = {}) {
  const deadline = Date.now() + budgetMs;
  const home = os.homedir();
  const found = new Map();
  const roots = [
    ...extra.map((d) => [d, 0]),
    ...['Desktop', 'Downloads', 'Documents', 'Games', 'OneDrive\\Desktop', 'OneDrive\\Documents']
      .map((d) => [path.join(home, d), 3]),
    [home, 1],
  ];
  if (process.platform === 'win32') {
    for (const letter of 'CDEFGHIJ') {
      const drive = `${letter}:\\`;
      if (await exists(drive)) roots.push([drive, 2]);
    }
  }
  for (const [dir, depth] of roots) {
    await walk(dir, depth, found, deadline);
  }
  return [...found.values()].sort((a, b) => b.modified - a.modified);
}

module.exports = { scan, describe };
