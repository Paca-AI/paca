package middleware

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

type bindReq struct {
	Name string `json:"name" binding:"required"`
}

func TestBindJSON_Success(t *testing.T) {
	r := gin.New()
	r.POST("/bind", func(c *gin.Context) {
		var req bindReq
		if !BindJSON(c, &req) {
			return
		}
		c.JSON(http.StatusOK, gin.H{"name": req.Name})
	})

	body := bytes.NewBufferString(`{"name":"alice"}`)
	req := httptest.NewRequest(http.MethodPost, "/bind", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestBindJSON_Failure(t *testing.T) {
	r := gin.New()
	r.POST("/bind", func(c *gin.Context) {
		var req bindReq
		if !BindJSON(c, &req) {
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	body := bytes.NewBufferString(`{"name":""}`)
	req := httptest.NewRequest(http.MethodPost, "/bind", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", w.Code, w.Body.String())
	}

	var env struct {
		ErrorCode string `json:"error_code"`
	}
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if env.ErrorCode != "BAD_REQUEST" {
		t.Fatalf("expected BAD_REQUEST, got %q", env.ErrorCode)
	}
}
