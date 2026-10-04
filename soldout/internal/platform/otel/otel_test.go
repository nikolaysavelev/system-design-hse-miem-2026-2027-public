package otel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/go-chi/chi/v5"
	gootel "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func setup(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	prevTP, prevProp := gootel.GetTracerProvider(), gootel.GetTextMapPropagator()
	gootel.SetTracerProvider(tp)
	gootel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { gootel.SetTracerProvider(prevTP); gootel.SetTextMapPropagator(prevProp) })
	return rec
}

func attr(attrs []attribute.KeyValue, key string) (attribute.Value, bool) {
	for _, a := range attrs {
		if string(a.Key) == key {
			return a.Value, true
		}
	}
	return attribute.Value{}, false
}

func TestNameByRouteUsesTemplate(t *testing.T) {
	rec := setup(t)
	r := chi.NewRouter()
	r.Use(NameByRoute)
	r.Post("/v1/orders/{id}/pay", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := Handler(r, "test")

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/orders/42/pay", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/metrics", nil)) // служебный — без span'а

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("ожидали 1 span (метрики не трассируются), получили %d", len(spans))
	}
	if got := spans[0].Name(); got != "POST /v1/orders/{id}/pay" {
		t.Fatalf("имя span'а по шаблону маршрута: получили %q", got)
	}
	if v, ok := attr(spans[0].Attributes(), "http.route"); !ok || v.AsString() != "/v1/orders/{id}/pay" {
		t.Fatalf("http.route: %v", v)
	}
}

func TestTransportRecordsAttemptAndPropagates(t *testing.T) {
	rec := setup(t)
	var gotTraceparent []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTraceparent = append(gotTraceparent, r.Header.Get("traceparent"))
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	client := &http.Client{Transport: Transport(nil, "mailgw")}

	ctx, parent := Start(context.Background(), "notification.Send")
	for attempt := 0; attempt < 2; attempt++ {
		req, _ := http.NewRequestWithContext(WithAttempt(ctx, attempt), http.MethodPost, srv.URL+"/send", nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	parent.End()

	var attempts []int64
	for _, s := range rec.Ended() {
		if s.Name() != "mailgw.POST /send" {
			continue
		}
		v, ok := attr(s.Attributes(), "retry.attempt")
		if !ok {
			t.Fatal("нет retry.attempt у клиентского span'а")
		}
		attempts = append(attempts, v.AsInt64())
		if s.Parent().SpanID() != parent.SpanContext().SpanID() {
			t.Fatal("клиентский span должен быть дочерним к notification.Send")
		}
	}
	if len(attempts) != 2 || attempts[0] != 0 || attempts[1] != 1 {
		t.Fatalf("ожидали две попытки 0 и 1 отдельными span'ами, получили %v", attempts)
	}
	for _, tp := range gotTraceparent {
		if !regexp.MustCompile(`^00-` + parent.SpanContext().TraceID().String() + `-[0-9a-f]{16}-01$`).MatchString(tp) {
			t.Fatalf("traceparent не передан шлюзу: %q", tp)
		}
	}
}

func TestTraceparentRoundTrip(t *testing.T) {
	setup(t)
	ctx, span := Start(context.Background(), "booking.PayOrder")
	defer span.End()
	tp := Traceparent(ctx)
	if tp == "" {
		t.Fatal("пустой traceparent при активном span'е")
	}
	remote := Extract(context.Background(), map[string]string{"Traceparent": tp})
	if TraceID(remote) != span.SpanContext().TraceID().String() {
		t.Fatalf("trace_id не восстановился из заголовка: %q", TraceID(remote))
	}
	if Traceparent(context.Background()) != "" {
		t.Fatal("без span'а traceparent должен быть пустым")
	}
}

func TestRouteSamplerDropsNoisyRoutes(t *testing.T) {
	s := routeSampler{def: sdktrace.AlwaysSample(), noisy: sdktrace.NeverSample(), prefixes: []string{"POST /v1/holds", "GET /v1/events/"}}
	cases := map[string]sdktrace.SamplingDecision{
		"POST /v1/holds":            sdktrace.Drop,
		"GET /v1/events/42/seatmap": sdktrace.Drop,
		"POST /v1/orders/42/pay":    sdktrace.RecordAndSample,
		"notifier.consume":          sdktrace.RecordAndSample,
	}
	for name, want := range cases {
		if got := s.ShouldSample(sdktrace.SamplingParameters{Name: name}).Decision; got != want {
			t.Fatalf("%s: решение %v, ожидали %v", name, got, want)
		}
	}
}
