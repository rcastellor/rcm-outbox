package router

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/rcastellor/rcm-outbox/orders-api/internal/repository"
	"github.com/rcastellor/rcm-outbox/orders-api/internal/usecase"
)

// Router enruta las peticiones de API Gateway hacia los casos de uso del CRUD.
type Router struct {
	orders *usecase.Orders
}

// New crea el router de órdenes.
func New(orders *usecase.Orders) *Router {
	return &Router{orders: orders}
}

type apiError struct {
	Error string `json:"error"`
}

// Route despacha una petición y devuelve el código HTTP y el cuerpo a serializar.
// El parámetro proxy es el resto de la ruta tras /orders (vacío para la colección
// o el id de la orden).
func (r *Router) Route(ctx context.Context, method, proxy string, body []byte) (int, any) {
	if proxy == "" {
		switch method {
		case http.MethodGet:
			orders, err := r.orders.List(ctx)
			if err != nil {
				return http.StatusInternalServerError, apiError{"error interno"}
			}
			return http.StatusOK, orders

		case http.MethodPost:
			var in usecase.CreateOrderInput
			if err := json.Unmarshal(body, &in); err != nil {
				return http.StatusBadRequest, apiError{"cuerpo JSON inválido"}
			}
			o, err := r.orders.Create(ctx, in)
			if err != nil {
				return r.mapError(err)
			}
			return http.StatusCreated, o

		default:
			return http.StatusMethodNotAllowed, apiError{"método no soportado"}
		}
	}

	switch method {
	case http.MethodGet:
		o, err := r.orders.Get(ctx, proxy)
		if err != nil {
			return r.mapError(err)
		}
		return http.StatusOK, o

	case http.MethodPut:
		var in usecase.UpdateOrderInput
		if err := json.Unmarshal(body, &in); err != nil {
			return http.StatusBadRequest, apiError{"cuerpo JSON inválido"}
		}
		o, err := r.orders.Update(ctx, proxy, in)
		if err != nil {
			return r.mapError(err)
		}
		return http.StatusOK, o

	case http.MethodDelete:
		if err := r.orders.Delete(ctx, proxy); err != nil {
			return r.mapError(err)
		}
		return http.StatusNoContent, nil

	default:
		return http.StatusMethodNotAllowed, apiError{"método no soportado"}
	}
}

func (r *Router) mapError(err error) (int, any) {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return http.StatusNotFound, apiError{err.Error()}
	case errors.Is(err, usecase.ErrInvalidInput):
		return http.StatusBadRequest, apiError{err.Error()}
	default:
		return http.StatusInternalServerError, apiError{"error interno"}
	}
}
