# aide Python 工具包

Python 工具以 ZIP 插件包导入，JS 入口负责向 aide 注册工具，再调用包内 Python 脚本。普通 `.js` 插件仍按原来的单文件方式上传。

## 包结构

```text
python-cad-tools/
├── manifest.json
├── index.js
└── worker.py
```

`manifest.json`：

```json
{"id":"python-cad-tools","name":"Python CAD 工具","version":"1.0.0","main":"index.js"}
```

JS 入口：

```js
module.exports = {
  apply(ctx) {
    ctx.tool({
      name: 'cad-inspect',
      description: '调用 Python 检查 CAD 数据',
      parameters: { type: 'object', properties: { path: { type: 'string' } }, required: ['path'] },
      async handler(args, api) {
        const stdout = await api.runPython('worker.py', args, { timeoutMs: 120000 });
        return { text: stdout.trim() };
      },
    });
  },
};
```

Python 脚本从 stdin 读取 JSON 参数，并把结构化结果写到 stdout：

```python
import json
import sys

request = json.load(sys.stdin)
print(json.dumps({"path": request["path"], "status": "inspected"}))
```

插件面板选择 ZIP 并上传。ZIP 可直接包含上述文件，也可把它们放在同一个顶层目录里。导入会校验 manifest 和 JavaScript 入口、拒绝路径穿越/链接条目、限制压缩包 24 MiB、解压总量 64 MiB、单文件 16 MiB、文件数 256。ZIP 中的 `requirements.txt` 只作为说明，不会自动安装；需要额外 Python 库时，先将固定版本加入 aide 开发镜像与 Release 镜像。

## 执行与输出

`api.runPython(script, input?, options?)` 只允许执行插件目录内的 Python 脚本，以 JSON stdin/stdout 传值，不经 shell。默认超时 60 秒，单次最多 280 秒；stdout 上限 8 MiB，stderr 最多保留 64 KiB。工作目录为容器 `/workspace`。插件与 Python 同属 aide 容器用户，能够使用该用户已有的文件权限；插件包必须来自可信来源。`-I` 不是操作系统权限沙箱。

需要交付新建或修改的文本文件时，插件应通过 `api.proposeWrite(path, content)` 返回 aide 提案，让工作流进入现有审批流程。DXF、ASCII STL、CSV、JSON、Markdown 等文本格式可直接采用此方式；大文件或二进制产物需要后续增加专门的工件 API。工作流可以连续调用工具，工作流总时限为 15 分钟。

源码开发模式与 Release 容器使用相同的 Python 3.12 和锁定依赖。开发启动器构建 Dockerfile 的 `dev` target；Release 默认 target 仍是 `runtime`。aide 不提供浏览器自动化或任意网页读取能力。
