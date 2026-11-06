package response

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Gtollm/TripGO/internal/http/domainerror"
	"github.com/Gtollm/TripGO/internal/http/requeststate"
)

func ToJSON(w http.ResponseWriter, r *http.Request, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	toWrite(w, r, status, body)
}

func ToError(w http.ResponseWriter, r *http.Request, err error) {
	status, body := domainerror.ToProblem(err)
	instance := r.URL.Path
	body.Instance = &instance
	requeststate.Fail(r.Context(), err, body.Code)
	w.Header().Set("Content-Type", "application/problem+json")
	toWrite(w, r, status, body)
}

func toWrite(w http.ResponseWriter, r *http.Request, status int, body any) {
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		requeststate.Fail(r.Context(), fmt.Errorf("encode response: %w", err), "response_write_error")
	}
}
