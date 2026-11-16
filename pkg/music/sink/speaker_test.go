package sink

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestSpeakerPausePreservesPCMAndStopWins(t *testing.T) {
	pcm := bytes.NewReader([]byte{1, 2, 3, 4})
	stop := make(chan struct{})
	control := &Control{Stop: stop}
	r := &speakerStopReader{r: pcm, control: control}
	packet := make([]byte, 2)
	_, err := r.Read(packet)
	if err != nil || !bytes.Equal(packet, []byte{1, 2}) {
		t.Fatalf("first read: %v, %v", packet, err)
	}

	control.Pause()
	_, err = r.Read(packet)
	if err != nil || !bytes.Equal(packet, []byte{0, 0}) || pcm.Len() != 2 {
		t.Fatalf("pause consumed PCM: %v, %v, remaining=%d", packet, err, pcm.Len())
	}
	control.Resume()
	_, err = r.Read(packet)
	if err != nil || !bytes.Equal(packet, []byte{3, 4}) {
		t.Fatalf("resume lost PCM: %v, %v", packet, err)
	}

	control.Pause()
	close(stop)
	_, err = r.Read(packet)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("stopping paused reader: %v", err)
	}
}
