package gateway

import (
	"context"

	"crypto/x509"

	workerpb "github.com/project-ai-services/ai-services/internal/pkg/worker/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type contextKey string

const authenticatedWorkerKey contextKey = "authenticatedWorker"

type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context {
	return w.ctx
}

// authUnaryInterceptor bypasses mTLS for Register (which uses token auth) and
// enforces it for all other unary RPCs.
func (g *Gateway) authUnaryInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	if info.FullMethod == workerpb.WorkerGateway_Register_FullMethodName {
		return handler(ctx, req)
	}
	if _, err := enforceClientCert(ctx); err != nil {
		return nil, err
	}

	return handler(ctx, req)
}

// authStreamInterceptor enforces mTLS for all streaming RPCs (CommandStream) and
// injects the authenticated worker identity from the verified client certificate.
func (g *Gateway) authStreamInterceptor(srv interface{}, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	leaf, err := enforceClientCert(ss.Context())
	if err != nil {
		return err
	}

	workerName := leaf.Subject.CommonName
	ctx := context.WithValue(ss.Context(), authenticatedWorkerKey, workerName)

	return handler(srv, &wrappedStream{ServerStream: ss, ctx: ctx})
}

// enforceClientCert returns Unauthenticated if the request does not carry a
// verified CA-signed client certificate and returns the verified leaf certificate.
func enforceClientCert(ctx context.Context) (*x509.Certificate, error) {
	p, ok := peer.FromContext(ctx)
	if !ok || p.AuthInfo == nil {
		return nil, status.Error(codes.Unauthenticated, "no peer identity found")
	}
	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.VerifiedChains) == 0 || len(tlsInfo.State.VerifiedChains[0]) == 0 {
		return nil, status.Error(codes.Unauthenticated, "mTLS client certificate required")
	}

	return tlsInfo.State.VerifiedChains[0][0], nil
}
