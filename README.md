# vulhub-cli

在一台机器上管理 [vulhub](https://github.com/vulhub/vulhub) 靶场生命周期的命令行工具。

vulhub 提供了大量开箱即用的漏洞靶场，但靶场数量上百之后，逐个 `cd` 进目录再敲 `docker compose` 就不可行了；共用一台机器时，谁起了什么、哪些还开着，也没有统一的视图。这个工具解决的就是这件事。

设计见 [design.md](design.md)，一期规格见 [docs/spec.md](docs/spec.md)，术语见 [CONTEXT.md](CONTEXT.md)。

## 构建

目标平台是 **linux-amd64**。

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o vulhub .
```

`CGO_ENABLED=0` 不是可选项：spec 要求的是静态二进制，开着 cgo 交叉编译会去找目标平台的 C 工具链。产物约 2.7M。

## 使用

```
vulhub init                 # 拉取 vulhub 到 ~/vulhub 并分配编号
vulhub ls                   # 列出全部靶场
vulhub search log4j         # 按漏洞标题或靶场路径检索
vulhub up 68                # 启动 68 号靶场
vulhub status               # 看看现在开着哪些靶场
vulhub stop 68              # 暂停
vulhub del 68               # 销毁（保留 volume）
vulhub del -a 68            # 连 volume 和镜像一起销毁
vulhub update               # 更新 vulhub 检出
```

靶场参数既可以是**编号**（`68`）也可以是**路径**（`activemq/CVE-2023-46604`）。编号由本地索引分配，一旦分配就不随 vulhub 更新改变，因此可以记住、可以写进脚本、可以口头传达给同事。

逗号分隔的多选适用于 `pull`、`up`、`stop`；`del` 是唯一不可逆的操作，一次只能销毁一个。

跑 `vulhub help` 看完整用法。

## 依赖

机器上需要 `git`、`docker`，以及 `docker compose`（v2）或 `docker-compose`（v1）之一。`init` 会做前置检查并明确告诉你缺的是哪一个。

## 发布

制品由 GitHub Actions 构建，当前只支撑 linux-amd64。

- 推分支 / 开 PR：`.github/workflows/ci.yml` 跑 `gofmt`、`go vet`、`go test`，并构建一份 linux-amd64 作为 workflow 制品
- **打 `v*` 标签**：`.github/workflows/release.yml` 构建静态二进制、打 SHA256 校验和、创建 GitHub Release 并挂上制品

```sh
git tag v0.1.0
git push origin v0.1.0
```

要加平台时改这两个 workflow 里的 `GOOS`/`GOARCH` 矩阵即可，代码本身没有平台假设（目标平台只影响 `CGO_ENABLED=0` 这个构建开关）。

## 开发

```sh
go test ./...
```

测试全部从 `cli.Run` 这一个缝进入：喂命令行参数，断言输出流、被调用的外部依赖、以及退出码。除三个外部依赖（编排 / git / 本机 IP）用假对象外，文件系统与 `environments.toml` 解析都走真实实现。
