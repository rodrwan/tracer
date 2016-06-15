package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"golang.org/x/net/context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"

	"github.com/codegangsta/negroni"
	"github.com/rodrwan/traces/grpcd"
	"github.com/rodrwan/traces/ptypes"

	gcontext "github.com/gorilla/context"
	"github.com/gorilla/mux"
	"sourcegraph.com/sourcegraph/appdash"
	"sourcegraph.com/sourcegraph/appdash/httptrace"
)

// HeaderSpanID ....
const (
	HeaderSpanID       = "Span-ID"
	HeaderParentSpanID = "Parent-Span-ID"
	CtxSpanID          = 0
)

var recorder *appdash.Recorder

func init() { appdash.RegisterEvent(GRPCEvent{}) }

func main() {
	collector := appdash.NewRemoteCollector("localhost:7701")

	s := &Server{
		Port: 9000,
		Addr: "localhost",
		Col:  collector,
	}
	// Run grpc Server
	go func() {
		s.grpcServer()
	}()

	tracemw := httptrace.Middleware(collector, &httptrace.MiddlewareConfig{
		RouteName: func(r *http.Request) string { return r.URL.Path },
		SetContextSpan: func(r *http.Request, spanID appdash.SpanID) {
			gcontext.Set(r, CtxSpanID, spanID)
		},
	})

	// Setup our router (for information, see the gorilla/mux docs):
	router := mux.NewRouter()
	router.HandleFunc("/", s.Home(collector))

	// Setup Negroni for our app (for information, see the negroni docs):
	n := negroni.Classic()
	n.Use(negroni.HandlerFunc(tracemw))
	n.UseHandler(router)
	n.Run(":8699")
}

// Home ...
func (s *Server) Home(c appdash.Collector) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		span := gcontext.Get(r, CtxSpanID).(appdash.SpanID)

		recorder = appdash.NewRecorder(span, s.Col)
		resp, err := s.grpcClient()

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

// Server ...
type Server struct {
	Port int
	Addr string
	Col  appdash.Collector
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

	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(
			tracerServerInterceptor(s.Col),
		),
	)
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
	SpanID := recorder.SpanID
	// Set SpanID to meta
	md := metadata.Pairs("spanid", SpanID.String())
	ctx = metadata.NewContext(ctx, md)
	return t.Show(ctx, &ptypes.TestRequest{
		User: "Test",
	})
}

func tracerServerInterceptor(c appdash.Collector) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {

		fmt.Println("[GRPC] Started", info.FullMethod)
		// md, ok := metadata.FromContext(ctx)
		// if !ok {
		// 	panic("Bad metadata")
		// }
		// spanid := md["spanid"][0]
		// span, err := appdash.ParseSpanID(spanid)
		// if err != nil {
		// 	panic(err)
		// }

		s := NewServerEvent(info)
		s.ServerRecv = time.Now()

		start := time.Now()
		// GRPC Response
		resp, err := handler(ctx, req)

		if err != nil {
			s.StatusCode = codes.Internal
		}
		since := time.Since(start)
		s.StatusCode = codes.OK
		s.ServerSend = time.Now()

		fmt.Println(recorder.IsRoot())
		child := recorder.Child()
		fmt.Println(child.IsRoot())
		child.Name("GRPC " + info.FullMethod)
		child.Event(s)
		child.Finish()
		recorder.Finish()
		fmt.Println("[GRPC] Completed in", since)
		return resp, err
	}
}

// NewServerEvent ...
func NewServerEvent(info *grpc.UnaryServerInfo) *GRPCEvent {
	return &GRPCEvent{
		Method: info.FullMethod,
	}
}

// GRPCEvent ...
type GRPCEvent struct {
	Method     string     `trace:"GRPC.Method"`
	StatusCode codes.Code `trace:"GRPC.StatusCode"`
	ServerRecv time.Time  `trace:"GRPC.Recv"`
	ServerSend time.Time  `trace:"GRPC.Send"`
}

// Schema returns the constant "GRPCServer".
func (GRPCEvent) Schema() string { return "GRPCServer" }

// Important implements the appdash ImportantEvent.
func (GRPCEvent) Important() []string {
	return []string{"GRPC.StatusCode"}
}

// Start implements the appdash TimespanEvent interface.
func (e GRPCEvent) Start() time.Time { return e.ServerRecv }

// End implements the appdash TimespanEvent interface.
func (e GRPCEvent) End() time.Time { return e.ServerSend }
