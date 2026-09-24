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
    return {
      stopMode: btn.classList.contains('stop-mode') && btn.textContent.includes('■'),
      cornerRight: rect.right <= ta.right && rect.right >= ta.right - 120,
    };
  });
  console.log('SEND_MODE_SWAP_RUNNING:', JSON.stringify(modeSwap));
  if (!modeSwap.stopMode) fail('运行中发送箭头未切换为停止图标');
  if (!modeSwap.cornerRight) fail('按钮不在输入框右下角');

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
  await browser.close();
  console.log('PASS: 排队/插话 UI 流程');
})().catch(e => { console.error('FATAL', e); process.exit(1); });
