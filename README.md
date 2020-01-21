<h1 align="center">dockerstarts</h1>

<p align="center">
  <em>The easy way to collect docker stats.</em>
</p>

**Q: why dockerstats?**

A: 通常来讲，如果我们需要知道 docker 运行时的一些指标，如 CPU/MEM/IO 等信息，可以通过 `docker stats` 命令查看，对于 kubernetes 用户来讲，可能是 `kubectl top pods`。Google 为此开源了一个容器运行指标采集器 [google/cadvisor](https://github.com/google/cadvisor)，不过 cadvisor 跑起来相对占资源，且可能大部分指标用户其实都是不关心的，所以我觉得需要一个更轻量级的指标采集器。That's [dockerstats](https://github.com/chenjiandongx/dockerstats).

### 指标说明

| Name | Desc |
| ---- | ---- |
| container_id | 容器 ID |
| container_name | 容器名称 |
| cpu_usage_percentage | CPU 使用率 |
| memory_usage_in_bytes | 已使用内存 | 
| memory_usage_percentage | 内存占用百分比 |
| memory_limit_in_bytes | 内存限制 |
| network_rx_in_bytes | 网络接收 |
| network_tx_in_bytes | 网络发送 |
| block_read_in_bytes | 磁盘读 |
| block_write_in_bytes | 磁盘写 |
| kubernetes_labels | kubernetes 相关指标 |
| kubernetes_labels.kubernetes_container_name | k8s 容器名称 |
| kubernetes_labels.kubernetes_pod_name | k8s Pod 名称 |
| kubernetes_labels.kubernetes_pod_namespace | k8s Pod 命名空间 |


### 使用

#### 本地开发构建

```shell
# go path
$ go get -u github.com/chenjiandongx/dockerstats/...

# go module => go.mod
require (
  github.com/chenjiandongx/dockerstats
)
```

#### Docker case

```shell
$ docker run -v /var/run/docker.sock:/var/run/docker.sock -p 8099:8099 -d chenjiandongx/dockerstats:latest
# 获取指标
$ curl -s http://localhost:8099/stats | jq
```

#### Kubernetes case

在 kubernetes 中情况可能稍微复杂了，由于 Service 本身是个负载均衡，所以要采集所有 Node 节点的话需要获取 Service 对应的 Endpoints 列表，然后循环遍历发起请求，或者可以根据自身业务情况，在 Pod 中主动上报数据到监控中心（如果有的话 🐶）。

#### Glances

![](https://user-images.githubusercontent.com/19553554/72773397-f047ce80-3c41-11ea-8c23-3e25c96ba815.png)


### License

MIT [©chenjiandongx](https://github.com/chenjiandongx)
