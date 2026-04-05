package discord

import "testing"

// TestOp7GatewayReconnectVerify documents the manual check for gateway Op7 / internal reconnect:
// RunSession must not exit on discordgo.Disconnect; the bot stays up while discordgo reconnects,
// and music continues if voice rejoin succeeds (sink invalidate + retry). Fatal session loss is
// driven by the API probe calling notifyDisconnect.
func TestOp7GatewayReconnectVerify(t *testing.T) {
	t.Helper()
	t.Log("Manual: run cmd/discord, observe Op7 in logs; session should not restart unless API probe fails 3x.")
}
