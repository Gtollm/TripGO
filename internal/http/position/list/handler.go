package list

import (
	"net/http"

	api "github.com/Gtollm/TripGO/internal/generated"
	"github.com/Gtollm/TripGO/internal/http/position/convert"
	"github.com/Gtollm/TripGO/internal/http/response"
	"github.com/Gtollm/TripGO/internal/model"
)

type Handler struct{ usecase usecase }

func New(usecase usecase) *Handler { return &Handler{usecase: usecase} }

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	result, err := h.usecase.Run(r.Context(), model.ListPositions{TripID: tripID})
	if err != nil {
		response.ToError(w, r, err)
		return
	}

	response.ToJSON(w, r, http.StatusOK, convert.ToListAPI(result.Positions))
}
