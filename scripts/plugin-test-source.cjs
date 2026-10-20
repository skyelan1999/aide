"use strict";
const path = require('node:path');
module.exports = id => {
  const root = process.env.AIDE_PLUGIN_TEST_ROOT;
  if (!root) throw new Error('Set AIDE_PLUGIN_TEST_ROOT to independent plugin-ID checkouts before running plugin integration checks');
  return require(path.join(path.resolve(root), id, 'index.js'));
};
