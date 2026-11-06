// Package respond writes transport responses shared by handlers.
package respond

import (
	"encoding/json"
	"net/http"

	"github.com/Makk228/avito-labs/internal/domain/trip"
	api "github.com/Makk228/avito-labs/internal/generated"
	"github.com/Makk228/avito-labs/internal/http/domainerror"
	"github.com/Makk228/avito-labs/internal/http/requeststate"
)

func JSON(w http.ResponseWriter, status int, body any) { Body(w, status, "application/json", body) }

func Body(w http.ResponseWriter, status int, contentType string, body any) {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	// The response cannot be replaced once its headers have been sent.
	_ = json.NewEncoder(w).Encode(body)
}

func Error(w http.ResponseWriter, r *http.Request, err error) {
	requeststate.Record(r.Context(), err)
	status, body := domainerror.Problem(err)
	instance := r.URL.Path
	body.Instance = &instance
	Body(w, status, "application/problem+json", body)
}

func Invalid(w http.ResponseWriter, r *http.Request, detail string) {
	Error(w, r, &trip.ValidationError{Message: detail})
}

func Routing(w http.ResponseWriter, r *http.Request, status int, slug, title, code, detail string) {
	if status < 100 || status > 599 {
		status = http.StatusInternalServerError
	}
	instance := r.URL.Path
	Body(w, status, "application/problem+json", api.Problem{Type: "https://tripgo.example/problems/" + slug, Title: title, Status: int32(status), Code: code, Detail: &detail, Instance: &instance})
}
