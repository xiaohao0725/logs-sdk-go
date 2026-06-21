package logsdk

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// OfflineCache 离线缓存，网络故障时缓存日志到本地文件，恢复后自动重传。
type OfflineCache struct {
	mu       sync.Mutex
	dir      string    // 缓存目录
	maxSize  int64     // 最大缓存大小（字节），默认 50MB
	maxAge   time.Duration // 最大缓存时间，默认 24h
	enabled  bool
}

// NewOfflineCache 创建离线缓存实例。
// dir 为缓存目录路径，设为空字符串则使用系统临时目录。
func NewOfflineCache(dir string) *OfflineCache {
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "logs-sdk-offline")
	}
	_ = os.MkdirAll(dir, 0755)
	return &OfflineCache{
		dir:     dir,
		maxSize: 50 * 1024 * 1024, // 50MB
		maxAge:  24 * time.Hour,
		enabled: true,
	}
}

// Save 将一批日志保存到离线缓存文件。
// 文件命名格式: logs-{timestamp}-{random}.json
func (c *OfflineCache) Save(entries []*LogEntry) error {
	if !c.enabled || len(entries) == 0 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	// 检查缓存大小，超过限制则清理旧文件
	c.cleanup()

	filename := filepath.Join(c.dir, "offline-"+time.Now().UTC().Format("20060102T150405")+".json")
	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return err
	}
	log.Printf("[logs-sdk] 离线缓存已保存: %s (%d 条)", filename, len(entries))
	return nil
}

// FlushAll 读取所有离线缓存文件，通过回调发送，成功则删除。
// sendFn 为发送函数（通常是 Client.sendBatch）。
func (c *OfflineCache) FlushAll(sendFn func([]*LogEntry) error) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	files, err := filepath.Glob(filepath.Join(c.dir, "offline-*.json"))
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}

	for _, file := range files {
		// 检查文件是否过期
		info, err := os.Stat(file)
		if err != nil {
			continue
		}
		if time.Since(info.ModTime()) > c.maxAge {
			os.Remove(file)
			log.Printf("[logs-sdk] 过期离线缓存已删除: %s", file)
			continue
		}

		// 读取并解析日志
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		var entries []*LogEntry
		if err := json.Unmarshal(data, &entries); err != nil {
			os.Remove(file) // 损坏文件直接删除
			continue
		}

		// 尝试发送
		if err := sendFn(entries); err != nil {
			log.Printf("[logs-sdk] 离线缓存重传失败: %s (%v)", file, err)
			return err // 保留文件，下次重试
		}

		// 发送成功，删除缓存文件
		os.Remove(file)
		log.Printf("[logs-sdk] 离线缓存已重传: %s (%d 条)", file, len(entries))
	}

	return nil
}

// cleanup 清理超过大小限制的旧缓存文件。
func (c *OfflineCache) cleanup() {
	files, _ := filepath.Glob(filepath.Join(c.dir, "offline-*.json"))
	var totalSize int64
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		totalSize += info.Size()
	}

	// 超过最大缓存时，按修改时间删除最旧的文件
	for totalSize > c.maxSize && len(files) > 0 {
		oldest := files[0]
		oldestTime := time.Now()
		for _, f := range files {
			info, _ := os.Stat(f)
			if info != nil && info.ModTime().Before(oldestTime) {
				oldest = f
				oldestTime = info.ModTime()
			}
		}
		info, _ := os.Stat(oldest)
		if info != nil {
			totalSize -= info.Size()
		}
		os.Remove(oldest)
		log.Printf("[logs-sdk] 离线缓存清理: %s", oldest)
		files, _ = filepath.Glob(filepath.Join(c.dir, "offline-*.json"))
	}
}

// Enable 启用离线缓存。
func (c *OfflineCache) Enable()  { c.enabled = true }

// Disable 禁用离线缓存。
func (c *OfflineCache) Disable() { c.enabled = false }

// PendingCount 返回待重传的缓存文件数量。
func (c *OfflineCache) PendingCount() int {
	files, _ := filepath.Glob(filepath.Join(c.dir, "offline-*.json"))
	return len(files)
}
