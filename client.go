package logsdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

// Client 日志采集 SDK 核心客户端。
// 管理配置、缓冲、定时刷新、HTTP 上报和优雅关闭。
// 每个进程只需创建一个 Client 实例。
type Client struct {
	config       Config
	buffer       *ringBuffer
	httpClient   *http.Client
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	closed       bool
	closedMu     sync.Mutex

	// 离线缓存
	offlineCache *OfflineCache

	// 传参
	maxBodySize  int
	maxStackSize int
	hostname     string
	pid          string
}

// New 创建日志采集客户端并启动后台 flush 协程。
// 返回的 Client 需要调用 Close() 进行优雅关闭，确保缓冲日志全部上报。
func New(cfg Config) (*Client, error) {
	// 合并默认配置
	config := mergeConfig(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	hostname, _ := os.Hostname()
	pid := fmt.Sprintf("%d", os.Getpid())

	client := &Client{
		config:       config,
		httpClient:   &http.Client{Timeout: 15 * time.Second},
		ctx:          ctx,
		cancel:       cancel,
		offlineCache: NewOfflineCache(""), // 使用系统临时目录
		maxBodySize:  config.MaxBodySize,
		maxStackSize: config.MaxStackSize,
		hostname:     hostname,
		pid:          pid,
	}

	// 创建环形缓冲区，flush 回调为 HTTP 批量发送
	client.buffer = newRingBuffer(config.BufferSize, func(entries []*LogEntry) {
		go client.flushEntries(entries)
	})

	// 启动定时 flush 协程
	client.wg.Add(1)
	go client.flushLoop()

	return client, nil
}

// FlushOffline 尝试重传所有本地离线缓存的日志。
// 建议 Client 启动后调用，恢复断网期间缓存的日志。
func (c *Client) FlushOffline() {
	if c.offlineCache.PendingCount() == 0 { return }
	log.Printf("[logs-sdk] 检测到 %d 个离线缓存文件，开始重传...", c.offlineCache.PendingCount())
	if err := c.offlineCache.FlushAll(c.sendBatch); err != nil {
		log.Printf("[logs-sdk] 离线缓存重传失败: %v", err)
	} else {
		log.Printf("[logs-sdk] 离线缓存重传完成")
	}
}

// Send 异步发送一条日志条目到缓冲区。
// 非阻塞操作，日志写入内存环形缓冲后立即返回。
func (c *Client) Send(entry *LogEntry) {
	c.closedMu.Lock()
	closed := c.closed
	c.closedMu.Unlock()

	if closed {
		log.Println("[logs-sdk] Client 已关闭，日志将被丢弃")
		return
	}

	// 补充来源信息（应用层不需要手动设置）
	entry.Host = c.hostname
	entry.ProcessID = c.pid
	entry.Environment = c.config.Environment
	entry.ProjectSlug = c.config.ProjectSlug
	entry.ServiceName = c.config.ServiceName

	c.buffer.push(entry)
}

// Close 优雅关闭客户端，等待所有缓冲日志上报完成。
// 调用后不可再使用 Send 方法。
func (c *Client) Close() {
	c.closedMu.Lock()
	c.closed = true
	c.closedMu.Unlock()

	// 停止后台协程
	c.cancel()
	c.wg.Wait()

	// 最终刷新剩余日志
	remaining := c.buffer.flush()
	if len(remaining) > 0 {
		if err := c.sendBatch(remaining); err != nil {
			log.Printf("[logs-sdk] 关闭时上报失败 (数量=%d): %v — 保存到离线缓存", len(remaining), err)
			c.offlineCache.Save(remaining)
		}
	}

	// 尝试重传离线缓存
	c.FlushOffline()
}

// ──────────────── 内部方法 ────────────────

// flushLoop 定时刷新协程，每 flushInterval 秒触发一次。
func (c *Client) flushLoop() {
	defer c.wg.Done()

	interval := time.Duration(c.config.FlushInterval) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			entries := c.buffer.flush()
			if len(entries) > 0 {
				c.flushEntries(entries)
			}
		}
	}
}

// flushEntries 异步发送一批日志（包装了重试逻辑）。
func (c *Client) flushEntries(entries []*LogEntry) {
	retryCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := retryWithBackoff(retryCtx, defaultRetryConfig(), func() error {
		return c.sendBatch(entries)
	})
	if err != nil {
		log.Printf("[logs-sdk] 上报失败 (数量=%d，已重试): %v — 保存到离线缓存", len(entries), err)
		// 发送失败时自动缓存到本地，恢复后重传
		if saveErr := c.offlineCache.Save(entries); saveErr != nil {
			log.Printf("[logs-sdk] 离线缓存保存失败: %v", saveErr)
		}
	}
}

// sendBatch 通过 HTTP POST 发送批量日志到 Ingestion API。
func (c *Client) sendBatch(entries []*LogEntry) error {
	body, err := json.Marshal(map[string]interface{}{
		"logs": entries,
	})
	if err != nil {
		return fmt.Errorf("序列化日志失败: %w", err)
	}

	req, err := http.NewRequestWithContext(
		context.Background(),
		"POST",
		c.config.Endpoint,
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("创建 HTTP 请求失败: %w", err)
	}

	// 设置认证头和 Content-Type
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.config.APIKey)
	req.Header.Set("X-API-Secret", c.config.APISecret)
	req.Header.Set("X-SDK-Version", Version)
	req.Header.Set("X-SDK-Type", "go")
	req.Header.Set("User-Agent", "logs-sdk-go/"+Version)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("服务端返回异常状态码: %d", resp.StatusCode)
	}

	return nil
}

// mergeConfig 合并用户配置与默认值。
func mergeConfig(cfg Config) Config {
	defaults := DefaultConfig()
	if cfg.Environment == "" {
		cfg.Environment = defaults.Environment
	}
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = defaults.BufferSize
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = defaults.FlushInterval
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = defaults.MaxRetries
	}
	if cfg.MaxBodySize <= 0 {
		cfg.MaxBodySize = defaults.MaxBodySize
	}
	if cfg.MaxStackSize <= 0 {
		cfg.MaxStackSize = defaults.MaxStackSize
	}
	return cfg
}
