package interceptors

import (
	"context"

	"google.golang.org/grpc"
)

type GrpcInterceptor interface {
	Unary(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error)
	Streaming(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error
}

// Compile Time Checks
var (
	_ GrpcInterceptor = (*RequestIDGrpcInterceptor)(nil)
)
