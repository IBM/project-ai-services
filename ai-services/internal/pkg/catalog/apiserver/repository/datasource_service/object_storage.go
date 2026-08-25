package datasourceservice

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
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
	s3DefaultRegion   = "us-east-1"
)

// regionFromEndpointRe extracts the SigV4 region segment from AWS S3 and IBM COS endpoint URLs.
// It matches the same two hostname patterns as the digitize Python service (_REGION_FROM_URL_RE).
//
//	s3.<region>.amazonaws.com                              (AWS S3)
//	s3.<region>.cloud-object-storage.appdomain.cloud      (IBM COS)
var regionFromEndpointRe = regexp.MustCompile(
	`(?i)s3[.\-](?P<region>[a-z0-9\-]+)\.(?:amazonaws\.com|cloud-object-storage\.appdomain\.cloud)`,
)

// cosCrossRegionAliases maps IBM COS cross-region endpoint aliases to the canonical SigV4
// region that IBM COS accepts for signing. These three aliases are fixed IBM infrastructure
// geography mappings (documented by IBM COS) — identical to the Python digitize service.
//
//	us → us-south  (Dallas  — primary US cross-region PoP)
//	eu → eu-de     (Frankfurt — primary EU cross-region PoP)
//	ap → jp-tok    (Tokyo   — primary AP cross-region PoP)
var cosCrossRegionAliases = map[string]string{
	"us": "us-south",
	"eu": "eu-de",
	"ap": "jp-tok",
}

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

// regionFromEndpoint extracts the SigV4 region from an S3-compatible endpoint URL using
// the same regex-based approach as the digitize Python service (_REGION_FROM_URL_RE in config.py).
//
// It matches AWS S3 and IBM COS hostname patterns and resolves IBM COS cross-region aliases
// to their canonical SigV4 region values. Falls back to s3DefaultRegion ("us-east-1") for
// any URL that does not match a known pattern (e.g. MinIO, custom S3-compatible stores) —
// this is the AWS SDK default and is correct for SigV4 signing against most S3-compatible stores.
func regionFromEndpoint(endpointURL string) string {
	match := regionFromEndpointRe.FindStringSubmatch(endpointURL)
	if match == nil {
		return s3DefaultRegion
	}

	region := strings.ToLower(match[regionFromEndpointRe.SubexpIndex("region")])
	if canonical, ok := cosCrossRegionAliases[region]; ok {
		return canonical
	}

	return region
}

// Made with Bob
