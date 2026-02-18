package util

import (
	"context"
	"sync"
)

func Parallel[T any](inputs []T, workerLimit int, fn func(context.Context, T) error) error {
	if len(inputs) == 0 {
		return nil
	}

	if workerLimit <= 0 {
		workerLimit = 1
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tasks := make(chan T)
	errCh := make(chan error, 1)

	// workers
	wg := sync.WaitGroup{}
	for i := 0; i < workerLimit; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range tasks {
				if err := fn(ctx, item); err != nil {
					select {
					case errCh <- err:
						cancel() // stop others
					default:
					}
					return
				}
			}
		}()
	}

	// feed tasks
	go func() {
		defer close(tasks)
		for _, item := range inputs {
			// stop feeding when context canceled
			select {
			case <-ctx.Done():
				return
			case tasks <- item:
			}
		}
	}()

	// wait workers
	wg.Wait()
	cancel()

	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}
