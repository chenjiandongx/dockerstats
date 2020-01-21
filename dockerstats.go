package dockerstats

import (
	"context"
	"encoding/json"
	"io"
	"io/ioutil"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"
)

var (
	DefaultDockerClientOpts = []client.Opt{client.WithVersion("1.39")}

	// Kubernetes 往 docker label 注入的相关变量
	KubernetesLabels = map[string]string{
		"io.kubernetes.pod.namespace":  "kubernetes_pod_namespace",
		"io.kubernetes.pod.name":       "kubernetes_pod_name",
		"io.kubernetes.container.name": "kubernetes_container_name",
	}
)

// StatsEntry 定义了 Stats 的详细内容
type StatsEntry struct {
	ContainerID      string  `json:"container_id"`
	ContainerName    string  `json:"container_name"`
	CPUPercentage    float64 `json:"cpu_usage_percentage"`
	Memory           float64 `json:"memory_usage_in_bytes"`
	MemoryPercentage float64 `json:"memory_usage_percentage"`
	MemoryLimit      float64 `json:"memory_limit_in_bytes"`
	NetworkRx        float64 `json:"network_rx_in_bytes"`
	NetworkTx        float64 `json:"network_tx_in_bytes"`
	BlockRead        float64 `json:"block_read_in_bytes"`
	BlockWrite       float64 `json:"block_write_in_bytes"`
	// Kubernetes 相关 labels 变量，仅在 k8s 集中中运行才能采集到
	KubernetesLabels map[string]string `json:"kubernetes_labels,omitempty"`
}

// Exporter 负责采集 docker 运行的实时相关指标
type Exporter struct {
	// docker client instance
	dc *client.Client
	// docker client options
	dcOpts []client.Opt
}

// NewExporter 生成并返回 Exporter 实例
func NewExporter(opts ...client.Opt) *Exporter {
	if len(opts) == 0 {
		opts = DefaultDockerClientOpts
	}

	return &Exporter{
		dc:     newDockerClient(opts...),
		dcOpts: opts,
	}
}

func newDockerClient(opts ...client.Opt) *client.Client {
	dc, err := client.NewClientWithOpts(opts...)
	if err != nil {
		logrus.Fatalf("new docker client error: %+v", err)
	}
	return dc
}

// List 通过 docker.ContainerStats 接口采集 docker stats 指标
func (e *Exporter) List() ([]*StatsEntry, error) {
	containers, err := e.dc.ContainerList(context.Background(), types.ContainerListOptions{})
	if err != nil {
		return []*StatsEntry{}, err
	}

	stats := make([]*StatsEntry, 0)
	done := make(chan struct{}, 1)
	ch := make(chan *StatsEntry, 1)
	go func() {
		for c := range ch {
			stats = append(stats, c)
		}
		done <- struct{}{}
	}()

	wg := sync.WaitGroup{}
	empty := &StatsEntry{}
	for _, container := range containers {
		wg.Add(1)
		go func(containerID string) {
			defer wg.Done()
			resp, err := e.dc.ContainerStats(context.Background(), containerID, false)
			if err != nil {
				ch <- empty
			}

			s, err := e.getStats(resp)
			if err != nil {
				ch <- empty
			}
			e.getLabels(s)
			ch <- s
		}(container.ID)
	}
	wg.Wait()
	close(ch)

	<-done
	return stats, nil
}

// Watch 监听 Docker 错误事件，并重新生成新实例
func (e *Exporter) Watch() {
	logrus.Info("EVENT[WATCH]: Containers")
	filter := filters.NewArgs(filters.KeyValuePair{Key: "type", Value: "container"})
	options := types.EventsOptions{Filters: filter}

	_, eventErr := e.dc.Events(context.Background(), options)

	for {
		select {
		case err := <-eventErr:
			logrus.Warnf("watch event error: %+v", err)
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				e.dc = newDockerClient(e.dcOpts...)
				time.Sleep(time.Millisecond * 250)
			}
			_, eventErr = e.dc.Events(context.Background(), options)
		}
	}
}

func (e *Exporter) getLabels(stats *StatsEntry) {
	labels := make(map[string]string)
	info, err := e.dc.ContainerInspect(context.Background(), stats.ContainerID)
	if err != nil {
		e.dc = newDockerClient(e.dcOpts...)
		time.Sleep(200 * time.Millisecond)
		logrus.Warnf("get container inspect error: %+v", err)
	}

	for k, v := range info.Config.Labels {
		if KubernetesLabels[k] != "" {
			labels[KubernetesLabels[k]] = v
		}
	}
	stats.KubernetesLabels = labels
}

func (e *Exporter) getStats(response types.ContainerStats) (*StatsEntry, error) {
	var (
		v                      *types.StatsJSON
		prevCPU, prevSystem    uint64
		memPercent, cpuPercent float64
		blkRead, blkWrite      float64
		mem, memLimit          float64
	)

	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return &StatsEntry{}, err
	}

	defer response.Body.Close()

	if err := json.Unmarshal(body, &v); err != nil {
		return &StatsEntry{}, err
	}

	// 不兼容 Windows 系统
	if response.OSType != "windows" {
		prevCPU = v.PreCPUStats.CPUUsage.TotalUsage
		prevSystem = v.PreCPUStats.SystemUsage
		cpuPercent = e.calcCPUPercent(prevCPU, prevSystem, v)
		blkRead, blkWrite = e.calcBlockIO(v.BlkioStats)
		mem = e.calcMemUsageNoCache(v.MemoryStats)
		memLimit = float64(v.MemoryStats.Limit)
		memPercent = e.calcMemPercentNoCache(memLimit, mem)
	}

	netRx, netTx := e.calcNetwork(v.Networks)
	return &StatsEntry{
		ContainerID:      v.ID,
		ContainerName:    v.Name,
		CPUPercentage:    cpuPercent,
		Memory:           mem,
		MemoryPercentage: memPercent,
		MemoryLimit:      memLimit,
		NetworkRx:        netRx,
		NetworkTx:        netTx,
		BlockRead:        blkRead,
		BlockWrite:       blkWrite,
	}, nil
}

func (e *Exporter) calcCPUPercent(prevCPU, prevSystem uint64, v *types.StatsJSON) float64 {
	var (
		cpuPercent = 0.0
		// 计算两个 CPU 使用时间差值
		cpuDelta = float64(v.CPUStats.CPUUsage.TotalUsage) - float64(prevCPU)
		// 计算系统 CPU 使用时间差值
		systemDelta = float64(v.CPUStats.SystemUsage) - float64(prevSystem)
		onlineCPUs  = float64(v.CPUStats.OnlineCPUs)
	)

	if onlineCPUs == 0.0 {
		onlineCPUs = float64(len(v.CPUStats.CPUUsage.PercpuUsage))
	}
	if systemDelta > 0.0 && cpuDelta > 0.0 {
		cpuPercent = (cpuDelta / systemDelta) * onlineCPUs * 100.0
	}
	return cpuPercent
}

func (e *Exporter) calcBlockIO(blkio types.BlkioStats) (float64, float64) {
	var blkRead, blkWrite uint64
	for _, bioEntry := range blkio.IoServiceBytesRecursive {
		if len(bioEntry.Op) == 0 {
			continue
		}
		switch bioEntry.Op[0] {
		case 'r', 'R':
			blkRead = blkRead + bioEntry.Value
		case 'w', 'W':
			blkWrite = blkWrite + bioEntry.Value
		}
	}
	return float64(blkRead), float64(blkWrite)
}

func (e *Exporter) calcNetwork(network map[string]types.NetworkStats) (float64, float64) {
	var rx, tx float64
	for _, v := range network {
		rx += float64(v.RxBytes)
		tx += float64(v.TxBytes)
	}
	return rx, tx
}

func (e *Exporter) calcMemUsageNoCache(mem types.MemoryStats) float64 {
	// 使用内存 - 缓冲内存
	return float64(mem.Usage - mem.Stats["cache"])
}

func (e *Exporter) calcMemPercentNoCache(limit float64, usedNoCache float64) float64 {
	if limit != 0 {
		return usedNoCache / limit * 100.0
	}
	return 0
}
