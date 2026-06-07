package jobs

import (
	"context"
	"errors"
	"fmt"
	"os"

	"heph4estus/internal/cloud"
)

type fileChunkUpload struct {
	Path     string
	Key      string
	ByteSize int64
	Index    int
}

func uploadFileChunk(ctx context.Context, storage cloud.Storage, bucket string, chunk fileChunkUpload, maxChunkSize int64) error {
	if chunk.ByteSize > maxChunkSize && maxChunkSize > 0 {
		return fmt.Errorf("chunk %d (%s) is %d bytes, above max safe chunk size %d", chunk.Index, chunk.Key, chunk.ByteSize, maxChunkSize)
	}

	if streamer, ok := storage.(cloud.StreamingStorage); ok {
		file, err := os.Open(chunk.Path)
		if err != nil {
			return fmt.Errorf("opening chunk %d (%s): %w", chunk.Index, chunk.Key, err)
		}
		err = streamer.UploadStream(ctx, bucket, chunk.Key, file, chunk.ByteSize)
		closeErr := file.Close()
		if err == nil {
			if closeErr != nil {
				return fmt.Errorf("closing chunk %d (%s): %w", chunk.Index, chunk.Key, closeErr)
			}
			return nil
		}
		if errors.Is(err, cloud.ErrNotImplemented) {
			if closeErr != nil {
				return fmt.Errorf("closing chunk %d (%s): %w", chunk.Index, chunk.Key, closeErr)
			}
			return uploadFileChunkBuffered(ctx, storage, bucket, chunk, maxChunkSize)
		}
		if closeErr != nil {
			err = errors.Join(err, fmt.Errorf("closing chunk file: %w", closeErr))
		}
		return fmt.Errorf("uploading chunk %d (%s): %w", chunk.Index, chunk.Key, err)
	}

	return uploadFileChunkBuffered(ctx, storage, bucket, chunk, maxChunkSize)
}

func uploadFileChunkBuffered(ctx context.Context, storage cloud.Storage, bucket string, chunk fileChunkUpload, maxChunkSize int64) error {
	data, err := os.ReadFile(chunk.Path)
	if err != nil {
		return fmt.Errorf("reading chunk %d (%s): %w", chunk.Index, chunk.Key, err)
	}
	if int64(len(data)) > maxChunkSize && maxChunkSize > 0 {
		return fmt.Errorf("chunk %d (%s) is %d bytes, above max safe chunk size %d", chunk.Index, chunk.Key, len(data), maxChunkSize)
	}
	if err := storage.Upload(ctx, bucket, chunk.Key, data); err != nil {
		return fmt.Errorf("uploading chunk %d (%s): %w", chunk.Index, chunk.Key, err)
	}
	return nil
}
