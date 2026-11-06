package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	api "github.com/Gtollm/TripGO/internal/generated"
	"github.com/Gtollm/TripGO/internal/http/domainerror"
	"github.com/Gtollm/TripGO/internal/http/health"
	"github.com/Gtollm/TripGO/internal/http/ready"
	"github.com/Gtollm/TripGO/internal/http/response"
	"github.com/Gtollm/TripGO/internal/http/trip/create"
	"github.com/Gtollm/TripGO/internal/http/trip/finish"
	"github.com/Gtollm/TripGO/internal/http/trip/get"
)

type server struct {
	create *create.Handler
	get    *get.Handler
	finish *finish.Handler
	ready  *ready.Handler
}

func NewRouter(create *create.Handler, get *get.Handler, finish *finish.Handler, ready *ready.Handler) *chi.Mux {
	router := chi.NewRouter()
	router.NotFound(func(w http.ResponseWriter, r *http.Request) { response.ToError(w, r, domainerror.ErrRouteNotFound) })
	router.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) { response.ToError(w, r, domainerror.ErrMethodNotAllowed) })
	api.HandlerWithOptions(&server{
		create: create,
		get:    get,
		finish: finish,
		ready:  ready,
	}, api.ChiServerOptions{
		BaseRouter: router,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, _ error) {
			response.ToError(w, r, domainerror.ErrInvalidRequest)
		},
	})
	return router
}

func (s *server) CreateTrip(w http.ResponseWriter, r *http.Request, p api.CreateTripParams) {
	s.create.Handle(w, r, p)
}

func (s *server) GetTrip(w http.ResponseWriter, r *http.Request, id api.TripId) {
	s.get.Handle(w, r, id)
}

func (s *server) FinishTrip(w http.ResponseWriter, r *http.Request, id api.TripId) {
	s.finish.Handle(w, r, id)
}
func (s *server) Health(w http.ResponseWriter, r *http.Request) { health.Handle(w, r) }
func (s *server) Ready(w http.ResponseWriter, r *http.Request)  { s.ready.Handle(w, r) }
