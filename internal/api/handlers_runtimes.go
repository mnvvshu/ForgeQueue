package api

import (
	"net/http"

	"github.com/forgequeue/forgequeue/internal/runtime"
)

type RuntimesHandler struct {
	registry *runtime.Registry
}

func NewRuntimesHandler(reg *runtime.Registry) *RuntimesHandler {
	if reg == nil {
		reg = runtime.Default()
	}
	return &RuntimesHandler{registry: reg}
}

func (h *RuntimesHandler) List(w http.ResponseWriter, r *http.Request) {
	definitions := h.registry.List()
	RespondJSON(w, http.StatusOK, definitions)
}
