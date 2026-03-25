package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/paca/api/internal/apierr"
	"github.com/paca/api/internal/transport/http/presenter"
)

// BindJSON binds the JSON body into dst and aborts with 400 on failure.
// Use this in handlers to keep binding logic concise.
func BindJSON(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		presenter.Error(c, apierr.New(apierr.CodeBadRequest, err.Error()))
		return false
	}
	return true
}
