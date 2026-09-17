package logging

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var (
	errRequestIDNotSet     = fmt.Errorf("request id '%v' was not set", HeaderRequestID)
	errUUIDNotValid        = fmt.Errorf("request id '%v' uuid was not valid", HeaderRequestID)
	errMultipleRequestIDs  = fmt.Errorf("request id '%v' was provided multiple times", HeaderRequestID)
	errFailedSetHeader     = fmt.Errorf("failed to set header: %v", HeaderRequestID)
	errCouldNotGetMetadata = errors.New("could not get metadata")
)

func RequestIDUnaryInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Internal, errCouldNotGetMetadata.Error())
	}

	requestID, err := extractRequestID(md)
	if err != nil {
		if errors.Is(err, errRequestIDNotSet) {
			requestID = uuid.New()
		} else if errors.Is(err, errUUIDNotValid) || errors.Is(err, errMultipleRequestIDs) {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		} else {
			return nil, status.Error(codes.Internal, "failed to resolve request id")
		}
	}

	ctx = WithRequestID(ctx, requestID.String())
	responseMetadata := metadata.Pairs(HeaderRequestID, requestID.String())
	if err = grpc.SetHeader(ctx, responseMetadata); err != nil {
		return nil, status.Error(codes.Internal, errFailedSetHeader.Error())
	}

	return handler(ctx, req)
}

func RequestIDStreamingInterceptor(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctx := ss.Context()
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.Internal, errCouldNotGetMetadata.Error())
	}

	requestID, err := extractRequestID(md)
	if err != nil {
		if errors.Is(err, errRequestIDNotSet) {
			requestID = uuid.New()
		} else if errors.Is(err, errUUIDNotValid) || errors.Is(err, errMultipleRequestIDs) {
			return status.Error(codes.InvalidArgument, err.Error())
		} else {
			return status.Error(codes.Internal, "failed to resolve request id")
		}
	}

	ctx = WithRequestID(ctx, requestID.String())
	responseMetadata := metadata.Pairs(HeaderRequestID, requestID.String())
	if err = grpc.SetHeader(ctx, responseMetadata); err != nil {
		return status.Error(codes.Internal, errFailedSetHeader.Error())
	}

	return handler(srv, &contextServerStream{ServerStream: ss, ctx: ctx})
}

type contextServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *contextServerStream) Context() context.Context {
	return s.ctx
}

func extractRequestID(md metadata.MD) (uuid.UUID, error) {
	requestIds := md.Get(HeaderRequestID)

	if len(requestIds) == 0 {
		return uuid.Nil, errRequestIDNotSet
	}

	if len(requestIds) > 1 {
		return uuid.Nil, errMultipleRequestIDs
	}

	requestId := requestIds[0]

	if parsedRequestId, err := uuid.Parse(requestId); err != nil {
		return uuid.Nil, errUUIDNotValid
	} else {
		return parsedRequestId, nil
	}
}
