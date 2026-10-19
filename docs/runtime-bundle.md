# 启动与 Release 镜像回退

仓库不再携带 Docker 镜像或 Git LFS 总包。普通 Git clone/pull 只同步源码和受控清单，不需要 Git LFS。

## 默认启动顺序

1. 校验本地配置镜像的 Docker 平台、image ID、源码身份和 CAD 完整性；有效则直接复用，再执行启动健康检查。不根据文件时间或任意 tar 名称选镜像。
2. 本地无完整镜像时，优先本地编译。若保留了依赖镜像或已校验本地依赖包，沿用本地恢复机制。
3. 编译或依赖准备失败时，按 `docker/prebuilt-images.tsv` 选择官方 Release 的唯一匹配架构；先复用已校验的缓存包，缺包才下载。校验完整 SHA-256、导入后 image ID、平台、源码和 CAD，再激活。
4. 回退也失败则停止，不迁移数据或把不完整包当成成功。回退成功会明确提示，实际版本以 Release 镜像中的版本为准，可能比工作区源码旧。

macOS 双击 `start.command`；Windows 双击 `start.bat`；Linux 执行 `bash start.sh`。原始完整离线 Release 包继续按包内 `.aide-image` 导入启动，无现场编译。

普通启动优先复用可用本地镜像，Git pull 不自动强制替换正在使用的镜像。源码更新后需要构建时：

```sh
AIDE_FORCE_REBUILD=1 bash start.command
```

Windows PowerShell：

```powershell
$env:AIDE_FORCE_REBUILD = '1'
.\start.ps1
```

`AIDE_BUILD_FROM_SOURCE=1` 也绕过新增的普通启动本地复用路径。构建失败后默认允许 Release 回退；要求严格源码构建时设置 `AIDE_RELEASE_FALLBACK=0`，失败直接停止。`AIDE_BUILD_FROM_SOURCE=0` 显式选择原有镜像模式。

镜像、下载包与用户数据分开；本次不修改用户会话、工作区、凭据或生产镜像。Release 附件校验与镜像实际运行验收是独立步骤。Windows 原生启动及其他设备接收验收不能由静态检查代替。

## 从曾启用 Git LFS 的旧检出升级

main 已删除总包、LFS 属性与制作入口。新的普通克隆不会请求旧总包。旧设备可以在安装了 Git LFS 时执行 `git lfs uninstall --local` 撤下旧本地过滤器与钩子；不要把仍在使用的自定义 Git 钩子一起删除。历史提交中的 LFS 指针和服务端历史对象不通过强制改写历史清除，本次不执行远端历史重写。

本地缓存中的上一份总包可删除；可运行镜像和单架构 Release 包仍可留作本地启动缓存，无需为了升级而清空 Docker。
