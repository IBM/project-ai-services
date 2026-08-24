package datasourceservice

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

const (
	awsEndpointSuffix = "amazonaws.com"
	s3ConnectTimeout  = 10 * time.Second
)

// authErrorCodes are S3/STS codes that indicate invalid credentials.
var authErrorCodes = map[string]bool{
	"InvalidAccessKeyId":    true,
	"SignatureDoesNotMatch": true,
	"InvalidClientTokenId":  true,
	"AuthFailure":           true,
}

// accessErrorCodes are S3 codes that indicate valid credentials but insufficient
// permissions or a missing/inaccessible bucket.
var accessErrorCodes = map[string]bool{
	"AccessDenied":      true,
	"NoSuchBucket":      true,
	"AllAccessDisabled": true,
}

// objectStorageTester implements ConnectionTester for S3-compatible object storage.
// It handles both AWS S3 and IBM COS (and any other S3-compatible endpoint).
//
// AWS S3:  the SDK resolves the regional endpoint from cfg.Region automatically
//
//	using virtual-hosted-style addressing; no BaseEndpoint is set.
//
// IBM COS / S3-compatible: BaseEndpoint is set to the supplied URL and path-style
//
//	addressing is enabled, which is required by most non-AWS stores.
type objectStorageTester struct{}

// NewObjectStorageTester returns a ConnectionTester for S3-compatible object storage providers.
func NewObjectStorageTester() ConnectionTester {
	return &objectStorageTester{}
}

// TestConnection runs three sequential checks against an S3-compatible endpoint using a
// single ListObjectsV2(MaxKeys=0) call — a zero-cost probe that transfers no object data.
// The SDK response (or error) is classified into network / auth / access failure categories.
func (t *objectStorageTester) TestConnection(ctx context.Context, params map[string]any) error {
	endpointURL, _ := params["endpoint_url"].(string)
	bucket, _ := params["bucket_name"].(string)
	accessKeyID, _ := params["access_key_id"].(string)
	secretKey, _ := params["secret_access_key"].(string)
	prefix, _ := params["prefix"].(string)

	cfg := aws.Config{
		Region:      regionFromEndpoint(endpointURL),
		Credentials: credentials.NewStaticCredentialsProvider(accessKeyID, secretKey, ""),
		HTTPClient:  awshttp.NewBuildableClient().WithTimeout(s3ConnectTimeout),
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if strings.Contains(endpointURL, awsEndpointSuffix) {
			// AWS S3: SDK resolves the regional endpoint from cfg.Region automatically;
			// virtual-hosted-style addressing is used by default — no BaseEndpoint needed.
			return
		}
		// IBM COS, MinIO, and other S3-compatible stores: supply the custom
		// endpoint and enable path-style addressing.
		o.BaseEndpoint = aws.String(endpointURL)
		o.UsePathStyle = true
	})

	callCtx, cancel := context.WithTimeout(ctx, s3ConnectTimeout)
	defer cancel()

	maxKeys := int32(0)
	input := &s3.ListObjectsV2Input{
		Bucket:  aws.String(bucket),
		MaxKeys: &maxKeys,
	}
	if prefix != "" {
		input.Prefix = aws.String(prefix)
	}

	_, err := client.ListObjectsV2(callCtx, input)
	if err == nil {
		return nil
	}

	return classifyS3Error(err, endpointURL, bucket)
}

// classifyS3Error maps an AWS SDK error to a *ConnectionCheckError for the appropriate phase.
func classifyS3Error(err error, endpointURL, bucket string) error {
	// Network error: DNS failure, TCP refused, or context deadline from an unreachable host.
	var netErr *net.OpError
	if errors.As(err, &netErr) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) {
		return &ConnectionCheckError{
			CheckType: ConnectionCheckNetwork,
			Message:   fmt.Sprintf("cannot reach S3 endpoint %q: %v", endpointURL, err),
		}
	}

	// Network passed — a response was received; inspect the API error code.
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code := apiErr.ErrorCode()
		if authErrorCodes[code] {
			return &ConnectionCheckError{
				CheckType: ConnectionCheckAuth,
				Message:   fmt.Sprintf("credential rejected (code: %s) — check access_key_id / secret_access_key", code),
			}
		}
		if accessErrorCodes[code] {
			return &ConnectionCheckError{
				CheckType: ConnectionCheckAccess,
				Message:   fmt.Sprintf("bucket %q not accessible (code: %s)", bucket, code),
			}
		}
		// Unknown API error — surface code for diagnostics.
		return &ConnectionCheckError{
			CheckType: ConnectionCheckAuth,
			Message:   fmt.Sprintf("unexpected S3 error (code: %s): %v", code, err),
		}
	}

	// Non-API error with a network-level response (e.g. TLS failure).
	return &ConnectionCheckError{
		CheckType: ConnectionCheckNetwork,
		Message:   fmt.Sprintf("S3 connectivity error for %q: %v", endpointURL, err),
	}
}

// regionFromEndpoint extracts the SigV4 region from the endpoint URL.
// Falls back to "us-east-1" when the pattern is not recognised.
//
// Supported patterns:
//
//	s3.<region>.amazonaws.com                              (AWS S3)
//	s3.<region>.cloud-object-storage.appdomain.cloud      (IBM COS direct-region)
//	s3.us.cloud-object-storage.appdomain.cloud            (IBM COS cross-region alias → us-south)
func regionFromEndpoint(endpointURL string) string {
	cosAliases := map[string]string{"us": "us-south", "eu": "eu-de", "ap": "jp-tok"}
	u := strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(endpointURL, "https://"), "http://"))
	parts := strings.Split(u, ".")
	// ["s3", "<region>", "amazonaws", "com"]  or
	// ["s3", "<region>", "cloud-object-storage", "appdomain", "cloud"]
	if len(parts) >= 3 && parts[0] == "s3" {
		region := parts[1]
		if canonical, ok := cosAliases[region]; ok {
			return canonical
		}

		return region
	}

	return "us-east-1"
}

// Made with Bob
