package health

import (
	"net/http"

	api "github.com/Gtollm/TripGO/internal/generated"
	"github.com/Gtollm/TripGO/internal/http/response"
)

func Handle(w http.ResponseWriter, r *http.Request) {
	response.ToJSON(w, r, http.StatusOK, api.HealthResponse{Status: api.Ok})
}
