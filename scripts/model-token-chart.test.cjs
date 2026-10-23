'use strict';
const {test} = require('node:test');
const assert = require('node:assert/strict');
const chart = require('../internal/server/web/model-token-chart.js');
function fixture() {
  class Element {
    constructor(tag) { this.tagName=tag; this.children=[]; this.attributes={}; this.style={}; this.events={}; this.ownerDocument=document; }
    append(...children) { this.children.push(...children); }
    replaceChildren(...children) { this.children=children; }
    setAttribute(key,value) { this.attributes[key]=String(value); }
    addEventListener(key,fn) { this.events[key]=fn; }
  }
  const document = {createElement:tag=>new Element(tag),createElementNS:(_ns,tag)=>new Element(tag)};
  const host = document.createElement('div');
  const all = (n,tag) => [...(n.tagName===tag?[n]:[]),...n.children.flatMap(c=>all(c,tag))];
  return {host, all:tag=>all(host,tag), render:data=>chart.render(host,data,{t:x=>x,format:String})};
}
test('99.9 percent retains a separate remainder and proportional ring geometry',()=>{
  const f=fixture(); f.render({dominant:{total:9990},tiny:{total:10}});
  const circles=f.all('circle'); assert.equal(circles.length,2);
  const lengths=circles.map(c=>Number(c.attributes['stroke-dasharray'].split(' ')[0]));
  assert(Math.abs(lengths[0]/lengths[1]-999)<1e-8);
  assert.equal(f.all('polyline').length,0); assert.equal(f.all('text').length,0);
  assert.equal(f.all('button').length,2);
  assert.equal(f.all('button')[1].title,'tiny: 10 tokens');
});
test('single model is a complete ring, not a degenerate arc; invalid data renders empty',()=>{
  const f=fixture(); f.render({one:{total:10}});
  assert.equal(f.all('circle').length,1);
  assert.equal(f.all('circle')[0].attributes['stroke-dasharray'].split(' ')[1],'0');
  f.render({negative:{total:-1},bad:{total:NaN},infinite:{total:Infinity},zero:{total:0}});
  assert.equal(f.all('svg').length,0); assert.equal(f.host.children[1].textContent,'暂无模型调用数据');
});
test('small shares preserve every model in details; labels use text, not markup',()=>{
  const f=fixture(), hostile='<img src=x onerror=alert(1)>';
  f.render({[hostile]:{total:10000},a:{total:1},b:{total:2}});
  const buttons=f.all('button'); assert.equal(buttons.length,2);
  assert.equal(buttons[0].children[1].textContent,hostile);
  assert.equal(buttons[1].children[2].children[0].textContent,'<0.1%');
  assert.match(buttons[1].title,/a: 1 tokens/); assert.match(buttons[1].title,/b: 2 tokens/);
  assert.equal(f.all('img').length,0);
});
test('many long model names keep an HTML legend and keyboard highlight resets',()=>{
  const f=fixture(), data=Object.fromEntries(Array.from({length:12},(_,i)=>['provider/model-'+i+'-'.repeat(80),{total:1}]));
  f.render(data); assert.equal(f.all('button').length,12);
  const buttons=f.all('button'), circles=f.all('circle');
  buttons[3].events.focus(); assert.equal(circles[3].style.opacity,'1'); assert.equal(circles[0].style.opacity,'.28');
  buttons[3].events.blur(); assert(circles.every(c=>c.style.opacity==='1'));
  f.render({}); assert.equal(f.all('button').length,0);
});

test('token counts share decimal units and promote rounded boundaries',()=>{
  for (const [count, expected] of [[0,'0'],[999,'999'],[1000,'1.0K'],[45600,'45.6K'],[999949,'999.9K'],[999950,'1.0M'],[1000000,'1.0M'],[29814800,'29.8M'],[999950000,'1.0B'],[1000000000,'1.0B'],[1000000000000,'1.0T'],[-1,'0'],[Infinity,'0'],[NaN,'0']]) {
    assert.equal(chart.formatTokens(count),expected,String(count));
  }
});
