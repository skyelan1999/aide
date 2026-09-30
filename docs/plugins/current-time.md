# 当前日期时间插件

`current-time` 是 aide 默认启用的轻量插件，提供 `get_current_datetime` 和 `get_week_number` 两个工具。它直接读取插件运行环境的系统时钟，不访问网络。

不传 `timeZone` 时，`date` 和 `time` 使用 aide 容器/运行进程的默认 IANA 时区，并在 `timeZone` 字段中明确返回该时区；`isoUtc` 始终提供 UTC ISO-8601 时间戳，`unixMs` 提供 Unix 毫秒时间。可传 `Asia/Shanghai` 等 IANA 时区覆盖本地显示；无效时区会返回错误。

两个工具都返回 ISO-8601 周历结果：星期一为一周开始，第 1 周是包含当年 1 月 4 日的周。结果包含 `weekYear`、`weekNumber`、`weekStart` 和 `weekEnd`。`get_current_datetime` 将周历放在 `isoWeek` 字段中；`get_week_number` 则专门用于直接查询当前周数，未传日期时按指定时区（或运行时本地时区）计算今天，也可传入 `YYYY-MM-DD` 计算任意日期。

普通 aide 会话通过现有插件工具机制调用它，因此专业任务可直接调用 `get_week_number` 获取当前周数，无需用户再提供日期。小秘也可调用这两个只读时间工具，但不会因此获得其它 aide 插件的工具权限。使用前应根据返回的时区解释本地日期，避免把 UTC 日期误认为用户当地日期。
