package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httprate"
	"github.com/gorilla/websocket"
	httpSwagger "github.com/swaggo/http-swagger"
	"qms/internal/controller"
	"qms/internal/order"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Server struct {
	ctrl   *controller.Controller
	router chi.Router
}

func New(c *controller.Controller) *Server {
	return newServer(c, httprate.LimitByIP(3, time.Minute))
}

func newServer(c *controller.Controller, orderThrottle func(http.Handler) http.Handler) *Server {
	s := &Server{
		ctrl:   c,
		router: chi.NewRouter(),
	}
	s.router.With(orderThrottle).Post("/orders", s.createOrder)
	s.router.Post("/bots", s.addBot)
	s.router.Delete("/bots", s.removeBot)
	s.router.Get("/state", s.getState)
	s.router.Get("/swagger/*", httpSwagger.WrapHandler)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

type createOrderRequest struct {
	Type string `json:"type" example:"normal" enums:"normal,vip"`
}

// createOrder creates a new order.
// @Summary Create order
// @Description Creates a new order. VIP orders are prioritized ahead of normal orders.
// @Tags orders
// @Accept json
// @Produce json
// @Param request body createOrderRequest true "Order payload"
// @Success 201 {object} orderResp
// @Failure 400 {string} string
// @Failure 429 {string} string
// @Router /orders [post]
func (s *Server) createOrder(w http.ResponseWriter, r *http.Request) {
	var req createOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	var t order.Type
	switch req.Type {
	case "vip":
		t = order.VIP
	case "normal", "":
		t = order.Normal
	default:
		http.Error(w, `type must be "normal" or "vip"`, http.StatusBadRequest)
		return
	}

	o := s.ctrl.NewOrder(t)
	writeJSON(w, http.StatusCreated, orderJSON(o))
}

// addBot creates a new bot.
// @Summary Add bot
// @Description Adds a bot that can process pending orders.
// @Tags bots
// @Produce json
// @Success 201 {object} map[string]int
// @Router /bots [post]
func (s *Server) addBot(w http.ResponseWriter, r *http.Request) {
	s.ctrl.AddBot()
	writeJSON(w, http.StatusCreated, map[string]int{"bot_count": s.ctrl.BotCount()})
}

// removeBot removes the newest bot.
// @Summary Remove bot
// @Description Removes the newest bot. Returns 404 if no bots exist.
// @Tags bots
// @Produce json
// @Success 200 {object} map[string]int
// @Failure 404 {string} string
// @Router /bots [delete]
func (s *Server) removeBot(w http.ResponseWriter, r *http.Request) {
	// TODO add function to remove specific bot by ID
	if !s.ctrl.RemoveBot() {
		http.Error(w, "no bots to remove", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"bot_count": s.ctrl.BotCount()})
}

type botResp struct {
	ID           int        `json:"id"`
	CurrentOrder *orderResp `json:"current_order,omitempty"`
}
type stateResp struct {
	Pending   []orderResp `json:"pending"`
	Completed []orderResp `json:"completed"`
	Bots      []botResp   `json:"bots"`
}

func (s *Server) buildState() stateResp {
	st := s.ctrl.State()
	resp := stateResp{
		Pending:   make([]orderResp, len(st.Pending)),
		Completed: make([]orderResp, len(st.Completed)),
		Bots:      make([]botResp, len(st.Bots)),
	}
	for i, o := range st.Pending {
		resp.Pending[i] = orderJSON(o)
	}
	for i, o := range st.Completed {
		resp.Completed[i] = orderJSON(o)
	}
	for i, b := range st.Bots {
		br := botResp{ID: b.ID}
		if b.CurrentOrder != nil {
			v := orderJSON(b.CurrentOrder)
			br.CurrentOrder = &v
		}
		resp.Bots[i] = br
	}
	return resp
}

// getState returns current queue state.
// @Summary Get state
// @Description Returns a JSON snapshot of queue state. WebSocket upgrades on this path stream updates.
// @Tags state
// @Produce json
// @Success 200 {object} stateResp
// @Router /state [get]
func (s *Server) getState(w http.ResponseWriter, r *http.Request) {
	// If WebSocket upgrade requested, switch to streaming updates instead of one-time snapshot.
	if websocket.IsWebSocketUpgrade(r) {
		s.streamState(w, r)
		return
	}
	// Otherwise, return a single snapshot.
	writeJSON(w, http.StatusOK, s.buildState())
}

func (s *Server) streamState(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ch := s.ctrl.Subscribe()
	defer s.ctrl.Unsubscribe(ch)

	// Send initial snapshot immediately.
	if err := conn.WriteJSON(s.buildState()); err != nil {
		return
	}

	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
			if err := conn.WriteJSON(s.buildState()); err != nil {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

type orderResp struct {
	ID        int       `json:"id"`
	Type      string    `json:"type"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func orderJSON(o *order.Order) orderResp {
	return orderResp{
		ID:        o.ID,
		Type:      o.Type.String(),
		Status:    o.Status.String(),
		CreatedAt: o.CreatedAt,
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
