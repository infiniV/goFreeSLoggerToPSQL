package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"gofreeswitchesl/store"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

const (
	defaultLimit  = 10
	maxLimit      = 100
	defaultOffset = 0
)

// Server handles API requests
type Server struct {
	router *gin.Engine
	store  *store.Store
	log    *logrus.Logger
}

// NewServer creates a new API server
func NewServer(s *store.Store, logger *logrus.Logger) *Server {
	router := gin.New() // Using gin.New() for more control over middleware

	// Setup logger middleware
	router.Use(gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		logger.WithFields(logrus.Fields{
			"client_ip":  param.ClientIP,
			"method":     param.Method,
			"path":       param.Path,
			"status":     param.StatusCode,
			"latency":    param.Latency,
			"user_agent": param.Request.UserAgent(),
			"error":      param.ErrorMessage,
		}).Info("GIN Request")
		return "" // Don't write to stdout, logrus handles it
	}))

	// Setup recovery middleware
	router.Use(gin.CustomRecovery(func(c *gin.Context, recovered interface{}) {
		if err, ok := recovered.(string); ok {
			logger.WithField("error", err).Error("Panic recovered in GIN handler")
		}
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"error": "Internal Server Error",
		})
	}))

	srv := &Server{
		router: router,
		store:  s,
		log:    logger,
	}

	srv.setupRoutes()
	return srv
}

// setupRoutes defines the API routes
func (s *Server) setupRoutes() {
	api := s.router.Group("/api/v1") // Versioning the API
	{
		api.GET("/calls", s.getCallsHandler)
		api.GET("/calls/:uuid", s.getCallByUUIDHandler)
	}

	// Health check endpoint
	s.router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "UP"})
	})
}

// getCallsHandler handles GET /calls requests
func (s *Server) getCallsHandler(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", strconv.Itoa(defaultLimit))
	offsetStr := c.DefaultQuery("offset", strconv.Itoa(defaultOffset))

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 || limit > maxLimit {
		limit = defaultLimit
		s.log.Warnf("Invalid limit value '%s', using default %d", limitStr, limit)
	}

	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		offset = defaultOffset
		s.log.Warnf("Invalid offset value '%s', using default %d", offsetStr, offset)
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	calls, err := s.store.GetCalls(ctx, limit, offset)
	if err != nil {
		s.log.WithError(err).Error("Error retrieving calls from store")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve calls"})
		return
	}

	if calls == nil { // Ensure we return an empty list, not null, if no calls found
		calls = []store.Call{}
	}

	c.JSON(http.StatusOK, calls)
}

// getCallByUUIDHandler handles GET /calls/:uuid requests
func (s *Server) getCallByUUIDHandler(c *gin.Context) {
	uuid := c.Param("uuid")
	if uuid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "UUID parameter is required"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	call, err := s.store.GetCallByUUID(ctx, uuid)
	if err != nil {
		// TODO: Differentiate between not found and other errors
		// For now, assuming pgx.ErrNoRows will be logged by the store and we return 404
		// if errors.Is(err, pgx.ErrNoRows) { // Requires importing "errors" and "github.com/jackc/pgx/v5"
		// 	s.log.WithField("uuid", uuid).Warn("Call not found")
		// 	c.JSON(http.StatusNotFound, gin.H{"error": "Call not found"})
		// 	return
		// }
		s.log.WithError(err).WithField("uuid", uuid).Error("Error retrieving call by UUID from store")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve call"})
		return
	}

	if call == nil { // Should be handled by error check above, but as a safeguard
		c.JSON(http.StatusNotFound, gin.H{"error": "Call not found"})
		return
	}

	c.JSON(http.StatusOK, call)
}

// Start runs the API server
func (s *Server) Start(address string) error {
	s.log.Infof("API server starting on %s", address)
	return s.router.Run(address)
}

// GetRouter returns the underlying Gin router, useful for testing or embedding
func (s *Server) GetRouter() *gin.Engine {
	return s.router
}
