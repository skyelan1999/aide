'use strict';
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const {spawn} = require('node:child_process');
const readline = require('node:readline');
const HISTORY = 512 * 1024, PAGE = 64 * 1024, MAX_SESSIONS = 32, MAX_ACTIVE = 16;
const LOG_BYTES = 2 * 1024 * 1024, LOG_FILES = 4;
const {Communications,data:communicationData}=require('./communications');
const COMM_KINDS=['tcp','tcp-server','udp','serial'];
const ID = /^[a-f0-9]{24}$/;
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
function integer(value, fallback, min, max) {
  const n = value === undefined ? fallback : Number(value);
  if (!Number.isInteger(n) || n < min || n > max) throw Error(`数字须为 ${min}–${max} 的整数`);
  return n;
}
function text(value, fallback, limit) {
  const s = value === undefined ? fallback : String(value);
  if (!s || s.length > limit || /[\x00-\x1f\x7f]/.test(s)) throw Error('文本参数无效');
  return s;
}
function privateDir(dir, protect = true) {
  try { fs.mkdirSync(dir, {mode:0o700}); } catch (e) { if (e.code !== 'EEXIST') throw e; }
  const stat = fs.lstatSync(dir);
  if (!stat.isDirectory() || stat.isSymbolicLink()) throw Error('存储目录不能是符号链接');
  if (protect) fs.chmodSync(dir, 0o700);
}
function writePrivate(file, data, append = false) {
  const fd = fs.openSync(file, fs.constants.O_WRONLY | fs.constants.O_CREAT | fs.constants.O_NOFOLLOW | (append ? fs.constants.O_APPEND : fs.constants.O_TRUNC), 0o600);
  try { fs.fchmodSync(fd, 0o600); fs.writeFileSync(fd, data); } finally { fs.closeSync(fd); }
}
function readPrivate(file) {
  const fd = fs.openSync(file, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW);
  try { if (!fs.fstatSync(fd).isFile()) throw Error('存储文件无效'); return fs.readFileSync(fd); } finally { fs.closeSync(fd); }
}
function inside(root, relative, directory = true) {
  if (typeof relative !== 'string' || path.isAbsolute(relative) || relative.split(/[\\/]/).includes('..')) throw Error('路径必须位于当前工作区内');
  const real = fs.realpathSync(path.join(root, relative));
  if (real !== root && !real.startsWith(root + path.sep)) throw Error('路径越界');
  if (directory ? !fs.statSync(real).isDirectory() : !fs.statSync(real).isFile()) throw Error('路径类型无效');
  return real;
}
class Sessions {
  constructor() { this.spaces = new Map(); this.stopping = false; }
  space(workspace) {
    const context = typeof workspace === 'object' && workspace ? workspace : null;
    workspace = context ? context.storageRoot : workspace;
    if (!workspace || !path.isAbsolute(workspace)) throw Error('此工作区需要更新配套 Aide 的 SSH 工作区支持');
    const root = fs.realpathSync(workspace);
    if (this.spaces.has(root)) { const space=this.spaces.get(root); space.remote=context?.remote;space.disabledTools=context?.disabledTools||[]; return space; }
    const base = path.join(root, '.aide'); privateDir(base, false);
    const dir = path.join(base, 'terminal-workbench'); privateDir(dir);
    const space = {root, dir, remote:context?.remote, disabledTools:context?.disabledTools||[], records:new Map()};
    for (const file of fs.readdirSync(dir).filter(x => /^[a-f0-9]{24}\.json$/.test(x)).slice(0, MAX_SESSIONS)) {
      const rec = JSON.parse(readPrivate(path.join(dir, file)));
      if (!ID.test(rec.id) || file !== rec.id + '.json') throw Error('会话记录损坏');
      if (['starting','running'].includes(rec.status)) { rec.status = 'interrupted'; rec.endedAt = new Date().toISOString(); }
      rec.worker = null; rec.history = Buffer.alloc(0); rec.base = rec.offset = 0;
      // Restore a bounded output tail. Live processes are deliberately never auto-restarted.
      for (let i = LOG_FILES - 1; i >= 0; i--) {
        const log = this.logPath(space, rec.id, i);
        if (fs.existsSync(log)) this.remember(rec, readPrivate(log).subarray(-HISTORY));
      }
      space.records.set(rec.id, rec);
      this.persist(space, rec);
    }
    this.spaces.set(root, space);
    return space;
  }
  public(rec) {
    const {id,name,kind,host,port,username,cwd,identityFile,identitySource,auth,devicePath,baudRate,dataBits,stopBits,parity,flowControl,bindPort,resource,cols,rows,status,createdAt,endedAt,exitCode,logging,logError} = rec;
    return {id,name,kind,host,port,username,cwd,identityFile,identitySource,auth,devicePath,baudRate,dataBits,stopBits,parity,flowControl,bindPort,resource,cols,rows,status,createdAt,endedAt,exitCode,logging,logError};
  }
  persist(space, rec) {
    const temporary = path.join(space.dir, rec.id + '.json.tmp');
    writePrivate(temporary, JSON.stringify(this.public(rec), null, 2) + '\n');
    fs.renameSync(temporary, path.join(space.dir, rec.id + '.json'));
  }
  logPath(space, id, index = 0) { return path.join(space.dir, id + '.log' + (index ? '.' + index : '')); }
  remember(rec, buffer) {
    rec.history = Buffer.concat([rec.history, buffer]); rec.offset += buffer.length;
    if (rec.history.length > HISTORY) rec.history = rec.history.subarray(-HISTORY);
    rec.base = rec.offset - rec.history.length;
  }
  output(space, rec, buffer) {
    if (rec.logging && !rec.logError) {
      try {
        const log = this.logPath(space, rec.id);
        if (fs.existsSync(log) && fs.lstatSync(log).size + buffer.length > LOG_BYTES) {
          fs.rmSync(this.logPath(space, rec.id, LOG_FILES - 1), {force:true});
          for (let i = LOG_FILES - 2; i >= 0; i--) {
            const old = this.logPath(space, rec.id, i);
            if (fs.existsSync(old)) fs.renameSync(old, this.logPath(space, rec.id, i + 1));
          }
        }
        writePrivate(log, buffer, true);
      } catch (_) {
        rec.logError = '日志写入失败，请检查存储空间与权限';
        try { this.persist(space, rec); } catch (_) { /* output remains available in memory */ }
      }
    }
    this.remember(rec, buffer);
  }
  get(space, id) {
    if (typeof id !== 'string' || !ID.test(id) || !space.records.has(id)) throw Error('此工作区中不存在该会话');
    return space.records.get(id);
  }
  list(workspace) {
    const space = this.space(workspace);
    return {workspace:{mode:space.remote?'ssh':'local',host:space.remote?.host,path:space.remote?.path},sessions:[...space.records.values()].map(rec => this.public(rec)), storage:space.remote?'Aide 数据目录 / terminal-workspaces/<工作区标识>/.aide/terminal-workbench':'.aide/terminal-workbench', limits:{sessions:MAX_SESSIONS,active:MAX_ACTIVE,logBytes:LOG_BYTES,logFiles:LOG_FILES}};
  }
  async create(workspace, args) {
    if (this.stopping) throw Error('后台正在停止');
    if (process.platform === 'win32') throw Error('交互式 PTY 需要 Linux/macOS；Windows 请使用 Aide Linux 容器');
    const space = this.space(workspace);
    if (space.records.size >= MAX_SESSIONS) throw Error('最多保留32个会话，请先删除已结束的记录');
    const active = [...this.spaces.values()].flatMap(s => [...s.records.values()]).filter(r => r.worker).length;
    if (active >= MAX_ACTIVE) throw Error('最多同时运行16个会话');
    let kind = args.kind || 'local';
    if (kind === 'local' && space.remote) kind = 'workspace';
    if (!['local','ssh','workspace',...COMM_KINDS].includes(kind)) throw Error('未知会话类型');
    const cwd = text(args.cwd, '.', 512);
    if (path.isAbsolute(cwd) || cwd.split(/[\\/]/).includes('..')) throw Error('目录路径必须位于当前工作区内');
    const working = space.remote ? space.root : inside(space.root, cwd);
    const cols = integer(args.cols, 100, 20, 400), rows = integer(args.rows, 30, 5, 200);
    const rec = {id:crypto.randomBytes(12).toString('hex'), kind, name:text(args.name, kind === 'ssh' ? 'SSH 设备' : '本地终端', 80), cwd, cols, rows,
      createdAt:new Date().toISOString(),status:'starting',logging:args.logging !== false,offset:0,base:0,history:Buffer.alloc(0),worker:null};
    if(COMM_KINDS.includes(kind))return this.startCommunication(space,rec,args);
    let argv = ['/bin/bash', '--noprofile', '--norc', '-i'];
    if (kind === 'workspace') {
      if (!space.remote) throw Error('当前不是 SSH 工作区');
      const remote=space.remote, quote=value=>"'"+String(value).replace(/'/g,"'\"'\"'")+"'";
      rec.host=remote.host;rec.username=remote.username;rec.port=remote.port;
      const base=remote.path||'.', directory=cwd==='.'?base:base.replace(/\/$/,'')+'/'+cwd;
      // Borrow only the authenticated workspace master. If it disappears,
      // ProxyCommand fails closed instead of connecting with different credentials.
      argv=['ssh','-F','/dev/null','-tt','-S',remote.socket,'-o','ControlMaster=no','-o','BatchMode=yes','-o','ProxyCommand=/bin/false','-o','EscapeChar=none','-p',String(remote.port||22),'-l',remote.username,remote.host,"cd -- "+quote(directory)+" && exec /bin/bash --noprofile --norc -i"];
    }
    let identityMaterial;
    if (kind === 'ssh') {
      rec.host = text(args.host, '', 253);
      if (!/^[a-zA-Z0-9][a-zA-Z0-9.:%_-]*$/.test(rec.host) && !/^[a-fA-F0-9:]+$/.test(rec.host)) throw Error('SSH 主机名无效');
      rec.username = text(args.username, '', 64);
      if (!/^[a-zA-Z0-9_][a-zA-Z0-9_.@-]*$/.test(rec.username)) throw Error('SSH 用户名无效');
      rec.port = integer(args.port, 22, 1, 65535);
      const known = path.join(space.dir, 'known_hosts');
      // Refuse symlinks and ensure the file exists privately before OpenSSH reads it.
      writePrivate(known, '', true);
      argv = ['ssh','-F','/dev/null','-tt','-p',String(rec.port),'-l',rec.username,
        '-o','StrictHostKeyChecking=ask','-o','UserKnownHostsFile='+known,'-o','GlobalKnownHostsFile=/dev/null',
        '-o','ServerAliveInterval=30','-o','ServerAliveCountMax=3','-o','ConnectTimeout=15','-o','IdentitiesOnly=yes',
        '-o','EscapeChar=none','-o','ForwardAgent=no','-o','ClearAllForwardings=yes'];
      rec.auth = args.auth || 'default';
      if (!['default','password','key'].includes(rec.auth)) throw Error('认证方式无效');
      if (rec.auth === 'password') argv.push('-o','PubkeyAuthentication=no','-o','PreferredAuthentications=password,keyboard-interactive');
      if (rec.auth === 'key') argv.push('-o','PreferredAuthentications=publickey','-o','PasswordAuthentication=no','-o','KbdInteractiveAuthentication=no');
      if (args.identityFile && rec.auth !== 'password') {
        rec.identityFile = text(args.identityFile, '', 512);
        rec.identitySource = args.identitySource || '';
        identityMaterial = typeof workspace === 'object' ? workspace.identityMaterial : undefined;
        if (identityMaterial) argv.push('-i','@AIDE_MEMORY_KEY@');
        else {
          if (rec.identitySource || space.remote) throw Error('所选私钥需要配套宿主读取');
          argv.push('-i', inside(space.root, rec.identityFile, false));
        }
      } else if (rec.auth === 'key') throw Error('请选择私钥文件');
      argv.push(rec.host);
    }
    space.records.set(rec.id, rec);
    this.persist(space, rec);
    const worker = spawn('python3', ['-I', path.join(__dirname, 'pty-worker.py')], {
      cwd:working, env:{PATH:process.env.PATH || '/usr/local/bin:/usr/bin:/bin', HOME:process.env.HOME || '/home/aide', LANG:'C.UTF-8', PYTHONUNBUFFERED:'1'}, stdio:['pipe','pipe','pipe']});
    rec.worker = worker;
    worker.stdin.on('error', () => {});
    worker.stderr.on('data', () => {}); // no credentials or arbitrary helper stderr in daemon events
    let finish;
    const ready = new Promise((resolve, reject) => { finish = {resolve,reject}; });
    const timer = setTimeout(() => { worker.kill('SIGTERM'); finish.reject(Error('PTY 启动超时')); }, 15000);
    const lines = readline.createInterface({input:worker.stdout});
    lines.on('line', line => {
      try {
        if (line.length > 32768) throw Error('PTY 响应过大');
        const msg = JSON.parse(line);
        if (msg.type === 'ready') { rec.status = 'running'; this.persist(space, rec); clearTimeout(timer); finish.resolve(); }
        else if (msg.type === 'output') this.output(space, rec, Buffer.from(msg.data, 'base64'));
        else if (msg.type === 'exit') rec.exitCode = msg.code;
        else if (msg.type === 'error') { rec.status = 'error'; clearTimeout(timer); finish.reject(Error('PTY 运行失败')); }
      } catch (_) { rec.status = 'error'; worker.kill('SIGTERM'); finish.reject(Error('PTY 通信失败')); }
    });
    const end = () => {
      clearTimeout(timer); rec.worker = null;
      if (rec.status !== 'error') rec.status = 'exited';
      rec.endedAt = new Date().toISOString();
      try { this.persist(space, rec); } catch (_) { rec.logError = '会话状态保存失败'; }
      finish.reject(Error('PTY 已退出；请检查 Python、Shell 或 SSH 是否可用'));
    };
    worker.once('error', end); worker.once('close', end);
    worker.stdin.write(JSON.stringify({cwd:working, argv, cols, rows, identityMaterial})+'\n');
    await ready;
    return {session:this.public(rec)};
  }
  communication(space) {
    if(space.communications)return space.communications;
    space.resources=new Map();
    space.communications=new Communications(space,(buffer,meta)=>{
      const rec=space.resources.get(meta.connectionId||meta.socketId||meta.path);
      if(!rec||!rec.worker)return;
      const printable=buffer.toString('utf8').replace(/\uFFFD/g,'.').replace(/[\x00-\x1f\x7f]/g,c=>c==='\n'?'\\n':c==='\r'?'\\r':'.');
      const line=`[${new Date().toISOString()} ${meta.direction==='in'?'RX':'TX'}${meta.remote?' '+meta.remote:''} ${buffer.length} B] ${buffer.toString('hex').replace(/../g,'$& ').trim()} | ${printable}\r\n`;
      this.output(space,rec,Buffer.from(line));
    },event=>{
      if(event.subtype==='connected'&&event.role==='server'){
        const listener=[...space.records.values()].find(r=>r.kind==='tcp-server'&&r.port===event.listenerPort&&r.worker);
        if(this.stopping||!listener||space.records.size>=MAX_SESSIONS||[...this.spaces.values()].flatMap(s=>[...s.records.values()]).filter(r=>r.worker).length>=MAX_ACTIVE){space.communications.call('tcp_close',{connectionId:event.connectionId}).catch(()=>{});return;}
        const rec={id:crypto.randomBytes(12).toString('hex'),kind:'tcp',name:listener.name+' · '+event.remote,host:event.remote.slice(0,event.remote.lastIndexOf(':')),port:Number(event.remote.slice(event.remote.lastIndexOf(':')+1)),cwd:'.',cols:100,rows:30,createdAt:new Date().toISOString(),status:'running',logging:listener.logging,offset:0,base:0,history:Buffer.alloc(0),worker:{},resource:event.connectionId};
        rec.transport={close:()=>space.communications.call('tcp_close',{connectionId:event.connectionId})};space.resources.set(event.connectionId,rec);space.records.set(rec.id,rec);try{this.persist(space,rec);}catch(_){rec.status='error';rec.worker=null;space.communications.call('tcp_close',{connectionId:event.connectionId}).catch(()=>{});}
      }
      if(event.subtype==='closed'){
        const rec=space.resources.get(event.connectionId||event.socketId||event.path);if(rec&&rec.worker){rec.status='exited';rec.worker=null;rec.endedAt=new Date().toISOString();try{this.persist(space,rec);}catch(_){rec.logError='会话状态保存失败';}}
      }
    });
    return space.communications;
  }
  async startCommunication(space,rec,args) {
    const comm=this.communication(space);let result;
    if(['tcp','tcp-server','udp'].includes(rec.kind)){
      rec.host=text(args.host,'127.0.0.1',253);if(!/^[a-zA-Z0-9][a-zA-Z0-9.:%_-]*$/.test(rec.host)&&!/^[a-fA-F0-9:]+$/.test(rec.host))throw Error('通信主机名无效');
      rec.port=integer(args.port,rec.kind==='tcp'?undefined:0,rec.kind==='tcp'?1:0,65535);
    }
    if(rec.kind==='tcp')result=await comm.call('tcp_connect',{host:rec.host,port:rec.port});
    if(rec.kind==='tcp-server'){result=await comm.call('tcp_listen',{port:rec.port});rec.port=result.port;rec.host=result.host;}
    if(rec.kind==='udp'){rec.bindPort=integer(args.bindPort,0,0,65535);result=await comm.call('udp_bind',{port:rec.bindPort,multicast:!!args.multicast});rec.bindPort=result.port;}
    if(rec.kind==='serial'){
      rec.devicePath=text(args.devicePath,'',512);rec.baudRate=integer(args.baudRate,115200,1,4000000);rec.dataBits=integer(args.dataBits,8,5,8);rec.stopBits=Number(args.stopBits??1);rec.parity=args.parity||'none';rec.flowControl=!!args.flowControl;
      if(![1,1.5,2].includes(rec.stopBits)||!['none','even','odd','mark','space'].includes(rec.parity))throw Error('串口参数无效');
      result=await comm.call('serial_open',{path:rec.devicePath,baudRate:rec.baudRate,dataBits:rec.dataBits,stopBits:rec.stopBits,parity:rec.parity,flowControl:rec.flowControl});
    }
    rec.resource=result.connectionId||result.socketId||rec.devicePath||String(result.port);
    rec.status='running';rec.worker={};
    rec.transport={close:async()=>{
      if(rec.kind==='tcp')await comm.call('tcp_close',{connectionId:rec.resource});
      if(rec.kind==='tcp-server'){
        const mod=comm.modules.tcp,entry=mod.__state.listeners.get(rec.port);if(entry){for(const id of [...entry.conns])await comm.call('tcp_close',{connectionId:comm.token+':'+id});await new Promise(resolve=>entry.server.close(resolve));mod.__state.listeners.delete(rec.port);}
      }
      if(rec.kind==='udp')await comm.call('udp_close',{socketId:rec.resource});
      if(rec.kind==='serial')await comm.call('serial_close',{path:rec.devicePath});
    }};
    space.resources.set(rec.resource,rec);space.records.set(rec.id,rec);
    try{this.persist(space,rec);}catch(error){await rec.transport.close();space.records.delete(rec.id);throw error;}
    return {session:this.public(rec),communication:result};
  }
  async sendCommunication(space,rec,args){
    const comm=this.communication(space),buffer=communicationData(args.data,args.format||'utf8');
    if(rec.kind==='tcp-server')throw Error('请先选择已接入的TCP客户端会话');
    const values={data:buffer.toString('hex'),format:'hex'};
    if(rec.kind==='tcp')return comm.call('tcp_send',{...values,connectionId:rec.resource});
    if(rec.kind==='serial')return comm.call('serial_write',{...values,path:rec.devicePath});
    if(!rec.port)throw Error('UDP发送需要指定目标端口');
    return comm.call('udp_send',{...values,socketId:rec.resource,host:rec.host,port:rec.port});
  }
  async commCall(workspace,args){const space=this.space(workspace),comm=this.communication(space);const name=String(args.action||'').replace(/^terminal_/,'');
    const input=args.arguments||{};
    const converted={tcp_connect:{kind:'tcp',host:input.host,port:input.port},tcp_listen:{kind:'tcp-server',port:input.port},udp_bind:{kind:'udp',port:0,bindPort:input.port,multicast:input.multicast},serial_open:{...input,kind:'serial',devicePath:input.path}}[name];
    if(converted){const {session,communication:result}=await this.create(workspace,{...converted,name:input.name||name});return {...result,sessionId:session.id};}
    if(space.disabledTools?.includes('terminal_'+name))throw Error('通信工具已被管理员禁用');
    if(name==='ssh_fingerprint')return comm.fingerprint(args.arguments||{});
    const values={...(args.arguments||{})};
    if(name==='ssh_connect'&&typeof workspace==='object'&&workspace.identityMaterial){values.privateKey=Buffer.from(workspace.identityMaterial,'base64').toString();values.authType='publickey';}
    return comm.call(name,values);
  }
  send(workspace, args) {
    const rec = this.get(this.space(workspace), args.id);
    if (!rec.worker || rec.status !== 'running') throw Error('会话已结束，创建新会话后重试');
    if(rec.transport)return this.sendCommunication(this.space(workspace),rec,args);
    if (typeof args.data !== 'string' || Buffer.byteLength(args.data) > 16384) throw Error('单次输入不能超过16KiB');
    if (rec.worker.stdin.writableLength > 65536) throw Error('终端输入繁忙，请稍后重试');
    rec.worker.stdin.write(JSON.stringify({type:'input',data:Buffer.from(args.data).toString('base64')})+'\n');
    return {accepted:true};
  }
  resize(workspace, args) {
    const space = this.space(workspace), rec = this.get(space, args.id);
    rec.cols = integer(args.cols, 100, 20, 400); rec.rows = integer(args.rows, 30, 5, 200);
    if (rec.worker && !rec.transport) rec.worker.stdin.write(JSON.stringify({type:'resize',cols:rec.cols,rows:rec.rows})+'\n');
    this.persist(space, rec);
    return {session:this.public(rec)};
  }
  poll(workspace, args) {
    const space = this.space(workspace), rec = args.id ? this.get(space,args.id) : null;
    if (!rec) return this.list(workspace);
    const requested = integer(args.offset, 0, 0, Number.MAX_SAFE_INTEGER);
    const start = Math.max(rec.base, Math.min(requested,rec.offset)), end = Math.min(rec.offset,start+PAGE);
    return {...this.list(workspace), output:{id:rec.id, data:rec.history.subarray(start-rec.base,end-rec.base).toString('base64'), offset:end, reset:requested < rec.base || requested > rec.offset, more:end < rec.offset}};
  }
  async close(workspace, args) {
    const space = this.space(workspace), rec = this.get(space,args.id);
    return this.closeRecord(space,rec);
  }
  async closeRecord(space,rec) {
    if(rec.transport){await rec.transport.close();rec.worker=null;rec.status='exited';rec.endedAt=new Date().toISOString();this.persist(space,rec);return {session:this.public(rec)};}
    const worker = rec.worker;
    if (worker) {
      worker.stdin.write('{"type":"close"}\n');
      for (let i=0; i<40 && rec.worker; i++) await sleep(50);
      if (rec.worker) { worker.kill('SIGKILL'); await sleep(100); }
    }
    return {session:this.public(rec)};
  }
  logging(workspace, args) {
    const space = this.space(workspace), rec = this.get(space,args.id);
    if (typeof args.enabled !== 'boolean') throw Error('enabled 须为布尔值');
    rec.logging = args.enabled; delete rec.logError; this.persist(space,rec);
    return {session:this.public(rec)};
  }
  logs(workspace, args) {
    const space = this.space(workspace), rec = this.get(space,args.id);
    const files=[];
    for (let index=LOG_FILES-1; index>=0; index--) {
      const file=this.logPath(space,rec.id,index);
      if (fs.existsSync(file)) { const stat=fs.lstatSync(file); if (!stat.isFile() || stat.isSymbolicLink()) throw Error('日志文件无效'); files.push({index,bytes:stat.size}); }
    }
    if (args.index === undefined) return {files};
    const index=integer(args.index,0,0,LOG_FILES-1), file=this.logPath(space,rec.id,index), offset=integer(args.offset,0,0,Number.MAX_SAFE_INTEGER);
    // A frozen identity prevents a rotation from silently joining two different files.
    const fd=fs.openSync(file,fs.constants.O_RDONLY|fs.constants.O_NOFOLLOW);
    try {
      const stat=fs.fstatSync(fd), identity=`${stat.dev}:${stat.ino}`;
      if (args.identity && args.identity!==identity) throw Error('日志已轮转，请重新导出');
      const data=Buffer.alloc(Math.min(PAGE,Math.max(0,stat.size-offset)));
      const count=fs.readSync(fd,data,0,data.length,offset);
      return {index,identity,data:data.subarray(0,count).toString('base64'),offset:offset+count,bytes:stat.size,more:offset+count<stat.size};
    } finally { fs.closeSync(fd); }
  }
  remove(workspace, args) {
    const space=this.space(workspace), rec=this.get(space,args.id);
    if (rec.worker) throw Error('请先关闭会话，再删除记录与日志');
    for (let i=0;i<LOG_FILES;i++) fs.rmSync(this.logPath(space,rec.id,i),{force:true});
    fs.rmSync(path.join(space.dir,rec.id+'.json')); space.records.delete(rec.id);space.resources?.delete(rec.resource);
    return {deleted:true};
  }
  async stop() {
    this.stopping=true;
    await Promise.allSettled([...this.spaces.values()].flatMap(space=>[...space.records.values()].filter(rec=>rec.worker).map(rec=>this.closeRecord(space,rec))));
    for(const space of this.spaces.values()){await space.communications?.close();for(const rec of space.records.values())if(rec.transport&&rec.worker){rec.worker=null;rec.status='exited';rec.endedAt=new Date().toISOString();try{this.persist(space,rec);}catch(_){}}}
  }
}
module.exports={Sessions, HISTORY, PAGE, LOG_BYTES, LOG_FILES};
