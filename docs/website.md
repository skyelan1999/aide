# aide 产品主页

独立静态站点源文件位于 `site/`，用于 GitHub Pages 产品展示；应用工作台仍由 Go 服务提供。主页不依赖 npm、CDN、外部字体或后端接口。

## 本地预览

从 aide 仓库根目录运行：

```bash
python3 -m http.server 8765 --bind 127.0.0.1 --directory site
```

在浏览器打开 `http://127.0.0.1:8765`。不要通过这个端口访问 Aide API；它只提供主页静态资源。

## 内容与素材

- 定位、能力与使用边界参考 [README](../README.md)、[使用指南](user-guide.md)、[安装说明](installation.md) 和 [定制指南](customization.md)。
- `site/assets/favicon.svg` 复用工作台图标。
- `site/assets/workbench-preview.jpg` 复用 `docs/images/workbench-preview.jpg`，为隔离示例环境截图。主页已注明示例与当前版本差异，不以图片版本号宣称当前发行版本。
- 场景切换面板是静态工作方式示意，不调用模型、执行命令或连接真实项目。
- 安装命令采用现有源码安装脚本；下载入口指向 Releases，不固定版本或附件名称。
- GitHub、文档、License 与反馈链接均指向 `skyelan1999/aide`。

## GitHub Pages 发布准备

`.github/workflows/pages.yml` 只支持手动触发，上传内容限定 `site/`。写入或推送这些文件不会自动部署应用，不会发布会话、模型配置或工作区文件。

发布者准备独立启用 Pages 时：

1. 单独审阅并提交本次主页相关文件，避免包含其他任务的修改。
2. 确认仓库可见性与 GitHub 计划支持 Pages；免费个人账号的 Pages 使用公开仓库。
3. 在仓库 **Settings → Pages → Build and deployment** 选择 **GitHub Actions**。
4. 在 **Actions → Publish aide homepage → Run workflow** 手动运行。
5. 等待部署成功，用实际输出地址核对桌面、手机、资源和链接。默认项目站点地址通常为 `https://skyelan1999.github.io/aide/`，是否实际可访问以部署结果为准。

资源使用相对路径，可从 `/` 或 `/aide/` 部署。`site/.nojekyll` 支持直接静态托管。

官方流程参考：[使用自定义工作流发布 Pages](https://docs.github.com/en/pages/getting-started-with-github-pages/using-custom-workflows-with-github-pages)。

## 验收与回滚

需求、验收项及实际结果见[homepage-20261008](tasks/homepage-20261008.json)。本轮实际浏览器通过1280×900、390×844、320×844的布局与图片／锚点，移动菜单／Escape焦点、场景键盘、剪贴板精确比较和4项FAQ；截图和操作范围见[发布UI报告](reviews/2026-10-08-release-ui.md)。禁用JavaScript和OS减少动效真实运行未测，仅静态检查。此前[桌面全页截图](reviews/2026-10-08-homepage/desktop-full.png)与[首屏截图](reviews/2026-10-08-homepage/desktop-initial.png)保留为旧预览，不代替本轮收据。本地验收不等于Pages远端发布。

主页源码、手动部署工作流、旧预览与本轮验收截图及任务说明均保留，不包含应用数据卷、工作区文件或模型配置。发行包和 GitHub Release 与 Pages 是不同交付目标；本轮用户的 release 授权不表示 Pages 已启用或部署完成。

撤销本地主页可还原本任务新增的 `site/` 与 Pages 工作流；已发布 Pages 时，另需撤销部署或关闭站点。操作说明不证明回滚已经执行。
