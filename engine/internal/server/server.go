package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/global-fintech/agidb/internal/engine"
	"github.com/global-fintech/agidb/internal/model"
)

type Server struct {
	Engine *engine.Engine
	Token  string
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/status", s.auth(s.status))
	mux.HandleFunc("/v1/tx", s.auth(s.tx))
	mux.HandleFunc("/v1/state/", s.auth(s.state))
	mux.HandleFunc("/v1/block/", s.auth(s.block))
	mux.HandleFunc("/v1/verify", s.auth(s.verify))
	return mux
}
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Token != "" && r.Header.Get("Authorization") != "Bearer "+s.Token {
			http.Error(w, "unauthorized", 401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		next(w, r)
	}
}
func write(w http.ResponseWriter, v any)                        { _ = json.NewEncoder(w).Encode(v) }
func (s *Server) status(w http.ResponseWriter, r *http.Request) { write(w, s.Engine.Status()) }
func (s *Server) tx(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<20))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	var req struct {
		Sender  string            `json:"sender"`
		Nonce   uint64            `json:"nonce"`
		Ops     []model.Operation `json:"ops"`
		Payload []byte            `json:"payload"`
	}
	if err = json.Unmarshal(body, &req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	tx := engine.NewTransaction(req.Ops, req.Sender, req.Nonce, req.Payload)
	b, err := s.Engine.Commit([]model.Transaction{tx})
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	w.WriteHeader(201)
	write(w, b)
}
func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/v1/state/")
	h, _ := strconv.ParseUint(r.URL.Query().Get("height"), 10, 64)
	var v []byte
	var ok bool
	if h > 0 {
		v, ok = s.Engine.GetAt(key, h)
	} else {
		v, ok = s.Engine.Get(key)
	}
	if !ok {
		http.Error(w, "not found", 404)
		return
	}
	write(w, map[string]any{"key": key, "value": v, "height": h})
}
func (s *Server) block(w http.ResponseWriter, r *http.Request) {
	h, err := strconv.ParseUint(strings.TrimPrefix(r.URL.Path, "/v1/block/"), 10, 64)
	if err != nil {
		http.Error(w, "invalid height", 400)
		return
	}
	b, ok := s.Engine.Block(h)
	if !ok {
		http.Error(w, "not found", 404)
		return
	}
	write(w, b)
}
func (s *Server) verify(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	err := s.Engine.Verify()
	if err != nil {
		http.Error(w, fmt.Sprintf("verification failed: %v", err), 500)
		return
	}
	write(w, map[string]any{"verified": true, "elapsed_ms": time.Since(start).Milliseconds()})
}
