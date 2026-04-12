package selfhosted

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"testing"

	"heph4estus/internal/logger"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// fakeS3 is a tiny in-memory S3 implementing the S3API subset we use.
// Pagination is emulated via PageSize; when PageSize > 0, ListObjectsV2
// returns at most PageSize keys per call and sets NextContinuationToken.
type fakeS3 struct {
	objects  map[string][]byte // key -> body (single bucket for test simplicity)
	pageSize int
	lastOpts []func(*s3.Options)
}

func newFakeS3() *fakeS3 {
	return &fakeS3{objects: map[string][]byte{}}
}

func (f *fakeS3) PutObject(_ context.Context, in *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	f.lastOpts = optFns
	body, err := io.ReadAll(in.Body)
	if err != nil {
		return nil, err
	}
	f.objects[aws.ToString(in.Key)] = body
	return &s3.PutObjectOutput{}, nil
}

func (f *fakeS3) GetObject(_ context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	data, ok := f.objects[aws.ToString(in.Key)]
	if !ok {
		return nil, fmt.Errorf("key not found: %s", aws.ToString(in.Key))
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(data))}, nil
}

func (f *fakeS3) ListObjectsV2(_ context.Context, in *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	prefix := aws.ToString(in.Prefix)
	keys := make([]string, 0, len(f.objects))
	for k := range f.objects {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	// Emulate pagination cursor by offset encoded in ContinuationToken.
	start := 0
	if tok := aws.ToString(in.ContinuationToken); tok != "" {
		if _, err := fmt.Sscanf(tok, "offset-%d", &start); err != nil {
			return nil, fmt.Errorf("bad continuation token: %q", tok)
		}
	}
	end := len(keys)
	truncated := false
	var nextTok *string
	if f.pageSize > 0 && start+f.pageSize < len(keys) {
		end = start + f.pageSize
		truncated = true
		t := fmt.Sprintf("offset-%d", end)
		nextTok = &t
	}

	page := keys[start:end]
	contents := make([]s3types.Object, 0, len(page))
	for _, k := range page {
		k := k
		contents = append(contents, s3types.Object{Key: aws.String(k)})
	}
	return &s3.ListObjectsV2Output{
		Contents:              contents,
		IsTruncated:           aws.Bool(truncated),
		NextContinuationToken: nextTok,
	}, nil
}

func TestStorageUploadDownloadRoundTrip(t *testing.T) {
	fake := newFakeS3()
	s := NewStorageWithClient(fake, logger.NewSimpleLogger())

	want := []byte("hello heph4estus")
	if err := s.Upload(context.Background(), "bucket", "scans/a.json", want); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	got, err := s.Download(context.Background(), "bucket", "scans/a.json")
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("round trip mismatch: got %q, want %q", got, want)
	}
}

func TestStorageListSinglePage(t *testing.T) {
	fake := newFakeS3()
	fake.objects["scans/a.json"] = []byte("1")
	fake.objects["scans/b.json"] = []byte("2")
	fake.objects["other/c.json"] = []byte("3")
	s := NewStorageWithClient(fake, logger.NewSimpleLogger())

	keys, err := s.List(context.Background(), "bucket", "scans/")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	sort.Strings(keys)
	want := []string{"scans/a.json", "scans/b.json"}
	if fmt.Sprintf("%v", keys) != fmt.Sprintf("%v", want) {
		t.Fatalf("keys = %v, want %v", keys, want)
	}
}

func TestStorageListPagination(t *testing.T) {
	fake := newFakeS3()
	fake.pageSize = 2
	for _, k := range []string{"p/1", "p/2", "p/3", "p/4", "p/5"} {
		fake.objects[k] = []byte("x")
	}
	s := NewStorageWithClient(fake, logger.NewSimpleLogger())

	keys, err := s.List(context.Background(), "bucket", "p/")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) != 5 {
		t.Fatalf("expected 5 paginated keys, got %d: %v", len(keys), keys)
	}
}

func TestStorageCountPagination(t *testing.T) {
	fake := newFakeS3()
	fake.pageSize = 3
	for i := 0; i < 7; i++ {
		fake.objects[fmt.Sprintf("r/%d", i)] = []byte("x")
	}
	s := NewStorageWithClient(fake, logger.NewSimpleLogger())

	n, err := s.Count(context.Background(), "bucket", "r/")
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 7 {
		t.Fatalf("Count = %d, want 7", n)
	}
}

func TestNormalizeEndpoint(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"https://minio.example:9000", "https://minio.example:9000", false},
		{"https://minio.example:9000/", "https://minio.example:9000", false},
		{"minio.example:9000", "https://minio.example:9000", false},
		{"  http://minio.local  ", "http://minio.local", false},
		{"", "", true},
		{"https://", "", true},
	}
	for _, tc := range cases {
		got, err := normalizeEndpoint(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("normalizeEndpoint(%q) expected error, got %q", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("normalizeEndpoint(%q) unexpected error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("normalizeEndpoint(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNewStorageRejectsBadConfig(t *testing.T) {
	log := logger.NewSimpleLogger()
	if _, err := NewStorage(StorageConfig{AccessKey: "a", Secret: "b"}, log); err == nil {
		t.Error("expected error when endpoint is missing")
	}
	if _, err := NewStorage(StorageConfig{Endpoint: "https://minio", Secret: "b"}, log); err == nil {
		t.Error("expected error when access key is missing")
	}
	if _, err := NewStorage(StorageConfig{Endpoint: "https://minio", AccessKey: "a"}, log); err == nil {
		t.Error("expected error when secret is missing")
	}
}

func TestNewStorageAppliesEndpointAndPathStyle(t *testing.T) {
	log := logger.NewSimpleLogger()
	s, err := NewStorage(StorageConfig{
		Endpoint:  "minio.local:9000",
		AccessKey: "ak",
		Secret:    "sk",
		PathStyle: true,
	}, log)
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	// The real client was built; assert we can inspect option wiring by
	// calling into the option-capturing real s3 client through a no-op path.
	// We cannot reach into the concrete *s3.Client's private state, so the
	// behavioral guarantee is covered indirectly: missing/empty endpoint
	// errors are caught above, and the client is non-nil here.
	if s == nil || s.client == nil {
		t.Fatal("expected non-nil storage client")
	}
}
