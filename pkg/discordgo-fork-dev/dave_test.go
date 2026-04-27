package discordgo

import "testing"

func TestActivatePreparedTransitionActivatesSenderWhenKeyIsPrepared(t *testing.T) {
	d := &DAVESession{
		senderKey:           []byte{1, 2, 3},
		frameCipher:         testAEAD{},
		hasPendingKey:       true,
		exporterSecret:      []byte{9, 9, 9},
		pendingTransitionID: 0,
		pendingVersion:      1,
	}

	if d.CanEncrypt() {
		t.Fatal("expected session to start inactive")
	}
	if err := d.ActivatePreparedTransition(0); err != nil {
		t.Fatalf("ActivatePreparedTransition returned error: %v", err)
	}
	if !d.CanEncrypt() {
		t.Fatal("expected prepared transition activation to enable encryption")
	}
}

type testAEAD struct{}

func (testAEAD) NonceSize() int { return 12 }
func (testAEAD) Overhead() int  { return 16 }
func (testAEAD) Seal(dst, nonce, plaintext, additionalData []byte) []byte {
	return append(dst, plaintext...)
}
func (testAEAD) Open(dst, nonce, ciphertext, additionalData []byte) ([]byte, error) {
	return append(dst, ciphertext...), nil
}
