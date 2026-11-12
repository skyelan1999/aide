# 运行包、显式构建与恢复

按 2026-11-12 源码 `05e0b8a7` 核对。[English](en/runtime-bundle.md) · [安装](installation.md) · [标准启动接口](architecture/launcher-contract.md)。

## 日常启动与源码更新

macOS 双击 `start.command`，Windows 双击 `start.bat`，Linux 执行 `bash start.sh`。start 复用健康实例或启动已安装镜像，不编译源码、不自动用其他 Release 替换失败版本。源码目录尚无可用镜像时，会停止并提示先构建。

```sh
# 首次源码构建，或 Git 更新后应用源码变更
bash scripts/aide.sh build
# 此后日常启动
bash scripts/aide.sh start
```

Windows 使用 `build.bat` / `start.bat`。首次源码构建会在 Docker 中生成原生启动器，需 Docker Engine、Buildx 和系统 PowerShell；之后由原生启动器管理。构建失败保留错误，成功才启动新构建，不回退成旧 Release 冒充构建成功。源码目录、镜像和正在运行的二进制必须分别核对。

`AIDE_FORCE_REBUILD=1 start`、旧 `AIDE_RELEASE_FALLBACK` 操作和 `start.ps1` 不再是当前源码更新入口。刷新浏览器也不会替换内嵌程序。清理只使用[维护入口](docker-maintenance.md)，保留配置、会话和工作区。

## 完整发行包与开发 SDK

普通 Git clone/pull 只同步源码和受控清单，不下载 Docker 镜像或 Git LFS 总包。完整 Release ZIP 单独携带已验证运行镜像；按目标 Docker 引擎选择 linux/arm64 或 linux/amd64，核对 Release 的外层 SHA256SUMS 后解压并使用包内入口。首次启动校验 .aide-image、包内镜像归档摘要、平台和导入身份；后续复用已安装镜像，无现场源码编译。

可选开发 SDK 独立分发，用于源码测试和工具链，不是日常运行包，也不自动获得运行实例的数据或插件。主程序不捆绑公开插件；更新主程序后，按市场最低宿主版本分别升级所需插件。

发行安装可使用[应用内 A/B 更新](architecture/software-updates.md)：校验同平台包、暂存非活动槽，用户明确激活后导入并检查健康，失败按已有机制恢复。源码安装通过显式 build 更新，不启用发行自动升级。镜像回退不等于数据格式或用户文件恢复，升级前备份配置和数据、保留旧包及镜像。

## 依赖与验收边界

离线运行已下载完整包，与离线编译源码是两项准备工作。源码构建另需对应平台的基础镜像、wheelhouse、固定源码和工具链；依赖镜像只提供构建输入，不能作为新源码二进制的通过证明。默认分发不带 STEP/CAD 引擎，额外插件引擎独立准备。

受控默认清单只登记实际公开并核验的身份。候选清单、公开附件元数据、包内 CRC/SHA、实际导入和运行健康分别验证，不能互相替代。新发行以同 tag 的 RELEASE-VALIDATION.json 为准。验证包不表示已部署到维护者或客户机器；生产替换和物理设备权限仍须单独确认。

## 从曾启用 Git LFS 的旧检出升级

main 已删除总包、LFS 属性与制作入口。新的普通克隆不会请求旧总包。旧设备可以在安装了 Git LFS 时执行 `git lfs uninstall --local` 撤下旧本地过滤器与钩子；不要把仍在使用的自定义 Git 钩子一起删除。历史提交中的 LFS 指针和服务端历史对象不通过强制改写历史清除，本次不执行远端历史重写。

本地缓存中的上一份总包可删除；可运行镜像和单架构 Release 包仍可留作本地启动缓存，无需为了升级而清空 Docker。
