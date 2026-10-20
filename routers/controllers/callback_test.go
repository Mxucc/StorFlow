package controllers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Mxucc/StorFlow/pkg/filemanager/fs"
	"github.com/Mxucc/StorFlow/pkg/filemanager/manager"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestUpyunCallbackAuthAbortsInvalidSignature(t *testing.T) {
	gin.SetMode(gin.TestMode)

	nextCalled := false
	router := gin.New()
	router.POST(
		"/callback",
		func(c *gin.Context) {
			c.Set(manager.UploadSessionCtx, &fs.UploadSession{})
		},
		UpyunCallbackAuth,
		func(c *gin.Context) {
			nextCalled = true
			c.Status(http.StatusNoContent)
		},
	)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/callback", nil)
	router.ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
	assert.False(t, nextCalled)
}
