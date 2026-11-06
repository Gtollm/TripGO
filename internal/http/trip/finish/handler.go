package finish

import (
	"net/http"

	api "github.com/Gtollm/TripGO/internal/generated"
	"github.com/Gtollm/TripGO/internal/http/response"
	"github.com/Gtollm/TripGO/internal/http/trip/convert"
	"github.com/Gtollm/TripGO/internal/model"
)

type Handler struct{ usecase usecase }

func New(usecase usecase) *Handler { return &Handler{usecase: usecase} }

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request, id api.TripId) {
	item, err := h.usecase.Run(r.Context(), model.FinishTrip{ID: id})
	if err != nil {
		response.ToError(w, r, err)
		return
	}

	body, err := convert.ToAPI(item.Trip)
	if err != nil {
		response.ToError(w, r, err)
		return
	}

	response.ToJSON(w, r, http.StatusOK, body)
}
