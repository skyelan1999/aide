interface Window {
  AideMessageNotifications?: any;
  AideMessageCenter?: any;
  AideVoIP?: any;
}
(() => {
  'use strict';
  const notifications = window.AideMessageNotifications;
  const CHUNK = 1024 * 1024, MAX_FILE = 1024 * CHUNK;
  const english: any = {"账户名称":"Account name","未设置":"Not set","请先在设置 → 账户填写账户名称，才能启用设备通信。":"Set an account name in Settings \u2192 Account before enabling messaging.","前往账户设置":"Open account settings","允许助理读取和管理消息模块":"Allow assistant to read and manage messages","默认关闭。读取的消息会交给当前助理模型处理；可发送文字、标记已读和管理拉黑，不会自动同意联系人、群组、通话或文件。":"Off by default. Read messages are sent to the current assistant model for processing. Allows sending text, marking as read and blocking contacts. Contact, group, call and file consent remains manual.","设置已保存":"Settings saved",'退出群组':'Leave group','保存文件':'Save file','取消保存':'Cancel save','消息':'Messages','会话':'Chats','联系人':'Contacts','传输':'Transfers','提醒':'Reminders','消息设置':'Message settings','设备名称':'Device name','启用设备通信':'Enable device messaging','局域网发现':'LAN discovery','监听地址':'Listen address','自托管中继（每行一个 HTTPS 地址）':'Self-hosted relays (one HTTPS origin per line)','公网地址提示（可选，每行一个）':'Advertised origins (optional, one per line)','STUN / TURN（可选，每行一个）':'STUN / TURN (optional, one per line)','TURN 用户名':'TURN username','TURN 密码（留空保留）':'TURN credential (blank keeps existing)','保存设置':'Save settings','添加联系人':'Add contact','查找设备':'Find device','输入 6 位设备码':'Six-character device code','我的设备码':'My device code','复制设备码':'Copy device code','完整设备指纹':'Full device fingerprint','请通过其他可信渠道核对完整指纹。':'Verify the full fingerprint through another trusted channel.','已核对完整指纹':'I verified the full fingerprint','申请添加':'Request contact','未找到设备，请确认对方在线且使用相同中继或同一局域网。':'No device found. Check that the peer is online and uses the same relay or LAN.','同码设备有多个，请核对完整指纹后选择。':'Multiple devices have this code. Verify the full fingerprint before selecting.','联系人申请':'Contact request','群组邀请':'Group invitation','语音邀请':'Voice invitation','文件邀请':'File offer','同意':'Accept','拒绝':'Decline','接听':'Answer','待处理申请':'Pending requests','暂无待处理申请':'No pending requests','暂无联系人':'No contacts yet','等待对方同意':'Waiting for approval','申请已发出':'Request sent','暂未确认送达，可稍后重试':'Delivery unconfirmed. Retry later.','拉黑':'Block','解除拉黑':'Unblock','解除拉黑后需重新添加联系人。':'Unblocking does not restore contact access.','已拉黑':'Blocked','创建群组':'Create group','群组名称':'Group name','选择 1–7 位联系人，逐人邀请。':'Choose 1–7 contacts; each must accept.','创建并邀请':'Create and invite','群组':'Group','返回会话':'Back to chats','暂无消息':'No messages yet','开始聊天':'Start a conversation','发送':'Send','输入消息':'Write a message','语音':'Voice call','群语音':'Group voice','发送文件':'Offer file','选择原文件续传':'Reselect original file to resume','继续传输':'Resume transfer','取消':'Cancel','重试':'Retry','保存到工作目录':'Save to workspace','下载':'Download','已完成':'Complete','传输中':'Transferring','已取消':'Cancelled','接收中':'Receiving','暂无文件传输':'No file transfers','同意前不会发送文件内容。':'File contents are sent only after the recipient accepts.','请求接收文件':'Request file transfer','原文件内容不一致，无法续传。':'File contents do not match the original transfer.','文件最大为 1 GiB':'Maximum file size is 1 GiB','计算文件校验':'Checking file chunks','文件已保存到工作目录':'File saved to workspace','保存文件名':'Save as (workspace filename)','等待接听':'Calling…','连接中':'Connecting…','通话中':'In call','静音':'Mute','取消静音':'Unmute','结束通话':'End call','播放语音':'Play audio','请先结束当前通话':'End the current call first','无法建立语音连接，请检查 STUN / TURN 设置。':'Voice connection failed. Check STUN / TURN settings.','麦克风不可用或权限被拒绝':'Microphone unavailable or permission denied','提醒功能始终可用；设备通信默认关闭。':'Reminders remain available. Device messaging is off by default.','开启后接收联系人申请；聊天、通话、群组和文件分别征得同意。':'Enable to receive contact requests. Chat, calls, groups and files require separate consent.','高级网络设置':'Advanced network settings','局域网直连；跨网可配置自托管中继。':'Direct LAN connections; optional self-hosted relays across networks.','群主逐人邀请参加群语音':'The group owner invites each participant to the call','会话成员尚未同步':'Waiting for group membership sync','查看申请':'View requests','消息未确认送达，草稿已保留。':'Delivery unconfirmed; draft kept.','显示更早消息':'Show older messages','新建提醒':'New reminder','关闭设置':'Close settings','设备通信未启用':'Device messaging is off','刷新':'Refresh','只显示已同意的联系人和群组。':'Only accepted contacts and joined groups appear here.','传输已暂停，可重试或重新选择原文件。':'Transfer paused. Retry or reselect the original file.','已复制':'Copied','来自':'From','接收文件后不会自动打开或执行。':'Received files are never opened or executed automatically.','取消邀请':'Cancel invitation','语音正在其他窗口使用':'Voice is active in another window','正在校验，请稍候':'Checking file; please wait','连接状态':'Connection status','正在更新网络设置':'Updating network settings','通知对方中':'Notifying peer'};
  Object.assign(english,{"通知与角标": "Notifications and badges", "会话通知设置": "Conversation notifications", "通知设置": "Notification settings", "免打扰": "Do not disturb", "已屏蔽提示": "Notifications hidden", "提示方式": "Notification mode", "跟随全局": "Use global settings", "屏蔽全部提示": "Hide all notifications", "免打扰时长": "Mute duration", "永久": "Forever", "1 小时": "1 hour", "8 小时": "8 hours", "继续接收消息；免打扰保留灰色未读数，屏蔽隐藏角标。": "Messages still arrive. Muted chats retain a gray unread count; hidden chats have no badge.", "应用": "Apply", "角标样式": "Badge style", "数字": "Number", "仅红点": "Dot only", "关闭": "Off", "计入总角标的内容": "Include in total badge", "单聊未读": "Unread direct messages", "群聊未读": "Unread group messages", "联系人与群组申请": "Contact and group requests", "需处理的文件传输": "File transfers needing action", "未完成提醒": "Unfinished reminders", "提醒统计范围": "Reminder count scope", "全部未完成": "All unfinished", "仅到期与逾期": "Due and overdue only", "免打扰会话计入总角标": "Include muted conversations in total", "新消息提示": "New message alerts", "应用内提示": "In-app alerts", "提示音": "Sound", "浏览器桌面通知": "Browser desktop notifications", "关闭角标不会清除未读；查看提醒不会自动完成事项。声音和桌面通知默认关闭。": "Hiding badges does not mark messages read. Viewing reminders does not complete them. Sound and desktop alerts are off by default.", "浏览器未允许桌面通知，其他设置尚未保存。": "Desktop permission was not granted. Other settings have not been saved.", "屏蔽与免打扰列表": "Hidden and muted conversations", "暂无屏蔽或免打扰会话": "No hidden or muted conversations", "申请": "Requests", "未读": "Unread", "收到新消息或申请": "New message or request", "全部消息标记已读": "Mark all messages read", "标记已读": "Mark as read", "通知设置已在其他窗口修改，请刷新后重试": "Notification settings changed in another window. Refresh and try again."});
  const node = (tag: any, cls?: any, text?: any): any => { const e = document.createElement(tag); if (cls) e.className = cls; if (text != null) e.textContent = String(text); return e; };
  const bytes = (n: any): any => n >= CHUNK ? (n / CHUNK).toFixed(1) + ' MiB' : Math.ceil(n / 1024) + ' KiB';
  const hex = (data: any): any => [...new Uint8Array(data)].map(v => v.toString(16).padStart(2, '0')).join('');
  async function manifest(file: any, progress: any, alive = () => true): Promise<any> {
    if (file.size > MAX_FILE) throw new Error(uiTextMessageCenter('文件最大为 1 GiB'));
    const hashes: any[] = [];
    for (let offset = 0; offset < file.size; offset += CHUNK) {
      if (!alive()) throw new DOMException('Cancelled', 'AbortError');
      hashes.push(hex(await crypto.subtle.digest('SHA-256', await file.slice(offset, offset + CHUNK).arrayBuffer())));
      progress?.(Math.floor(Math.min(offset + CHUNK,file.size) / file.size * 100));
    }
    return {name: file.name, size: file.size, hashes};
  }
  function mount({center, api, raw, t, toast, isLocked, openSettings, token}: any): any {
    if (!center || center.dataset.messagesMounted) return;
    center.dataset.messagesMounted = 'true'; center.classList.add('message-center');
    const tr = (text: any): any => document.documentElement.lang.startsWith('en') ? (english[text] || t(text)) : t(text);
    const post = (path: any, body: any, options = {}): any => api('/messages/' + path, {method:'POST', body:JSON.stringify(body), ...options});
    const safe = (fn: any): any => (window.AideAsyncAction as any).wrap(fn, (error: any) => { if (error.name !== 'AbortError') toast(tr(error.message || '请求失败')); });
    const button = (label: any, fn?: any, cls = 'quiet'): any => { const b = node('button', cls, tr(label)); b.type = 'button'; if (fn) b.onclick = safe(fn); return b; };
    const iconButton = (label: any,kind: any,fn: any): any => {const b=button(label,fn,'quiet message-icon-button');b.title=tr(label);b.setAttribute('aria-label',tr(label));b.replaceChildren(notifications.icon(kind));return b;};
    const input = (placeholder: any, type: any = 'text'): any => { const e = node('input'); e.type = type; e.placeholder = tr(placeholder); e.setAttribute('aria-label', tr(placeholder)); return e; };
    const field = (label: any, control: any): any => { const e = node('label', 'message-field'); e.append(node('span','',tr(label)),control); return e; };
    const empty = (label: any): any => node('p', 'message-empty', tr(label));
    const head = center.querySelector('.reminder-center-head'), reminderBody = center.querySelector('.reminder-center-body'), createReminder: any = document.getElementById('reminder-create-toggle');
    const showSettings = (): any => openSettings?.('notifications');
    const tabs = node('div','message-tabs'); tabs.setAttribute('role','tablist'); tabs.setAttribute('aria-label',tr('消息'));
    const panes: any = {}, tabButtons: any = {};
    for (const [id,label] of [['chats','会话'],['contacts','联系人'],['files','传输'],['reminders','提醒']]) {
      const b = button(label, () => selectTab(id)); b.setAttribute('role','tab'); b.id='message-tab-'+id; b.setAttribute('aria-controls','message-pane-'+id); tabButtons[id]=b; tabs.append(b);
      const pane = id === 'reminders' ? reminderBody : node('section','message-pane'); pane.id='message-pane-'+id; pane.setAttribute('role','tabpanel'); pane.setAttribute('aria-labelledby',b.id); panes[id]=pane;
    }
    head.after(tabs); for (const id of ['chats','contacts','files']) reminderBody.before(panes[id]);
    let settingsBody: any = null, notificationsBody: any = null, groupsOpen = true;
    const seenEvents = new Set<any>(), reading = new Set<any>();let receivedBaseline = false, notificationAudio: any = null, disposed = false, pollSerial = 0;
    const notificationButton=button('通知与角标',showSettings,'quiet message-notification-settings-button');notificationButton.setAttribute('aria-label',tr('通知与角标'));notificationButton.title=tr('通知与角标');notificationButton.replaceChildren(notifications.icon('settings'));head.querySelector('.reminder-panel-actions').prepend(notificationButton);
    const callBar=node('section','message-call hidden');callBar.setAttribute('aria-live','polite');center.append(callBar);
    const status=node('p','message-status');status.setAttribute('role','status');head.after(status);
    const badge=node('span','message-unread-count hidden');document.getElementById('reminder-center-button')!.append(badge);const reminderBadge: any=document.querySelector('.reminder-count');if(reminderBadge)reminderBadge.hidden=true;
    let snapshot: any=null, activeTab='chats', selected: any=null, busy=false, epoch=0, digesting=false, lastDevice='', lastPaint='', activeVoice: any=null, older=100, viewDraft='', pollTimer: any=0;
    const files=new Map<any,any>(), uploads=new Set<any>(), paused=new Set<any>(), processed=new Set<any>();
    const data=(): any=>snapshot?.data, own=(): any=>data()?.device?.fingerprint||'', invitations=(): any=>Object.values(data()?.consent?.invitations||{}), relations=(): any=>data()?.consent?.relationships||{};
    const name=(peer: any): any=>data()?.peers?.[peer]?.name||data()?.peers?.[peer]?.device?.code||peer.slice(0,8);
    const groupAllowed=(g: any): any=>g.owner===own()|| (g.members?.[own()]&&data()?.consent?.invitations?.[g.invitation]?.status==='accepted'&&!relations()[g.owner]?.blocked);
    const pending=(): any=>invitations().filter((i: any)=>i.direction==='incoming'&&i.status==='pending'&&new Date(i.expires)>new Date()&&!relations()[i.peer]?.blocked);
    const visible=(): boolean=>document.body.classList.contains('reminders-mode')&&!isLocked();
    function selectTab(id: any): void {activeTab=id;for(const key of Object.keys(panes)){panes[key].hidden=key!==id;tabButtons[key].setAttribute('aria-selected',String(key===id));tabButtons[key].tabIndex=key===id?0:-1;}createReminder.hidden=id!=='reminders';paint(true);}
    tabs.onkeydown=(event: any): void=>{const keys=Object.keys(panes),index=keys.indexOf(activeTab);if(!['ArrowLeft','ArrowRight','Home','End'].includes(event.key))return;event.preventDefault();const next=event.key==='Home'?0:event.key==='End'?keys.length-1:(index+(event.key==='ArrowRight'?1:-1)+keys.length)%keys.length;selectTab(keys[next]);tabButtons[keys[next]].focus();};
    const notificationConfig=(): any=>snapshot?.notifications?.config;
    const notificationSummary=(): any=>snapshot?.notifications?.summary;
    const labelFor=(key: any): any=>key.startsWith('group:')?data()?.groups?.[key.slice(6)]?.name||tr('群组')+' '+key.slice(6,14):name(key);
    function updateBadge(): void {
      const summary=notificationSummary();if(!summary)return;
      const style=notificationConfig()?.settings.badge||'number';
      const label=tr('消息')+': '+summary.total+' · '+tr('单聊未读')+' '+summary.chats+' · '+tr('群聊未读')+' '+summary.groups+' · '+tr('申请')+' '+summary.contacts+' · '+tr('传输')+' '+summary.files+' · '+tr('未完成提醒')+' '+summary.reminders;
      notifications.badge(badge,summary.total,style,label);
      const entry: any=document.getElementById('reminder-center-button');entry.title=label;entry.setAttribute('aria-label',style==='off'?tr('消息'):label);
      for(const[id,count]of Object.entries({chats:summary.chats+summary.groups,contacts:summary.contacts,files:summary.files,reminders:summary.reminders})){
        let counter=tabButtons[id].querySelector('.message-unread-count');if(!counter){counter=node('span','message-unread-count');tabButtons[id].append(counter);}
        const title: any=({chats:'会话',contacts:'联系人',files:'传输',reminders:'提醒'} as any)[id];notifications.badge(counter,count,style,tr(title)+': '+count);counter.setAttribute('aria-hidden','true');tabButtons[id].setAttribute('aria-label',style==='off'?tr(title):tr(title)+': '+count);
      }
    }
    function prepareNotificationAudio(): void {if(!(notificationConfig()?.settings.sound)||notificationAudio?.state==='closed')return;try{const Audio=window.AudioContext||window.webkitAudioContext;if(!Audio)return;notificationAudio ||= new Audio();if(notificationAudio.state==='suspended')void notificationAudio.resume().catch(()=>{});}catch(_){}}
    window.addEventListener('pointerdown',prepareNotificationAudio,{passive:true});
    async function saveNotifications(change: any,revision: any,{preserveForm=false}={}): Promise<any> {
      if(change.settings?.sound){try{const Audio=window.AudioContext||window.webkitAudioContext;if(Audio){notificationAudio ||= new Audio();void notificationAudio.resume().catch(()=>{});}}catch(_){}}
      try{
        const config=await api('/messages/notifications',{method:'PUT',body:JSON.stringify({revision:revision??notificationConfig()?.revision??0,...change})});
        if(snapshot)snapshot.notifications.config=config;
        await refresh(true);if(!preserveForm)toast(tr('设置已保存'));
        if(!preserveForm&&notificationsBody?.isConnected)buildNotificationSettings();
        return config;
      }catch(error: any){if(error.status===409){await refresh(true);error.notificationConfig=await api('/messages/notifications').catch(()=>notificationConfig());}if(!preserveForm)toast(tr(error.message||'请求失败'));throw error;}
    }
    function buildNotificationSettings(): void {if(!notificationsBody||!notificationConfig()||isLocked())return;const config=notificationConfig();notifications.settingsForm({host:notificationsBody,config,tr,labelFor,save:(change: any,revision: any)=>saveNotifications(change,revision??config.revision,{preserveForm:true})});notificationsBody.dataset.notificationsReady='true';}
    function ruleControl(key: any) {const config=notificationConfig();return notifications.ruleControl({key,config,tr,save:(conversation: any)=>saveNotifications({conversation},config.revision)});}
    function addThread(host: any,key: any,label: any,description: any,select: any): void {
      const row=node('div','message-thread-row'),open=button(label,select,'message-thread'),copy=node('span','message-thread-copy');copy.append(node('strong','',label),node('small','',description));open.replaceChildren(copy);
      const count=notificationSummary()?.conversations?.[key];
      const indicator=node('span','message-unread-count');notifications.badge(indicator,count?.unread||0,count?.mode==='hidden'?'off':notificationConfig()?.settings.badge,tr('未读')+': '+(count?.unread||0),count?.mode==='mute');open.append(indicator);
      row.append(open,ruleControl(key));host.append(row);
    }
    async function markThrough(captured: any,through: any): Promise<void> {
      const key=notifications.keyOf(captured),id=key+':'+through;
      if(!through||reading.has(id))return;reading.add(id);
      try{await post('read',{peer:captured.peer||'',group:captured.group||'',through});await refresh();}catch(error: any){if(!isLocked())status.textContent=tr(error.message||'请求失败');}finally{reading.delete(id);}
    }
    function notifyNewEvents(): void {
      const config=notificationConfig();if(!config)return;
      const events=(data()?.messages||[]).filter((m: any)=>m.incoming).map((m: any)=>({id:'message:'+m.id,key:notifications.keyOf(m),kind:m.group?'groups':'chats'}));
      for(const inv of pending())events.push({id:'invitation:'+inv.id,key:inv.kind==='group'?'group:'+inv.resource:inv.kind==='voice'&&data()?.calls?.[inv.resource]?.group?'group:'+data().calls[inv.resource].group:inv.peer,kind:inv.kind==='file'?'files':'contacts'});
      const fresh=events.filter((e: any)=>!seenEvents.has(e.id));for(const event of events)seenEvents.add(event.id);
      if(seenEvents.size>16000){seenEvents.clear();for(const event of events)seenEvents.add(event.id);}
      if(!receivedBaseline){receivedBaseline=true;return;}
      if(isLocked()||window.LockCluster?.isSlave?.())return;
      let sound=false;
      const readingKey=visible()&&document.visibilityState==='visible'&&activeTab==='chats'&&selected?notifications.keyOf(selected):'';
      const batches=notifications.batches(config,fresh,readingKey);
      let delivered=0;
      for(const event of batches){
        const text=labelFor(event.key)+' · '+tr('收到新消息或申请')+(event.count>1?' × '+event.count:'');
        if(delivered<5){
          if(event.allow.popup&&document.visibilityState==='visible')toast(text);
          if(event.allow.desktop&&window.Notification?.permission==='granted'){try{new window.Notification('aide',{body:text,tag:'aide-message:'+event.key});}catch(_){}}
        }
        sound||=!!event.allow.sound;delivered++;
      }
      if(sound&&notificationAudio?.state==='running'){try{const wave=notificationAudio.createOscillator(),gain=notificationAudio.createGain();gain.gain.value=.035;wave.frequency.value=660;wave.connect(gain);gain.connect(notificationAudio.destination);wave.start();wave.stop(notificationAudio.currentTime+.12);wave.onended=(): void=>{wave.disconnect();gain.disconnect();};}catch(_){}}

    }
    function identityCard() {const e=node('div','message-identity');e.append(node('span','message-eyebrow',tr('我的设备码')));const row=node('div','message-row');row.append(node('strong','message-device-code',data()?.device?.code||'—'),button('复制设备码',async(): Promise<void>=>{await navigator.clipboard.writeText(data().device.code);toast(tr('已复制'));}));e.append(row);const more=node('details','message-fingerprint');more.append(node('summary','',tr('完整设备指纹')),node('code','',own()));e.append(more);return e;}
    function buildSettings(): void {if(!settingsBody)return;settingsBody.replaceChildren();const config=snapshot?.config||{enabled:false,name:'',network:{listen:':45840',lan:true,relays:[],advertise:[]}};const form=node('form','message-config-form');const enabled=input('启用设备通信','checkbox');enabled.checked=!!config.enabled&&!!config.name;enabled.disabled=!config.name;const label=field('启用设备通信',enabled);label.classList.add('message-checkbox');form.append(label,empty('开启后接收联系人申请；聊天、通话、群组和文件分别征得同意。'));
      form.append(field('账户名称',node('strong','message-account-name',config.name||tr('未设置'))));if(!config.name)form.append(empty('请先在设置 → 账户填写账户名称，才能启用设备通信。'),button('前往账户设置',()=>openSettings?.('account')));const assistant=input('允许助理读取和管理消息模块','checkbox');assistant.checked=!!config.assistantAccess;const assistantLabel=field('允许助理读取和管理消息模块',assistant);assistantLabel.classList.add('message-checkbox');form.append(assistantLabel,empty('默认关闭。读取的消息会交给当前助理模型处理；可发送文字、标记已读和管理拉黑，不会自动同意联系人、群组、通话或文件。'));
      const advanced=node('details');advanced.append(node('summary','',tr('高级网络设置')));const lan=input('局域网发现','checkbox');lan.checked=!!config.network?.lan;const lanLabel=field('局域网发现',lan);lanLabel.classList.add('message-checkbox');advanced.append(lanLabel);
      const listen=input('监听地址');listen.value=config.network?.listen||':45840';advanced.append(field('监听地址',listen));
      const area=(label: any,value: any)=>{const e=node('textarea');e.rows=2;e.value=(value||[]).join('\n');advanced.append(field(label,e));return e;};
      const relays=area('自托管中继（每行一个 HTTPS 地址）',config.network?.relays),advertise=area('公网地址提示（可选，每行一个）',config.network?.advertise),ice=area('STUN / TURN（可选，每行一个）',config.iceURLs);
      const username=input('TURN 用户名'),credential=input('TURN 密码（留空保留）','password');username.value=config.iceUsername||'';credential.autocomplete='new-password';advanced.append(field('TURN 用户名',username),field('TURN 密码（留空保留）',credential));form.append(advanced);
      const save=button('保存设置');save.type='submit';save.className='primary';form.append(save);form.onsubmit=safe(async(event: any): Promise<void>=>{event.preventDefault();save.disabled=true;epoch++;await stopVoice();status.textContent=tr('正在更新网络设置');try{const lines=(e: any)=>e.value.split(/\r?\n/).map((v: any)=>v.trim()).filter(Boolean);await api('/messages/config',{method:'PUT',body:JSON.stringify({config:{enabled:enabled.checked,assistantAccess:assistant.checked,network:{listen:listen.value.trim(),lan:lan.checked,relays:lines(relays),advertise:lines(advertise)},iceURLs:lines(ice),iceUsername:username.value},credential:credential.value})});credential.value='';await refresh(true);toast(tr('设置已保存')); }finally{save.disabled=false;}});form.append(button('通知与角标',showSettings));settingsBody.append(form);
    }
    function requestCard(inv: any) {const card=node('article','message-request');const kind: any=({contact:'联系人申请',group:'群组邀请',voice:'语音邀请',file:'文件邀请'} as any)[inv.kind]||inv.kind;card.append(node('strong','',tr(kind)),node('span','message-caption',name(inv.peer)));if(inv.kind==='file'){const f=data()?.transfers?.[inv.id];if(f)card.append(node('p','message-filename',f.manifest.name+' · '+bytes(f.manifest.size)));card.append(empty('同意前不会发送文件内容。'));}if(inv.kind==='group'){const g=data()?.groups?.[inv.resource];if(g)card.append(node('p','',g.name));}const fingerprint=node('details','message-fingerprint');fingerprint.append(node('summary','',tr('完整设备指纹')),node('code','',inv.peer));card.append(fingerprint);const ops=node('div','message-row');ops.append(button(inv.kind==='voice'?'接听':'同意',async(): Promise<any>=>{if(inv.kind==='voice')return answerVoice(inv);await post('decide',{id:inv.id,accept:true});await refresh(true);},'primary'),button('拒绝',async(): Promise<void>=>{await post('decide',{id:inv.id,accept:false});await refresh(true);}));card.append(ops);return card;}
    function paintContacts(): void {const host=panes.contacts;host.replaceChildren(identityCard());const search=node('form','message-add-form');const code=input('输入 6 位设备码');code.maxLength=6;code.autocomplete='off';code.spellcheck=false;code.className='message-code-input';const find=button('查找设备');find.type='submit';search.append(code,find);const results=node('div','message-discovery');search.onsubmit=safe(async(event: any): Promise<void>=>{event.preventDefault();find.disabled=true;results.replaceChildren();try{const cards=await post('discover',{code:code.value.trim().toUpperCase()});if(!cards.length)results.append(empty('未找到设备，请确认对方在线且使用相同中继或同一局域网。'));if(cards.length>1)results.append(empty('同码设备有多个，请核对完整指纹后选择。'));for(const card of cards){const item=node('article','message-request');item.append(node('strong','',card.name),node('code','message-full-fingerprint',card.device.fingerprint),empty('请通过其他可信渠道核对完整指纹。'));const checked=input('已核对完整指纹','checkbox');const checkedLabel=field('已核对完整指纹',checked);checkedLabel.classList.add('message-checkbox');const add=button('申请添加',async(): Promise<void>=>{await post('invite',{peer:card.device.fingerprint,card,kind:'contact'});toast(tr('申请已发出'));results.replaceChildren();await refresh(true);},'primary');add.disabled=true;checked.onchange=(): void=>{add.disabled=!checked.checked;};item.append(checkedLabel,add);results.append(item);}}finally{find.disabled=false;}});host.append(node('h3','',tr('添加联系人')),search,results,node('h3','',tr('待处理申请')));
      const requests=pending();if(!requests.length)host.append(empty('暂无待处理申请'));for(const inv of requests)host.append(requestCard(inv));
      const all=Object.entries<any>(relations());host.append(node('h3','',tr('联系人')));if(!all.length)host.append(empty('暂无联系人'));
      for(const [peer,rel]of all){const row=node('article','message-contact');const label=node('div');label.append(node('strong','',name(peer)),node('small','',rel.blocked?tr('已拉黑'):rel.localAccepted&&rel.remoteAccepted?data()?.peers?.[peer]?.device?.code||peer.slice(0,6):tr('等待对方同意')));row.append(label);if(rel.localAccepted&&rel.remoteAccepted&&!rel.blocked)row.append(button('开始聊天',(): void=>{selected={peer};selectTab('chats');}));row.append(button(rel.blocked?'解除拉黑':'拉黑',async(): Promise<void>=>{if(activeVoice?.peer===peer||activeVoice?.participants?.has(peer))await stopVoice();await post('block',{peer,blocked:!rel.blocked});if(rel.blocked)toast(tr('解除拉黑后需重新添加联系人。'));await refresh(true);}));host.append(row);}
      const outgoing=invitations().filter((i: any)=>i.direction==='outgoing'&&i.status==='pending'&&new Date(i.expires)>new Date());for(const inv of outgoing){const row=node('div','message-request');row.append(node('span','',name(inv.peer)+' · '+tr('等待对方同意')),button('重试',async(): Promise<void>=>{await post('refresh',{id:inv.id});await refresh(true);}),button('取消邀请',async(): Promise<void>=>{await post('cancel',{id:inv.id});await refresh(true);}));host.append(row);}
      groupForm(host);
    }
    function groupForm(host: any): void {const details=node('details','message-group-create');details.append(node('summary','',tr('创建群组')));const form=node('form');const title=input('群组名称');title.maxLength=60;form.append(field('群组名称',title),empty('选择 1–7 位联系人，逐人邀请。'));const choices: any[]=[];for(const[peer,rel]of Object.entries<any>(relations()))if(rel.localAccepted&&rel.remoteAccepted&&!rel.blocked){const check=input(name(peer),'checkbox');choices.push([peer,check]);const label=field(name(peer),check);label.classList.add('message-checkbox');form.append(label);}const submit=button('创建并邀请');submit.type='submit';submit.className='primary';form.append(submit);form.onsubmit=safe(async(e: any): Promise<void>=>{e.preventDefault();const peers=choices.filter(([,check])=>check.checked).map(([peer])=>peer);if(!peers.length||peers.length>7)throw new Error(uiTextMessageCenter('选择 1–7 位联系人，逐人邀请。'));submit.disabled=true;try{const group=await post('group',{name:title.value.trim(),peers});selected={group:group.id};selectTab('chats');await refresh(true);}finally{submit.disabled=false;}});details.append(form);host.append(details);}
    function paintChats(): void {const host=panes.chats;const previous=host.querySelector('.message-timeline');const previousView=previous?{key:previous.dataset.conversation,top:previous.scrollTop,bottom:previous.scrollHeight-previous.scrollTop-previous.clientHeight<28}:null;host.replaceChildren();if(!selected){
      const heading=node('div','message-section-heading');heading.append(node('h3','',tr('会话')),iconButton('添加联系人','add',()=>selectTab('contacts')),iconButton('全部消息标记已读','read',async(): Promise<void>=>{await post('read-all',{boundaries:data()?.readThrough||{}});await refresh(true);}));host.append(heading,empty('只显示已同意的联系人和群组。'));let count=0;
      for(const[peer,rel]of Object.entries<any>(relations()))if(rel.localAccepted&&rel.remoteAccepted&&!rel.blocked){addThread(host,peer,name(peer),data()?.peers?.[peer]?.device?.code||'',(): void=>{selected={peer};older=100;viewDraft='';paint(true);});count++;}
      const groups=Object.values<any>(data()?.groups||{}).filter(groupAllowed);if(groups.length){const wrapper=node('details','message-chat-groups');wrapper.open=groupsOpen;wrapper.ontoggle=(): void=>{groupsOpen=wrapper.open;};const title=node('summary','',tr('群组'));const counter=node('span','message-unread-count');const groupCounts=groups.map((g: any)=>notificationSummary()?.conversations?.['group:'+g.id]).filter((c: any)=>c&&c.mode!=='hidden');const total=groupCounts.reduce((n: any,c: any)=>n+c.unread,0);notifications.badge(counter,total,notificationConfig()?.settings.badge,tr('群聊未读')+': '+total,!groupCounts.some((c: any)=>c.mode!=='mute'&&c.unread>0));title.append(counter);wrapper.append(title);for(const group of groups){addThread(wrapper,'group:'+group.id,group.name,tr('群组')+' · '+Object.keys(group.members||{}).length,(): void=>{selected={group:group.id};older=100;viewDraft='';paint(true);});count++;}host.append(wrapper);}
      if(!count)host.append(empty('暂无联系人'));return;}
      const captured={...selected},group=captured.group?data()?.groups?.[captured.group]:null,rel=relations()[captured.peer];if((group&&!groupAllowed(group))||(!group&&(!rel?.localAccepted||!rel.remoteAccepted||rel.blocked))){selected=null;paintChats();return;}
      const toolbar=node('div','message-thread-heading');toolbar.append(button('返回会话',(): void=>{selected=null;viewDraft='';paint(true);}),node('strong','',group?.name||name(captured.peer)));host.append(toolbar);
      const ops=node('div','message-row');const voice=button(group?'群语音':'语音',()=>startVoice(captured));voice.disabled=!!activeVoice||(group&&group.owner!==own());if(group&&group.owner!==own())voice.title=tr('群主逐人邀请参加群语音');ops.append(voice);if(group&&group.owner!==own())ops.append(button('退出群组',async(): Promise<void>=>{if(activeVoice?.group===group.id)await stopVoice();try{await post('cancel',{id:group.invitation});}finally{selected=null;await refresh(true);}}));if(!group)ops.append(button('发送文件',()=>chooseFile(null,captured.peer)));ops.append(ruleControl(notifications.keyOf(captured)),iconButton('标记已读','read',()=>markThrough(captured,data()?.readThrough?.[notifications.keyOf(captured)])));host.append(ops);
      const messages=(data()?.messages||[]).filter((m: any)=>captured.group?m.group===captured.group:!m.group&&m.peer===captured.peer);const timeline=node('div','message-timeline');timeline.dataset.conversation=notifications.keyOf(captured);timeline.setAttribute('role','log');timeline.setAttribute('aria-label',tr('消息'));if(messages.length>older)timeline.append(button('显示更早消息',(): void=>{older+=100;paint(true);}));if(!messages.length)timeline.append(empty('暂无消息'));
      for(const message of messages.slice(-older)){const bubble=node('article','message-bubble'+(message.incoming?' incoming':' outgoing'));if(group&&message.incoming)bubble.append(node('small','',name(message.peer)));bubble.append(node('p','',message.text));const stamp=node('time','',new Date(message.at).toLocaleTimeString([],{hour:'2-digit',minute:'2-digit'}));stamp.dateTime=message.at;bubble.dataset.message=message.id;bubble.append(stamp);timeline.append(bubble);}host.append(timeline);
      const form=node('form','message-compose');const draft=node('textarea');draft.id='message-chat-draft';draft.rows=2;draft.maxLength=16000;draft.placeholder=tr('输入消息');draft.setAttribute('aria-label',tr('输入消息'));draft.value=viewDraft;draft.oninput=(): void=>{viewDraft=draft.value;};const send=button('发送');send.type='submit';send.className='primary';form.append(draft,send);form.onsubmit=safe(async(e: any): Promise<void>=>{e.preventDefault();if(!draft.value.trim())return;const text=draft.value;send.disabled=true;try{await post('text',{peer:captured.peer||'',group:captured.group||'',text});if(viewDraft===text)viewDraft='';await refresh(true);}catch(error){toast(tr('消息未确认送达，草稿已保留。'));throw error;}finally{send.disabled=false;}});draft.onkeydown=(e: any): void=>{if(e.key==='Enter'&&(e.ctrlKey||e.metaKey)){e.preventDefault();form.requestSubmit();}};host.append(form);
      const incoming=messages.filter((m: any)=>m.incoming),lastIncoming=incoming[incoming.length-1];const capturedEpoch=epoch;
      const markVisible=(): void=>{
        const readable=notifications.canRead({visible:document.visibilityState==='visible'&&visible(),active:activeTab==='chats'&&selected&&notifications.keyOf(selected)===notifications.keyOf(captured)&&capturedEpoch===epoch,locked:isLocked(),bottom:timeline.scrollHeight-timeline.scrollTop-timeline.clientHeight<28,connected:timeline.isConnected});
        if(!readable||!lastIncoming||!(data()?.unread?.[notifications.keyOf(captured)]>0))return;
        const bubble=[...timeline.querySelectorAll('.message-bubble')].find((e: any)=>e.dataset.message===lastIncoming.id);
        if(bubble&&bubble.getBoundingClientRect().bottom>timeline.getBoundingClientRect().top&&bubble.getBoundingClientRect().top<timeline.getBoundingClientRect().bottom)void markThrough(captured,lastIncoming.id);
      };
      timeline.addEventListener('scroll',markVisible,{passive:true});requestAnimationFrame(()=>{if(!timeline.isConnected)return;timeline.scrollTop=previousView?.key===timeline.dataset.conversation&&!previousView!.bottom?previousView!.top:timeline.scrollHeight;markVisible();});

    }
    function chooseFile(id: any,peer: any): void {if(digesting)throw new Error(uiTextMessageCenter('正在校验，请稍候'));const picker=document.createElement('input');picker.type='file';picker.onchange=safe(async(): Promise<void>=>{const file=picker.files?.[0];picker.remove();if(!file)return;const captured=epoch,device=own();digesting=true;try{const m=await manifest(file,(percent: any)=>{status.textContent=tr('计算文件校验')+' '+percent+'%';},()=>captured===epoch&&device===own()&&!isLocked());if(captured!==epoch||device!==own()||isLocked())return;if(id){const original=data()?.transfers?.[id];if(!original||!sameManifest(m,original.manifest))throw new Error(uiTextMessageCenter('原文件内容不一致，无法续传。'));files.set(id,file);paused.delete(id);}else{const result=await post('invite',{peer,kind:'file',manifest:m});id=result.invitation.id;files.set(id,file);if(result.deliveryPending)toast(tr('暂未确认送达，可稍后重试'));}selectTab('files');await refresh(true);}finally{digesting=false;status.textContent='';}});picker.hidden=true;picker.oncancel=()=>picker.remove();center.append(picker);picker.click();}
    async function upload(id: any): Promise<void> {if(uploads.size>=2||uploads.has(id)||paused.has(id)||!files.has(id)||isLocked())return;const transfer=data()?.transfers?.[id],inv=data()?.consent?.invitations?.[id];if(!transfer||transfer.cancelled||inv?.status!=='accepted')return;const captured=epoch,device=own(),file=files.get(id);uploads.add(id);try{await post('refresh',{id});await refresh();const confirmed=data()?.transfers?.[id]?.received||{};for(let index=0;index<transfer.manifest.hashes.length;index++){if(confirmed[index])continue;if(captured!==epoch||device!==own()||isLocked())throw new DOMException('Cancelled','AbortError');const current=data()?.consent?.invitations?.[id];if(current?.status!=='accepted'||data()?.transfers?.[id]?.cancelled||relations()[transfer.peer]?.blocked)throw new DOMException('Cancelled','AbortError');await api('/messages/chunks/'+encodeURIComponent(id)+'/'+index,{method:'PUT',headers:{'Content-Type':'application/octet-stream'},body:file.slice(index*CHUNK,(index+1)*CHUNK)});await refresh();}files.delete(id);}catch(error: any){paused.add(id);if(error.name!=='AbortError')toast(tr('传输已暂停，可重试或重新选择原文件。'));}finally{uploads.delete(id);await refresh(true);}}
    function paintFiles(): void {const host=panes.files;host.replaceChildren(node('h3','',tr('传输')),empty('同意前不会发送文件内容。'),empty('接收文件后不会自动打开或执行。'));const transfers=Object.values<any>(data()?.transfers||{}).sort((a: any,b: any)=>(new Date(b.created) as any)-(new Date(a.created) as any));if(!transfers.length)host.append(empty('暂无文件传输'));
      for(const f of transfers){const inv=data()?.consent?.invitations?.[f.id],count=Object.keys(f.received||{}).filter(key=>f.received[key]).length,complete=count===f.manifest.hashes.length;const card=node('article','message-transfer');card.append(node('strong','message-filename',f.manifest.name),node('small','',name(f.peer)+' · '+bytes(f.manifest.size)));const progress=node('progress');progress.max=Math.max(f.manifest.hashes.length,1);progress.value=complete?progress.max:count;progress.setAttribute('aria-label',tr('传输')+' '+f.manifest.name);card.append(progress,node('span','message-caption',f.cancelled||inv?.status==='revoked'?tr('已取消'):inv?.status==='pending'?tr('等待对方同意'):complete?tr('已完成'):tr(f.incoming?'接收中':'传输中')+' '+count+'/'+f.manifest.hashes.length));const ops=node('div','message-row');
        if(inv?.status==='pending'&&f.incoming){ops.append(button('同意',async(): Promise<void>=>{await post('decide',{id:f.id,accept:true});await refresh(true);},'primary'),button('拒绝',async(): Promise<void>=>{try{await post('decide',{id:f.id,accept:false});}finally{await refresh(true);}}));}
        if(!f.incoming&&inv?.status==='pending')ops.append(button('重试',async(): Promise<void>=>{await post('refresh',{id:f.id});await refresh(true);}));
        if(!f.incoming&&!complete&&inv?.status==='accepted'&&!f.cancelled){if(files.has(f.id))ops.append(button('继续传输',(): void=>{paused.delete(f.id);void upload(f.id);}));else ops.append(button('选择原文件续传',()=>chooseFile(f.id,f.peer)));}
        if(f.incoming&&complete&&inv?.status==='accepted'&&!f.cancelled){ops.append(button('保存到工作目录',()=>saveFileForm(f,card)));if(f.manifest.size<=64*CHUNK)ops.append(button('下载',()=>download(f)));}
        if(!f.cancelled&&inv?.status!=='revoked'&&inv?.status!=='declined')ops.append(button('取消',async(): Promise<void>=>{paused.add(f.id);files.delete(f.id);try{await post('cancel',{id:f.id});}finally{await refresh(true);}}));if(f.cleanupError){card.append(node('p','message-empty',f.cleanupError));ops.append(button('重试',async(): Promise<void>=>{try{await post('cancel',{id:f.id});}finally{await refresh(true);}}));}card.append(ops);host.append(card);
      }
    }
    function saveFileForm(file: any,card: any): void {const existing=card.querySelector('.message-save-form');if(existing){existing.querySelector('input')?.focus();return;}const form=node('form','message-save-form'),name=input('保存文件名');name.value=file.manifest.name.split(/[\\/]/).pop();name.maxLength=240;const save=button('保存文件');save.type='submit';save.className='primary';form.append(field('保存文件名',name),save,button('取消保存',()=>form.remove()));form.onsubmit=safe(async(event: any): Promise<void>=>{event.preventDefault();if(!name.value.trim())return;save.disabled=true;try{await post('save',{id:file.id,name:name.value});toast(tr('文件已保存到工作目录'));form.remove();}finally{save.disabled=false;}});card.append(form);name.focus();}
    async function download(file: any): Promise<void> {const response=await raw('/messages/download/'+encodeURIComponent(file.id),{method:'GET'});if(!response.ok){const error=await response.json();throw new Error(error.error||'请求失败');}const blob=await response.blob();if(blob.size!==file.manifest.size)throw new Error(uiTextMessageCenter('文件尚未接收完成'));const url=URL.createObjectURL(blob),link=node('a');link.href=url;link.download=file.manifest.name.split(/[\\/]/).pop();document.body.append(link);link.click();link.remove();setTimeout(()=>URL.revokeObjectURL(url),60000);}
    const voiceOwner=crypto.randomUUID().replace(/-/g,'');let voiceSerial=0;
    function prepareVoiceContext() {
      const Ctor=window.AudioContext||window.webkitAudioContext;
      if(!window.AideVoIP||!Ctor)return null;
      try{const context=new Ctor();void context.resume().catch(()=>{});return context;}catch(_){return null;}
    }
    async function startVoice(target: any): Promise<void> {
      if(activeVoice)throw new Error(uiTextMessageCenter('请先结束当前通话'));
      const captured=epoch,voiceContext=prepareVoiceContext();
      let id: any,invitation='';
      try{
        if(target.group){id=(await post('group-call',{group:target.group})).id;}
        else{const result=await post('invite',{peer:target.peer,kind:'voice'});id=result.invitation.resource;invitation=result.invitation.id;}
        if(captured!==epoch||isLocked())return;
        activeVoice={id,peer:target.peer||'',group:target.group||'',invitation,voiceContext,pcs:new Map(),audio:new Map(),participants:new Set(),serial:++voiceSerial,status:'等待接听'};
        paintCall();await refresh(true);
      }finally{
        if(voiceContext&&activeVoice?.voiceContext!==voiceContext&&voiceContext.state!=='closed')void voiceContext.close().catch(()=>{});
      }
    }
    async function answerVoice(inv: any): Promise<void> {
      if(activeVoice)throw new Error(uiTextMessageCenter('请先结束当前通话'));
      const captured=epoch,voiceContext=prepareVoiceContext();
      try{
        await post('decide',{id:inv.id,accept:true});
        await refresh(true);
        if(captured!==epoch||isLocked())return;
        const call=data()?.calls?.[inv.resource];
        activeVoice={id:inv.resource,peer:call?'':inv.peer,group:call?.group||'',invitation:inv.id,voiceContext,pcs:new Map(),audio:new Map(),participants:new Set(),serial:++voiceSerial,status:'连接中'};
        paintCall();await syncVoice();
      }finally{
        if(voiceContext&&activeVoice?.voiceContext!==voiceContext&&voiceContext.state!=='closed')void voiceContext.close().catch(()=>{});
      }
    }
    function voiceParticipants(call: any) {if(call.group){const record=data()?.calls?.[call.id];if(!record||record.ended||new Date(record.expires)<=new Date())return [];return Object.keys(record.members||{}).filter(peer=>peer!==own()&&!relations()[peer]?.blocked);}const inv=data()?.consent?.invitations?.[call.invitation];return inv?.status==='accepted'&&!relations()[call.peer]?.blocked&&new Date(inv.expires).getTime()+2*60*60*1000>Date.now()?[call.peer]:[];}
    function voicePermissionPayload(call: any) {const peers=voiceParticipants(call);return{owner:voiceOwner,call:call.id,peer:peers[0]||call.peer,invitation:call.group?call.id:call.invitation,group:call.group};}
    async function ensureMicrophone(call: any): Promise<void> {
      if(call.stream||call.starting)return;
      call.starting=true;
      const captured=epoch;
      const valid=(): boolean=>activeVoice===call&&captured===epoch&&!isLocked();
      let stream: any;
      try{
        const permission=voicePermissionPayload(call);
        await post('voice-lease',permission);
        if(!valid())return;
        call.leaseTimer=setInterval(()=>{
          if(activeVoice===call)post('voice-lease',voicePermissionPayload(call)).catch(()=>stopVoice());
        },5000);
        const route=window.AideVoIP?await api('/voip/settings'):null;
        if(!valid())return;
        if(route?.messages){
          call.phone=window.AideVoIP.open({mode:'messages',token:token(),permission,context:call.voiceContext,
            onStatus:(status: any)=>{if(valid()){call.status=status;paintCall();}},
            onEnd:(message: any)=>{if(valid()){if(message)toast(tr(message));void stopVoice();}}});
          await call.phone.ready;
          if(!valid())return;
          stream=call.phone.stream;
        }else{
          if(call.voiceContext){void call.voiceContext.close().catch(()=>{});call.voiceContext=null;}
          try{stream=await navigator.mediaDevices.getUserMedia({audio:{echoCancellation:true,noiseSuppression:true,autoGainControl:true},video:false});}
          catch(_){throw new Error('麦克风不可用或权限被拒绝');}
        }
        if(!valid())return;
        await post('voice-lease',voicePermissionPayload(call));
        const ice=await api('/messages/ice');
        if(!valid())return;
        call.stream=stream;call.ice=ice;call.ready=true;call.status='连接中';
        void syncVoice().catch((error: any)=>{if(valid()){void stopVoice();toast(tr(error.message));}});
      }catch(error: any){
        if(valid()){await stopVoice();toast(tr(error.message||'无法开启语音'));}
      }finally{
        if(call.stream!==stream)stream?.getTracks().forEach((track: any)=>track.stop());
        call.starting=false;
      }
    }
    async function sendSignal(call: any,peer: any,value: any): Promise<void> {if(activeVoice!==call||isLocked()||!call.participants.has(peer))return;await post('signal',{peer,invitation:call.group?call.id:call.invitation,call:call.id,group:call.group,data:value});}
    async function publishDescription(call: any,peer: any,entry: any): Promise<void> {
      // The answer can arrive before signed acceptance reaches the caller.
      // Retry the same description, then send ICE collected during that wait.
      const valid=(): boolean=>activeVoice===call&&!isLocked()&&call.pcs.get(peer)===entry;
      const description=entry.pc.localDescription.toJSON();
      for(let attempt=0;valid();attempt++){
        try{
          await sendSignal(call,peer,{description});
          entry.localSent=true;
          for(const candidate of entry.outbound.splice(0))await sendSignal(call,peer,{candidate}).catch(()=>{});
          return;
        }catch(error){
          if(attempt>=7)throw error;
          await new Promise<void>(resolve=>setTimeout(resolve,700));
        }
      }
    }
    function makePeer(call: any,peer: any) {if(call.pcs.has(peer))return call.pcs.get(peer);const pc=new RTCPeerConnection({iceServers:call.ice||[]});const entry={pc,candidates:[] as any[],outbound:[] as any[],localSent:false,making:false};call.pcs.set(peer,entry);for(const track of call.stream.getAudioTracks())pc.addTrack(track,call.stream);
      pc.onicecandidate=(event: any): void=>{
        if(!event.candidate)return;
        const candidate=event.candidate.toJSON();
        if(entry.localSent)void sendSignal(call,peer,{candidate}).catch(()=>{});
        else if(entry.outbound.length<128)entry.outbound.push(candidate);
      };
      pc.onnegotiationneeded=safe(async(): Promise<void>=>{
        if(own()>peer||entry.making||activeVoice!==call)return;
        entry.making=true;
        try{await pc.setLocalDescription(await pc.createOffer());await publishDescription(call,peer,entry);}
        finally{entry.making=false;}
      });
      pc.ontrack=(event: any): void=>{
        if(activeVoice!==call)return;
        let audio=call.audio.get(peer);
        if(!audio){
          audio=node('audio');audio.autoplay=true;audio.playsInline=true;audio.setAttribute('aria-label',name(peer));
          call.audio.set(peer,audio);callBar.querySelector('.message-call-media')?.append(audio);
        }
        // Chromium can deliver silence to Web Audio for a remote track that
        // is not attached to a playing media element. Keep local playout muted.
        audio.muted=!!call.phone;
        audio.srcObject=event.streams[0]||new MediaStream([event.track]);
        call.phone?.attach(peer,audio.srcObject);
        audio.play().catch(()=>{call.playRequired=true;paintCall();});
      };
      pc.onconnectionstatechange=(): void=>{if(activeVoice!==call)return;call.status=pc.connectionState==='connected'?'通话中':pc.connectionState==='failed'?'无法建立语音连接，请检查 STUN / TURN 设置。':'连接中';paintCall();};return entry;
    }
    async function processSignal(call: any,signal: any): Promise<void> {if(processed.has(signal.id)||!call.participants.has(signal.peer))return;processed.add(signal.id);while(processed.size>512)processed.delete(processed.values().next().value);const entry=makePeer(call,signal.peer);const value=signal.data;if(!value||typeof value!=='object')return;
      if(value.description){if(!['offer','answer'].includes(value.description.type)||typeof value.description.sdp!=='string')return;if(entry.pc.remoteDescription?.type===value.description.type&&entry.pc.remoteDescription.sdp===value.description.sdp)return;await entry.pc.setRemoteDescription(value.description);for(const candidate of entry.candidates)await entry.pc.addIceCandidate(candidate);entry.candidates=[];if(value.description.type==='offer'){await entry.pc.setLocalDescription(await entry.pc.createAnswer());await publishDescription(call,signal.peer,entry);}}
      else if(value.candidate){if(entry.pc.remoteDescription)await entry.pc.addIceCandidate(value.candidate);else if(entry.candidates.length<128)entry.candidates.push(value.candidate);}
    }
    async function syncVoice(): Promise<void> {const call=activeVoice;if(!call)return;if(isLocked()||!snapshot?.enabled){await stopVoice();return;}const inv=data()?.consent?.invitations?.[call.invitation];if(!call.group&&inv&&(['declined','revoked'].includes(inv.status)||(inv.status==='pending'&&new Date(inv.expires)<=new Date())||new Date(inv.expires).getTime()+2*60*60*1000<=Date.now())){await stopVoice();return;}if(call.group){const record=data()?.calls?.[call.id],memberInv=data()?.consent?.invitations?.[record?.invitation];if(record?.ended||new Date(record?.expires||0)<=new Date()||(record?.owner!==own()&&memberInv?.status!=='accepted')){await stopVoice();return;}}
      const participants=voiceParticipants(call);call.participants=new Set(participants);for(const[peer,entry]of call.pcs)if(!call.participants.has(peer)){entry.pc.close();call.pcs.delete(peer);call.phone?.detach(peer);const audio=call.audio.get(peer);if(audio){audio.pause();audio.srcObject=null;audio.remove();call.audio.delete(peer);}}
      if(!participants.length){if(call.group&&data()?.calls?.[call.id]?.owner===own()&&!invitations().some((i: any)=>i.kind==='voice'&&i.resource===call.id&&i.status==='pending'&&new Date(i.expires)>new Date())){await stopVoice();return;}paintCall();return;}if(!call.ready){void ensureMicrophone(call);return;}if(activeVoice!==call||!call.stream)return;for(const peer of participants)makePeer(call,peer);for(const signal of data()?.signals||[])if(signal.call===call.id&&(signal.group||'')===call.group)try{await processSignal(call,signal);}catch(error){call.status='无法建立语音连接，请检查 STUN / TURN 设置。';paintCall();}paintCall();
    }
    function paintCall(): void {if(!activeVoice){callBar.classList.add('hidden');callBar.replaceChildren();return;}callBar.classList.remove('hidden');const call=activeVoice;let controls=callBar.querySelector('.message-call-controls'),media=callBar.querySelector('.message-call-media');if(!media){media=node('div','message-call-media');callBar.append(media);}if(controls)controls.remove();controls=node('div','message-call-controls');controls.append(node('strong','',tr(call.status)),node('small','',call.group?data()?.groups?.[call.group]?.name||tr('群语音'):name(call.peer)));const ops=node('div','message-row');if(call.stream)ops.append(button(call.muted?'取消静音':'静音',(): void=>{call.muted=!call.muted;call.stream.getAudioTracks().forEach((track: any)=>{track.enabled=!call.muted;});paintCall();}));if(call.playRequired)ops.append(button('播放语音',async(): Promise<void>=>{await Promise.all([...call.audio.values()].map((audio: any)=>audio.play()));call.playRequired=false;paintCall();}));ops.append(button('结束通话',()=>stopVoice(),'message-end-call'));controls.append(ops);callBar.prepend(controls);for(const audio of call.audio.values())if(audio.parentNode!==media)media.append(audio);}
    async function stopVoice(): Promise<void> {const call=activeVoice;if(!call)return;activeVoice=null;voiceSerial++;clearInterval(call.leaseTimer);call.phone?.close();if(call.voiceContext)void call.voiceContext.close().catch(()=>{});call.stream?.getTracks().forEach((track: any)=>track.stop());for(const{pc}of call.pcs.values())pc.close();for(const audio of call.audio.values()){audio.pause();audio.srcObject=null;audio.remove();}paintCall();const ids=call.group?invitations().filter((i: any)=>i.kind==='voice'&&i.resource===call.id&&['pending','accepted'].includes(i.status)).map((i: any)=>i.id):[call.invitation];const requests=ids.filter(Boolean).map((id: any)=>post('cancel',{id},{keepalive:true}).catch(()=>{}));requests.push(post('voice-release',{owner:voiceOwner,call:call.id},{keepalive:true}).catch(()=>{}));await Promise.all(requests);}
    function paint(force=false): void {if(!snapshot)return;updateBadge();const key=JSON.stringify([snapshot.enabled,snapshot.networkError,data()?.consent,data()?.messages,data()?.transfers,data()?.groups,data()?.read,data()?.unread,snapshot.notifications,activeTab,selected]);if(!force&&key===lastPaint)return;status.replaceChildren();if(snapshot.networkError)status.append(node('span','',snapshot.networkError));else if(pending().length){status.append(node('span','',tr('待处理申请')+' '+pending().length),button('查看申请',()=>selectTab('contacts')));}if(!force&&panes[activeTab].querySelector('.message-notification-rule-form:not([hidden])'))return;if(!force&&lastPaint&&panes[activeTab].contains(document.activeElement)&&document.activeElement!.matches('input,textarea,select')&&document.activeElement!.id!=='message-chat-draft')return;lastPaint=key;if(!snapshot.enabled){for(const id of['chats','contacts','files'])panes[id].replaceChildren(empty('设备通信未启用'),empty('提醒功能始终可用；设备通信默认关闭。'),button('消息设置',showSettings,'primary'));return;}
      const focused: any=document.activeElement,selection=focused?.id==='message-chat-draft'?{start:focused.selectionStart,end:focused.selectionEnd}:null;if(activeTab==='contacts')paintContacts();if(activeTab==='files')paintFiles();if(activeTab==='chats')paintChats();if(selection){const draft: any=document.getElementById('message-chat-draft');draft?.focus({preventScroll:true});draft?.setSelectionRange(selection.start,selection.end);}
    }
    async function refresh(force=false): Promise<void> {if(disposed||busy||isLocked())return;busy=true;const capturedEpoch=epoch;try{const next=await api('/messages/state');if(isLocked()||capturedEpoch!==epoch)return;const previousRevision=notificationConfig()?.revision;snapshot=next;if(notificationsBody?.isConnected&&notificationsBody.dataset.notificationsReady!=='true')buildNotificationSettings();notifyNewEvents();if(previousRevision!==notificationConfig()?.revision)window.dispatchEvent(new Event('aide:notification-policy'));for(const[id]of files){const inv=data()?.consent?.invitations?.[id];if(inv&&['declined','revoked'].includes(inv.status)){files.delete(id);paused.delete(id);}}const device=own();if(lastDevice&&device&&lastDevice!==device){epoch++;files.clear();paused.clear();selected=null;void stopVoice();}if(device)lastDevice=device;paint(force);await syncVoice();}catch(error: any){if(!isLocked())status.textContent=tr(error.message||'请求失败');}finally{busy=false;}if(snapshot?.enabled&&!isLocked())for(const[id]of files)if(!uploads.has(id)&&!paused.has(id))void upload(id);}
    function privacyChange(): void {if(isLocked()){settingsBody?.replaceChildren();notificationsBody?.replaceChildren();if(notificationsBody)notificationsBody.dataset.notificationsReady='false';epoch++;for(const id of files.keys())paused.add(id);void stopVoice();snapshot=null;for(const id of['chats','contacts','files'])panes[id].replaceChildren();status.replaceChildren();badge.classList.add('hidden');}}
    const lock=document.getElementById('lock-screen');if(lock)new MutationObserver(privacyChange).observe(lock,{attributes:true,attributeFilter:['hidden','class']});window.addEventListener('pagehide',()=>{disposed=true;pollSerial++;epoch++;clearTimeout(pollTimer);if(notificationAudio)void notificationAudio.close().catch(()=>{});void stopVoice();files.clear();});document.addEventListener('visibilitychange',()=>{if(document.visibilityState==='visible')void refresh();});window.addEventListener('aide:language',()=>{tabs.setAttribute('aria-label',tr('消息'));for(const[id,label]of[['chats','会话'],['contacts','联系人'],['files','传输'],['reminders','提醒']])tabButtons[id].textContent=tr(label);paint(true);if(settingsBody?.isConnected)buildSettings();if(notificationsBody?.isConnected)buildNotificationSettings();});
    async function poll(): Promise<void> {const serial=pollSerial;if(disposed)return;await refresh();if(disposed||serial!==pollSerial)return;pollTimer=setTimeout(poll,document.visibilityState==='hidden'?15000:2000);}window.addEventListener('pageshow',(event: any)=>{if(event.persisted){disposed=false;pollSerial++;clearTimeout(pollTimer);notificationAudio=null;void poll();}});selectTab('chats');void poll();
    return {open:()=>refresh(true),refresh:()=>refresh(true),allowReminderPopup:()=>!isLocked()&&!!notificationConfig()?.settings.popup,renderNotifications:(host: any)=>{notificationsBody=host;void safe(async(): Promise<void>=>{await refresh(true);buildNotificationSettings();})();}, selectTab, stopVoice, renderSettings:(host: any)=>{settingsBody=host;void safe(async(): Promise<void>=>{await refresh(true);buildSettings();})();}};
  }
  function sameManifest(a: any,b: any): boolean {return a.size===b.size&&a.hashes.length===b.hashes.length&&a.hashes.every((hash: any,index: any)=>hash===b.hashes[index]);}
  window.AideMessageCenter=Object.freeze({mount,manifest,sameManifest});
})();

function uiTextMessageCenter(key: string, ...values: unknown[]): string { return globalThis.window?.aideI18n?.t(key, ...values) ?? String(key).replace(/\{(\d+)\}/g, (match, index) => index < values.length ? String(values[index]) : match); }
