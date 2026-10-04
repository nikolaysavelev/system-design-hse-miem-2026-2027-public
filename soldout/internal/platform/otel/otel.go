// Package otel — трейсинг OpenTelemetry для soldout и его сервисов (занятие 4).
//
// Init регистрирует TracerProvider (OTLP/gRPC → otel-collector) и пропагатор W3C traceparent + baggage.
// Модули не импортируют OpenTelemetry напрямую: span'ы создаются через Start/End этого пакета,
// HTTP — через Handler/NameByRoute/Transport. Так правило «новый внешний вызов оборачивается в span»
// выполняется одной строкой и не тянет вендора в каждый модуль (.go-arch-lint.yml).
package otel

import (
	"context"
	"fmt"
	"strings"
	"time"

	gootel "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// instrumentation — имя инструментирующей библиотеки (scope) для всех span'ов проекта.
const instrumentation = "github.com/nikolaysavelev/soldout"

// Options — настройки трейсинга.
type Options struct {
	ServiceName    string
	ServiceVersion string
	Environment    string  // deployment.environment: lab
	Endpoint       string  // OTLP/gRPC, например http://otel-collector:4317; пусто — экспорт выключен
	SampleRatio    float64 // доля корневых трейсов (ParentBased(TraceIDRatioBased)); на стенде 1.0
	// NoisyRatio — доля для шумных маршрутов шторма (hold, вход в очередь, карта зала); 0 — как SampleRatio.
	// Под штормом это сотни трейсов в секунду: Tempo и коллектор не успевают, а pay нужен целиком.
	NoisyRatio  float64
	NoisyRoutes []string // префиксы имени корневого span'а ("POST /v1/holds")
}

// Init настраивает глобальные TracerProvider и пропагатор. Возвращает shutdown с flush буфера span'ов.
// Пропагатор ставится всегда: даже без экспорта сервис пробрасывает traceparent дальше по цепочке.
func Init(ctx context.Context, opts Options) (func(context.Context) error, error) {
	gootel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	if opts.Endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}
	exp, err := otlptracegrpc.New(ctx, otlptracegrpc.WithEndpointURL(opts.Endpoint))
	if err != nil {
		return nil, fmt.Errorf("otel: экспортёр OTLP: %w", err)
	}
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		attribute.String("service.name", opts.ServiceName),
		attribute.String("service.version", opts.ServiceVersion),
		attribute.String("deployment.environment", opts.Environment),
	))
	if err != nil {
		return nil, fmt.Errorf("otel: ресурс: %w", err)
	}
	ratio := opts.SampleRatio
	if ratio < 0 || ratio > 1 {
		ratio = 1
	}
	var root sdktrace.Sampler = sdktrace.TraceIDRatioBased(ratio)
	if opts.NoisyRatio > 0 && opts.NoisyRatio < ratio && len(opts.NoisyRoutes) > 0 {
		root = routeSampler{def: root, noisy: sdktrace.TraceIDRatioBased(opts.NoisyRatio), prefixes: opts.NoisyRoutes}
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(root)),
		// под штормом span'ов много: очередь побольше, при переполнении span'ы отбрасываются, а не блокируют запрос
		sdktrace.WithBatcher(exp, sdktrace.WithMaxQueueSize(16384), sdktrace.WithMaxExportBatchSize(1024), sdktrace.WithBatchTimeout(2*time.Second)),
	)
	gootel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

// routeSampler — head-sampling по имени корневого span'а: шумные маршруты с меньшей долей.
// Решение принимает корень, дочерние span'ы и другие сервисы наследуют его (ParentBased).
type routeSampler struct {
	def, noisy sdktrace.Sampler
	prefixes   []string
}

func (s routeSampler) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	for _, pre := range s.prefixes {
		if strings.HasPrefix(p.Name, pre) {
			return s.noisy.ShouldSample(p)
		}
	}
	return s.def.ShouldSample(p)
}

func (s routeSampler) Description() string { return "routeSampler" }

// Start открывает внутренний span name (например, "booking.PayOrder") дочерним к span'у из ctx.
func Start(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return gootel.Tracer(instrumentation).Start(ctx, name, trace.WithAttributes(attrs...))
}

// StartClient — span исходящего вызова внешней системы (SpanKind=client).
func StartClient(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return gootel.Tracer(instrumentation).Start(ctx, name, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attrs...))
}

// End закрывает span; ошибка из *errp записывается в span и выставляет статус Error.
// Использование: defer otel.End(span, &err) при именованном возврате err.
func End(span trace.Span, errp *error) {
	if errp != nil && *errp != nil {
		RecordError(span, *errp)
	}
	span.End()
}

// RecordError отмечает ошибку на span'е (context.Canceled клиента — тоже ошибка: запрос не выполнен).
func RecordError(span trace.Span, err error) {
	if err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// Event добавляет событие на текущий span из ctx (например, «outbox: событие записано»).
func Event(ctx context.Context, name string, attrs ...attribute.KeyValue) {
	trace.SpanFromContext(ctx).AddEvent(name, trace.WithAttributes(attrs...))
}

// SetAttributes добавляет атрибуты текущему span'у из ctx.
func SetAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	trace.SpanFromContext(ctx).SetAttributes(attrs...)
}

// SetUser — атрибут enduser.id на текущем span'е (только user_id, без PII: NFR-7).
func SetUser(ctx context.Context, userID string) {
	trace.SpanFromContext(ctx).SetAttributes(attribute.String("enduser.id", userID))
}

// String, Int, Int64, Bool — атрибуты span'а без импорта OpenTelemetry в модулях.
func String(k, v string) attribute.KeyValue  { return attribute.String(k, v) }
func Int(k string, v int) attribute.KeyValue { return attribute.Int(k, v) }
func Int64(k string, v int64) attribute.KeyValue {
	return attribute.Int64(k, v)
}
func Bool(k string, v bool) attribute.KeyValue { return attribute.Bool(k, v) }

// TraceID — trace_id текущего span'а или пустая строка.
func TraceID(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.HasTraceID() {
		return ""
	}
	return sc.TraceID().String()
}

// Traceparent — контекст ctx в формате W3C traceparent ("00-<trace>-<span>-01") или пустая строка.
// Нужен, когда контекст едет не по HTTP: строка outbox → заголовок Kafka (занятие 4, ветка cdc).
func Traceparent(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	gootel.GetTextMapPropagator().Inject(ctx, carrier)
	return carrier["traceparent"]
}

// Extract восстанавливает удалённый контекст из заголовков (traceparent, baggage) произвольного носителя.
func Extract(ctx context.Context, headers map[string]string) context.Context {
	return gootel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(lowerKeys(headers)))
}

// Inject записывает traceparent/baggage из ctx в headers.
func Inject(ctx context.Context, headers map[string]string) {
	gootel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(headers))
}

func lowerKeys(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[strings.ToLower(k)] = v
	}
	return out
}
