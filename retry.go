package logsdk

import (
	"context"
	"math"
	"time"
)

// retryConfig 重试策略配置。
type retryConfig struct {
	maxRetries int           // 最大重试次数
	baseDelay  time.Duration // 基础延迟
	maxDelay   time.Duration // 最大延迟
}

// retryWithBackoff 使用指数退避策略重试执行 fn。
// 重试间隔: baseDelay * 2^attempt，最大不超过 maxDelay。
// 返回最后一次执行的结果错误。
func retryWithBackoff(ctx context.Context, cfg retryConfig, fn func() error) error {
	var lastErr error
	for attempt := 0; attempt <= cfg.maxRetries; attempt++ {
		// 首次不重试
		if attempt > 0 {
			delay := time.Duration(math.Min(
				float64(cfg.baseDelay)*math.Pow(2, float64(attempt-1)),
				float64(cfg.maxDelay),
			))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		lastErr = fn()
		if lastErr == nil {
			return nil
		}
	}
	return lastErr
}

// defaultRetryConfig 返回 SDK 默认重试配置。
func defaultRetryConfig() retryConfig {
	return retryConfig{
		maxRetries: 3,               // 最多重试 3 次
		baseDelay:  500 * time.Millisecond, // 基础延迟 500ms
		maxDelay:   10 * time.Second,       // 最大延迟 10s
	}
}
