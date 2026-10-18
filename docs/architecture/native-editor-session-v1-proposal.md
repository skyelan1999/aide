# 原生编辑器会话 v1 · 最小宿主契约提案

状态：供BrdPlan最终准确契约接续的设计建议，未实现/未部署，不是现有插件能力。响应用户在BrdPlan中手画、微调、保存重开的要求。保留TestBench当前运行挂载、网络、导出能力和浏览器时段。

## 入口与身份

可信父层显示明确“在原生编辑器中打开”动作。插件只提供已知provider ID、已授权当前workspace中的project/session ID和要打开的原理图/PCB种类；不接受URL、端口、命令行、任意绝对路径或可执行文件。宿主从自己的注册provider配置与工程元数据派生文件、启动参数和受控视图地址，沿用当前workspace根/代次和权限，不以插件包安装根代替项目根。引用工程另须原有可见/可写锁，编辑器入口不绕过锁。

## 生命周期

1. editor.prepare：只读确认workspace/revision/project绑定、文件存在、当前内容版本、provider可用、授权与冲突状态。只给父层可审核的启动描述，不启动程序或授予写权限。
2. 用户父层明确打开：宿主创建editing lease，绑定provider、workspace/revision、工程相对文件集合和原内容摘要；记录生命周期及被阻止的AI写入口。调度/普通助手/工作台不能绕过此lease写同一工程。
3. editor.open：受控broker启动固定原生进程/专用viewer会话；返回非全局身份的opaque session/ticket。原生KiCad CLI/GUI为独立进程；其运行/第三方许可边界不混成插件核心链接。
4. 手工编辑：展示实际进程/会话状态，用户可保存。关闭浏览器tab、WebSocket断开、心跳失联或TTL到期都只能标未知/待恢复，不能自动解除AI写保护或宣称程序已关闭。
5. editor.finish：用户明确结束；broker确认所跟踪编辑进程/原生窗口会话已结束且文件稳定，宿主按原授权根重读工程文件与摘要。手工修改作为新版本采用，旧AI提案不能复用旧基线自动写回。若进程关闭无法确认，保持保护并提供人工恢复路径，不能强杀未保存程序。
6. workspace/lock/plugin撤销：立即撤销viewer/control能力、拒绝旧实例新动作；保留不确定编辑状态与文件恢复信息，不自动把未知进程当已退出。

## 视图隔离

不能把任意noVNC直接塞入现有frame-src/connect-src none的插件iframe，也不能简单把第三方viewer JS以Aide主origin服务，否则会扩大ambient权限。可选：独立已注册原生应用；或独立HTTPS/loopback origin的专用viewer，通过父层新用户动作打开。该origin和证书须走正常浏览器许可/安全检查，不以换端口/代理路径绕过拒绝。viewer只获本会话短期能力，不含Aide访问令牌、全局文件代理或其他工程权限。

若采用远程framebuffer，端点仅来自宿主provider配置，验证Origin与session绑定，端口不任意对外暴露，控制信道鉴权/撤销与只读观看分开。一次打开ticket不替代长期编辑lease，reload/reconnect应受相同scope审核。

## 最小验收

真实当前STM32工程手画/微调→保存→关闭→重开→内容读回；并验切区/锁屏/撤销/旧实例/只读引用拒绝、编辑中AI写回被阻止、断连不误解锁、重复打开和未知进程恢复。原生DRC/ERC或制造结果另验，不能用viewer打开成功替代设计合格。插件方须先交准确provider/session/文件和启动读回契约，再由唯一宿主负责人实现与统筹安排一次更新。

## 与BrdPlan准确契约的对接建议（2026-10-18，待接口冻结）

已读aide-pkg-BrdPlan/docs/integration/native-editor-contract-v1.md。可以复用其固定私网broker、只读token、精确配置sessionPath、view、generation和lease接口；不能把“同源capability路径”当作自动隔离。

建议请求字段冻结为父层POST /api/plugins/brdplan/editor/open body {protocol:1,requestId,sessionPath,view}，仅view=schematic|pcb。插件消息中的字段完全相同且只能让父层显示按钮。workspaceId/revision、package digest、绝对路径、brokerURL/token和project stem均由宿主捕获/派生；模型/插件不得传入覆盖。响应 {protocol:1,status:'ready',editorId,url,expiresAt}，url只允许宿主生成的/plugin-editor/<256bit-one-use-ticket>/；不要把broker回传的viewer字符串当任意URL导航。

若首版使用同源URL，必须为宿主拥有且版本固定的专用viewer页面，并通过该响应自身CSP sandbox allow-scripts（无allow-same-origin）使文档成为opaque origin，不能让viewer获得Aide主站localStorage或访问令牌。普通插件frame策略保持不变。只传固定session的RFB图像/键鼠协议，不代理服务端任意HTML或未审查脚本。viewer静态依赖由宿主固定版本分发、保留对应第三方NOTICE；单独设计受限WS能力握手（opaque origin不等于豁免鉴权），cap仅绑定该editor/workspace/revision，不能复用全局api bearer。专用独立origin或原生应用仍是可选安全边界；正式浏览器许可不得绕过。

推荐broker /lease/open输入仍为{generation,view,project}，但返回只接受已验证格式{leaseId,project,view,generation,expiresIn}；宿主固定重写WS到已注册broker的/websockify，不从viewer query构造任意后端目的地。/health和心跳不证明工程保存。编辑lease需要宿主可查询，AI普通工具/命令/提案/原生桥所有写入口都应看到同一工程编辑占用，不只靠页面的.lck提示。

/lease/close不能只以closed:true就默认为用户内容已保存。至少反馈{leaseId,state:'closed'|'closing'|'unknown',saved:'confirmed'|'unknown',generation}并提供重新读回后的文件版本/摘要。浏览器关闭或切区时先撤销WS控制；是否能无损关闭原生GUI由broker明确报告，未知时保留写保护和恢复信息，不能强制杀掉未保存窗口后立刻AI写回。旧制造/AI基线在任何手工修改后失效。

此处是建议，不代表接口已冻结或宿主已实现。需要BrdPlan先确认可满足opaque viewer/固定资源、持久编辑lease与关闭保存语义，再共同确定一次部署窗口。当前TestBench已通过的网络/凭据/镜像/导出能力不能回退覆盖。
