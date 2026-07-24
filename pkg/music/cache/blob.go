package cache

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"os"

	"github.com/keshon/melodix/pkg/music/opus"
)

// Blob format: a 5-byte header (magic + version) followed by a flat log of
// [uint16 length][packet bytes] records. It is a custom container, not a
// playable media file — so the cache directory is not a casual media library.
var blobMagic = [4]byte{'M', 'X', 'O', 'P'}

const (
	blobVersion = 1
	blobExt     = ".mxo"
	maxPacket   = 0xFFFF // Opus packets are well under this; length is a uint16
)

var errPacketTooLarge = errors.New("cache: opus packet exceeds uint16 length")

// Writer streams packets to a temp file and, on Commit, atomically renames it
// into place and registers it in the store. Abort discards the partial file.
type Writer struct {
	store     *Store
	key       string
	meta      Meta
	tmp       *os.File
	tmpPath   string
	finalPath string
	bw        *bufio.Writer
	packets   int
	done      bool // committed or aborted
}

// Write appends one Opus packet to the blob.
func (w *Writer) Write(pkt []byte) error {
	if len(pkt) > maxPacket {
		return errPacketTooLarge
	}
	var lb [2]byte
	binary.LittleEndian.PutUint16(lb[:], uint16(len(pkt)))
	if _, err := w.bw.Write(lb[:]); err != nil {
		return err
	}
	if _, err := w.bw.Write(pkt); err != nil {
		return err
	}
	w.packets++
	return nil
}

// Commit flushes and atomically publishes the blob, then registers it (and runs
// eviction). A no-op after a prior Commit/Abort.
func (w *Writer) Commit() error {
	if w.done {
		return nil
	}
	w.done = true
	if err := w.bw.Flush(); err != nil {
		w.tmp.Close()
		_ = os.Remove(w.tmpPath)
		return err
	}
	if err := w.tmp.Close(); err != nil {
		_ = os.Remove(w.tmpPath)
		return err
	}
	fi, err := os.Stat(w.tmpPath)
	if err != nil {
		_ = os.Remove(w.tmpPath)
		return err
	}
	if err := os.Rename(w.tmpPath, w.finalPath); err != nil {
		_ = os.Remove(w.tmpPath)
		return err
	}
	w.store.register(w.key, w.finalPath, fi.Size(), w.packets, w.meta)
	return nil
}

// Abort discards the partial blob. A no-op after a prior Commit/Abort.
func (w *Writer) Abort() error {
	if w.done {
		return nil
	}
	w.done = true
	w.tmp.Close()
	return os.Remove(w.tmpPath)
}

// openBlobAt opens a blob and discards the first seekPackets packets, returning
// an opus.Reader positioned at the seek point. A truncated tail reads as io.EOF.
func openBlobAt(path string, seekPackets int) (opus.Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	br := bufio.NewReaderSize(f, 1<<16)
	if err := readHeader(br); err != nil {
		f.Close()
		return nil, err
	}
	r := &blobReader{f: f, br: br}
	for i := 0; i < seekPackets; i++ {
		if _, err := r.next(); err != nil {
			f.Close()
			return nil, err // io.EOF here means the seek ran past the blob end
		}
	}
	return r, nil
}

func newBufWriter(w io.Writer) *bufio.Writer { return bufio.NewWriterSize(w, 1<<16) }

func writeHeader(w io.Writer) error {
	if _, err := w.Write(blobMagic[:]); err != nil {
		return err
	}
	_, err := w.Write([]byte{blobVersion})
	return err
}

func readHeader(r io.Reader) error {
	var h [5]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return err
	}
	if [4]byte(h[:4]) != blobMagic || h[4] != blobVersion {
		return errors.New("cache: bad blob header")
	}
	return nil
}

type blobReader struct {
	f  *os.File
	br *bufio.Reader
}

func (r *blobReader) next() ([]byte, error) {
	var lb [2]byte
	if _, err := io.ReadFull(r.br, lb[:]); err != nil {
		return nil, mapEOF(err)
	}
	pkt := make([]byte, binary.LittleEndian.Uint16(lb[:]))
	if _, err := io.ReadFull(r.br, pkt); err != nil {
		return nil, mapEOF(err) // truncated tail → clean EOF
	}
	return pkt, nil
}

func (r *blobReader) ReadPacket() ([]byte, error) { return r.next() }
func (r *blobReader) Close() error                { return r.f.Close() }

func mapEOF(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return io.EOF
	}
	return err
}
