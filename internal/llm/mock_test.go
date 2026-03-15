package llm_test

import (
	"context"
	"errors"
	"testing"

	"github.com/baochen10luo/stagenthand/internal/llm"
	"github.com/stretchr/testify/assert"
)

func TestMockClient_GenerateTransformation(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		mock := &llm.MockClient{
			GenerateFunc: func(ctx context.Context, systemPrompt string, inputData []byte) ([]byte, error) {
				return []byte(`{"result":"ok"}`), nil
			},
		}

		res, err := mock.GenerateTransformation(context.Background(), "sys prompt", []byte("input"))
		assert.NoError(t, err)
		assert.Equal(t, []byte(`{"result":"ok"}`), res)
		assert.Equal(t, 1, mock.CallCount)
	})

	t.Run("error", func(t *testing.T) {
		mockErr := errors.New("mock error")
		mock := &llm.MockClient{
			GenerateFunc: func(ctx context.Context, systemPrompt string, inputData []byte) ([]byte, error) {
				return nil, mockErr
			},
		}

		res, err := mock.GenerateTransformation(context.Background(), "sys prompt", []byte("input"))
		assert.ErrorIs(t, err, mockErr)
		assert.Nil(t, res)
		assert.Equal(t, 1, mock.CallCount)
	})

	t.Run("nil GenerateFunc returns nil nil", func(t *testing.T) {
		// When GenerateFunc is nil, MockClient should return (nil, nil) without panicking.
		mock := &llm.MockClient{}

		res, err := mock.GenerateTransformation(context.Background(), "sys", []byte("in"))
		assert.NoError(t, err)
		assert.Nil(t, res)
		assert.Equal(t, 1, mock.CallCount)
	})

	t.Run("call count increments on each call", func(t *testing.T) {
		mock := &llm.MockClient{
			GenerateFunc: func(ctx context.Context, systemPrompt string, inputData []byte) ([]byte, error) {
				return []byte(`{}`), nil
			},
		}

		for i := 1; i <= 3; i++ {
			_, _ = mock.GenerateTransformation(context.Background(), "s", []byte("d"))
			assert.Equal(t, i, mock.CallCount)
		}
	})
}
