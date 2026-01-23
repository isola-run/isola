package middleware

import (
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	apierrors "github.com/isola-ai/isola-sb/internal/api/errors"
)

// Recovery middleware recovers from panics and logs them with Zap.
func Recovery(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				var requestID string
				if id, exists := c.Get("request_id"); exists {
					requestID = id.(string)
				}

				logger.Error("panic recovered",
					zap.Any("panic", r),
					zap.String("request_id", requestID),
					zap.String("path", c.Request.URL.Path),
					zap.String("method", c.Request.Method),
					zap.ByteString("stack", debug.Stack()),
				)

				c.AbortWithStatusJSON(http.StatusInternalServerError, apierrors.ErrorResponse{
					Error:     "internal server error",
					RequestID: requestID,
				})
			}
		}()
		c.Next()
	}
}
