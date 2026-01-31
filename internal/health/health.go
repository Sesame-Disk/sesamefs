package health

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// DatabaseChecker checks database connectivity.
type DatabaseChecker interface {
	Ping(ctx context.Context) error
}

// StorageChecker checks storage backend connectivity.
type StorageChecker interface {
	HeadBucket(ctx context.Context) error
}

// Checker performs health checks against dependencies.
type Checker struct {
	db      DatabaseChecker
	storage StorageChecker
	timeout time.Duration
	version string
}

// NewChecker creates a new health checker.
func NewChecker(db DatabaseChecker, storage StorageChecker, timeout time.Duration, version string) *Checker {
	return &Checker{
		db:      db,
		storage: storage,
		timeout: timeout,
		version: version,
	}
}

// checkResult holds the result of a single dependency check.
type checkResult struct {
	Name   string
	Status string
	Err    error
}

// HandleLiveness handles GET /health — always returns 200 if the process is alive.
func (h *Checker) HandleLiveness(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"version": h.version,
	})
}

// HandleReadiness handles GET /ready — checks all dependencies.
func (h *Checker) HandleReadiness(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), h.timeout)
	defer cancel()

	checks := make(map[string]string)
	allOK := true

	var mu sync.Mutex
	var wg sync.WaitGroup

	// Check database
	if h.db != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := h.db.Ping(ctx)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				checks["database"] = fmt.Sprintf("error: %v", err)
				allOK = false
			} else {
				checks["database"] = "ok"
			}
		}()
	}

	// Check storage
	if h.storage != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := h.storage.HeadBucket(ctx)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				checks["storage"] = fmt.Sprintf("error: %v", err)
				allOK = false
			} else {
				checks["storage"] = "ok"
			}
		}()
	}

	wg.Wait()

	if allOK {
		c.JSON(http.StatusOK, gin.H{
			"status": "ready",
			"checks": checks,
		})
	} else {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "not_ready",
			"checks": checks,
		})
	}
}
