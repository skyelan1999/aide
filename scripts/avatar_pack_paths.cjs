'use strict';
const fs = require('node:fs/promises');
const path = require('node:path');

module.exports = async function createAvatarPackPaths({root, inputDir, outputDir, auditDir}) {
  const base = await fs.realpath(root);
  const within = (directory, target) => {
    const relative = path.relative(directory, target);
    return relative === '' || (!path.isAbsolute(relative) && relative !== '..' && !relative.startsWith('..' + path.sep));
  };
  async function checkPath(target) {
    target = path.resolve(target);
    if (!within(base, target)) throw Error('Avatar path outside repository');
    let current = base;
    for (const component of path.relative(base, target).split(path.sep).filter(Boolean)) {
      current = path.join(current, component);
      try {
        if ((await fs.lstat(current)).isSymbolicLink()) throw Error('Avatar path must not use symlinks');
      } catch (error) {if (error.code !== 'ENOENT') throw error;}
    }
    return target;
  }
  for (const directory of [inputDir, outputDir, auditDir]) await checkPath(directory);
  const expected = JSON.parse(await fs.readFile(path.join(base, 'docs/design/virtual-avatar-generation.json'), 'utf8')).jobs;
  const jobs = new Map(expected.map(job => [job.id, job]));
  function knownJob(job) {
    if (!job || typeof job.id !== 'string' || !/^[a-z][a-z0-9_]*-v[1-9][0-9]*$/.test(job.id) || !jobs.has(job.id)) throw Error('Invalid avatar job ID');
    if (job.scene !== jobs.get(job.id).scene) throw Error('Avatar job scene does not match registered ID');
  }
  function record(record, job) {
    knownJob(record);
    if (job && record.id !== job.id) throw Error('Cached avatar record ID mismatch');
    const names = {atlas: record.id + '-atlas.webp', animation: record.id + '.webp', poster: record.id + '-poster.webp'};
    for (const [field, name] of Object.entries(names)) if (record[field] !== name) throw Error('Invalid avatar asset filename');
  }
  async function job(job) {
    knownJob(job);
    if (typeof job.source !== 'string' || !job.source.trim()) throw Error('Avatar artwork source required');
    const source = path.resolve(inputDir, job.source);
    if (!within(inputDir, source)) throw Error('Avatar artwork source outside input directory');
    await checkPath(source);
    if (!within(await fs.realpath(inputDir), await fs.realpath(source)) || !(await fs.stat(source)).isFile()) throw Error('Avatar artwork source must be an input file');
    const audit = await checkPath(path.join(auditDir, job.id + '.json'));
    for (const file of [job.id + '-contact.jpg']) await checkPath(path.join(auditDir, file));
    for (const file of [job.id + '-atlas.webp', job.id + '.webp', job.id + '-poster.webp']) await checkPath(path.join(outputDir, file));
    return {source, audit};
  }
  // Check every fixed publication destination as well as per-job destinations.
  for (const file of ['manifest.json', 'art-audit.json']) await checkPath(path.join(outputDir, file));
  await fs.mkdir(outputDir, {recursive: true});await fs.mkdir(auditDir, {recursive: true});
  return {job, record};
};
