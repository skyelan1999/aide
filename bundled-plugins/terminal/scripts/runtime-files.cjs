'use strict';
// Resolve the production dependency graph from the frozen installation. Include
// runtime JS/native assets and upstream licenses; omit tests, docs and hidden files.
const fs=require('node:fs'),path=require('node:path'),root=path.resolve(__dirname,'..'),packages=new Set(),files=new Set();
function packageRoot(name,from){let current=path.dirname(require.resolve(name,{paths:[from]}));while(current!==path.dirname(current)){const file=path.join(current,'package.json');if(fs.existsSync(file)&&JSON.parse(fs.readFileSync(file)).name===name)return current;current=path.dirname(current);}throw Error('Cannot resolve production package '+name);}
function visit(name,from){const base=packageRoot(name,from);if(packages.has(base))return;packages.add(base);const manifest=JSON.parse(fs.readFileSync(path.join(base,'package.json')));
 function walk(dir){for(const item of fs.readdirSync(dir,{withFileTypes:true})){if(item.name.startsWith('.')||['node_modules','test','tests','examples','benchmark','benchmarks','doc','docs'].includes(item.name))continue;const file=path.join(dir,item.name);if(item.isDirectory())walk(file);else if(item.isFile()&&(/\.(js|json|node|wasm)$/.test(item.name)||/^(licen[cs]e|copying)/i.test(item.name)))files.add(path.relative(root,file).split(path.sep).join('/'));}}
 walk(base);for(const dependency of Object.keys(manifest.dependencies||{}))visit(dependency,base);
}
const manifest=JSON.parse(fs.readFileSync(path.join(root,'package.json')));for(const name of Object.keys(manifest.dependencies||{}))visit(name,root);process.stdout.write(JSON.stringify([...files].sort()));
