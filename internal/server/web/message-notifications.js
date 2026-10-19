(() => {
  'use strict';
  const make = (tag, text, cls) => { const e=document.createElement(tag); if(text!=null)e.textContent=text;if(cls)e.className=cls;return e; };
  const keyOf = item => item.group ? 'group:'+item.group : item.peer;
  const mode = (config,key,now=Date.now()) => {
    const rule=config?.conversations?.[key];
    return !rule || (rule.until && !(Date.parse(rule.until)>now)) ? 'default' : rule.mode;
  };
  function badge(element,count,style='number',label='',muted=false) {
    const n=Math.max(0,Number(count)||0);
    element.classList.toggle('hidden',!n||style==='off');
    element.classList.toggle('is-dot',style==='dot');
    element.classList.toggle('is-muted',muted);
    element.textContent=style==='dot'?'':n>99?'99+':String(n);
    element.title=label; element.setAttribute('aria-label',label);
    element.dataset.count=String(n);
  }
  function canRead({visible,active,locked,bottom,connected}) { return !!(visible && active && !locked && bottom && connected); }
  function channels(config,key,kind='chats',now=Date.now()) {
    const s=config?.settings||{};
    return mode(config,key,now)==='default' ? {popup:!!s.popup,sound:!!s.sound,desktop:!!s.desktop} : {};
  }
  function batches(config,events,readingKey='') {
    const grouped=new Map();
    for(const event of events){
      if(event.key===readingKey)continue;
      const allow=channels(config,event.key,event.kind);
      if(!allow.popup&&!allow.sound&&!allow.desktop)continue;
      grouped.set(event.key,{...event,allow,count:(grouped.get(event.key)?.count||0)+1});
    }
    return [...grouped.values()];
  }
  function icon(kind='bell') {
    const svg=document.createElementNS('http://www.w3.org/2000/svg','svg');
    for(const [name,value] of Object.entries({viewBox:'0 0 24 24',fill:'none',stroke:'currentColor','stroke-width':'1.7','stroke-linecap':'round','stroke-linejoin':'round','aria-hidden':'true',focusable:'false'}))svg.setAttribute(name,value);
    const paths=kind==='settings'?['M4 6h4m4 0h8M4 18h8m4 0h4M8 3v6m8 6v6']:kind==='add'?['M9 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8','M2 21v-2a6 6 0 0 1 12 0v2m4-13v8m-4-4h8']:kind==='read'?['m3 12 5 5L19 6','m12 17 9-11']:kind==='close'?['m6 6 12 12M18 6 6 18']:['M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9','M10 21h4',...(kind==='muted'?['M3 3l18 18']:[])];
    for(const d of paths){const path=document.createElementNS('http://www.w3.org/2000/svg','path');path.setAttribute('d',d);svg.append(path);}return svg;
  }
  function ruleControl({key,config,save,tr}) {
    const control=make('div',null,'message-notification-rule');
    const current=mode(config,key),label=tr(current==='default'?'通知设置':current==='mute'?'免打扰':'已屏蔽提示');
    const toggle=make('button',null,'quiet message-notification-toggle');toggle.type='button';toggle.title=label;toggle.setAttribute('aria-label',tr('会话通知设置')+' · '+label);toggle.setAttribute('aria-expanded','false');toggle.classList.toggle('is-suppressed',current!=='default');toggle.append(icon(current==='default'?'bell':'muted'));
    const form=make('form',null,'message-notification-rule-form');form.hidden=true;form.setAttribute('aria-label',tr('会话通知设置'));
    const close=()=>{form.hidden=true;toggle.setAttribute('aria-expanded','false');};
    toggle.onclick=()=>{form.hidden=!form.hidden;toggle.setAttribute('aria-expanded',String(!form.hidden));};
    const heading=make('div',null,'message-notification-rule-heading');const dismiss=make('button',null,'quiet message-notification-dismiss');dismiss.type='button';dismiss.title=tr('关闭');dismiss.setAttribute('aria-label',tr('关闭'));dismiss.append(icon('close'));dismiss.onclick=()=>{close();toggle.focus();};heading.append(make('strong',tr('会话通知设置')),dismiss);
    const field=(label,input)=>{const item=make('label',null,'message-field');item.append(make('span',tr(label)),input);return item;};
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
    form.onkeydown=event=>{if(event.key==='Escape'){event.preventDefault();event.stopPropagation();close();toggle.focus();}};
    form.onsubmit=async event=>{event.preventDefault();apply.disabled=true;error.textContent='';try{await save({key,mode:choice.value,until:choice.value!=='default'&&Number(duration.value)?new Date(Date.now()+Number(duration.value)*3600000).toISOString():''});close();}catch(failure){error.textContent=failure.message;}finally{apply.disabled=false;}};
    control.append(toggle,form);return control;
  }
  function settingsForm({host,config,save,tr,labelFor}) {
    const form=make('form',null,'message-config-form notification-config-form');
    const fields={};const add=(label,control)=>{const field=make('label',null,'message-field');field.append(make('span',tr(label)),control);form.append(field);return control;};
    const select=(name,label,options,value)=>{const input=make('select');for(const[v,l]of options){const option=make('option',tr(l));option.value=v;input.append(option);}input.value=value;fields[name]=add(label,input);};
    const check=(name,label,value)=>{const input=make('input');input.type='checkbox';input.checked=!!value;fields[name]=add(label,input);input.parentNode.classList.add('message-checkbox');};
    const s=config.settings;
    select('badge','角标样式',[['number','数字'],['dot','仅红点'],['off','关闭']],s.badge);
    form.append(make('h4',tr('计入总角标的内容')));
    for(const[key,label]of[['chats','单聊未读'],['groups','群聊未读'],['contacts','联系人与群组申请'],['files','需处理的文件传输'],['reminders','未完成提醒']])check(key,label,s.categories[key]);
    select('reminderScope','提醒统计范围',[['all','全部未完成'],['due','仅到期与逾期']],s.reminderScope);
    check('includeMuted','免打扰会话计入总角标',s.includeMuted);
    form.append(make('h4',tr('新消息提示')));
    check('popup','应用内提示',s.popup);check('sound','提示音',s.sound);check('desktop','浏览器桌面通知',s.desktop);
    form.append(make('p',tr('关闭角标不会清除未读；查看提醒不会自动完成事项。声音和桌面通知默认关闭。'),'message-empty'));
    const error=make('p',null,'message-empty');error.setAttribute('role','alert');const apply=make('button',tr('保存设置'),'primary');apply.type='submit';form.append(error,apply);
    form.onsubmit=async event=>{event.preventDefault();apply.disabled=true;try{
      if(fields.desktop.checked && !s.desktop){if(!('Notification' in window)||await window.Notification.requestPermission()!=='granted'){fields.desktop.checked=false;throw new Error(tr('浏览器未允许桌面通知，其他设置尚未保存。'));}}
      const settings={badge:fields.badge.value,reminderScope:fields.reminderScope.value,categories:{},popup:fields.popup.checked,sound:fields.sound.checked,desktop:fields.desktop.checked,includeMuted:fields.includeMuted.checked};
      for(const k of ['chats','groups','contacts','files','reminders'])settings.categories[k]=fields[k].checked;
      await save({settings});
    }catch(failure){error.textContent=failure.message;}finally{apply.disabled=false;}};
    host.replaceChildren(form);
    const rules=make('section',null,'notification-suppressed-list');rules.append(make('h4',tr('屏蔽与免打扰列表')));
    const keys=Object.keys(config.conversations||{}).filter(key=>mode(config,key)!=='default');
    if(!keys.length)rules.append(make('p',tr('暂无屏蔽或免打扰会话'),'message-empty'));
    for(const key of keys){const row=make('div',null,'message-notification-suppressed');row.append(make('span',labelFor(key)),ruleControl({key,config,tr,save:conversation=>save({conversation})}));rules.append(row);}
    host.append(rules);return form;
  }
  window.AideMessageNotifications=Object.freeze({keyOf,mode,badge,canRead,channels,batches,icon,ruleControl,settingsForm});
})();
