package middleware

import (
	"net/http"

	"github.com/Paca-AI/api/internal/apierr"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// BindJSON decodes the JSON body into dst and writes a 400 on failure,
// including when anything but whitespace follows the body's JSON value.
// Use this in handlers to keep binding logic concise.
func BindJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := decodeSingleJSON(r.Body, dst); err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, err.Error()))
		return false
	}
	return true
}
