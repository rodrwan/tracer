package grpcd

import (
	"errors"
	"time"

	"github.com/rodrwan/traces/ptypes"
	"golang.org/x/net/context"
)

// ErrBadMetadata ...
// ErrWrongSpan ...
const (
	ErrBadMetadata = "Bad metadata format"
	ErrWrongSpan   = "Cannot create span"
	ErrBadUserID   = "Wrong user ID"
)

// TestServer implements ptypes.CallbackServiceServer interface
type TestServer struct {
	Msg string
}

// NewTestServer constructs a new CallbackServer
func NewTestServer() (*TestServer, error) {
	return &TestServer{
		Msg: "Init message",
	}, nil
}

// Show returns endpoint for a given user_id and task_id.
func (s *TestServer) Show(ctx context.Context, req *ptypes.TestRequest,
) (*ptypes.TestReply, error) {
	userID := req.User

	if userID == "" {
		return nil, errors.New(ErrBadUserID)
	}

	// Fake process
	time.Sleep(200 * time.Millisecond)

	return &ptypes.TestReply{
		Msg: "Hello " + userID,
	}, nil
}
