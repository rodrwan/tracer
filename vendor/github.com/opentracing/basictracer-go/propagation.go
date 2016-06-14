package basictracer

import (
	"time"

	opentracing "github.com/opentracing/opentracing-go"
)

type accessorPropagator struct {
	tracer *tracerImpl
}

// DelegatingCarrier is a flexible carrier interface which can be implemented
// by types which have a means of storing the trace metadata and already know
// how to serialize themselves (for example, protocol buffers).
type DelegatingCarrier interface {
	SetState(traceID, spanID uint64, sampled bool)
	State() (traceID, spanID uint64, sampled bool)
	SetBaggageItem(key, value string)
	GetBaggage(func(key, value string))
}

func (p *accessorPropagator) Inject(
	sp opentracing.Span,
	carrier interface{},
) error {
	ac, ok := carrier.(DelegatingCarrier)
	if !ok || ac == nil {
		return opentracing.ErrInvalidCarrier
	}
	si, ok := sp.(*spanImpl)
	if !ok {
		return opentracing.ErrInvalidSpan
	}
	meta := si.raw.Context
	ac.SetState(meta.TraceID, meta.SpanID, meta.Sampled)
	for k, v := range si.raw.Baggage {
		ac.SetBaggageItem(k, v)
	}
	return nil
}

func (p *accessorPropagator) Join(
	operationName string,
	carrier interface{},
) (opentracing.Span, error) {
	ac, ok := carrier.(DelegatingCarrier)
	if !ok || ac == nil {
		return nil, opentracing.ErrInvalidCarrier
	}

	sp := p.tracer.getSpan()
	ac.GetBaggage(func(k, v string) {
		if sp.raw.Baggage == nil {
			sp.raw.Baggage = map[string]string{}
		}
		sp.raw.Baggage[k] = v
	})

	traceID, parentSpanID, sampled := ac.State()
	sp.raw.Context = Context{
		TraceID:      traceID,
		SpanID:       randomID(),
		ParentSpanID: parentSpanID,
		Sampled:      sampled,
	}

	return p.tracer.startSpanInternal(
		sp,
		operationName,
		time.Now(),
		nil,
	), nil
}
