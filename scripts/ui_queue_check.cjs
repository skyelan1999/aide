// 无头浏览器验收 aide 排队/插话 UI（需要 playwright-core + CHROME_PATH；缺依赖时 SKIP）。
// 覆盖：运行中发送按钮可见、排队条渲染与操作、插话消息入时间线、live 状态不被队列操作重置。
// 用法：BASE_URL=... AIDE_TOKEN=<token> CHROME_PATH=<path> node scripts/ui_queue_check.cjs
'use strict';
// 排队/插话 UI 验收：运行中发送按钮可见、排队条渲染与操作、插话入时间线、live 不因队列操作重置。
const { chromium } = require('playwright-core');
const BASE = process.env.BASE_URL || 'http://127.0.0.1:18097';
const TOKEN = process.env.AIDE_TOKEN || '';
const CHROME = process.env.CHROME_PATH || '';

function fail(msg) { console.error('FAIL: ' + msg); process.exit(1); }

(async () => {
  const browser = await chromium.launch({ executablePath: CHROME, headless: true, args: ['--no-sandbox'] });
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  await page.goto(BASE, { waitUntil: 'load' });
  await page.evaluate(t => localStorage.setItem('aide-token', t), TOKEN);
  await page.reload({ waitUntil: 'load' });
  await page.waitForTimeout(1000);

  // 启动一个慢速 chat 任务
  await page.fill('#prompt', '队列验收任务');
  await page.click('#send');
  await page.waitForTimeout(1200);

  // 1) 运行中发送按钮必须可见（豆包原来把它隐藏了）
  const sendVisible = await page.evaluate(() => {
    const s = document.getElementById('send');
    return !s.classList.contains('hidden') && !s.disabled;
  });
  console.log('SEND_VISIBLE_WHILE_RUNNING:', sendVisible);
  if (!sendVisible) fail('运行中发送按钮被隐藏，无法排队/插话');
  const modeSwap = await page.evaluate(() => {
    const btn = document.getElementById('send');
    const rect = btn.getBoundingClientRect();
    const ta = document.getElementById('prompt').getBoundingClientRect();
    const it = document.querySelector('#sessions .session-item.active');
    const dot = it?.querySelector('.session-dot');
    return {
      stopMode: btn.classList.contains('stop-mode') && btn.textContent.includes('■'),
      cornerRight: rect.right <= ta.right && rect.right >= ta.right - 120,
      runningDotKept: !!it?.querySelector('.session-dot.dot-running'),
      dotBlinking: dot ? getComputedStyle(dot).animationName !== 'none' : false,
    };
  });
  console.log('SEND_MODE_SWAP_RUNNING:', JSON.stringify(modeSwap));
  if (!modeSwap.stopMode) fail('运行中发送箭头未切换为停止图标');
  if (!modeSwap.cornerRight) fail('按钮不在输入框右下角');
  if (!modeSwap.runningDotKept) fail('点击运行中会话后绿灯不应消失');
  if (!modeSwap.dotBlinking) fail('运行中绿灯应保持闪烁');

  // 2) 排队发送（运行中按钮已是停止态：用 Enter 提交排队）
  await page.evaluate(() => { state.queueMode = true; });
  await page.fill('#prompt', '排队验证消息');
  await page.press('#prompt', 'Enter');
  await page.waitForTimeout(1200);
  const queueState = await page.evaluate(() => ({
    barHidden: document.getElementById('queue-bar').classList.contains('hidden'),
    text: document.querySelector('#queue-bar .q-content')?.textContent || '',
    btns: [...document.querySelectorAll('#queue-bar .q-btn')].map(b => b.textContent),
  }));
  console.log('QUEUE_BAR:', JSON.stringify(queueState));
  if (queueState.barHidden || !queueState.text.includes('排队验证消息')) fail('排队条未显示排队消息');
  if (queueState.btns.length !== 3) fail('排队条缺少 修改/删除/插话 按钮');
  const codexStyle = await page.evaluate(() => ({
    head: document.querySelector('.queue-head')?.textContent || '',
    arrow: document.querySelector('.queue-item .q-arrow')?.textContent || '',
    hint: document.querySelector('.queue-hint')?.textContent || '',
  }));
  console.log('CODEX_STYLE:', JSON.stringify(codexStyle));
  if (!codexStyle.head.includes('排队消息') || codexStyle.arrow !== '↳' || !codexStyle.hint.includes('插话')) fail('队列条未按 Codex 风格渲染（标题/↳/提示行）');

  // 3) 排队操作后 live 流式状态不得被重置
  const liveAfter = await page.evaluate(() => {
    const run = state?.session?.runs?.[0];
    return (state?.live?.[run?.id] || '').length;
  });
  console.log('LIVE_LEN_AFTER_QUEUE_ACTION:', liveAfter);

  // 4) 插话发送 → 时间线出现 .steer-msg（Enter 提交）
  await page.evaluate(() => { state.queueMode = false; });
  await page.fill('#prompt', '插话验证消息');
  await page.press('#prompt', 'Enter');
  await page.waitForTimeout(1500);
  const steerState = await page.evaluate(() => ({
    steerMsgs: [...document.querySelectorAll('#timeline .steer-msg')].map(m => m.textContent),
  }));
  console.log('STEER_MSGS:', JSON.stringify(steerState));
  if (!steerState.steerMsgs.some(s => s.includes('插话验证消息'))) fail('插话消息未渲染进时间线');

  // 5) 等待任务完成：队列与插话都被消费，最终状态 completed
  let final = null;
  for (let i = 0; i < 40; i++) {
    await page.waitForTimeout(500);
    final = await page.evaluate(() => {
      const run = state?.session?.runs?.[0];
      return { status: run?.status, steers: run?.steers?.length || 0, answer: document.querySelector('#timeline .chat-answer')?.textContent?.slice(0, 40) || '' };
    });
    if (final.status && final.status !== 'running') break;
  }
  console.log('FINAL:', JSON.stringify(final));
  if (final.status !== 'completed') fail('任务终态异常：' + final.status);
  if (final.steers < 2) fail('插话记录缺失');
  // 状态点与排序：完成后当前会话显示蓝点；点击后点无色、加粗取消；
  // 非置顶会话中当前会话排最前（跟进/完成置顶；置顶不受影响）
  await page.waitForTimeout(600);
  const dot = await page.evaluate(() => {
    const it = document.querySelector('#sessions .session-item.active');
    const label = it?.querySelector('.session-label');
    const d = it?.querySelector('.session-dot');
    const others = [...document.querySelectorAll('#sessions .session-item:not(.active)')];
    const completedOthers = others.filter(o => o.querySelector('.session-dot.dot-done'));
    const firstNonPinned = [...document.querySelectorAll('#sessions .session-item:not(.pinned)')][0];
    return {
      activeNoDot: !it?.querySelector('.session-dot'), // 查看后高亮已持久清除
      activeWeight: label ? getComputedStyle(label).fontWeight : null,
      othersCompletedBold: completedOthers.length === 0 || completedOthers.every(o => getComputedStyle(o.querySelector('.session-label')).fontWeight === '800'),
      topAmongNonPinned: firstNonPinned === it, // 完成按时间置顶（与点击无关）
    };
  });
  console.log('STATUS_DOT:', JSON.stringify(dot));
  if (!dot.activeNoDot) fail('查看后完成会话的高亮未持久清除');
  if (dot.activeWeight !== '400') fail('查看后标题应为正常字重');
  if (!dot.othersCompletedBold) fail('未查看的完成会话应加粗 800');
  if (!dot.topAmongNonPinned) fail('完成会话未按时间置顶');

  // 6) 一键回底：滚离底部约一屏后按钮出现且真实可见（屏幕内）；点击平滑滚动回底并恢复自动跟随
  const jump = await page.evaluate(async () => {
    const c = document.getElementById('conversation');
    c.style.scrollBehavior = 'auto'; // 仅测试准备阶段瞬时定位
    const spacer = document.createElement('div');
    spacer.style.height = '3000px';
    document.getElementById('timeline').append(spacer); // 真实布局：内容在 timeline 内，按钮保持最后子元素
    c.scrollTop = c.scrollHeight;
    await new Promise(r => setTimeout(r, 150));
    // 距底部 0.4 屏：按钮不应出现
    c.scrollTop = c.scrollHeight - c.clientHeight * 1.4;
    await new Promise(r => setTimeout(r, 250));
    const btn = document.getElementById('jump-bottom');
    const hiddenNearBottom = btn.classList.contains('hidden');
    // 滚离约一屏以上：按钮出现且 rect 在会话可视区内（sticky 不随内容滚出屏幕）
    const convRect = c.getBoundingClientRect();
    c.scrollTop = c.scrollHeight - c.clientHeight * 2;
    await new Promise(r => setTimeout(r, 250));
    const rect = btn.getBoundingClientRect();
    const visibleOnScreen = !btn.classList.contains('hidden') && rect.bottom > convRect.top && rect.top < convRect.bottom;
    const centered = Math.abs((rect.left + rect.right) / 2 - (convRect.left + convRect.right) / 2) < 12;
    c.style.scrollBehavior = ''; // 点击应平滑滚动
    const startPos = c.scrollTop;
    btn.click();
    await new Promise(r => setTimeout(r, 100));
    const midScrollTop = c.scrollTop; // 100ms 采样：动画应在途中（短距离动画约 300ms 完成）
    await new Promise(r => setTimeout(r, 1300));
    return {
      hiddenNearBottom,
      visibleOnScreen,
      centered,
      smoothMid: midScrollTop > startPos + 40 && midScrollTop < c.scrollHeight - c.clientHeight - 10,
      atBottom: c.scrollHeight - c.scrollTop - c.clientHeight < 20,
      autoScroll: state.autoScroll,
      hiddenAfter: btn.classList.contains('hidden'),
    };
  });
  console.log('JUMP:', JSON.stringify(jump));
  if (!jump.hiddenNearBottom) fail('距底部不足一屏时按钮不应出现');
  if (!jump.visibleOnScreen) fail('滚离一屏后按钮未在屏幕内出现');
  if (!jump.centered) fail('按钮未水平居中');
  if (!jump.smoothMid) fail('点击回底未平滑滚动（瞬时跳底）');
  if (!jump.atBottom || !jump.autoScroll || !jump.hiddenAfter) fail('回底后状态异常');
  // 7) 停止：新任务运行中点击 ■ 取消
  await page.fill('#prompt', '停止验证任务');
  await page.press('#prompt', 'Enter');
  await page.waitForTimeout(1500);
  const stopState = await page.evaluate(() => ({
    stopMode: document.getElementById('send').classList.contains('stop-mode'),
    running: !!state?.session?.runs?.find(r => r.status === 'running'),
  }));
  console.log('STOP_PRE:', JSON.stringify(stopState));
  if (!stopState.stopMode || !stopState.running) fail('停止验证前置失败：任务未运行或按钮非停止态');
  await page.click('#send'); // 停止态点击 = 取消
  let cancelled = false;
  for (let i = 0; i < 30; i++) {
    await page.waitForTimeout(400);
    const st = await page.evaluate(() => state?.session?.runs?.some(r => r.status === 'cancelled'));
    if (st) { cancelled = true; break; }
  }
  console.log('STOP_RESULT:', JSON.stringify({ cancelled }));
  if (!cancelled) fail('点击停止图标未取消任务');
  const backToSend = await page.evaluate(() => !document.getElementById('send').classList.contains('stop-mode') && document.getElementById('send').textContent.includes('↑'));
  if (!backToSend) fail('任务结束后按钮未恢复为发送箭头');
  // 8) 会话管理：三个点菜单 → 图层（向下/向上）→ 置顶 / 归档 / 归档视图 / 取消归档
  await page.evaluate(async () => {
    state.showArchived = false;
    // 填充会话使列表超出容器高度，让底部项的菜单触发向上弹出（唯一标题避免历史数据污染）
    window.__fillPrefix = '填充' + Date.now() + '_';
    for (let i = 0; i < 6; i++) {
      await api('/sessions', { method: 'POST', body: JSON.stringify({ title: window.__fillPrefix + i }) });
    }
    await loadSessions();
  });
  const mgmt = await page.evaluate(async () => {
    const results = {};
    const listEl = document.getElementById('sessions');
    const cr = listEl.getBoundingClientRect();
    const menuItems = () => [...document.querySelectorAll('.session-menu .menu-item')];
    const findItemByTitle = t => [...document.querySelectorAll('#sessions .session-item')].find(it => it.querySelector('.session-label').textContent.trim() === t);
    const title = window.__fillPrefix + '5'; // 本 run 最新创建的填充会话（唯一标题，不受历史置顶排序影响）
    const hasTitle = t => [...document.querySelectorAll('#sessions .session-label')].some(l => l.textContent.trim() === t);
    const openMenu = async it => { it.querySelector('.session-more').click(); await new Promise(r => setTimeout(r, 200)); return document.querySelector('.session-menu'); }; // 菜单挂在 body
    const clickItem = async (label) => { menuItems().find(b => b.textContent.trim() === label).click(); await new Promise(r => setTimeout(r, 800)); };
    results.hasDots = !!findItemByTitle(title)?.querySelector('.session-more');
    const inViewport = r => r.top >= 0 && r.bottom <= window.innerHeight + 1 && r.left >= 0 && r.right <= window.innerWidth + 1;
    // 顶部项菜单：fixed 挂 body、完整在视口内且锚定在 ⋯ 按钮附近（向下）
    const firstItem = document.querySelector('#sessions .session-item');
    const m1 = await openMenu(firstItem);
    const mr1 = m1.getBoundingClientRect();
    const br1 = firstItem.querySelector('.session-more').getBoundingClientRect();
    results.menuInBody = m1.parentElement === document.body;
    results.menuDownVisible = inViewport(mr1) && mr1.top >= br1.top - 2 && mr1.top <= br1.bottom + 40;
    document.querySelectorAll('.session-menu').forEach(m => m.remove());
    // 底部项菜单：完整在视口内且锚定在按钮附近（有空间则向下）
    listEl.scrollTop = listEl.scrollHeight;
    await new Promise(r => setTimeout(r, 150));
    const lastItem = [...document.querySelectorAll('#sessions .session-item')].pop();
    const m2 = await openMenu(lastItem);
    const mr2 = m2.getBoundingClientRect();
    const br2 = lastItem.querySelector('.session-more').getBoundingClientRect();
    results.menuBottomVisible = inViewport(mr2) && mr2.top <= br2.bottom + 40 && mr2.bottom >= br2.top - 40;
    document.querySelectorAll('.session-menu').forEach(m => m.remove());
    // 强制向上分支：把按钮临时移到视口底部附近，菜单必须向上弹出且完整可见
    const btn = firstItem.querySelector('.session-more');
    const orig = btn.style.cssText;
    btn.style.position = 'fixed';
    btn.style.bottom = '5px';
    btn.style.right = '300px';
    btn.click();
    await new Promise(r => setTimeout(r, 250));
    const m3 = document.querySelector('.session-menu');
    const mr3 = m3.getBoundingClientRect();
    const br3 = btn.getBoundingClientRect();
    results.menuUpWhenNoRoom = inViewport(mr3) && mr3.bottom <= br3.top + 8;
    document.querySelectorAll('.session-menu').forEach(m => m.remove());
    btn.style.cssText = orig;
    listEl.scrollTop = 0;
    await new Promise(r => setTimeout(r, 150));
    // 置顶目标项（先复位历史置顶，再置顶；菜单项按成对状态校验）
    let target = findItemByTitle(title);
    let menu = await openMenu(target);
    const labels = menuItems().map(b => b.textContent.trim());
    results.menuOk = labels.includes('删除') && (labels.includes('归档') || labels.includes('取消归档')) && (labels.includes('置顶') || labels.includes('取消置顶'));
    if (labels.includes('取消置顶')) {
      await clickItem('取消置顶');
      target = findItemByTitle(title);
      menu = await openMenu(target);
    }
    await clickItem('置顶');
    results.pinned = findItemByTitle(title)?.classList.contains('pinned') === true;
    // 归档目标项 → 侧栏消失 → 设置「归档」窗格可见 → 从设置窗格恢复
    target = findItemByTitle(title);
    await openMenu(target);
    await clickItem('归档');
    results.goneFromDefault = !hasTitle(title);
    document.getElementById('brand-button').click();
    await new Promise(r => setTimeout(r, 700));
    [...document.querySelectorAll('#settings-nav .settings-nav-item')].find(b => b.textContent.trim() === '归档').click();
    await new Promise(r => setTimeout(r, 600));
    results.inSettingsArchive = [...document.querySelectorAll('.archived-title')].some(l => l.textContent.trim() === title);
    const row = [...document.querySelectorAll('#settings-content .archived-item')].find(r => r.querySelector('.archived-title')?.textContent.trim() === title);
    row?.querySelectorAll('button').forEach(btn => { if (btn.textContent.includes('恢复')) btn.click(); });
    await new Promise(r => setTimeout(r, 800));
    document.getElementById('settings-sheet-close').click();
    await new Promise(r => setTimeout(r, 400));
    results.backInDefault = hasTitle(title);
    return results;
  });
  console.log('SESSION_MGMT:', JSON.stringify(mgmt));
  if (mgmt.hasDots !== true) fail('会话项缺少三个点按钮');
  if (mgmt.menuDownVisible !== true) fail('顶部项菜单未锚定按钮或越出视口');
  if (mgmt.menuBottomVisible !== true) fail('底部项菜单越出视口或未锚定按钮');
  if (mgmt.menuUpWhenNoRoom !== true) fail('视口底部空间不足时菜单未向上弹出');
  if (mgmt.menuOk !== true) fail('三个点菜单缺少 置顶/归档/删除');
  if (mgmt.pinned !== true) fail('置顶未生效');
  if (mgmt.goneFromDefault !== true) fail('归档后会话仍在默认列表');
  if (mgmt.menuInBody !== true) fail('菜单未挂到 body（仍受侧栏裁切）');
  if (mgmt.inSettingsArchive !== true) fail('设置归档窗格未显示归档会话');
  if (mgmt.backInDefault !== true) fail('恢复后未回到会话列表');
  // 9) 设置面板：栏目顺序、归档窗格（归档列表 + 恢复 + 全部导出）
  // 先归档一个会话，验证设置窗格能看到并能恢复
  await page.evaluate(async () => {
    const list = await api('/sessions');
    if (list[0]) await api(`/sessions/${list[0].id}`, { method: 'PATCH', body: JSON.stringify({ archived: true }) });
  });
  // 强制中文界面，避免无头浏览器默认英文导致栏目名本地化
  await page.evaluate(() => { if (window.aideUI?.set) window.aideUI.set('language', 'zh-CN'); else if (window.aideUI) { try { window.aideUI.setAppearance(window.aideUI.get() || {}); } catch (e) {} } });
  await page.click('#brand-button');
  await page.waitForTimeout(700);
  const settings = await page.evaluate(async () => {
    const out = {};
    out.order = [...document.querySelectorAll('#settings-nav .settings-nav-item')].map(b => b.textContent.trim());
    [...document.querySelectorAll('#settings-nav .settings-nav-item')].find(b => b.textContent.trim() === '归档').click();
    await new Promise(r => setTimeout(r, 600));
    out.hasExportBtn = [...document.querySelectorAll('#settings-content button')].some(b => b.textContent.includes('全部导出'));
    out.hasRestoreBtn = [...document.querySelectorAll('#settings-content .archived-item button')].some(b => b.textContent.includes('恢复'));
    out.archivedCount = document.querySelectorAll('.archived-item').length;
    // 导出接口可用（含归档会话）
    const res = await fetch('/api/export', { headers: { Authorization: 'Bearer ' + state.token } });
    const data = await res.json();
    out.exportOk = res.ok && typeof data.count === 'number' && Array.isArray(data.sessions) && data.sessions.some(x => x.archived);
    // 通过设置窗格恢复
    [...document.querySelectorAll('#settings-content .archived-item button')].find(b => b.textContent.includes('恢复'))?.click();
    await new Promise(r => setTimeout(r, 700));
    out.restored = document.querySelectorAll('.archived-item').length === 0 || [...document.querySelectorAll('.archived-title')].length < out.archivedCount;
    document.getElementById('settings-sheet-close').click();
    return out;
  });
  console.log('SETTINGS:', JSON.stringify(settings));
  if (settings.order.join(',') !== '消耗统计,外观,语言,模型参数,归档,关于') fail('设置栏目顺序不符：' + settings.order.join(','));
  if (!settings.hasExportBtn) fail('归档窗格缺少全部导出按钮');
  if (!settings.exportOk) fail('全部导出接口不可用或未含归档会话');
  if (settings.hasRestoreBtn !== true) fail('归档窗格缺少恢复按钮');
  if (settings.restored !== true) fail('归档窗格恢复未生效');

  // 10) 菜单遮挡：打开菜单后元素采样必须落在菜单内部（图层在最顶）
  const occ = await page.evaluate(async () => {
    const first = document.querySelector('#sessions .session-item');
    first.querySelector('.session-more').click();
    await new Promise(r => setTimeout(r, 200));
    const menu = document.querySelector('.session-menu'); // 菜单挂在 body
    const r = menu.getBoundingClientRect();
    const el = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
    return { onTop: !!(el && menu.contains(el)), itemZ: getComputedStyle(first).zIndex };
  });
  console.log('MENU_OCCLUSION:', JSON.stringify(occ));
  if (!occ.onTop) fail('菜单仍被其他元素遮挡');
  await browser.close();
  console.log('PASS: 排队/插话 UI 流程');
})().catch(e => { console.error('FATAL', e); process.exit(1); });
