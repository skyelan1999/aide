'use strict';
const fs=require('node:fs'),path=require('node:path');
const SERIAL=/^(?:tty(?:USB|ACM|AMA|S|THS|WCH|XRUSB|MFD|O|AP|HS)\d+|rfcomm\d+)$/;
function discoverLinuxPorts(devRoot='/dev') {
 const found=new Map();
 const add=file=>{try{const real=fs.realpathSync(file);if(!fs.statSync(real).isCharacterDevice())return;let port=found.get(real);if(!port){port={path:file,manufacturer:null,serialNumber:null,vendorId:null,productId:null};found.set(real,port);}return port;}catch(_){}};
 let names=[];try{names=fs.readdirSync(devRoot);}catch(_){}
 for(const name of names.filter(name=>SERIAL.test(name)).sort())add(path.join(devRoot,name));
 // Stable USB aliases remain useful inside containers without a udev database.
 const byId=path.join(devRoot,'serial','by-id');try{for(const name of fs.readdirSync(byId).sort()){const port=add(path.join(byId,name));if(port)port.pnpId=name;}}catch(_){}
 return [...found.values()];
}
function validateSerialPath(value,platform=process.platform) {
 const device=String(value||'').trim();
 if(platform==='win32') {if(!/^(?:\\\\\.\\)?COM[1-9]\d*$/i.test(device))throw Error('请选择有效的 COM 串口，例如 COM3');return device;}
 const real=fs.realpathSync(device);if(!real.startsWith('/dev/')||!fs.statSync(real).isCharacterDevice())throw Error('请选择实际串口字符设备');return device;
}
module.exports={discoverLinuxPorts,validateSerialPath};
