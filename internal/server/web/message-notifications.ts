interface Window {
  AideMessageNotifications?: any;
}
(() => {
  'use strict';
  const make = (tag: string, text?: any, cls?: string): any => { const e=document.createElement(tag); if(text!=null)e.textContent=text;if(cls)e.className=cls;return e; };
  const keyOf = (item: any): any => item.group ? 'group:'+item.group : item.peer;
  const mode = (config: any,key: any,now=Date.now()): any => {
    const rule=config?.conversations?.[key];
    return !rule || (rule.until && !(Date.parse(rule.until)>now)) ? 'default' : rule.mode;
  };
  function badge(element: any,count: any,style='number',label='',muted=false): void {
    const n=Math.max(0,Number(count)||0);
    element.classList.toggle('hidden',!n||style==='off');
    element.classList.toggle('is-dot',style==='dot');
    element.classList.toggle('is-muted',muted);
    element.textContent=style==='dot'?'':n>99?'99+':String(n);
    element.title=label; element.setAttribute('aria-label',label);
    element.dataset.count=String(n);
  }
  function canRead({visible,active,locked,bottom,connected}: any): boolean { return !!(visible && active && !locked && bottom && connected); }
  function channels(config: any,key: any,kind='chats',now=Date.now()) {
    const s=config?.settings||{};
    return mode(config,key,now)==='default' ? {popup:!!s.popup,sound:!!s.sound,desktop:!!s.desktop} : {};
  }
  function batches(config: any,events: any,readingKey='') {
    const grouped=new Map<any, any>();
    for(const event of events){
      if(event.key===readingKey)continue;
      const allow=channels(config,event.key,event.kind);
      if(!allow.popup&&!allow.sound&&!allow.desktop)continue;
      grouped.set(event.key,{...event,allow,count:(grouped.get(event.key)?.count||0)+1});
    }
    return [...grouped.values()];
  }
  function icon(kind='bell'): any {
    const svg=document.createElementNS('http://www.w3.org/2000/svg','svg');
    for(const [name,value] of Object.entries({viewBox:'0 0 24 24',fill:'none',stroke:'currentColor','stroke-width':'1.7','stroke-linecap':'round','stroke-linejoin':'round','aria-hidden':'true',focusable:'false'}))svg.setAttribute(name,value);
    const paths=kind==='settings'?['M4 6h4m4 0h8M4 18h8m4 0h4M8 3v6m8 6v6']:kind==='add'?['M9 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8','M2 21v-2a6 6 0 0 1 12 0v2m4-13v8m-4-4h8']:kind==='read'?['m3 12 5 5L19 6','m12 17 9-11']:kind==='close'?['m6 6 12 12M18 6 6 18']:['M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9','M10 21h4',...(kind==='muted'?['M3 3l18 18']:[])];
    for(const d of paths){const path=document.createElementNS('http://www.w3.org/2000/svg','path');path.setAttribute('d',d);svg.append(path);}return svg;
  }
  function ruleControl({key,config,save,tr}: any): any {
    const control=make('div',null,'message-notification-rule');
    const current=mode(config,key),label=tr(current==='default'?'通知设置':current==='mute'?'免打扰':'已屏蔽提示');
    const toggle=make('button',null,'quiet message-notification-toggle');toggle.type='button';toggle.title=label;toggle.setAttribute('aria-label',tr('会话通知设置')+' · '+label);toggle.setAttribute('aria-expanded','false');toggle.classList.toggle('is-suppressed',current!=='default');toggle.append(icon(current==='default'?'bell':'muted'));
    const form=make('form',null,'message-notification-rule-form');form.hidden=true;form.setAttribute('aria-label',tr('会话通知设置'));
    const close=(): void=>{form.hidden=true;toggle.setAttribute('aria-expanded','false');};
    toggle.onclick=(): void=>{form.hidden=!form.hidden;toggle.setAttribute('aria-expanded',String(!form.hidden));};
    const heading=make('div',null,'message-notification-rule-heading');const dismiss=make('button',null,'quiet message-notification-dismiss');dismiss.type='button';dismiss.title=tr('关闭');dismiss.setAttribute('aria-label',tr('关闭'));dismiss.append(icon('close'));dismiss.onclick=()=>{close();toggle.focus();};heading.append(make('strong',tr('会话通知设置')),dismiss);
    const field=(label: any,input: any): any=>{const item=make('label',null,'message-field');item.append(make('span',tr(label)),input);return item;};
    const choice=make('select');choice.setAttribute('aria-label',tr('提示方式'));
    for(const[value,label]of[['default','跟随全局'],['mute','免打扰'],['hidden','屏蔽全部提示']]){const option=make('option',tr(label));option.value=value;choice.append(option);}
    choice.value=current;
    const duration=make('select');duration.setAttribute('aria-label',tr('免打扰时长'));
    for(const[value,label]of[['0','永久'],['1','1 小时'],['8','8 小时']]){const option=make('option',tr(label));option.value=value;duration.append(option);}
    duration.disabled=current==='default';choice.onchange=()=>{duration.disabled=choice.value==='default';};
    const fields=make('div',null,'message-notification-rule-fields');fields.append(field('提示方式',choice),field('免打扰时长',duration));
    const info=make('small',tr('继续接收消息；免打扰保留灰色未读数，屏蔽隐藏角标。'));
    if(config.conversations?.[key]?.until && current!=='default')info.append(make('span',' '+new Date(config.conversations[key].until).toLocaleString()));
    const error=make('p',null,'message-empty');error.setAttribute('role','alert');const apply=make('button',tr('应用'),'primary');apply.type='submit';const actions=make('div',null,'message-notification-rule-actions');actions.append(apply);form.append(heading,fields,info,error,actions);
    form.onkeydown=(event: any): void=>{if(event.key==='Escape'){event.preventDefault();event.stopPropagation();close();toggle.focus();}};
    form.onsubmit=async(event: any): Promise<void>=>{event.preventDefault();apply.disabled=true;error.textContent='';try{await save({key,mode:choice.value,until:choice.value!=='default'&&Number(duration.value)?new Date(Date.now()+Number(duration.value)*3600000).toISOString():''});close();}catch(failure: any){error.textContent=failure.message;}finally{apply.disabled=false;}};
    control.append(toggle,form);return control;
  }
  function settingsForm({host,config,save,tr,labelFor}: any): any {
    const form=make('form',null,'message-config-form notification-config-form');
    const fields: Record<string, any>={};const add=(label: any,control: any): any=>{control.setAttribute('aria-label',tr(label));const field=make('label',null,'message-field');field.append(make('span',tr(label)),control);form.append(field);return control;};
    const select=(name: any,label: any,options: any,value: any): void=>{const input=make('select');for(const[v,l]of options){const option=make('option',tr(l));option.value=v;input.append(option);}input.value=value;fields[name]=add(label,input);};
    const check=(name: any,label: any,value: any): void=>{const input=make('input');input.type='checkbox';input.checked=!!value;fields[name]=add(label,input);input.parentNode.classList.add('message-checkbox');};
    const s=config.settings;
    select('badge','角标样式',[['number','数字'],['dot','仅红点'],['off','关闭']],s.badge);
    form.append(make('h4',tr('计入总角标的内容')));
    for(const[key,label]of[['chats','单聊未读'],['groups','群聊未读'],['contacts','联系人与群组申请'],['files','需处理的文件传输'],['reminders','未完成提醒']])check(key,label,s.categories[key]);
    select('reminderScope','提醒统计范围',[['all','全部未完成'],['due','仅到期与逾期']],s.reminderScope);
    check('includeMuted','免打扰会话计入总角标',s.includeMuted);
    form.append(make('h4',tr('新消息提示')));
    check('popup','应用内提示',s.popup);check('sound','提示音',s.sound);check('desktop','浏览器桌面通知',s.desktop);
    form.append(make('p',tr('关闭角标不会清除未读；查看提醒不会自动完成事项。声音和桌面通知默认关闭。'),'message-empty'));
    const error=make('p',null,'message-empty');error.setAttribute('role','alert');
    const status=make('p',null,'message-empty');status.setAttribute('role','status');status.setAttribute('aria-live','polite');status.hidden=error.hidden=true;form.append(status,error);
    let accepted=JSON.parse(JSON.stringify(s)),revision=config.revision,queue=Promise.resolve(),edit=0,conflictEpoch=0,permissionEpoch=0;
    let desktopAllowed=!!s.desktop;
    const restore=(): void=>{for(const[name,input]of Object.entries(fields)){
      if(name==='badge'||name==='reminderScope')input.value=accepted[name];
      else input.checked=name in accepted.categories?!!accepted.categories[name]:!!accepted[name];
    }desktopAllowed=!!accepted.desktop;};
    const read=(): any=>{const settings: any={badge:fields.badge.value,reminderScope:fields.reminderScope.value,categories:{},popup:fields.popup.checked,sound:fields.sound.checked,desktop:fields.desktop.checked&&desktopAllowed,includeMuted:fields.includeMuted.checked};
      for(const k of ['chats','groups','contacts','files','reminders'])settings.categories[k]=fields[k].checked;return settings;};
    // Share the revision and write order with suppression rules on this page.
    // A conflict invalidates queued stale snapshots; only a new user change
    // may write again after displaying the server's current values.
    const commit=(change: any): any=>{
      const epoch=conflictEpoch;
      const job=queue.catch(()=>{}).then(async()=>{
        if(epoch!==conflictEpoch)throw new Error(tr('设置已在其他窗口更改，请重新选择。'));
        try{
          const next=await save(change,revision);
          revision=next?.revision??revision+1;accepted=JSON.parse(JSON.stringify(next?.settings||change.settings||accepted));
          config.revision=revision;config.settings=JSON.parse(JSON.stringify(accepted));return next;
        }catch(failure: any){if(failure.status===409){conflictEpoch++;if(failure.notificationConfig){revision=failure.notificationConfig.revision;accepted=JSON.parse(JSON.stringify(failure.notificationConfig.settings));}}
          throw failure;
        }
      });queue=job.catch(()=>{});return job;
    };
    const changed=(): any=>{
      const version=++edit,settings=read();status.textContent=tr('正在保存…');status.hidden=false;error.textContent='';error.hidden=true;
      const job=commit({settings}).then(()=>{if(version===edit){status.textContent=tr('已保存');}}).catch((failure: any)=>{
        if(version===edit){restore();status.textContent='';status.hidden=true;error.hidden=false;error.textContent=failure.status===409?tr('设置已在其他窗口更改，请重新选择。'):tr('保存失败：{0}').replace('{0}',failure.message||tr('请求失败'));}
      });return job;
    };
    for(const input of Object.values(fields))input.onchange=changed;
    fields.desktop.onchange=async(): Promise<any>=>{
      const epoch=++permissionEpoch;
      if(!fields.desktop.checked||desktopAllowed)return changed();
      status.textContent=tr('等待浏览器通知授权…');status.hidden=false;error.textContent='';error.hidden=true;
      try{
        const allowed='Notification' in window&&await window.Notification.requestPermission()==='granted';
        if(epoch!==permissionEpoch||form.isConnected===false)return;
        if(!allowed)throw new Error(tr('浏览器未允许桌面通知。'));
        desktopAllowed=true;return changed();
      }catch(failure: any){if(epoch===permissionEpoch){fields.desktop.checked=false;desktopAllowed=false;status.textContent='';status.hidden=true;error.hidden=false;error.textContent=failure.message;}}
    };
    // Enter must not reload the page. Normal controls save on change.
    form.onsubmit=(event: any): void=>event.preventDefault();
    host.replaceChildren(form);
    const rules=make('section',null,'notification-suppressed-list');
    const paintRules=(): void=>{
      rules.replaceChildren(make('h4',tr('屏蔽与免打扰列表')));
      const keys=Object.keys(config.conversations||{}).filter(key=>mode(config,key)!=='default');
      if(!keys.length)rules.append(make('p',tr('暂无屏蔽或免打扰会话'),'message-empty'));
      for(const key of keys){const row=make('div',null,'message-notification-suppressed');row.append(make('span',labelFor(key)),ruleControl({key,config,tr,save:async(conversation: any)=>{const next=await commit({conversation});if(next?.conversations)config.conversations=next.conversations;paintRules();}}));rules.append(row);}
    };paintRules();
    host.append(rules);return form;
  }
  window.AideMessageNotifications=Object.freeze({keyOf,mode,badge,canRead,channels,batches,icon,ruleControl,settingsForm});
})();
