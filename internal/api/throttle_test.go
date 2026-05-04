package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/httprate"
	"qms/internal/controller"
)

func TestOrdersThrottle(t *testing.T) {
	c := controller.New(controller.WithProcessDuration(time.Hour))
	s := newServer(c, httprate.LimitByIP(2, time.Minute))

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(`{"type":"normal"}`))
		rr := httptest.NewRecorder()
		s.ServeHTTP(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("request %d status=%d, want=%d", i+1, rr.Code, http.StatusCreated)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(`{"type":"vip"}`))
	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("throttled request status=%d, want=%d", rr.Code, http.StatusTooManyRequests)
	}
}
