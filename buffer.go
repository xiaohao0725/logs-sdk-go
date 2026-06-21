package logsdk

import (
	"sync"
)

// ringBuffer 线程安全的环形缓冲区，用于暂存待上报的日志条目。
// 当缓冲使用率达到 80% 时自动触发 flush 回调。
type ringBuffer struct {
	mu       sync.Mutex
	buf      []*LogEntry // 环形缓冲数组
	capacity int         // 缓冲容量
	head     int         // 写入位置
	tail     int         // 读取位置
	count    int         // 当前元素数量
	flushFn  func([]*LogEntry)
}

// newRingBuffer 创建指定容量的环形缓冲区。
func newRingBuffer(capacity int, flushFn func([]*LogEntry)) *ringBuffer {
	if capacity < 10 {
		capacity = 100 // 最小容量
	}
	return &ringBuffer{
		buf:      make([]*LogEntry, capacity),
		capacity: capacity,
		flushFn:  flushFn,
	}
}

// push 向缓冲区中追加一条日志。
// 若缓冲使用率达到 80%，自动触发 flushCallback。
func (b *ringBuffer) push(entry *LogEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.buf[b.head] = entry
	b.head = (b.head + 1) % b.capacity
	b.count++

	// 缓冲区满 80% 自动触发 flush
	if b.count >= b.capacity*80/100 {
		entries := b.drain()
		if b.flushFn != nil {
			b.flushFn(entries)
		}
	}
}

// drain 取出当前所有待上报日志并清空缓冲。
// 调用方需持有锁。
func (b *ringBuffer) drain() []*LogEntry {
	if b.count == 0 {
		return nil
	}
	entries := make([]*LogEntry, 0, b.count)
	for b.count > 0 {
		entries = append(entries, b.buf[b.tail])
		b.buf[b.tail] = nil // 释放引用，帮助 GC
		b.tail = (b.tail + 1) % b.capacity
		b.count--
	}
	return entries
}

// flush 强制刷新缓冲区，返回当前所有缓存的日志。
func (b *ringBuffer) flush() []*LogEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.drain()
}

// len 返回当前缓冲区中的日志数量。
func (b *ringBuffer) len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.count
}
