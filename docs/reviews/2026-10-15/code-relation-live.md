# 代码关系词法绑定与真实 SFTP 验收

任务：[code-relation-live-20261015](../../tasks/code-relation-live-20261015.json)。基线 main `3bb8dcb9d52a2000284ed911e281bdd79052d9d3`。本轮延续代码关系可信度与证据路径目标，不代表总目标完成。

## 已复现的问题与修复

旧候选镜像 `236ad9bc3e3c` 在隔离 Docker 网络中通过真实 OpenSSH/SFTP 读取准备源码。JavaScript 与 Python 的 `entry(target)` 中调用参数 `target()`，均错误连接到模块同名函数；正常的 `positive()` 调用也存在。复现探针在错误边断言处失败，不能视为后续路径接口已通过。

解析器改为先收集作用域与绑定，再解析调用。参数、赋值、闭包、块作用域、JavaScript 变量提升及 Python global／nonlocal、推导式和模式捕获均参与遮蔽判断。支持的函数绑定用声明键与声明行匹配；导入携带当前作用域的路径及名称。可变或未知绑定不再回退连接无关声明。关系仍标为静态候选，不提升为类型证明。

JavaScript 输入改用唯一的显式模板标记，Go 将 JSON 字符串安全嵌入解析器程序。初次重排脚本破坏了旧的精确行文本替换，定向测试发现后已修复，并再次通过实际解析子进程测试。

## 当前验证

在 `aide:local` 的 Go 1.26.9 环境执行：

```sh
go test -mod=vendor -race ./internal/server \
  -run '^TestKnowledge(Code|GoTypes|GoType|Path)' -count=1 -timeout=180s
```

PASS，2.834 秒。测试从只读源码挂载运行，网络关闭，包含现有 Go 类型／路径回归，以及 JS／Python 遮蔽、有效本地调用、不同作用域同名导入、同名不同声明行、默认参数及接收者边界。源码只解析、不执行或安装依赖。私有原始证据保留于 `.agent-state/code-relation-live/`。

新版实际二进制 SFTP／路径／原文哈希验证、quick/full、运行实例与离线包、main 推送尚未完成，后续按实际结果追加。

## 限制与回滚

赋值采用保守判断，不进行执行顺序或控制流推断；eval、动态分派、第三方依赖、JS 默认导入与 TS／JSX 等原有限制未消除。候选边不证明运行时调用。真实用户 SSH、浏览器、规模与跨设备交付不属于本次已通过范围。

可回退本轮解析器及测试提交；保留上一候选镜像和离线包，不改用户来源、会话、设置或鉴权。继承的五项 drawio 厂商资源删除不归本任务，保留且不提交。
