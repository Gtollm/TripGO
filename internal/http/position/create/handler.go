package create

import (
	"net/http"

	"github.com/Gtollm/TripGO/internal/domain/trip"
	api "github.com/Gtollm/TripGO/internal/generated"
	"github.com/Gtollm/TripGO/internal/http/domainerror"
	"github.com/Gtollm/TripGO/internal/http/position/convert"
	"github.com/Gtollm/TripGO/internal/http/response"
	"github.com/Gtollm/TripGO/internal/model"
)

type Handler struct{ usecase usecase }

func New(usecase usecase) *Handler { return &Handler{usecase: usecase} }

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	body, err := decodeCreatePosition(w, r)
	if err != nil {
		response.ToError(w, r, domainerror.ErrInvalidRequest)
		return
	}

	result, err := h.usecase.Run(r.Context(), model.CreatePosition{
		TripID: tripID,
		Coordinates: trip.Coordinates{
			Latitude:  body.Coordinates.Latitude,
			Longitude: body.Coordinates.Longitude,
		},
		RecordedAt: body.RecordedAt,
	})
	if err != nil {
		response.ToError(w, r, err)
		return
	}

	response.ToJSON(w, r, http.StatusCreated, convert.ToAPI(result.Position))
}
