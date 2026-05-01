package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"qms/internal/controller"
	"qms/internal/order"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Server struct {
	ctrl *controller.Controller
	mux  *http.ServeMux
}

func New(c *controller.Controller) *Server {
	s := &Server{ctrl: c, mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /orders", s.createOrder)
	s.mux.HandleFunc("POST /bots", s.addBot)
	s.mux.HandleFunc("DELETE /bots", s.removeBot)
	s.mux.HandleFunc("GET /state", s.getState)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// POST /orders  body: {"type":"normal"|"vip"}
func (s *Server) createOrder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type string `json:"type"`
	}
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

// POST /bots
func (s *Server) addBot(w http.ResponseWriter, r *http.Request) {
	s.ctrl.AddBot()
	writeJSON(w, http.StatusCreated, map[string]int{"bot_count": s.ctrl.BotCount()})
}

// DELETE /bots
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

// GET /state – plain HTTP returns a JSON snapshot; WebSocket clients receive a
// push on every state change.
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
