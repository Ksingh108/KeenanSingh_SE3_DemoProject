package handler

import (
	"mime"
	"net/http"

	"github.com/gringottsbankbranchbooking/internal/apierror"
)

func requireJSONContentType(w http.ResponseWriter, r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		apierror.Write(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return false
	}
	return true
}
