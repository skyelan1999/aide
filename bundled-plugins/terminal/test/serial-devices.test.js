'use strict';
const {test}=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),os=require('node:os');
const {discoverLinuxPorts,validateSerialPath}=require('../communications/serial-devices');
test('Linux fallback discovers mapped character devices and aliases without udev, excludes ordinary files',t=>{
 const dir=fs.mkdtempSync(path.join(os.tmpdir(),'serial-discovery-'));t.after(()=>fs.rmSync(dir,{recursive:true,force:true}));
 fs.symlinkSync('/dev/null',path.join(dir,'ttyUSB42'));fs.writeFileSync(path.join(dir,'ttyUSB43'),'not a device');fs.mkdirSync(path.join(dir,'serial','by-id'),{recursive:true});fs.symlinkSync('/dev/null',path.join(dir,'serial','by-id','usb-fixture'));
 const ports=discoverLinuxPorts(dir);assert.equal(ports.length,1);assert.equal(ports[0].path,path.join(dir,'ttyUSB42'));assert.equal(ports[0].pnpId,'usb-fixture');
});
test('serial path validation supports Windows COM names and rejects arbitrary paths',()=>{
 for(const value of ['COM1','COM12','\\\\.\\COM99'])assert.equal(validateSerialPath(value,'win32'),value);
 for(const value of ['COM0','COM1/other','C:\\secret','file.txt'])assert.throws(()=>validateSerialPath(value,'win32'),/COM/);
 assert.throws(()=>validateSerialPath(__filename,'linux'),/字符设备/);
});
test('failed native open releases bookkeeping so retry reaches the driver again',async()=>{
 const {Sessions}=require('../sessions');const root=fs.mkdtempSync(path.join(os.tmpdir(),'serial-retry-')),m=new Sessions();try{
 for(let i=0;i<2;i++){await assert.rejects(m.create(root,{kind:'serial',devicePath:'/dev/null'}),error=>!/已打开/.test(error.message));assert.equal(m.communication(m.space(root)).modules.serial.__state.ports.size,0);}
 }finally{await m.stop();fs.rmSync(root,{recursive:true,force:true});}
});
test('missing udevadm returns discovery guidance instead of a fatal enumeration error',()=>{
 const {spawnSync}=require('node:child_process');const code="const m=require('./communications/serial'),t={};m.apply({tool:x=>t[x.name]=x,emit(){}});t.serial_list.handler().then(r=>console.log(JSON.stringify(r)))";
 const child=spawnSync(process.execPath,['-e',code],{cwd:path.resolve(__dirname,'..'),env:{...process.env,PATH:'/missing-udev-test'},encoding:'utf8',timeout:10000});assert.equal(child.status,0,child.stderr);const result=JSON.parse(child.stdout);assert(Array.isArray(result.ports));assert.equal(result.error,undefined);assert.match(result.warning,/udev/);
});
