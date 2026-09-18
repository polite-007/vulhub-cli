# vulhub-cli

管理 [vulhub](https://github.com/vulhub/vulhub) 靶场生命周期的命令行工具。

[![CI](https://github.com/polite-007/vulhub-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/polite-007/vulhub-cli/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/polite-007/vulhub-cli)](https://github.com/polite-007/vulhub-cli/releases/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

[English](README.md) · **简体中文**

## 概述

[vulhub](https://github.com/vulhub/vulhub) 提供了大量开箱即用的漏洞靶场，每个靶场是一个 Docker Compose 项目。手工管理它们难以规模化：想知道有哪些可用，要遍历目录树；想启动一个，要先进入它的目录再直接调用 `docker compose`；而当多个靶场同时运行时，没有一个统一的视图能看到当前开着什么。

vulhub-cli 在本地维护一份 vulhub 检出，并提供命令来列出、检索、启动、停止和销毁靶场。

靶场可以用它的**路径**（`activemq/CVE-2023-46604`）或一个简短的**编号**（`68`）来指代。编号一经分配便不再改变，因此可以放心记住、写进脚本，也可以口头传达给同事。

## 环境要求

- **Linux, amd64。** 当前不支持其他平台。
- `git`
- Docker，且 `docker compose`（Compose v2）或 `docker-compose`（Compose v1）至少有一个可用

`vulhub init` 会检查以上全部要求，并明确指出缺少的是哪一项。

## 安装

从[最新发布](https://github.com/polite-007/vulhub-cli/releases/latest)下载二进制并放入 `PATH`：

```sh
curl -LO https://github.com/polite-007/vulhub-cli/releases/latest/download/vulhub-cli-linux-amd64
curl -LO https://github.com/polite-007/vulhub-cli/releases/latest/download/SHA256SUMS
sha256sum -c SHA256SUMS

chmod +x vulhub-cli-linux-amd64
sudo mv vulhub-cli-linux-amd64 /usr/local/bin/vulhub
```

资产名不含版本号，因此这些地址在各次发布之间保持稳定，版本由 release tag 体现。该二进制为静态链接，没有运行时依赖。

## 快速开始

```sh
vulhub init              # 拉取 vulhub 到 ~/vulhub 并分配编号
vulhub search log4j      # 按标题或路径检索靶场
vulhub up 68             # 启动 68 号
vulhub status            # 查看当前运行中的靶场
vulhub stop 68           # 停止，保留容器
vulhub del 68            # 销毁，保留 volume
```

## 命令参考

`<靶场>` 接受编号（`68`）或路径（`activemq/CVE-2023-46604`）。

| 命令 | 作用对象 | 说明 |
| --- | --- | --- |
| `init` | — | 拉取 vulhub 到 `~/vulhub` 并分配编号 |
| `update` | — | 更新本地 vulhub 检出 |
| `ls [--json]` | — | 列出全部靶场 |
| `search <关键词>… [--json]` | — | 按漏洞标题或靶场路径检索 |
| `up <靶场>[,…]` | 单个或多个 | 启动靶场 |
| `stop <靶场>[,…]` | 单个或多个 | 停止靶场 |
| `del <靶场>` | 单个 | 销毁靶场 |
| `status [--json]` | — | 查看当前运行中的靶场 |
| `pull <靶场>[,…] \| --all` | 单个或多个 | 提前拉取镜像 |
| `help` | — | 打印用法 |

逗号分隔的多选适用于 `pull`、`up`、`stop`。`del` 是唯一不可逆的操作，只能作用于一个靶场。

`del` 的选项：

- `-a` —— 连同该靶场的 volume 与它引用的镜像一并删除
- `-y` —— 跳过确认提示

### 退出码

| 码 | 含义 |
| --- | --- |
| `0` | 成功 |
| `1` | 运行失败——靶场不存在、Docker 报错、`search` 无命中 |
| `2` | 用法错误——参数数量不对、未知选项 |

## 工作原理

**vulhub 检出**位于 `~/vulhub`。`init` 以浅克隆创建它，`update` 以 `git pull` 维护它。它被视为只读——你在其中做的修改会与 `update` 冲突，届时工具会直接失败，而不会丢弃你的改动。

**索引**位于 `~/.local/share/vulhub-cli/index.json`，刻意放在检出之外，因为检出是一个 git 仓库。它记录靶场路径到编号的映射。编号在 `init` 时按字典序分配，此后不再改变，新增靶场追加到末尾。消失的靶场其编号作废且不再复用。索引一旦丢失或损坏会自动重建，同时会警告你此前分配的编号已不再有效——这正是**路径而非编号才是靶场身份**的原因。

**靶场识别**依赖 Docker Compose 为每个容器打上的标签（`com.docker.compose.project` 与 `com.docker.compose.project.working_dir`），而非容器名。官方 vulhub 的 Compose 文件通常不设置 `container_name`，容器名由 Compose 生成，解析它等于依赖 Compose 的内部命名规则。代价是 `status` 只能看见由 Compose 创建的容器。

**靶场元数据**读取自 `environments.toml`，即 vulhub 自己的环境注册表。该文件缺失或无法解析时，工具降级为扫描目录树，此时标题不可用，且只能按路径检索。

## 从源码构建

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o vulhub .
```

`CGO_ENABLED=0` 是必须的，不是可选项：交付物是静态二进制，保留 cgo 会使交叉编译去寻找目标平台的 C 工具链。

## 发布

制品由 GitHub Actions 构建。当前只支撑 linux amd64。

- **推送分支或发起 Pull Request** 会运行 `.github/workflows/ci.yml`：`gofmt`、`go vet`、`go test`，并构建一份 linux-amd64 作为 workflow 制品。
- **推送 `v*` 标签** 会运行 `.github/workflows/release.yml`：构建静态二进制、生成 `SHA256SUMS`，并创建附带两者的 GitHub Release。

```sh
git tag v0.1.0
git push origin v0.1.0
```

两个 workflow 都会断言制品是 `statically linked`。要支持其他平台，只需在这两个文件里扩充 `GOOS`/`GOARCH` 矩阵；代码本身没有平台假设。

## 开发

```sh
go test ./...
```

所有测试从唯一的缝 `cli.Run` 进入：喂入命令行参数，断言输出流、被调用的外部依赖以及退出码。只有三个依赖用假对象替换——编排、git、本机 IP 发现——因为它们的真实实现分别需要运行中的 Docker daemon、数百 MB 的网络传输、以及取决于具体机器的网络配置。文件系统与 `environments.toml` 解析均使用真实实现并作用于临时目录，因此测试覆盖的是真实的文件读写与真实的解析。

## 文档

- [design.md](design.md) —— 完整设计，含刻意**不做**的事项
- [docs/spec.md](docs/spec.md) —— 一期规格
- [CONTEXT.md](CONTEXT.md) —— 领域术语表
- [docs/adr/](docs/adr/) —— 架构决策记录

## 许可

基于 [MIT 许可证](LICENSE) 发布。
