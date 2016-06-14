package grpcd

import (
	"errors"
	"fmt"
	"time"

	kitot "github.com/go-kit/kit/tracing/opentracing"
	"github.com/go-kit/kit/transport/grpc"
	opentracing "github.com/opentracing/opentracing-go"
	"github.com/rodrwan/traces/ptypes"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
)

// ErrBadMetadata ...
// ErrWrongSpan ...
const (
	ErrBadMetadata = "Bad metadata format"
	ErrWrongSpan   = "Cannot create span"
)

// TestServer implements ptypes.CallbackServiceServer interface
type TestServer struct {
	Msg    string
	Tracer opentracing.Tracer
}

// NewTestServer constructs a new CallbackServer
func NewTestServer(tracer opentracing.Tracer) (*TestServer, error) {
	return &TestServer{
		Msg:    "Init message",
		Tracer: tracer,
	}, nil
}

// Show returns endpoint for a given user_id and task_id.
func (s *TestServer) Show(ctx context.Context, req *ptypes.TestRequest,
) (*ptypes.TestReply, error) {
	md, ok := metadata.FromContext(ctx)
	if !ok {
		return nil, errors.New(ErrBadMetadata)
	}

	var fromGRPCFunc grpc.RequestFunc = kitot.FromGRPCRequest(
		s.Tracer,
		"Tracer /show",
		nil,
	)
	fmt.Printf("%+v\n\n\n", ctx)

	joinCtx := fromGRPCFunc(ctx, &md)
	span := opentracing.SpanFromContext(joinCtx)
	defer span.Finish()

	span.SetTag("GRPC.Method", "Show")

	userID := req.User

	if userID == "" {
		span.SetTag("GRPC.Codes", codes.InvalidArgument)
		span.LogEvent("UserID cannot be nil")
	}

	// Fake process
	time.Sleep(200 * time.Millisecond)

	span.SetTag("GRPC.Codes", codes.OK)
	return &ptypes.TestReply{
		Msg: "Hello " + userID,
	}, nil
}
