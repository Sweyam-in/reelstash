package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// pathInt64 reads a decimal chi path parameter. Callers choose the status
// code and message, so an invalid id still looks the same to clients.
func pathInt64(r *http.Request, name string) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, name), 10, 64)
}
