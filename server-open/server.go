package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"

	"github.com/codegangsta/negroni"
	"github.com/rodrwan/traces/grpcd"
	"github.com/rodrwan/traces/ptypes"

	kitot "github.com/go-kit/kit/tracing/opentracing"
	kitrpc "github.com/go-kit/kit/transport/grpc"
	"github.com/gorilla/mux"
	opentracing "github.com/opentracing/opentracing-go"
	"sourcegraph.com/sourcegraph/appdash"
	appdashtracer "sourcegraph.com/sourcegraph/appdash/opentracing"
)

// HeaderSpanID ....
const (
	HeaderSpanID       = "Span-ID"
	HeaderParentSpanID = "Parent-Span-ID"
	CtxSpanID          = 0
)

func main() {
	collector := appdash.NewRemoteCollector("localhost:7701")
	tracer := appdashtracer.NewTracer(collector)
	opentracing.InitGlobalTracer(tracer)

	s := &Server{
		Port: 9000,
		Addr: "localhost",
	}
	// Run grpc Server
	go func() {
		s.grpcServer()
	}()

	router := mux.NewRouter()
	router.HandleFunc("/", s.Home(collector))

	n := negroni.Classic()
	n.UseHandler(router)
	n.Run(":8699")
}

// Server ...
type Server struct {
	Port int
	Addr string
}

// Home ...
func (s *Server) Home(c appdash.Collector) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		span := opentracing.StartSpan("HTTP " + r.URL.Path)
		defer span.Finish()

		span.SetTag("Request.Host", r.Host)
		span.SetTag("Request.Address", r.RemoteAddr)
		addHeaderTags(span, r.Header)
		span.SetBaggageItem("User", "new_user")

		resp, err := s.grpcClient(span)

		if err != nil {
			log.Printf("Show method has failed: %+v", err)
		}

		if resp == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(resp.Msg))
	}
}

func (s *Server) grpcServer() {
	grpcAddr := fmt.Sprintf(":%d", s.Port)
	fmt.Println("GRPC Server listen on: ", grpcAddr)
	lis, err := net.Listen("tcp", grpcAddr)

	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	testServer, err := grpcd.NewTestServer()
	if err != nil {
		log.Fatalf("failed to connect db: %v", err)
	}

	tracer := opentracing.GlobalTracer()
	// add a middleware to track traces.
	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(
			tracerServerInterceptor(tracer),
		),
	)
	ptypes.RegisterTestServer(grpcServer, testServer)

	log.Fatalln(grpcServer.Serve(lis))
}

// GRPC Client
func (s *Server) grpcClient(span opentracing.Span) (*ptypes.TestReply, error) {
	url := fmt.Sprintf("%s:%d", s.Addr, s.Port)
	conn, err := grpc.Dial(url, grpc.WithInsecure())

	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	t := ptypes.NewTestClient(conn)
	ctx := context.Background()
	md := metadata.New(map[string]string{
		"User":       "test",
		"request_id": "1234asdf",
	})

	ctx = CreateContextWithSpan(ctx, md, span)
	return t.Show(ctx, &ptypes.TestRequest{
		User: "Test",
	})
}

// CreateContextWithSpan ...
func CreateContextWithSpan(ctx context.Context, md metadata.MD, span opentracing.Span) context.Context {
	var toGRPCFunc kitrpc.RequestFunc

	tracer := opentracing.GlobalTracer()
	toGRPCFunc = kitot.ToGRPCRequest(tracer, nil)
	ctx = metadata.NewContext(ctx, md)
	ctx = opentracing.ContextWithSpan(ctx, span)

	return toGRPCFunc(ctx, &md)
}

// ErrBadMetadata ...
// ErrWrongSpan ...
const (
	ErrBadMetadata = "Bad metadata format"
	ErrWrongSpan   = "Cannot create span"
	ErrBadUserID   = "Wrong user ID"
)

func tracerServerInterceptor(tracer opentracing.Tracer) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		var fromGRPCFunc kitrpc.RequestFunc
		fmt.Println("[GRPC] Started", info.FullMethod)
		start := time.Now()
		md, ok := metadata.FromContext(ctx)
		if !ok {
			return nil, errors.New(ErrBadMetadata)
		}

		fromGRPCFunc = kitot.FromGRPCRequest(
			tracer,
			("GRPC " + info.FullMethod),
			nil,
		)

		joinCtx := fromGRPCFunc(ctx, &md)
		span := opentracing.SpanFromContext(joinCtx)
		defer span.Finish()

		// GRPC Call
		resp, err := handler(ctx, req)
		if err != nil {
			span.SetTag("GRPC.Response.StatusCode", codes.Internal)
		}
		span.SetTag("GRPC.Response.StatusCode", codes.OK)
		since := time.Since(start)
		fmt.Println("[GRPC] Completed in", since)
		return resp, err
	}
}

const headerTagPrefix = "Request.Header."

// addHeaderTags adds header key:value pairs to a span as a tag with the prefix
// "Request.Header.*"
func addHeaderTags(span opentracing.Span, h http.Header) {
	for k, v := range h {
		span.SetTag(headerTagPrefix+k, strings.Join(v, ", "))
	}
}
