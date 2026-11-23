const fs = require('node:fs');
const path = require('node:path');
const ts = require('../../node_modules/typescript');
const { sourceIndex, sourceIncludes, sourceMatch, sourceRegex } = require('./starmap-source.cjs');

function sourceTree(source) {
  return ts.createSourceFile('app.js', source, ts.ScriptTarget.ES2021, true, ts.ScriptKind.JS);
}

// Extract complete declarations after tsc has expanded their original one-line
// formatting. Tests can execute the real production functions without relying
// on a following comment or the next compact `function` token.
function sourceFunction(source, name) {
  const tree = sourceTree(source);
  let match;
  function visit(node) {
    if (match) return;
    if (ts.isFunctionDeclaration(node) && node.name?.text === name) match = node;
    else if (ts.isVariableDeclaration(node) && node.name.getText(tree) === name &&
      node.initializer && (ts.isArrowFunction(node.initializer) || ts.isFunctionExpression(node.initializer))) {
      match = node.parent.parent;
    }
    ts.forEachChild(node, visit);
  }
  visit(tree);
  if (!match) throw new Error('Source function not found: ' + name);
  return source.slice(match.getStart(tree), match.end);
}

function sourceAssignment(source, name) {
  const tree = sourceTree(source);
  let match;
  function visit(node) {
    if (match) return;
    if (ts.isExpressionStatement(node) && ts.isBinaryExpression(node.expression) &&
      node.expression.operatorToken.kind === ts.SyntaxKind.EqualsToken) {
      const left = node.expression.left.getText(tree).replace(/\s+/g, '');
      if (left === name || left.endsWith('.' + name)) match = node;
    }
    ts.forEachChild(node, visit);
  }
  visit(tree);
  if (!match) throw new Error('Source assignment not found: ' + name);
  return source.slice(match.getStart(tree), match.end);
}

function sourceStatement(source, marker) {
  const tree = sourceTree(source);
  const needle = marker.replace(/\s+/g, '');
  let match;
  function visit(node) {
    if (match) return;
    if ((ts.isExpressionStatement(node) || ts.isVariableStatement(node)) &&
      node.getText(tree).replace(/\s+/g, '').includes(needle)) match = node;
    ts.forEachChild(node, visit);
  }
  visit(tree);
  if (!match) throw new Error('Source statement not found: ' + marker);
  return source.slice(match.getStart(tree), match.end);
}

function sourceProperty(source, name) {
  const tree = sourceTree(source);
  let match;
  function visit(node) {
    if (match) return;
    if (ts.isPropertyAssignment(node) && node.name.getText(tree).replace(/^['"]|['"]$/g, '') === name) match = node;
    ts.forEachChild(node, visit);
  }
  visit(tree);
  if (!match) throw new Error('Source property not found: ' + name);
  return source.slice(match.getStart(tree), match.end);
}

// Tests historically evaluated one monolithic app.js. Preserve that view while
// the classic script is split; the array remains in the original source order.
function readAppSource() {
  return ['app-core.js', 'app-sidebar.js', 'app-runtime.js', 'app-config.js', 'app.js']
    .map(name => fs.readFileSync(path.join(__dirname, '../../internal/server/web', name), 'utf8'))
    .join('');
}

module.exports = { readAppSource, sourceFunction, sourceAssignment, sourceStatement, sourceProperty, sourceIndex, sourceIncludes, sourceMatch, sourceRegex };
