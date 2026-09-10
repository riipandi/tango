package routes

import (
	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/internal/transport/handler"
	"github.com/riipandi/tango/internal/transport/responder"
)

func RegisterAPI(r chi.Router) {
	r.NotFound(responder.NotFoundJSON)
	r.MethodNotAllowed(responder.MethodNotAllowedJSON)

	r.Get("/", handler.APIRootHandler)
	r.Get("/healthz", handler.HealthzHandler)

	DefineUserRoutes(r)
}

func DefineUserRoutes(r chi.Router) {
	r.Post("/users", handler.CreateUserHandler)
	r.Get("/users", handler.ListUsersHandler)
	r.Get("/users/{id}", handler.GetUserHandler)
}
