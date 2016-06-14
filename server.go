package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"

	"golang.org/x/net/context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/codegangsta/negroni"
	kitot "github.com/go-kit/kit/tracing/opentracing"
	"github.com/rodrwan/traces/grpcd"
	"github.com/rodrwan/traces/ptypes"

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
)

func main() {
	collector := appdash.NewRemoteCollector("localhost:7701")
	tracer := appdashtracer.NewTracer(collector)
	opentracing.InitGlobalTracer(tracer)

	s := &Server{
		Tracer: tracer,
		Port:   9000,
		Addr:   "localhost",
		Col:    collector,
	}
	// Run grpc Server
	go func() {
		s.grpcServer()
	}()

	// Setup our router (for information, see the gorilla/mux docs):
	router := mux.NewRouter()
	router.HandleFunc("/", s.Home)

	// Setup Negroni for our app (for information, see the negroni docs):
	n := negroni.Classic()
	n.UseHandler(router)
	n.Run(":8699")
}

// Home ...
func (s *Server) Home(w http.ResponseWriter, r *http.Request) {
	span := opentracing.StartSpan("WebService" + r.URL.Path)
	defer span.Finish()

	fmt.Printf("\n\n%+v\n\n\n", r.Header)

	span.SetTag("Request.Host", r.Host)
	span.SetTag("Request.Address", r.RemoteAddr)
	addHeaderTags(span, r.Header)

	span.SetBaggageItem("User", "rodrwan")

	carrier := opentracing.HTTPHeaderTextMapCarrier(r.Header)
	span.Tracer().Inject(
		span,
		opentracing.TextMap,
		carrier,
	)
	s.Span = span
	resp, err := s.grpcClient()
	if err != nil {
		log.Printf("Show method has failed: %+v", err)
	}

	if resp == nil {
		span.SetTag("Response.Status", http.StatusBadRequest)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	span.SetTag("Response.Status", http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(resp.Msg))
}

const headerTagPrefix = "Request.Header."

// addHeaderTags adds header key:value pairs to a span as a tag with the prefix
// "Request.Header.*"
func addHeaderTags(span opentracing.Span, h http.Header) {
	for k, v := range h {
		span.SetTag(headerTagPrefix+k, strings.Join(v, ", "))
	}
}

// Server ...
type Server struct {
	Tracer opentracing.Tracer
	Port   int
	Addr   string
	Col    appdash.Collector
	Span   opentracing.Span
}

func (s *Server) grpcServer() {
	grpcAddr := fmt.Sprintf(":%d", s.Port)
	fmt.Println("GRPC Server listen on: ", grpcAddr)
	lis, err := net.Listen("tcp", grpcAddr)

	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	testServer, err := grpcd.NewTestServer(s.Tracer)
	if err != nil {
		log.Fatalf("failed to connect db: %v", err)
	}

	grpcServer := grpc.NewServer()
	ptypes.RegisterTestServer(grpcServer, testServer)

	log.Fatalln(grpcServer.Serve(lis))
}

// GRPC Client
func (s *Server) grpcClient() (*ptypes.TestReply, error) {
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

	var toGRPCFunc kitrpc.RequestFunc = kitot.ToGRPCRequest(s.Tracer, nil)
	ctx = metadata.NewContext(ctx, md)
	ctx = opentracing.ContextWithSpan(ctx, s.Span)
	ctx = toGRPCFunc(ctx, &md)
	// There's nothing we can do with an error here.

	if err != nil {
		s.Span.LogEvent(err.Error())
		return nil, errors.New("Error while create tracer")
	}

	return t.Show(ctx, &ptypes.TestRequest{
		User: "Test",
	})
}
