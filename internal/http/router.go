package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	api "github.com/Gtollm/TripGO/internal/generated"
	"github.com/Gtollm/TripGO/internal/http/domainerror"
	"github.com/Gtollm/TripGO/internal/http/health"
	createposition "github.com/Gtollm/TripGO/internal/http/position/create"
	listpositions "github.com/Gtollm/TripGO/internal/http/position/list"
	"github.com/Gtollm/TripGO/internal/http/ready"
	"github.com/Gtollm/TripGO/internal/http/response"
	createtrip "github.com/Gtollm/TripGO/internal/http/trip/create"
	finishtrip "github.com/Gtollm/TripGO/internal/http/trip/finish"
	gettrip "github.com/Gtollm/TripGO/internal/http/trip/get"
)

type server struct {
	createTrip     *createtrip.Handler
	getTrip        *gettrip.Handler
	finishTrip     *finishtrip.Handler
	createPosition *createposition.Handler
	listPositions  *listpositions.Handler
	ready          *ready.Handler
}

func NewRouter(
	createTrip *createtrip.Handler,
	getTrip *gettrip.Handler,
	finishTrip *finishtrip.Handler,
	createPosition *createposition.Handler,
	listPositions *listpositions.Handler,
	ready *ready.Handler,
) *chi.Mux {
	router := chi.NewRouter()
	router.NotFound(func(w http.ResponseWriter, r *http.Request) { response.ToError(w, r, domainerror.ErrRouteNotFound) })
	router.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) { response.ToError(w, r, domainerror.ErrMethodNotAllowed) })
	api.HandlerWithOptions(&server{
		createTrip:     createTrip,
		getTrip:        getTrip,
		finishTrip:     finishTrip,
		createPosition: createPosition,
		listPositions:  listPositions,
		ready:          ready,
	}, api.ChiServerOptions{
		BaseRouter: router,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, _ error) {
			response.ToError(w, r, domainerror.ErrInvalidRequest)
		},
	})
	return router
}

func (s *server) CreateTrip(w http.ResponseWriter, r *http.Request, p api.CreateTripParams) {
	s.createTrip.Handle(w, r, p)
}

func (s *server) GetTrip(w http.ResponseWriter, r *http.Request, id api.TripId) {
	s.getTrip.Handle(w, r, id)
}

func (s *server) FinishTrip(w http.ResponseWriter, r *http.Request, id api.TripId) {
	s.finishTrip.Handle(w, r, id)
}

func (s *server) CreateTripPosition(w http.ResponseWriter, r *http.Request, id api.TripId) {
	s.createPosition.Handle(w, r, id)
}

func (s *server) ListTripPositions(w http.ResponseWriter, r *http.Request, id api.TripId) {
	s.listPositions.Handle(w, r, id)
}

func (s *server) Health(w http.ResponseWriter, r *http.Request) { health.Handle(w, r) }
func (s *server) Ready(w http.ResponseWriter, r *http.Request)  { s.ready.Handle(w, r) }
