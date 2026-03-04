package aws

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"heph4estus/internal/cloud"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sfn"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

// ---------- test logger ----------

type nopLogger struct{}

func (nopLogger) Info(string, ...interface{})  {}
func (nopLogger) Error(string, ...interface{}) {}
func (nopLogger) Fatal(string, ...interface{}) {}

// ---------- SDK-level mocks ----------

type mockS3API struct {
	putObjectFunc      func(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	getObjectFunc      func(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	listObjectsV2Func func(context.Context, *s3.ListObjectsV2Input, ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
}

func (m *mockS3API) PutObject(ctx context.Context, in *s3.PutObjectInput, opts ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	return m.putObjectFunc(ctx, in, opts...)
}
func (m *mockS3API) GetObject(ctx context.Context, in *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	return m.getObjectFunc(ctx, in, opts...)
}
func (m *mockS3API) ListObjectsV2(ctx context.Context, in *s3.ListObjectsV2Input, opts ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	return m.listObjectsV2Func(ctx, in, opts...)
}

var _ S3API = (*mockS3API)(nil)

type mockSQSAPI struct {
	sendMessageFunc    func(context.Context, *sqs.SendMessageInput, ...func(*sqs.Options)) (*sqs.SendMessageOutput, error)
	receiveMessageFunc func(context.Context, *sqs.ReceiveMessageInput, ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	deleteMessageFunc  func(context.Context, *sqs.DeleteMessageInput, ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error)
}

func (m *mockSQSAPI) SendMessage(ctx context.Context, in *sqs.SendMessageInput, opts ...func(*sqs.Options)) (*sqs.SendMessageOutput, error) {
	return m.sendMessageFunc(ctx, in, opts...)
}
func (m *mockSQSAPI) ReceiveMessage(ctx context.Context, in *sqs.ReceiveMessageInput, opts ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	return m.receiveMessageFunc(ctx, in, opts...)
}
func (m *mockSQSAPI) DeleteMessage(ctx context.Context, in *sqs.DeleteMessageInput, opts ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error) {
	return m.deleteMessageFunc(ctx, in, opts...)
}

var _ SQSAPI = (*mockSQSAPI)(nil)

type mockSFNAPI struct {
	startExecutionFunc func(context.Context, *sfn.StartExecutionInput, ...func(*sfn.Options)) (*sfn.StartExecutionOutput, error)
}

func (m *mockSFNAPI) StartExecution(ctx context.Context, in *sfn.StartExecutionInput, opts ...func(*sfn.Options)) (*sfn.StartExecutionOutput, error) {
	return m.startExecutionFunc(ctx, in, opts...)
}

var _ SFNAPI = (*mockSFNAPI)(nil)

// ---------- S3 tests ----------

func TestS3Upload_Success(t *testing.T) {
	client := &S3Client{
		logger: nopLogger{},
		client: &mockS3API{
			putObjectFunc: func(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
				if aws.ToString(in.Bucket) != "b" || aws.ToString(in.Key) != "k" {
					t.Fatalf("unexpected bucket/key: %s/%s", aws.ToString(in.Bucket), aws.ToString(in.Key))
				}
				return &s3.PutObjectOutput{}, nil
			},
		},
	}
	if err := client.Upload(context.Background(), "b", "k", []byte("data")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestS3Upload_Error(t *testing.T) {
	want := errors.New("put failed")
	client := &S3Client{
		logger: nopLogger{},
		client: &mockS3API{
			putObjectFunc: func(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
				return nil, want
			},
		},
	}
	if err := client.Upload(context.Background(), "b", "k", nil); !errors.Is(err, want) {
		t.Fatalf("expected %v, got %v", want, err)
	}
}

func TestS3Download_Success(t *testing.T) {
	client := &S3Client{
		logger: nopLogger{},
		client: &mockS3API{
			getObjectFunc: func(_ context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
				return &s3.GetObjectOutput{
					Body: io.NopCloser(strings.NewReader("hello")),
				}, nil
			},
		},
	}
	data, err := client.Download(context.Background(), "b", "k")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("expected hello, got %s", string(data))
	}
}

func TestS3Download_Error(t *testing.T) {
	want := errors.New("get failed")
	client := &S3Client{
		logger: nopLogger{},
		client: &mockS3API{
			getObjectFunc: func(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
				return nil, want
			},
		},
	}
	_, err := client.Download(context.Background(), "b", "k")
	if !errors.Is(err, want) {
		t.Fatalf("expected %v, got %v", want, err)
	}
}

func TestS3List_Success(t *testing.T) {
	client := &S3Client{
		logger: nopLogger{},
		client: &mockS3API{
			listObjectsV2Func: func(_ context.Context, in *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
				return &s3.ListObjectsV2Output{
					Contents: []s3types.Object{
						{Key: aws.String("a")},
						{Key: aws.String("b")},
					},
				}, nil
			},
		},
	}
	keys, err := client.List(context.Background(), "bucket", "prefix")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 2 || keys[0] != "a" || keys[1] != "b" {
		t.Fatalf("unexpected keys: %v", keys)
	}
}

func TestS3List_Empty(t *testing.T) {
	client := &S3Client{
		logger: nopLogger{},
		client: &mockS3API{
			listObjectsV2Func: func(context.Context, *s3.ListObjectsV2Input, ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
				return &s3.ListObjectsV2Output{}, nil
			},
		},
	}
	keys, err := client.List(context.Background(), "bucket", "prefix")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("expected empty, got %v", keys)
	}
}

func TestS3PutObject_BackwardCompat(t *testing.T) {
	called := false
	client := &S3Client{
		logger: nopLogger{},
		client: &mockS3API{
			putObjectFunc: func(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
				called = true
				return &s3.PutObjectOutput{}, nil
			},
		},
	}
	if err := client.PutObject(context.Background(), "b", "k", []byte("data")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("PutObject did not delegate to Upload")
	}
}

// ---------- SQS tests ----------

func TestSQSSend_Success(t *testing.T) {
	client := &SQSClient{
		logger: nopLogger{},
		client: &mockSQSAPI{
			sendMessageFunc: func(_ context.Context, in *sqs.SendMessageInput, _ ...func(*sqs.Options)) (*sqs.SendMessageOutput, error) {
				if *in.QueueUrl != "q" || *in.MessageBody != "body" {
					t.Fatalf("unexpected params: %s %s", *in.QueueUrl, *in.MessageBody)
				}
				return &sqs.SendMessageOutput{}, nil
			},
		},
	}
	if err := client.Send(context.Background(), "q", "body"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSQSSend_Error(t *testing.T) {
	want := errors.New("send failed")
	client := &SQSClient{
		logger: nopLogger{},
		client: &mockSQSAPI{
			sendMessageFunc: func(context.Context, *sqs.SendMessageInput, ...func(*sqs.Options)) (*sqs.SendMessageOutput, error) {
				return nil, want
			},
		},
	}
	if err := client.Send(context.Background(), "q", "b"); !errors.Is(err, want) {
		t.Fatalf("expected %v, got %v", want, err)
	}
}

func TestSQSReceive_WithMessage(t *testing.T) {
	client := &SQSClient{
		logger: nopLogger{},
		client: &mockSQSAPI{
			receiveMessageFunc: func(context.Context, *sqs.ReceiveMessageInput, ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
				return &sqs.ReceiveMessageOutput{
					Messages: []sqstypes.Message{
						{
							MessageId:     aws.String("id-1"),
							Body:          aws.String("task-body"),
							ReceiptHandle: aws.String("rh-1"),
						},
					},
				}, nil
			},
		},
	}
	msg, err := client.Receive(context.Background(), "q")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg == nil {
		t.Fatal("expected message, got nil")
	}
	if msg.ID != "id-1" || msg.Body != "task-body" || msg.ReceiptHandle != "rh-1" {
		t.Fatalf("unexpected message: %+v", msg)
	}
}

func TestSQSReceive_NoMessages(t *testing.T) {
	client := &SQSClient{
		logger: nopLogger{},
		client: &mockSQSAPI{
			receiveMessageFunc: func(context.Context, *sqs.ReceiveMessageInput, ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
				return &sqs.ReceiveMessageOutput{}, nil
			},
		},
	}
	msg, err := client.Receive(context.Background(), "q")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg != nil {
		t.Fatalf("expected nil message, got %+v", msg)
	}
}

func TestSQSReceive_Error(t *testing.T) {
	want := errors.New("receive failed")
	client := &SQSClient{
		logger: nopLogger{},
		client: &mockSQSAPI{
			receiveMessageFunc: func(context.Context, *sqs.ReceiveMessageInput, ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
				return nil, want
			},
		},
	}
	_, err := client.Receive(context.Background(), "q")
	if !errors.Is(err, want) {
		t.Fatalf("expected %v, got %v", want, err)
	}
}

func TestSQSDelete_Success(t *testing.T) {
	client := &SQSClient{
		logger: nopLogger{},
		client: &mockSQSAPI{
			deleteMessageFunc: func(_ context.Context, in *sqs.DeleteMessageInput, _ ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error) {
				if *in.QueueUrl != "q" || *in.ReceiptHandle != "rh" {
					t.Fatalf("unexpected params: %s %s", *in.QueueUrl, *in.ReceiptHandle)
				}
				return &sqs.DeleteMessageOutput{}, nil
			},
		},
	}
	if err := client.Delete(context.Background(), "q", "rh"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSQSDelete_Error(t *testing.T) {
	want := errors.New("delete failed")
	client := &SQSClient{
		logger: nopLogger{},
		client: &mockSQSAPI{
			deleteMessageFunc: func(context.Context, *sqs.DeleteMessageInput, ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error) {
				return nil, want
			},
		},
	}
	if err := client.Delete(context.Background(), "q", "rh"); !errors.Is(err, want) {
		t.Fatalf("expected %v, got %v", want, err)
	}
}

// ---------- SFN tests ----------

func TestSFNStartExecution_Success(t *testing.T) {
	client := &SFNClient{
		logger: nopLogger{},
		client: &mockSFNAPI{
			startExecutionFunc: func(_ context.Context, in *sfn.StartExecutionInput, _ ...func(*sfn.Options)) (*sfn.StartExecutionOutput, error) {
				if aws.ToString(in.StateMachineArn) != "arn" {
					t.Fatalf("unexpected ARN: %s", aws.ToString(in.StateMachineArn))
				}
				return &sfn.StartExecutionOutput{
					ExecutionArn: aws.String("exec-arn"),
				}, nil
			},
		},
	}
	out, err := client.StartExecution(context.Background(), "arn", "{}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if aws.ToString(out.ExecutionArn) != "exec-arn" {
		t.Fatalf("unexpected execution ARN: %s", aws.ToString(out.ExecutionArn))
	}
}

func TestSFNStartExecution_Error(t *testing.T) {
	want := errors.New("start failed")
	client := &SFNClient{
		logger: nopLogger{},
		client: &mockSFNAPI{
			startExecutionFunc: func(context.Context, *sfn.StartExecutionInput, ...func(*sfn.Options)) (*sfn.StartExecutionOutput, error) {
				return nil, want
			},
		},
	}
	_, err := client.StartExecution(context.Background(), "arn", "{}")
	if !errors.Is(err, want) {
		t.Fatalf("expected %v, got %v", want, err)
	}
}

// ---------- Compute stub tests ----------

func TestStubCompute_AllMethodsReturnErrNotImplemented(t *testing.T) {
	c := stubCompute{}
	ctx := context.Background()

	_, err := c.RunContainer(ctx, cloud.ContainerOpts{})
	if !errors.Is(err, cloud.ErrNotImplemented) {
		t.Fatalf("RunContainer: expected ErrNotImplemented, got %v", err)
	}

	_, err = c.RunSpotInstances(ctx, cloud.SpotOpts{})
	if !errors.Is(err, cloud.ErrNotImplemented) {
		t.Fatalf("RunSpotInstances: expected ErrNotImplemented, got %v", err)
	}

	_, err = c.GetSpotStatus(ctx, nil)
	if !errors.Is(err, cloud.ErrNotImplemented) {
		t.Fatalf("GetSpotStatus: expected ErrNotImplemented, got %v", err)
	}
}
