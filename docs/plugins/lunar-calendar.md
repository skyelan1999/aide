# 农历日历插件

`lunar-calendar` 默认启用，向模型提供只读工具 `get_lunar_date`。省略 `date` 时查询中国标准时间（Asia/Shanghai）的今天；也可传 `YYYY-MM-DD` 查询指定公历日。

结果包含公历日期、农历年/月/日、闰月标记、干支年、生肖和易读农历日期，例如 `甲辰年正月初一`。换算由 Node.js Intl/ICU 内置的 Chinese calendar 完成本地计算，不请求网络。日期按中国标准时间解释；支持范围由运行时 ICU 农历数据决定。

普通 aide 会话沿用已有插件机制调用。小秘仅额外获得此农历插件和既有当前时间插件，不会因此访问其他工作区插件。

运行插件自测：

```sh
node plugins/lunar-calendar/test.js
```
