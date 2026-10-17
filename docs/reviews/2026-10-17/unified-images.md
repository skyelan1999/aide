# 单文件 Docker 镜像导入交付

任务 [unified-images-20261017](../../tasks/unified-images-20261017.json)。用户要求“docker需要导入的images和到一个里面”。本批交付直接可被Docker读取的单个tar.gz，架构各一份；不要求分别导入运行／CAD／工具链镜像。保留原来的带启动器完整离线包，两者用途不同。

## 文件与用法

文件位于当前项目 docker-images 目录，各有同名.sha256旁证：

- ARM64：aide-images-0.1.17.0-RC2-linux-arm64-6ad1d1179c2c.tar.gz，770941775字节，SHA-256：3f64abf811c857a75895a7b77ae26e1e13afc727a395d8ff2240a0e97ec9cef8。
- AMD64：aide-images-0.1.17.0-RC2-linux-amd64-0c173c73296e.tar.gz，774153279字节，SHA-256：d8d5f382758130b6e36c166ef533bc10720da167e7e6bf0c3e06be5d56ab06a9。

在文件所在目录，一次执行（按机器选一条）：

```sh
docker load -i aide-images-0.1.17.0-RC2-linux-arm64-6ad1d1179c2c.tar.gz
```

```sh
docker load -i aide-images-0.1.17.0-RC2-linux-amd64-0c173c73296e.tar.gz
```

导入后使用匹配版本的仓库启动器或原完整离线启动包。原始镜像归档本身没有启动脚本、个人配置或会话；旧版本仓库的预构建清单可能指向别的镜像，应同时更新启动器或使用匹配的完整离线包，不能宣称任意旧仓库都会选中此包。不要先在Docker界面把文件普通解压再逐个导入。

## 真实核对与边界

ARM包含完整运行镜像及已经保留的工具链底座，两份独立镜像共享公共层；运行镜像同时带offline、local和cad-dependencies标签。AMD包中的完整运行镜像本身包含Go/Python/Node/原生CAD，工作台与CAD共享同一镜像，三个标签不造成三份镜像层。AMD首次显式源码构建可由运行镜像离线准备工具链底座；普通启动不要求构建底座。

两包逐项验证内容寻址blob摘要、tar成员不重复及manifest标签集合，均执行一次实际docker load通过。没有容器export、宿主文件、卷、配置、密钥或会话进入归档。AMD打包／导入时临时切换同名标签，finally精确恢复本机原ARM标签；原运行容器ID、镜像和StartedAt精确不变。无新增构建或pull；复用已验证的6ad／0c完整运行镜像。不是新源码66b0a89a重新构建的二进制，资源保护启动脚本仍由当前仓库／下一完整离线启动包交付。接收Windows原生由用户验收。

Docker共享层去重已经完成，不能把别名的显示大小相加当作真实磁盘占用。原带启动器的完整包和新直接导入包均为明确交付物，未自动删除历史回滚档案。公开Release上传和main分发门禁仍单独待完成。脱敏[证据](unified-images-proof.json)，私有导出与导入日志位于.agent-state/unified-images-20261017。
