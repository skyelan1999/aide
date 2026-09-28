# 渲染测试文档

## 1. 文本样式

这是**加粗**、*斜体*、~~删除线~~、`inline code`。

## 2. 代码块

```go
package main

import "fmt"

func main() {
    fmt.Println("Hello, Aide!")
}
```

## 3. 列表

1. 有序项一
2. 有序项二
3. 有序项三

- 无序项 A
- 无序项 B

## 4. 表格

| 功能 | 状态 | 说明 |
|------|------|------|
| 流式输出 | ✅ | SSE 逐 token |
| 插话 | ✅ | Steer channel |
| 排队 | ✅ | Queue slice |

## 5. 引用

> 这是一段引用文字，用于测试 blockquote 渲染。

## 6. 链接和图片

[GitHub](https://github.com) — 外部链接

