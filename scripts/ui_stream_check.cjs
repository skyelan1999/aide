'use strict';
// 无头浏览器验收 aide SSE 流式显示（DSH/Codex 风格）。
// 前置：npm i playwright-core（本机缓存亦可）；CHROME_PATH 指向 chromium/chrome-headless-shell。
// 用法：BASE_URL=http://127.0.0.1:18097 AIDE_TOKEN=<token> CHROME_PATH=<path> node scripts/ui_stream_check.cjs [tool]
//   chat 流程：思考点 → 流式文本+光标 → 完成持久化一致；
//   tool 流程：叙述流 → live 工具行 → 轮次重置 → 最终回答不含叙述。
const BASE = process.env.BASE_URL || 'http://127.0.0.1:18097';
const TOKEN = process.env.AIDE_TOKEN || '';
const CHROME = process.env.CHROME_PATH || '';
const MODE = process.argv[2] === 'tool' ? 'tool' : 'chat';

let chromium;
try {
  ({ chromium } = require('playwright-core'));
} catch (e) {
  console.log('SKIP: playwright-core 未安装（npm i playwright-core）');
  process.exit(0);
}
if (!CHROME) {
  console.log('SKIP: 未提供 CHROME_PATH');
  process.exit(0);
}

function fail(msg) { console.error('FAIL: ' + msg); process.exit(1); }

(async () => {
  const browser = await chromium.launch({ executablePath: CHROME, headless: true, args: ['--no-sandbox'] });
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  await page.goto(BASE, { waitUntil: 'load' });
  await page.evaluate(t => localStorage.setItem('aide-token', t), TOKEN);
  await page.reload({ waitUntil: 'load' });
  await page.waitForTimeout(1000);
  await page.fill('#prompt', MODE === 'tool' ? '带工具调用的流式验证' : '流式显示验证');
  await page.click('#send');

  let sawThinking = false, sawCursor = false, sawLive = false, sawTool = false, sawReset = false;
  let lastLive = '', prevRoundNum = undefined;
  for (let i = 0; i < 60; i++) {
    await page.waitForTimeout(250);
    const s = await page.evaluate(() => {
      const run = state?.session?.runs?.[0];
      const ans = document.querySelector('#timeline .chat-answer');
      const tool = document.querySelector('#timeline .live-tool');
      return {
        status: run?.status,
        live: state?.live?.[run?.id] || '',
        round: state?.liveRound?.[run?.id],
        hasCursor: !!ans?.querySelector('.stream-cursor'),
        hasThinking: !!ans?.querySelector('.thinking-dot'),
        tool: tool?.textContent || '',
      };
    });
    if (s.hasThinking) sawThinking = true;
    if (s.hasCursor && s.live.length > 0) sawCursor = true;
    if (s.live.length > 0) sawLive = true;
    if (s.tool.includes('⚒')) sawTool = true;
    // round 0 事件不带 round 字段（omitempty）：首个带编号轮次出现且此前已有 live 文本即视为重置
    if (typeof s.round === 'number' && s.round !== prevRoundNum && lastLive !== '') sawReset = true;
    if (typeof s.round === 'number') prevRoundNum = s.round;
    lastLive = s.live;
    if (s.status && s.status !== 'running' && i > 4) break;
  }
  const finalText = (await page.evaluate(() => document.querySelector('#timeline .chat-answer')?.textContent?.trim())) || '';
  const finalState = await page.evaluate(() => ({
    status: state?.session?.runs?.[0]?.status,
    toolUses: state?.session?.runs?.[0]?.toolUses?.length || 0,
  }));
  await browser.close();

  if (!sawLive) fail('未观察到任何 live 文本（EventSource 可能未连上）');
  if (!sawCursor) fail('流式期间未出现光标');
  if (MODE === 'chat') {
    if (!sawThinking) console.log('WARN: 未捕获思考点阶段（流太快时正常）');
    if (finalState.status !== 'completed') fail('chat 任务终态异常：' + finalState.status);
    if (!finalText) fail('最终回答为空');
  } else {
    if (!sawTool) fail('未显示 live 工具行');
    if (!sawReset) fail('未观察到轮次重置');
    if (finalState.toolUses < 1) fail('工具调用未落库');
    if (finalText.includes('让我')) fail('最终回答混入第 1 轮叙述：' + finalText);
  }
  console.log('PASS: ' + MODE + ' 流式显示（thinking=' + sawThinking + ' cursor=' + sawCursor + ' tool=' + sawTool + ' reset=' + sawReset + '）');
  console.log('FINAL: ' + finalText.slice(0, 60));
})().catch(e => { console.error('FATAL', e); process.exit(1); });
