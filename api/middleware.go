package api

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// 全局锁映射，用于控制每个 token 的并发请求
var (
	tokenLocks      = make(map[string]*sync.Mutex)
	tokenLocksGuard = sync.Mutex{}
)

// getTokenLock 获取指定 token 的锁
func getTokenLock(token string) *sync.Mutex {
	tokenLocksGuard.Lock()
	defer tokenLocksGuard.Unlock()

	if lock, exists := tokenLocks[token]; exists {
		return lock
	}

	lock := &sync.Mutex{}
	tokenLocks[token] = lock
	return lock
}

// TokenConcurrencyMiddleware 控制Redis中token的使用频率
func TokenConcurrencyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 只对聊天完成请求进行并发控制
		if !strings.HasSuffix(c.Request.URL.Path, "/chat/completions") {
			c.Next()
			return
		}

		// 获取一个可用的token
		token, tenantURL := GetAvailableToken()
		if token == "" || tenantURL == "" {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "当前所有token都在使用中或冷却中，请稍后再试"})
			c.Abort()
			return
		}

		// 获取该token的锁
		lock := getTokenLock(token)

		// 尝试获取锁，这会阻塞直到获取到锁
		lock.Lock()

		// 更新请求状态
		err := SetTokenRequestStatus(token, TokenRequestStatus{
			InProgress:    true,
			LastRequestAt: time.Now(),
		})

		if err != nil {
			lock.Unlock()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "更新token请求状态失败"})
			c.Abort()
			return
		}

		// 在请求完成后释放锁
		c.Set("token_lock", lock)
		c.Set("token", token)
		c.Set("tenant_url", tenantURL)

		c.Next()
	}
}
