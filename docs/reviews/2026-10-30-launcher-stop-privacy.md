# #640 Safari 停止隐私验收（2026-10-30）

代码提交：`43e80b45`，本地 quick 内容指纹：`6bdbc2800e6c365d6c1df7993b271a3dfedc4810da69a67961f162d0bc75ec65`。通过 Safari 原生窗口执行，使用当前生产 `runtime-lifecycle.js` 和从 `app.js` 原样提取的 `setupGlobalEvents()`，在独立回环 HTTP 页面接收真实 SSE。页面、令牌、会话和文件名全部为模拟内容，未停止或替换已有 Aide。

## 实际观察

1. 普通关页：新建测试标签显示“模拟敏感会话内容：停止后这一行必须消失”和“SSE 已连接，等待停止”。点击“模拟停止当前测试服务”，服务发送 `runtime-stopping`；Safari 实际关闭测试标签，标签数量从六个回到原有五个，模拟内容不再显示。
2. 拒绝关页：第二个独立测试页显式令 `window.close()` 抛出异常，其他代码不变。点击停止后实际转到 `about:blank`；AX 树仅有空白 HTML 内容，截图显示空白页面，没有模拟会话内容。
3. 第二页离开时通过本地 beacon 返回以下收据，证明导航前文档和认证状态已经清空，且已调用其余清理边界。第一轮标签关闭没有收到 beacon，不据此推断其内存字段状态。

```json
{
  "tokenCleared": true,
  "sessionCleared": true,
  "fileCleared": true,
  "documentCleared": true,
  "storedTokenCleared": true,
  "draftPreserved": true,
  "disposed": [
    "stream",
    "command",
    "call",
    "live",
    "voice"
  ]
}
```

测试结束后关闭第二轮自建空白标签，两个临时 HTTP 服务均通过其各自执行会话停止并返回退出码 0。已有用户标签和运行中的 `9999` 保留。

## 证据边界

这是生产浏览器事件处理器和隐私模块的真实 Safari 组件验收；语音、命令及任务对象是可观察的模拟边界。服务端事件优先级、取消和新请求拒绝另由真实 HTTP/SSE Go race 测试覆盖。它不等于完整产品镜像的端到端 stop 验收，也不建立 Windows Docker Desktop、Windows ARM64 实机或其他浏览器的行为。以上路径仍须平台验收，#640 保持打开。

## 生产输入摘要

- `internal/server/web/runtime-lifecycle.js`: `880aa29eb28de4e7d2f8a564283c2ad6b4ea19024dbdbb1ec5939228f20753ca`
- `internal/server/web/app.js`: `14984f0943ecedd46c81b498e5fa8c45a6ca72a9619244b61547ead070a03763`
