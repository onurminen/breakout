package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	resultsv1 "github.com/onurminen/breakout/gen/go/breakout/results/v1"
	"github.com/onurminen/breakout/internal/store"
)

var (
	wsClients     = promauto.NewGauge(prometheus.GaugeOpts{Name: "api_ws_clients", Help: "Connected WebSocket clients."})
	wsMessages    = promauto.NewCounterVec(prometheus.CounterOpts{Name: "api_ws_messages_total", Help: "WebSocket messages written to clients."}, []string{"type"})
	wsDisconnects = promauto.NewCounterVec(prometheus.CounterOpts{Name: "api_ws_disconnects_total", Help: "WebSocket clients disconnected."}, []string{"reason"})
	httpSeconds   = promauto.NewHistogramVec(prometheus.HistogramOpts{Name: "api_http_request_seconds", Help: "HTTP request duration by route.", Buckets: prometheus.DefBuckets}, []string{"route"})
)

type historyStore interface {
	Symbols(context.Context) ([]store.Symbol, error)
	History(context.Context, string, int64, int64) ([]*resultsv1.EmaResult, []*resultsv1.Advisory, error)
}

type api struct {
	store         historyStore
	upstream      *http.Client
	prometheusURL string
	uiDir         string
	hub           *hub
}

func (a *api) routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /api/symbols", a.timed("/api/symbols", http.HandlerFunc(a.symbols)))
	mux.Handle("GET /api/history", a.timed("/api/history", http.HandlerFunc(a.history)))
	mux.Handle("GET /api/kpis", a.timed("/api/kpis", http.HandlerFunc(a.kpis)))
	mux.Handle("GET /ws", a.timed("/ws", http.HandlerFunc(a.websocket)))
	mux.Handle("GET /", a.timed("/", http.HandlerFunc(a.static)))
	return mux
}

func (a *api) timed(route string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		httpSeconds.WithLabelValues(route).Observe(time.Since(start).Seconds())
	})
}

func (a *api) symbols(w http.ResponseWriter, r *http.Request) {
	symbols, err := a.store.Symbols(r.Context())
	if err != nil {
		log.Printf("list symbols: %v", err)
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	items := make([]struct {
		Symbol  string `json:"symbol"`
		SecType string `json:"secType"`
	}, 0, len(symbols))
	for _, s := range symbols {
		items = append(items, struct {
			Symbol  string `json:"symbol"`
			SecType string `json:"secType"`
		}{s.Symbol, s.SecType.String()})
	}
	writeJSON(w, items)
}

func (a *api) history(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	symbol := q.Get("symbol")
	from, fromErr := strconv.ParseInt(q.Get("from"), 10, 64)
	to, toErr := strconv.ParseInt(q.Get("to"), 10, 64)
	if symbol == "" || strings.TrimSpace(symbol) != symbol || fromErr != nil || toErr != nil || from >= to {
		http.Error(w, "symbol, from, and to are required; from and to must be epoch milliseconds with from < to", http.StatusBadRequest)
		return
	}
	results, advisories, err := a.store.History(r.Context(), symbol, from, to)
	if err != nil {
		log.Printf("load history: %v", err)
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	emaJSON, err := messagesJSON(results)
	if err != nil {
		http.Error(w, "encode history", http.StatusInternalServerError)
		return
	}
	advisoryJSON, err := messagesJSON(advisories)
	if err != nil {
		http.Error(w, "encode history", http.StatusInternalServerError)
		return
	}
	writeJSON(w, struct {
		EmaResults []json.RawMessage `json:"emaResults"`
		Advisories []json.RawMessage `json:"advisories"`
	}{emaJSON, advisoryJSON})
}

func messagesJSON[T proto.Message](messages []T) ([]json.RawMessage, error) {
	out := make([]json.RawMessage, 0, len(messages))
	for _, message := range messages {
		data, err := protojson.Marshal(message)
		if err != nil {
			return nil, err
		}
		out = append(out, data)
	}
	return out, nil
}

func (a *api) static(w http.ResponseWriter, r *http.Request) {
	if a.uiDir == "" {
		http.Error(w, "UI is not built yet", http.StatusNotFound)
		return
	}
	index := filepath.Join(a.uiDir, "index.html")
	if _, err := os.Stat(index); err != nil {
		http.Error(w, "UI is not built yet", http.StatusNotFound)
		return
	}
	http.FileServer(http.Dir(a.uiDir)).ServeHTTP(w, r)
}

func (a *api) websocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return // Accept has already written the HTTP error.
	}
	conn.SetReadLimit(16 * 1024)
	client := a.hub.add(conn)
	ctx, cancel := context.WithCancel(context.Background())
	writeDone := make(chan struct{})
	go func() { defer close(writeDone); client.write(ctx) }()
	defer func() {
		client.close("normal")
		cancel()
		<-writeDone
	}()
	for {
		kind, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if kind != websocket.MessageText {
			client.close("bad_request")
			return
		}
		var req struct {
			Subscribe   []string `json:"subscribe"`
			Unsubscribe []string `json:"unsubscribe"`
		}
		if err := json.Unmarshal(data, &req); err != nil || (req.Subscribe == nil) == (req.Unsubscribe == nil) {
			client.close("bad_request")
			return
		}
		names, subscribing := req.Subscribe, true
		if req.Unsubscribe != nil {
			names, subscribing = req.Unsubscribe, false
		}
		if !validSymbols(names) {
			client.close("bad_request")
			return
		}
		if !a.hub.change(client, names, subscribing) {
			client.close("slow")
			return
		}
	}
}

var kpiQueries = []struct{ key, query string }{
	{"eventsPerSecond", `sum(rate(engine_events_total[15s]))`},
	{"eventLatencyP95Seconds", `histogram_quantile(0.95, sum by (le) (rate(engine_event_latency_seconds_bucket[30s])))`},
	{"windowResultLatencyP95Seconds", `histogram_quantile(0.95, sum by (le) (rate(engine_window_result_latency_seconds_bucket[30s])))`},
	{"lateEvents", `sum(late_events_total)`},
}

func (a *api) kpis(w http.ResponseWriter, r *http.Request) {
	values := make(map[string]*float64, len(kpiQueries))
	for _, metric := range kpiQueries {
		value, err := a.queryPrometheus(r.Context(), metric.query)
		if err != nil {
			log.Printf("query Prometheus: %v", err)
			http.Error(w, "Prometheus unavailable", http.StatusBadGateway)
			return
		}
		values[metric.key] = value
	}
	writeJSON(w, values)
}

func (a *api) queryPrometheus(ctx context.Context, query string) (*float64, error) {
	endpoint, err := url.Parse(strings.TrimRight(a.prometheusURL, "/") + "/api/v1/query")
	if err != nil {
		return nil, err
	}
	q := endpoint.Query()
	q.Set("query", query)
	endpoint.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	response, err := a.upstream.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Prometheus returned %s", response.Status)
	}
	var body struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Value []json.RawMessage `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return nil, err
	}
	if body.Status != "success" || body.Data.ResultType != "vector" {
		return nil, fmt.Errorf("unexpected Prometheus response")
	}
	if len(body.Data.Result) == 0 {
		return nil, nil
	}
	if len(body.Data.Result) != 1 || len(body.Data.Result[0].Value) != 2 {
		return nil, fmt.Errorf("unexpected Prometheus vector")
	}
	var raw string
	if err := json.Unmarshal(body.Data.Result[0].Value[1], &raw); err != nil {
		return nil, err
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid Prometheus value %q", raw)
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil, nil // Prometheus may emit NaN before a rate window has samples.
	}
	return &value, nil
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write JSON response: %v", err)
	}
}
