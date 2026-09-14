package s3client

import (
	"net/http"

	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
)

const (
	// s3MaxConnsPerHost bounds concurrent connections per endpoint for the whole
	// process. It equals the idle limit on purpose: a dial that loses the race to
	// a freed idle connection then always fits in the idle pool instead of being
	// closed unused. The SDK default (2048 connections, 10 idle) let bursts of
	// short HEAD requests complete TLS handshakes on connections that never
	// carried a request, and providers such as Mega S4 block clients for that.
	s3MaxConnsPerHost = 16
	// s3MaxIdleConns bounds the idle pool across every S3 endpoint and the
	// external delivery endpoint probed by ObjectAvailable.
	s3MaxIdleConns = 64
)

// sharedHTTPClientValue is the one HTTP client behind every S3 client and the
// delivery probe, so the per-host caps apply to the process rather than to each
// bucket role separately. The SDK's dial, TLS and keep-alive defaults are kept.
var sharedHTTPClientValue = &http.Client{
	Transport: awshttp.NewBuildableClient().WithTransportOptions(func(tr *http.Transport) {
		tr.MaxConnsPerHost = s3MaxConnsPerHost
		tr.MaxIdleConnsPerHost = s3MaxConnsPerHost
		tr.MaxIdleConns = s3MaxIdleConns
	}).GetTransport(),
}

func sharedHTTPClient() *http.Client { return sharedHTTPClientValue }

func sharedTransport() *http.Transport { return sharedHTTPClientValue.Transport.(*http.Transport) }
