package create

import (
	"net/http"

	"github.com/Gtollm/TripGO/internal/domain/trip"
	api "github.com/Gtollm/TripGO/internal/generated"
	"github.com/Gtollm/TripGO/internal/http/domainerror"
	"github.com/Gtollm/TripGO/internal/http/response"
	"github.com/Gtollm/TripGO/internal/http/trip/convert"
	"github.com/Gtollm/TripGO/internal/model"
)

type Handler struct{ usecase usecase }

func New(usecase usecase) *Handler { return &Handler{usecase: usecase} }

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request, params api.CreateTripParams) {
	body, err := decodeCreateTrip(w, r)
	if err != nil {
		response.ToError(w, r, domainerror.ErrInvalidRequest)
		return
	}

	result, err := h.usecase.Run(r.Context(), model.CreateTrip{
		IdempotencyKey: params.IdempotencyKey,
		UserID:         body.UserId,
		DriverID:       body.DriverId,
		Start:          trip.Coordinates{Latitude: body.StartPoint.Latitude, Longitude: body.StartPoint.Longitude},
		End:            trip.Coordinates{Latitude: body.EndPoint.Latitude, Longitude: body.EndPoint.Longitude},
		Price:          body.Price,
	})
	if err != nil {
		response.ToError(w, r, err)
		return
	}

	out, err := convert.ToAPI(result.Trip)
	if err != nil {
		response.ToError(w, r, err)
		return
	}

	status := http.StatusCreated
	if result.Repeated {
		status = http.StatusOK
	} else {
		w.Header().Set("Location", "/api/v1/trips/"+result.Trip.ID.String())
	}

	response.ToJSON(w, r, status, out)
}
