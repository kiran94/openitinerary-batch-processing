package interceptors

import (
	"context"

	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"google.golang.org/grpc"
)

type TimeoutGrpcInterceptor struct {
	Config config.Config
}

func (i *TimeoutGrpcInterceptor) Unary(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
	timeout := i.Config.GetGRPCUploadTimeout()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return handler(ctx, req)
}

func (i *TimeoutGrpcInterceptor) Streaming(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctx := ss.Context()

	timeout := i.Config.GetGRPCUploadTimeout()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ss = &deadlineServerStream{ServerStream: ss, ctx: ctx}

	return handler(srv, ss)
}

type deadlineServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (d *deadlineServerStream) Context() context.Context {
	return d.ctx
}

func (d *deadlineServerStream) RecvMsg(m any) error {
	result := make(chan error, 1)

	go func() {
		result <- d.ServerStream.RecvMsg(m)
	}()

	select {
	case err := <-result:
		return err
	case <-d.ctx.Done():
		return d.ctx.Err()
	}
}
