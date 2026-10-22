package server

import (
	"net/http"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
)

// Runtime diagnostics remain administrator-only and expose no paths or tokens.
func (a *App) adminMetrics(c *gin.Context) {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	movies := 0
	for _, status := range []string{"success", "manual", "pending", "incompatible"} {
		count, err := a.db.CountByStatus(status)
		if err != nil {
			c.JSON(500, gin.H{"error": "metrics unavailable"})
			return
		}
		movies += count
	}
	users, err := a.db.Users()
	if err != nil {
		c.JSON(500, gin.H{"error": "metrics unavailable"})
		return
	}
	libraries, err := a.db.Libraries()
	if err != nil {
		c.JSON(500, gin.H{"error": "metrics unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"uptime_seconds": int64(time.Since(a.startAt).Seconds()), "goroutines": runtime.NumGoroutine(),
		"heap_alloc": memory.HeapAlloc, "heap_inuse": memory.HeapInuse, "sys": memory.Sys, "num_gc": memory.NumGC,
		"movies": movies, "users": len(users), "libraries": len(libraries), "cache_stats": a.cache.Stats(),
	})
}
