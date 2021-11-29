package main

import (
	"fmt"
	"log"

	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

/**
 * Main entry point for the web service
 */
func main() {
	log.Printf("===> virgo4-shelf-browse-ws starting up <===")

	cfg := loadConfig()
	svc := initializeService(cfg)

	gin.SetMode(gin.ReleaseMode)
	gin.DisableConsoleColor()

	router := gin.Default()

	router.Use(gzip.Gzip(gzip.DefaultCompression))

	corsCfg := cors.DefaultConfig()
	corsCfg.AllowAllOrigins = true
	corsCfg.AllowCredentials = true
	corsCfg.AddAllowHeaders("Authorization")
	router.Use(cors.New(corsCfg))

	//
	// we are removing Prometheus support for now
	//
	//p := ginprometheus.NewPrometheus("gin")

	// roundabout setup of /metrics endpoint to avoid double-gzip of response
	//router.Use(p.HandlerFunc())
	//h := promhttp.InstrumentMetricHandler(prometheus.DefaultRegisterer, promhttp.HandlerFor(prometheus.DefaultGatherer, promhttp.HandlerOpts{DisableCompression: true}))

	//router.GET(p.MetricsPath, func(c *gin.Context) {
	//	h.ServeHTTP(c.Writer, c.Request)
	//})

	router.GET("/favicon.ico", svc.ignoreHandler)

	router.GET("/version", svc.versionHandler)
	router.GET("/healthcheck", svc.healthCheckHandler)

	if api := router.Group("/api"); api != nil {
		api.GET("/browse/:id", svc.authenticateHandler, svc.browseHandler)
	}

	portStr := fmt.Sprintf(":%s", svc.config.Port)
	log.Printf("[MAIN] listening on %s", portStr)

	log.Fatal(router.Run(portStr))
}
