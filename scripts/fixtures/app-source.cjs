const fs = require('node:fs');
const path = require('node:path');

// Tests historically evaluated one monolithic app.js. Preserve that view while
// the classic script is split; the array remains in the original source order.
function readAppSource() {
  return ['app-core.js', 'app-sidebar.js', 'app.js']
    .map(name => fs.readFileSync(path.join(__dirname, '../../internal/server/web', name), 'utf8'))
    .join('');
}

module.exports = { readAppSource };
