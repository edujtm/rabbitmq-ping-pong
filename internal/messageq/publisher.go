package messageq

import "context"

type Publisher interface {
	Publish(ctx context.Context, data any) error
}
