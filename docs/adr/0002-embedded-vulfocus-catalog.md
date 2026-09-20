# vulfocus 的镜像清单内嵌于二进制，而不由用户拉取

vulfocus 来源的靶场只有 Docker Hub 上的镜像，没有 compose 文件。要把它变成一个靶场，必须知道它暴露哪些端口，而这份信息只能靠逐张读镜像的 config blob 得到（每个镜像三次请求：列表、manifest、config）。全部 452 个镜像约合一千四百次请求。

**我们让项目在发布前跑一次 `gen-vulfocus`，把结果写成 `data/vulfocus.json` 提交进仓库，再用 `go:embed` 打进二进制。**用户的 `init` 因此完全不碰 Docker Hub，`update` 也只管 `~/vulhub` 的 `git pull`。刷新清单是项目侧的动作。

**替代方案**是让每个用户在 `init` 时自己拉。它被否决，因为代价与收益不成比例：一千四百次请求会被 Docker Hub 限流、要跑十几分钟，而每个用户拉到的都是**同一份**数据。把一次性的昂贵操作放在项目侧，用户就同时得到了即时可用的清单、不会被限流、以及可审查可复现的结果——每一版二进制对应的清单在 git 历史里看得见。

**代价**是这份清单会随二进制一起变旧：vulfocus 上游新增镜像时，用户不会自动拿到，必须等新版本发布。这是刻意的取舍——这些镜像年份集中在 2014–2022，更新频率低，用"清单新鲜度"换"用户零网络"是划算的。要刷新时，维护者跑一次子命令并提交产物即可。

## 运行生成器需要 Docker Hub 凭证

**Docker Hub 对匿名请求限制翻页：offset 到 100 就拒绝**，报 `pagination offset too large for anonymous requests`。而 vulfocus 命名空间有 452 个镜像，匿名只能取到前 100 个。所有端点（`/v2/repositories/`、`/v2/namespaces/`、`/v2/search/repositories/`）都是这个限制。

因此生成器需要凭证：

```sh
DOCKERHUB_USERNAME=<用户名> DOCKERHUB_TOKEN=<PAT> vulhub gen-vulfocus
```

PAT 权限 Read-only 即可。实测带凭证后可以正常翻页（第 2 页起正常返回，对照组匿名仍 403）。

生成器**宁可失败也不发半份清单**：取到一半翻不动时直接报错，并提示补凭证。一份"看起来正常但少了 350 个靶场"的清单，比直接失败难发现得多。

单个镜像不可用（最常见的是仓库没有 `latest` 标签，`docker pull` 也拉不动它）不会拖垮整轮，但会被逐个列出来。我们合成的 compose 文件不写标签、也就是用 `latest`，所以没有 `latest` 的镜像本来就不能作为靶场使用。
