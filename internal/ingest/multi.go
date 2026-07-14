package ingest

import "context"

// multiConsumer fans a single frame out to several Consumers sequentially
// within one worker's Consume call. This is safe because the frame is only
// released back to the pool after Consume returns (see runWorker), so every
// sub-consumer sees a valid frame; each sub-consumer still owns its own
// buffered state exclusively, since it was itself built fresh per worker by
// its underlying factory.
type multiConsumer struct {
	consumers []Consumer
}

func (m multiConsumer) Consume(ctx context.Context, frame *TelemetryFrame) {
	for _, c := range m.consumers {
		c.Consume(ctx, frame)
	}
}

func (m multiConsumer) Flush(ctx context.Context) {
	for _, c := range m.consumers {
		c.Flush(ctx)
	}
}

// NewMultiConsumerFactory composes several ConsumerFactories into one,
// letting a single WorkerPool run multiple independent downstream pipelines
// (e.g. a Redis time-series writer and a tectonic-rupture radar) off the
// same frame stream without duplicating the pool/channel/backpressure
// machinery.
func NewMultiConsumerFactory(factories ...ConsumerFactory) ConsumerFactory {
	return func(workerID int) Consumer {
		consumers := make([]Consumer, len(factories))
		for i, f := range factories {
			consumers[i] = f(workerID)
		}
		return multiConsumer{consumers: consumers}
	}
}
