package opus

import (
	"io"
	"sync"
)

// BufferedReader reads ahead from a source Reader on a background goroutine,
// keeping up to depth packets queued so short source stalls drain the buffered
// lead instead of stalling the consumer (an anti-skip buffer, like a CD
// player's). Queued packets are returned first; the source's terminal error
// (io.EOF or a real failure) surfaces only once the queue has drained. It does
// not pre-fill — the lead grows during playback, so there is no startup delay.
// Single-consumer.
type BufferedReader struct {
	src  Reader
	pkts chan []byte
	done chan struct{}
	stop sync.Once
	wg   sync.WaitGroup
	err  error // terminal error; visible after pkts is closed (see run/ReadPacket)
}

// NewBufferedReader starts reading ahead from src, keeping up to depth packets
// queued. depth <= 0 disables buffering and returns src unchanged.
func NewBufferedReader(src Reader, depth int) Reader {
	if depth <= 0 {
		return src
	}
	b := &BufferedReader{
		src:  src,
		pkts: make(chan []byte, depth),
		done: make(chan struct{}),
	}
	b.wg.Add(1)
	go b.run()
	return b
}

func (b *BufferedReader) run() {
	defer b.wg.Done()
	defer close(b.pkts)
	for {
		pkt, err := b.src.ReadPacket()
		if err != nil {
			b.err = err // published to the consumer by the deferred close(b.pkts)
			return
		}
		select {
		case b.pkts <- pkt:
		case <-b.done:
			return
		}
	}
}

// ReadPacket returns the next buffered packet, or the source's terminal error
// once the buffer has drained.
func (b *BufferedReader) ReadPacket() ([]byte, error) {
	pkt, ok := <-b.pkts
	if !ok {
		if b.err != nil {
			return nil, b.err
		}
		return nil, io.EOF
	}
	return pkt, nil
}

// Stop halts the read-ahead goroutine without closing the source; the caller
// owns source teardown (which also unblocks a producer parked in ReadPacket).
// Idempotent and non-blocking.
func (b *BufferedReader) Stop() {
	b.stop.Do(func() { close(b.done) })
}

// Close stops read-ahead, closes the source, and waits for the goroutine to
// exit. For standalone use; when composed into a RecoveryStream the caller pairs
// Stop with the parser's own cleanup instead.
func (b *BufferedReader) Close() error {
	b.Stop()
	err := b.src.Close()
	b.wg.Wait()
	return err
}
