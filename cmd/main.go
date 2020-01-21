package main

import (
	"net/http"
	"time"

	"github.com/chenjiandongx/dockerstats"
	"github.com/gin-contrib/cache"
	"github.com/gin-contrib/cache/persistence"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

var store = persistence.NewInMemoryStore(2 * time.Second)

func cacheMiddleware(handle gin.HandlerFunc) gin.HandlerFunc {
	return cache.CachePageAtomic(store, 4*time.Second, handle)
}

func main() {
	exporter := dockerstats.NewExporter()
	go exporter.Watch()

	type Response struct {
		Stats []*dockerstats.StatsEntry `json:"stats"`
		Msg   string                    `json:"msg"`
	}

	r := gin.Default()
	r.GET("/stats", cacheMiddleware(func(c *gin.Context) {
		result, err := exporter.List()
		if err != nil {
			c.JSON(http.StatusInternalServerError, Response{Msg: err.Error()})
		}
		c.JSON(http.StatusOK, Response{Stats: result})
	}))

	if err := r.Run("0.0.0.0:8099"); err != nil {
		logrus.Fatalf("start gin-server error: %+v", err)
	}
}
