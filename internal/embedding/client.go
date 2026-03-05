package embedding

import "context"

// Client generates embedding vectors from text inputs.
type Client interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Dimension() int
}
