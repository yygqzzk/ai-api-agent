package embedding

import "context"

// NoopClient returns zero vectors. Used in memory mode and tests.
type NoopClient struct {
	dim int
}

func NewNoopClient(dim int) *NoopClient {
	if dim <= 0 {
		dim = 1024
	}
	return &NoopClient{dim: dim}
}

func (c *NoopClient) Embed(_ context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i := range texts {
		result[i] = make([]float32, c.dim)
	}
	return result, nil
}

func (c *NoopClient) Dimension() int { return c.dim }
