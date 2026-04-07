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
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
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
	sendMessageFunc      func(context.Context, *sqs.SendMessageInput, ...func(*sqs.Options)) (*sqs.SendMessageOutput, error)
	sendMessageBatchFunc func(context.Context, *sqs.SendMessageBatchInput, ...func(*sqs.Options)) (*sqs.SendMessageBatchOutput, error)
	receiveMessageFunc   func(context.Context, *sqs.ReceiveMessageInput, ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	deleteMessageFunc    func(context.Context, *sqs.DeleteMessageInput, ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error)
}

func (m *mockSQSAPI) SendMessage(ctx context.Context, in *sqs.SendMessageInput, opts ...func(*sqs.Options)) (*sqs.SendMessageOutput, error) {
	return m.sendMessageFunc(ctx, in, opts...)
}
func (m *mockSQSAPI) SendMessageBatch(ctx context.Context, in *sqs.SendMessageBatchInput, opts ...func(*sqs.Options)) (*sqs.SendMessageBatchOutput, error) {
	return m.sendMessageBatchFunc(ctx, in, opts...)
}
func (m *mockSQSAPI) ReceiveMessage(ctx context.Context, in *sqs.ReceiveMessageInput, opts ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	return m.receiveMessageFunc(ctx, in, opts...)
}
func (m *mockSQSAPI) DeleteMessage(ctx context.Context, in *sqs.DeleteMessageInput, opts ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error) {
	return m.deleteMessageFunc(ctx, in, opts...)
}

var _ SQSAPI = (*mockSQSAPI)(nil)

type mockECSAPI struct {
	runTaskFunc func(context.Context, *ecs.RunTaskInput, ...func(*ecs.Options)) (*ecs.RunTaskOutput, error)
}

func (m *mockECSAPI) RunTask(ctx context.Context, in *ecs.RunTaskInput, opts ...func(*ecs.Options)) (*ecs.RunTaskOutput, error) {
	return m.runTaskFunc(ctx, in, opts...)
}

var _ ECSAPI = (*mockECSAPI)(nil)

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

// ---------- SendBatch tests ----------

func TestSQSSendBatch_SingleBatch(t *testing.T) {
	var called int
	client := &SQSClient{
		logger: nopLogger{},
		client: &mockSQSAPI{
			sendMessageBatchFunc: func(_ context.Context, in *sqs.SendMessageBatchInput, _ ...func(*sqs.Options)) (*sqs.SendMessageBatchOutput, error) {
				called++
				if len(in.Entries) != 3 {
					t.Fatalf("expected 3 entries, got %d", len(in.Entries))
				}
				return &sqs.SendMessageBatchOutput{}, nil
			},
		},
	}
	err := client.SendBatch(context.Background(), "q", []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called != 1 {
		t.Fatalf("expected 1 batch call, got %d", called)
	}
}

func TestSQSSendBatch_MultiBatch(t *testing.T) {
	var called int
	client := &SQSClient{
		logger: nopLogger{},
		client: &mockSQSAPI{
			sendMessageBatchFunc: func(_ context.Context, in *sqs.SendMessageBatchInput, _ ...func(*sqs.Options)) (*sqs.SendMessageBatchOutput, error) {
				called++
				return &sqs.SendMessageBatchOutput{}, nil
			},
		},
	}
	bodies := make([]string, 25)
	for i := range bodies {
		bodies[i] = "msg"
	}
	err := client.SendBatch(context.Background(), "q", bodies)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called != 3 { // 10 + 10 + 5
		t.Fatalf("expected 3 batch calls, got %d", called)
	}
}

func TestSQSSendBatch_Empty(t *testing.T) {
	client := &SQSClient{
		logger: nopLogger{},
		client: &mockSQSAPI{
			sendMessageBatchFunc: func(context.Context, *sqs.SendMessageBatchInput, ...func(*sqs.Options)) (*sqs.SendMessageBatchOutput, error) {
				t.Fatal("should not be called for empty input")
				return nil, nil
			},
		},
	}
	err := client.SendBatch(context.Background(), "q", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------- Paginated List tests ----------

func TestS3List_Paginated(t *testing.T) {
	callCount := 0
	client := &S3Client{
		logger: nopLogger{},
		client: &mockS3API{
			listObjectsV2Func: func(_ context.Context, in *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
				callCount++
				if callCount == 1 {
					return &s3.ListObjectsV2Output{
						Contents: []s3types.Object{
							{Key: aws.String("a")},
						},
						IsTruncated:           aws.Bool(true),
						NextContinuationToken: aws.String("token1"),
					}, nil
				}
				return &s3.ListObjectsV2Output{
					Contents: []s3types.Object{
						{Key: aws.String("b")},
					},
					IsTruncated: aws.Bool(false),
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
	if callCount != 2 {
		t.Fatalf("expected 2 API calls, got %d", callCount)
	}
}

// ---------- Count tests ----------

func TestS3Count_Success(t *testing.T) {
	client := &S3Client{
		logger: nopLogger{},
		client: &mockS3API{
			listObjectsV2Func: func(context.Context, *s3.ListObjectsV2Input, ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
				return &s3.ListObjectsV2Output{
					Contents: []s3types.Object{
						{Key: aws.String("a")},
						{Key: aws.String("b")},
						{Key: aws.String("c")},
					},
					IsTruncated: aws.Bool(false),
				}, nil
			},
		},
	}
	count, err := client.Count(context.Background(), "bucket", "prefix")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected 3, got %d", count)
	}
}

func TestS3Count_Paginated(t *testing.T) {
	callCount := 0
	client := &S3Client{
		logger: nopLogger{},
		client: &mockS3API{
			listObjectsV2Func: func(_ context.Context, _ *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
				callCount++
				if callCount == 1 {
					return &s3.ListObjectsV2Output{
						Contents:              []s3types.Object{{Key: aws.String("a")}, {Key: aws.String("b")}},
						IsTruncated:           aws.Bool(true),
						NextContinuationToken: aws.String("tok"),
					}, nil
				}
				return &s3.ListObjectsV2Output{
					Contents:    []s3types.Object{{Key: aws.String("c")}},
					IsTruncated: aws.Bool(false),
				}, nil
			},
		},
	}
	count, err := client.Count(context.Background(), "bucket", "prefix")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected 3, got %d", count)
	}
}

// ---------- ECS tests ----------

func TestECS_RunContainer_Success(t *testing.T) {
	client := &ECSClient{
		logger: nopLogger{},
		client: &mockECSAPI{
			runTaskFunc: func(_ context.Context, in *ecs.RunTaskInput, _ ...func(*ecs.Options)) (*ecs.RunTaskOutput, error) {
				if aws.ToString(in.Cluster) != "my-cluster" {
					t.Fatalf("unexpected cluster: %s", aws.ToString(in.Cluster))
				}
				return &ecs.RunTaskOutput{
					Tasks: []ecstypes.Task{
						{TaskArn: aws.String("arn:task:1")},
						{TaskArn: aws.String("arn:task:2")},
					},
				}, nil
			},
		},
	}
	arn, err := client.RunContainer(context.Background(), cloud.ContainerOpts{
		Cluster:        "my-cluster",
		TaskDefinition: "arn:taskdef",
		Count:          2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if arn != "arn:task:1,arn:task:2" {
		t.Fatalf("unexpected ARN: %s", arn)
	}
}

func TestECS_RunContainer_MultipleBatches(t *testing.T) {
	var calls int
	client := &ECSClient{
		logger: nopLogger{},
		client: &mockECSAPI{
			runTaskFunc: func(_ context.Context, in *ecs.RunTaskInput, _ ...func(*ecs.Options)) (*ecs.RunTaskOutput, error) {
				calls++
				count := aws.ToInt32(in.Count)
				tasks := make([]ecstypes.Task, count)
				for i := range tasks {
					arn := "arn:task"
					tasks[i] = ecstypes.Task{TaskArn: &arn}
				}
				return &ecs.RunTaskOutput{Tasks: tasks}, nil
			},
		},
	}
	_, err := client.RunContainer(context.Background(), cloud.ContainerOpts{
		Cluster:        "c",
		TaskDefinition: "td",
		Count:          15,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 2 { // 10 + 5
		t.Fatalf("expected 2 RunTask calls, got %d", calls)
	}
}

func TestECS_RunContainer_Error(t *testing.T) {
	want := errors.New("run failed")
	client := &ECSClient{
		logger: nopLogger{},
		client: &mockECSAPI{
			runTaskFunc: func(context.Context, *ecs.RunTaskInput, ...func(*ecs.Options)) (*ecs.RunTaskOutput, error) {
				return nil, want
			},
		},
	}
	_, err := client.RunContainer(context.Background(), cloud.ContainerOpts{
		Cluster:        "c",
		TaskDefinition: "td",
		Count:          1,
	})
	if !errors.Is(err, want) {
		t.Fatalf("expected %v, got %v", want, err)
	}
}

func TestECS_SpotMethods_ReturnNotImplemented(t *testing.T) {
	client := &ECSClient{logger: nopLogger{}, client: &mockECSAPI{}}
	ctx := context.Background()

	_, err := client.RunSpotInstances(ctx, cloud.SpotOpts{})
	if !errors.Is(err, cloud.ErrNotImplemented) {
		t.Fatalf("expected ErrNotImplemented, got %v", err)
	}

	_, err = client.GetSpotStatus(ctx, nil)
	if !errors.Is(err, cloud.ErrNotImplemented) {
		t.Fatalf("expected ErrNotImplemented, got %v", err)
	}
}

func TestECS_ContainerName_UsedInOverrides(t *testing.T) {
	var capturedName string
	client := &ECSClient{
		logger: nopLogger{},
		client: &mockECSAPI{
			runTaskFunc: func(_ context.Context, in *ecs.RunTaskInput, _ ...func(*ecs.Options)) (*ecs.RunTaskOutput, error) {
				if in.Overrides != nil && len(in.Overrides.ContainerOverrides) > 0 {
					capturedName = aws.ToString(in.Overrides.ContainerOverrides[0].Name)
				}
				return &ecs.RunTaskOutput{
					Tasks: []ecstypes.Task{{TaskArn: aws.String("arn:task:1")}},
				}, nil
			},
		},
	}
	_, err := client.RunContainer(context.Background(), cloud.ContainerOpts{
		Cluster:        "c",
		TaskDefinition: "td",
		ContainerName:  "nmap-worker",
		Count:          1,
		Env:            map[string]string{"FOO": "bar"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedName != "nmap-worker" {
		t.Fatalf("expected container name nmap-worker, got %s", capturedName)
	}
}

func TestECS_ContainerName_DefaultsToWorker(t *testing.T) {
	var capturedName string
	client := &ECSClient{
		logger: nopLogger{},
		client: &mockECSAPI{
			runTaskFunc: func(_ context.Context, in *ecs.RunTaskInput, _ ...func(*ecs.Options)) (*ecs.RunTaskOutput, error) {
				if in.Overrides != nil && len(in.Overrides.ContainerOverrides) > 0 {
					capturedName = aws.ToString(in.Overrides.ContainerOverrides[0].Name)
				}
				return &ecs.RunTaskOutput{
					Tasks: []ecstypes.Task{{TaskArn: aws.String("arn:task:1")}},
				}, nil
			},
		},
	}
	_, err := client.RunContainer(context.Background(), cloud.ContainerOpts{
		Cluster:        "c",
		TaskDefinition: "td",
		Count:          1,
		Env:            map[string]string{"FOO": "bar"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedName != "worker" {
		t.Fatalf("expected default container name worker, got %s", capturedName)
	}
}

// ---------- EC2 tests ----------

type mockEC2API struct {
	createFleetFunc          func(context.Context, *ec2.CreateFleetInput, ...func(*ec2.Options)) (*ec2.CreateFleetOutput, error)
	describeInstancesFunc    func(context.Context, *ec2.DescribeInstancesInput, ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
	terminateInstancesFunc   func(context.Context, *ec2.TerminateInstancesInput, ...func(*ec2.Options)) (*ec2.TerminateInstancesOutput, error)
	createLaunchTemplateFunc func(context.Context, *ec2.CreateLaunchTemplateInput, ...func(*ec2.Options)) (*ec2.CreateLaunchTemplateOutput, error)
	deleteLaunchTemplateFunc func(context.Context, *ec2.DeleteLaunchTemplateInput, ...func(*ec2.Options)) (*ec2.DeleteLaunchTemplateOutput, error)
}

func (m *mockEC2API) CreateFleet(ctx context.Context, in *ec2.CreateFleetInput, opts ...func(*ec2.Options)) (*ec2.CreateFleetOutput, error) {
	return m.createFleetFunc(ctx, in, opts...)
}
func (m *mockEC2API) DescribeInstances(ctx context.Context, in *ec2.DescribeInstancesInput, opts ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	return m.describeInstancesFunc(ctx, in, opts...)
}
func (m *mockEC2API) TerminateInstances(ctx context.Context, in *ec2.TerminateInstancesInput, opts ...func(*ec2.Options)) (*ec2.TerminateInstancesOutput, error) {
	return m.terminateInstancesFunc(ctx, in, opts...)
}
func (m *mockEC2API) CreateLaunchTemplate(ctx context.Context, in *ec2.CreateLaunchTemplateInput, opts ...func(*ec2.Options)) (*ec2.CreateLaunchTemplateOutput, error) {
	return m.createLaunchTemplateFunc(ctx, in, opts...)
}
func (m *mockEC2API) DeleteLaunchTemplate(ctx context.Context, in *ec2.DeleteLaunchTemplateInput, opts ...func(*ec2.Options)) (*ec2.DeleteLaunchTemplateOutput, error) {
	return m.deleteLaunchTemplateFunc(ctx, in, opts...)
}

var _ EC2API = (*mockEC2API)(nil)

func newMockEC2() *mockEC2API {
	return &mockEC2API{
		createLaunchTemplateFunc: func(_ context.Context, _ *ec2.CreateLaunchTemplateInput, _ ...func(*ec2.Options)) (*ec2.CreateLaunchTemplateOutput, error) {
			return &ec2.CreateLaunchTemplateOutput{
				LaunchTemplate: &ec2types.LaunchTemplate{
					LaunchTemplateId: aws.String("lt-123"),
				},
			}, nil
		},
		deleteLaunchTemplateFunc: func(context.Context, *ec2.DeleteLaunchTemplateInput, ...func(*ec2.Options)) (*ec2.DeleteLaunchTemplateOutput, error) {
			return &ec2.DeleteLaunchTemplateOutput{}, nil
		},
	}
}

func TestEC2_RunSpotInstances_Success(t *testing.T) {
	mock := newMockEC2()
	mock.createFleetFunc = func(_ context.Context, in *ec2.CreateFleetInput, _ ...func(*ec2.Options)) (*ec2.CreateFleetOutput, error) {
		if aws.ToInt32(in.TargetCapacitySpecification.TotalTargetCapacity) != 3 {
			t.Fatalf("expected 3 instances, got %d", aws.ToInt32(in.TargetCapacitySpecification.TotalTargetCapacity))
		}
		return &ec2.CreateFleetOutput{
			Instances: []ec2types.CreateFleetInstance{
				{InstanceIds: []string{"i-1", "i-2", "i-3"}},
			},
		}, nil
	}

	client := &EC2Client{client: mock, logger: nopLogger{}}
	ids, err := client.RunSpotInstances(context.Background(), cloud.SpotOpts{
		AMI:           "ami-123",
		InstanceTypes: []string{"c5.xlarge"},
		Count:         3,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 3 {
		t.Fatalf("expected 3 instances, got %d", len(ids))
	}
}

func TestEC2_RunSpotInstances_FleetError(t *testing.T) {
	mock := newMockEC2()
	deleteCalled := false
	mock.deleteLaunchTemplateFunc = func(context.Context, *ec2.DeleteLaunchTemplateInput, ...func(*ec2.Options)) (*ec2.DeleteLaunchTemplateOutput, error) {
		deleteCalled = true
		return &ec2.DeleteLaunchTemplateOutput{}, nil
	}
	mock.createFleetFunc = func(context.Context, *ec2.CreateFleetInput, ...func(*ec2.Options)) (*ec2.CreateFleetOutput, error) {
		return nil, errors.New("capacity error")
	}

	client := &EC2Client{client: mock, logger: nopLogger{}}
	_, err := client.RunSpotInstances(context.Background(), cloud.SpotOpts{
		AMI:   "ami-123",
		Count: 1,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "CreateFleet") {
		t.Fatalf("expected CreateFleet in error, got: %v", err)
	}
	if !deleteCalled {
		t.Fatal("expected launch template cleanup on fleet error")
	}
}

func TestEC2_GetSpotStatus(t *testing.T) {
	mock := newMockEC2()
	mock.describeInstancesFunc = func(_ context.Context, in *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
		return &ec2.DescribeInstancesOutput{
			Reservations: []ec2types.Reservation{
				{
					Instances: []ec2types.Instance{
						{
							InstanceId:      aws.String("i-1"),
							State:           &ec2types.InstanceState{Name: ec2types.InstanceStateNameRunning},
							PublicIpAddress: aws.String("1.2.3.4"),
						},
						{
							InstanceId:      aws.String("i-2"),
							State:           &ec2types.InstanceState{Name: ec2types.InstanceStateNamePending},
							PublicIpAddress: nil,
						},
					},
				},
			},
		}, nil
	}

	client := &EC2Client{client: mock, logger: nopLogger{}}
	statuses, err := client.GetSpotStatus(context.Background(), []string{"i-1", "i-2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(statuses))
	}
	if statuses[0].State != "running" || statuses[0].PublicIP != "1.2.3.4" {
		t.Fatalf("unexpected first status: %+v", statuses[0])
	}
	if statuses[1].State != "pending" || statuses[1].PublicIP != "" {
		t.Fatalf("unexpected second status: %+v", statuses[1])
	}
}

func TestEC2_RunContainer_NotImplemented(t *testing.T) {
	client := &EC2Client{client: newMockEC2(), logger: nopLogger{}}
	_, err := client.RunContainer(context.Background(), cloud.ContainerOpts{})
	if !errors.Is(err, cloud.ErrNotImplemented) {
		t.Fatalf("expected ErrNotImplemented, got %v", err)
	}
}

// ---------- CompositeCompute tests ----------

func TestCompositeCompute_DelegatesToECS(t *testing.T) {
	ecsCalled := false
	ecsClient := &ECSClient{
		logger: nopLogger{},
		client: &mockECSAPI{
			runTaskFunc: func(context.Context, *ecs.RunTaskInput, ...func(*ecs.Options)) (*ecs.RunTaskOutput, error) {
				ecsCalled = true
				return &ecs.RunTaskOutput{
					Tasks: []ecstypes.Task{{TaskArn: aws.String("arn:task")}},
				}, nil
			},
		},
	}
	composite := &CompositeCompute{ecs: ecsClient, ec2: &EC2Client{client: newMockEC2(), logger: nopLogger{}}}

	_, err := composite.RunContainer(context.Background(), cloud.ContainerOpts{
		Cluster:        "c",
		TaskDefinition: "td",
		Count:          1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ecsCalled {
		t.Fatal("expected ECS to be called for RunContainer")
	}
}

func TestCompositeCompute_DelegatesToEC2(t *testing.T) {
	mock := newMockEC2()
	ec2Called := false
	mock.createFleetFunc = func(context.Context, *ec2.CreateFleetInput, ...func(*ec2.Options)) (*ec2.CreateFleetOutput, error) {
		ec2Called = true
		return &ec2.CreateFleetOutput{
			Instances: []ec2types.CreateFleetInstance{{InstanceIds: []string{"i-1"}}},
		}, nil
	}
	ec2Client := &EC2Client{client: mock, logger: nopLogger{}}
	composite := &CompositeCompute{
		ecs: &ECSClient{logger: nopLogger{}, client: &mockECSAPI{}},
		ec2: ec2Client,
	}

	_, err := composite.RunSpotInstances(context.Background(), cloud.SpotOpts{
		AMI:   "ami-123",
		Count: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ec2Called {
		t.Fatal("expected EC2 to be called for RunSpotInstances")
	}
}
