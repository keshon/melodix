package discordgo

import "testing"

func TestVoiceConnectionDeadChannelReturnsDead(t *testing.T) {
	dead := make(chan struct{})
	vc := &VoiceConnection{Dead: dead}
	if got := vc.DeadChannel(); got != dead {
		t.Fatalf("DeadChannel() returned unexpected channel")
	}
}
