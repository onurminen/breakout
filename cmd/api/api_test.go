package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"

	resultsv1 "github.com/onurminen/breakout/gen/go/breakout/results/v1"
	ticksv1 "github.com/onurminen/breakout/gen/go/breakout/ticks/v1"
	"github.com/onurminen/breakout/internal/store"
	"github.com/onurminen/breakout/internal/stream"
)

type fakeStore struct {
	symbols        []store.Symbol
	results        []*resultsv1.EmaResult
	advisories     []*resultsv1.Advisory
	gotSymbol      string
	gotFrom, gotTo int64
}

func (f *fakeStore) Symbols(context.Context) ([]store.Symbol, error) { return f.symbols, nil }
func (f *fakeStore) History(_ context.Context, symbol string, from, to int64) ([]*resultsv1.EmaResult, []*resultsv1.Advisory, error) {
	f.gotSymbol, f.gotFrom, f.gotTo = symbol, from, to
	return f.results, f.advisories, nil
}

func testAPI(f *fakeStore) *api {
	return &api{store: f, hub: newHub(), upstream: &http.Client{Timeout: time.Second}}
}

func TestREST(t *testing.T) {
	f := &fakeStore{
		symbols:    []store.Symbol{{Symbol: "PX1.FR", SecType: ticksv1.SecType_SEC_TYPE_EQUITY}},
		results:    []*resultsv1.EmaResult{{Symbol: "PX1.FR", WindowStartMs: 300_000, LastPrice: 7}},
		advisories: []*resultsv1.Advisory{{Symbol: "PX1.FR", WindowStartMs: 300_000, Side: resultsv1.Side_SIDE_BUY}},
	}
	a := testAPI(f)
	for _, path := range []string{"/api/history", "/api/history?symbol=PX1.FR&from=x&to=5", "/api/history?symbol=PX1.FR&from=5&to=5"} {
		r := httptest.NewRecorder()
		a.routes().ServeHTTP(r, httptest.NewRequest("GET", path, nil))
		if r.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d", path, r.Code)
		}
	}
	r := httptest.NewRecorder()
	a.routes().ServeHTTP(r, httptest.NewRequest("GET", "/api/symbols", nil))
	if r.Code != http.StatusOK || !strings.Contains(r.Body.String(), `"secType":"SEC_TYPE_EQUITY"`) {
		t.Fatalf("symbols: %d %s", r.Code, r.Body.String())
	}
	r = httptest.NewRecorder()
	a.routes().ServeHTTP(r, httptest.NewRequest("GET", "/api/history?symbol=PX1.FR&from=0&to=600000", nil))
	if r.Code != http.StatusOK || f.gotSymbol != "PX1.FR" || f.gotFrom != 0 || f.gotTo != 600_000 {
		t.Fatalf("history request: %d %s", r.Code, r.Body.String())
	}
	var body struct {
		EmaResults []map[string]any `json:"emaResults"`
		Advisories []map[string]any `json:"advisories"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.EmaResults) != 1 || body.EmaResults[0]["windowStartMs"] != "300000" || len(body.Advisories) != 1 || body.Advisories[0]["side"] != "SIDE_BUY" {
		t.Fatalf("history JSON: %s", r.Body.String())
	}
	r = httptest.NewRecorder()
	a.routes().ServeHTTP(r, httptest.NewRequest("GET", "/", nil))
	if r.Code != http.StatusNotFound || !strings.Contains(r.Body.String(), "UI is not built yet") {
		t.Fatalf("static: %d %s", r.Code, r.Body.String())
	}
}

func TestKPI(t *testing.T) {
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("query") == "" {
			t.Error("missing Prometheus query")
		}
		if strings.Contains(r.URL.Query().Get("query"), "late_events_total") {
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
		} else if strings.Contains(r.URL.Query().Get("query"), "engine_event_latency_seconds_bucket") {
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"value":[1,"NaN"]}]}}`))
		} else {
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"value":[1,"2.5"]}]}}`))
		}
	}))
	defer prom.Close()
	a := testAPI(&fakeStore{})
	a.prometheusURL = prom.URL
	r := httptest.NewRecorder()
	a.routes().ServeHTTP(r, httptest.NewRequest("GET", "/api/kpis", nil))
	if r.Code != http.StatusOK {
		t.Fatalf("kpis: %d %s", r.Code, r.Body.String())
	}
	var body map[string]*float64
	if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["eventsPerSecond"] == nil || *body["eventsPerSecond"] != 2.5 || body["lateEvents"] != nil || body["eventLatencyP95Seconds"] != nil {
		t.Fatalf("kpis: %s", r.Body.String())
	}
	prom.Close()
	r = httptest.NewRecorder()
	a.routes().ServeHTTP(r, httptest.NewRequest("GET", "/api/kpis", nil))
	if r.Code != http.StatusBadGateway {
		t.Fatalf("upstream failure: %d", r.Code)
	}
}

func TestHubSubscriptionsAndSlowClient(t *testing.T) {
	h := newHub()
	a := h.add(nil)
	b := h.add(nil)
	defer a.close("test")
	defer b.close("test")
	if !h.change(a, []string{"A"}, true) || !h.change(b, []string{"B"}, true) {
		t.Fatal("subscribe failed")
	}
	if msg := <-a.send; msg.kind != "subscribed" {
		t.Fatalf("first message: %s", msg.kind)
	}
	<-b.send
	h.publish("A", "ema", []byte("A"))
	if msg := <-a.send; msg.kind != "ema" {
		t.Fatalf("A got %s", msg.kind)
	}
	select {
	case <-b.send:
		t.Fatal("B received A")
	default:
	}
	if !h.change(a, []string{"A"}, false) {
		t.Fatal("unsubscribe failed")
	}
	if msg := <-a.send; msg.kind != "unsubscribed" {
		t.Fatalf("unsubscribe confirmation: %s", msg.kind)
	}
	h.publish("A", "ema", []byte("A"))
	select {
	case <-a.send:
		t.Fatal("A received after unsubscribe")
	default:
	}
	for range sendQueueSize {
		h.publish("B", "ema", []byte("B"))
	}
	h.publish("B", "ema", []byte("B"))
	select {
	case <-b.stop:
	default:
		t.Fatal("slow client was not disconnected")
	}
}

func TestWebSocketAcknowledgesBeforeLive(t *testing.T) {
	a := testAPI(&fakeStore{})
	server := httptest.NewServer(a.routes())
	defer server.Close()
	conn, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"subscribe":["A"]}`)); err != nil {
		t.Fatal(err)
	}
	_, data, err := conn.Read(ctx)
	if err != nil || !strings.Contains(string(data), `"type":"subscribed"`) {
		t.Fatalf("ack: %s %v", data, err)
	}
	a.hub.publish("A", "ema", []byte(`{"type":"ema","data":{}}`))
	_, data, err = conn.Read(ctx)
	if err != nil || !strings.Contains(string(data), `"type":"ema"`) {
		t.Fatalf("live: %s %v", data, err)
	}
}

func TestEncodeRecord(t *testing.T) {
	value, err := proto.Marshal(&resultsv1.EmaResult{Symbol: "A", WindowStartMs: 300_000})
	if err != nil {
		t.Fatal(err)
	}
	kind, symbol, data, err := encodeRecord(&kgo.Record{Topic: stream.EmaResultsTopic, Value: value})
	if err != nil || kind != "ema" || symbol != "A" || !strings.Contains(string(data), `"windowStartMs":"300000"`) {
		t.Fatalf("encoded: %s %s %s %v", kind, symbol, data, err)
	}
}
