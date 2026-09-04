package gateway

import (
	"context"

	workerpb "github.com/project-ai-services/ai-services/internal/pkg/worker/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// authUnaryInterceptor bypasses mTLS for Register (which uses token auth) and
// enforces it for all other unary RPCs.
func (g *Gateway) authUnaryInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	if info.FullMethod == workerpb.WorkerGateway_Register_FullMethodName {
		return handler(ctx, req)
	}
	if err := enforceClientCert(ctx); err != nil {
		return nil, err
	}

	return handler(ctx, req)
}

// authStreamInterceptor enforces mTLS for all streaming RPCs (CommandStream).
func (g *Gateway) authStreamInterceptor(srv interface{}, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	if err := enforceClientCert(ss.Context()); err != nil {
		return err
	}

	return handler(srv, ss)
}

// enforceClientCert returns Unauthenticated if the request does not carry a
// verified CA-signed client certificate.
func enforceClientCert(ctx context.Context) error {
	p, ok := peer.FromContext(ctx)
	if !ok || p.AuthInfo == nil {
		return status.Error(codes.Unauthenticated, "no peer identity found")
	}
	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.VerifiedChains) == 0 || len(tlsInfo.State.VerifiedChains[0]) == 0 {
		return status.Error(codes.Unauthenticated, "mTLS client certificate required")
	}

	return nil
}
