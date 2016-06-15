package main

import (
	"fmt"
	"log"
	"net"
	"net/http"

	"golang.org/x/net/context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/Finciero/grpctracer"
	"github.com/codegangsta/negroni"
	"github.com/rodrwan/traces/grpcd"
	"github.com/rodrwan/traces/ptypes"

	"github.com/gorilla/mux"
	opentracing "github.com/opentracing/opentracing-go"
	"sourcegraph.com/sourcegraph/appdash"
	appdashtracer "sourcegraph.com/sourcegraph/appdash/opentracing"
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
		requestID := "new_request_id"
		spanName := "HTTP " + r.URL.Path
		// start a root span for HTTP call. This span is passed by value
		// to GRPC client.
		span := opentracing.StartSpan(spanName)
		span.SetTag("Request.Host", r.Host)
		span.SetTag("Request.Address", r.RemoteAddr)
		grpctracer.AddHeaderTags(span, r.Header)
		// Set baggage to share between spans.
		span.SetBaggageItem("RequestID", requestID)
		defer span.Finish()

		newClient := &GRPCClient{
			URL: fmt.Sprintf("%s:%d", s.Addr, s.Port),
		}
		newClient.Connect()

		fmt.Println(newClient.Hi(span))

		resp, err := newClient.Show(span)

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
			grpctracer.TracerServerInterceptor(tracer),
		),
	)
	ptypes.RegisterTestServer(grpcServer, testServer)

	log.Fatalln(grpcServer.Serve(lis))
}

// GRPCClient ...
type GRPCClient struct {
	URL    string
	client ptypes.TestClient
}

// Connect ...
func (c *GRPCClient) Connect() error {
	conn, err := grpc.Dial(c.URL, grpc.WithInsecure())

	if err != nil {
		log.Fatalf("failed to listen: %v", err)
		return err
	}
	c.client = ptypes.NewTestClient(conn)
	return nil
}

// Show ...
func (c *GRPCClient) Show(span opentracing.Span) (*ptypes.TestReply, error) {
	ctx := context.Background()
	md := metadata.New(map[string]string{
		"User":       "test",
		"request_id": "1234asdf",
	})

	// Child span example:
	// Here we start a new child span based on parant `span` also
	// we inherited `RequestID` to keep the trace consistent.
	newSpan := opentracing.StartChildSpan(span, "Call Inside Show")
	baggage := span.BaggageItem("RequestID")
	newSpan.SetBaggageItem("RequestID", baggage)
	c.Hi(newSpan)
	newSpan.Finish()

	// Here we keep using `parent span` cause this is not a child process
	// of show method. And join span to our context.
	ctx = grpctracer.CreateContextWithSpan(ctx, md, span)
	return c.client.Show(ctx, &ptypes.TestRequest{
		User: "Test",
	})
}

// Hi ...
func (c *GRPCClient) Hi(span opentracing.Span) (*ptypes.TestReply, error) {
	ctx := context.Background()
	md := metadata.New(map[string]string{
		"User":       "test",
		"request_id": "1234asdf",
	})

	// Now we glue context with and span, this helps to our intercetor to
	// create a new span record.
	ctx = grpctracer.CreateContextWithSpan(ctx, md, span)

	return c.client.Hi(ctx, &ptypes.TestRequest{
		User: "Test",
	})
}
