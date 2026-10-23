'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync('internal/server/web/app.js','utf8');const start=source.indexOf('function decorateGeneratedMedia('),end=source.indexOf('\nfunction renderMarkdown',start);
const media=[];const links=['generated/speech/qwen-abc123.mp3','generated/video/qwen-def456.mp4','https://evil.invalid/a.mp3','generated/speech/../../secret.mp3','generated/video/qwen-abc123.mp3'].map(href=>({href,attributes:{'data-internal-link':href},getAttribute(){return href},removeAttribute(k){delete this.attributes[k]},parentNode:{insertBefore(node){media.push(node)}}}));
const context={Set,document:{createElement:tag=>({tag})},fileRawUrl:path=>'/api/file/raw?path='+encodeURIComponent(path)};vm.createContext(context);vm.runInContext(source.slice(start,end),context);context.decorateGeneratedMedia({querySelectorAll:()=>links});
assert.equal(media.length,2);assert.deepEqual(media.map(x=>x.tag),['audio','video']);assert(media.every(x=>x.controls&&x.preload==='metadata'&&!x.autoplay));assert.equal(links[0].download,'qwen-abc123.mp3');assert(!links[0].attributes['data-internal-link']);assert(links[2].attributes['data-internal-link']);
console.log('Generated media PASS: local-only paths, explicit download, authenticated raw URL, controls, no autoplay');
