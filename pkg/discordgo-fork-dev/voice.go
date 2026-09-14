// Discordgo - Discord bindings for Go
// Available at https://github.com/bwmarrin/discordgo

// Copyright 2015-2016 Bruce Marriner <bruce@sqls.net>.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file contains code related to Discord voice suppport

package discordgo

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/disgoorg/godave"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/chacha20poly1305"
)

// ------------------------------------------------------------------------------------------------
// Code related to both VoiceConnection Websocket and UDP connections.
// ------------------------------------------------------------------------------------------------

// VoiceConnectionStatus is status of VoiceConnection
// New -> Connecting <-> Ready
// any -> Dead
type VoiceConnectionStatus int

const (
	// VoiceConnectionStatusInvalid means status not specified, maybe bug?
	VoiceConnectionStatusInvalid VoiceConnectionStatus = iota
	// VoiceConnectionStatusNew means initiating connection
	VoiceConnectionStatusNew
	// VoiceConnectionStatusConnecting means connecting websocket and udp (includes reconnecting)
	VoiceConnectionStatusConnecting
	// VoiceConnectionStatusReady means ready to send/receive audio
	VoiceConnectionStatusReady
	// VoiceConnectionStatusDead means already dead(error or disconnected normally)
	VoiceConnectionStatusDead
)

// A VoiceConnection struct holds all the data and functions related to a Discord Voice Connection.
type VoiceConnection struct {
	Cond *sync.Cond

	// Status of this connection. Read only
	Status VoiceConnectionStatus

	// Closed if this VoiceConection status become Dead
	Dead <-chan struct{}
	dead chan struct{}

	// contains unrecoverable error
	// if not nil, Status should be Dead
	Err error

	LogLevel int
	GuildID  string

	deaf     bool
	mute     bool
	speaking bool

	OpusSend chan []byte  // Chan for sending opus audio, automatically closed after dead, DON'T CLOSE YOURSELF
	OpusRecv chan *Packet // Chan for receiving opus audio, automatically closed after dead, DON'T CLOSE YOURSELF

	wsMu sync.Mutex

	// can be nil, use only for send message
	// mostly this is available connection or nil, but rarely closed connection
	wsConn *websocket.Conn

	// calling this may close websocket and all related connection.
	wsCancel context.CancelFunc

	udpConn *net.UDPConn

	session *Session

	sessionID string

	op2 voiceOP2
	op4 voiceOP4

	cipher cipher.AEAD

	// dave is the DAVE session for this connection, or nil on a channel that
	// never negotiated encryption. What implements it is chosen elsewhere;
	// this file decodes opcodes and hands them over.
	dave godave.Session

	// ssrcToUserID maps the transport's SSRC to the identity a DAVE session
	// keys its receive ratchets by. Fed by opcodes 5 and 12.
	ssrcToUserID map[uint32]string

	// daveMembers is who the gateway has said is in the channel. A DAVE
	// session will not commit an add proposal for somebody it has not been
	// told about, and opcode 11 arrives before opcode 4 builds the session,
	// so the names are kept here and replayed into each new one.
	daveMembers map[string]struct{}

	voiceSpeakingUpdateHandlers []VoiceSpeakingUpdateHandler

	seqAck int // for heartbeat and resume
}

// DeadChannel exposes the close notification channel for callers that need a
// stable exported liveness signal without reflecting on VoiceConnection internals.
func (v *VoiceConnection) DeadChannel() <-chan struct{} {
	if v == nil {
		return nil
	}
	return v.Dead
}

// VoiceSpeakingUpdateHandler type provides a function definition for the
// VoiceSpeakingUpdate event
type VoiceSpeakingUpdateHandler func(vc *VoiceConnection, vs *VoiceSpeakingUpdate)

// Speaking sends a speaking notification to Discord over the voice websocket.
// This must be sent as true prior to sending audio and should be set to false
// once finished sending audio.
// b : Send true if speaking, false if not.
func (v *VoiceConnection) Speaking(b bool) (err error) {

	v.log(LogDebug, "called (%t)", b)

	type voiceSpeakingData struct {
		Speaking bool `json:"speaking"`
		Delay    int  `json:"delay"`
	}

	type voiceSpeakingOp struct {
		Op   int               `json:"op"` // Always 5
		Data voiceSpeakingData `json:"d"`
	}

	v.Cond.L.Lock()
	defer v.Cond.L.Unlock()
	if v.wsConn == nil {
		return fmt.Errorf("no VoiceConnection websocket")
	}
	data := voiceSpeakingOp{5, voiceSpeakingData{b, 0}}
	v.wsMu.Lock()
	err = v.wsConn.WriteJSON(data)
	v.wsMu.Unlock()

	v.Cond.Broadcast()
	if err != nil {
		v.speaking = false
		v.log(LogError, "Speaking() write json error, %s", err)
		return
	}

	v.speaking = b

	return
}

func (v *VoiceConnection) WaitForDAVEReady(ctx context.Context) error {
	v.Cond.L.Lock()
	dave := v.dave
	v.Cond.L.Unlock()

	if dave == nil {
		return nil
	}

	// Polled rather than waited on v.Cond, because Cond.Wait can only be
	// called holding v.Cond.L and asking the session anything while holding it
	// is what deadlocks us. See the lock order note on daveSession. The
	// interval is short next to how long a handshake takes, and this runs once
	// per track rather than once per frame.
	ticker := time.NewTicker(daveReadyPollInterval)
	defer ticker.Stop()
	for {
		if dave.Ready() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// daveReadyPollInterval is how often WaitForDAVEReady asks whether encryption
// has come up.
const daveReadyPollInterval = 20 * time.Millisecond

// Disconnect requests disconnect from this voice channel and wait for disconencted
func (v *VoiceConnection) Disconnect(ctx context.Context) error {

	v.log(LogInformational, "called")

	err := v.session.VoiceStateUpdate(v.GuildID, "", true, true)
	if err != nil {
		return err
	}

	return v.waitUntilStatus(ctx, VoiceConnectionStatusDead)
}

// Kill stop all goroutines related to this voice conection, remove self from Session, and set status to dead.
// NOTE: unlock before calling this
func (v *VoiceConnection) Kill() {

	v.log(LogInformational, "called")

	v.session.Lock()
	if v.session.VoiceConnections[v.GuildID] == v {
		delete(v.session.VoiceConnections, v.GuildID)
	}
	v.session.Unlock()
	v.Cond.L.Lock()
	defer v.Cond.L.Unlock()
	if v.wsCancel != nil {
		v.wsCancel()
	}
	if v.Status != VoiceConnectionStatusDead {
		v.Status = VoiceConnectionStatusDead
		v.Cond.Broadcast()
		close(v.dead)
		go func() {
			time.Sleep(100 * time.Millisecond) // safe
			if v.OpusRecv != nil {
				close(v.OpusRecv)
			}
			if v.OpusSend != nil {
				close(v.OpusSend)
			}
		}()
	}

	v.log(LogInformational, "done")
}

// AddHandler adds a Handler for VoiceSpeakingUpdate events.
func (v *VoiceConnection) AddHandler(h VoiceSpeakingUpdateHandler) {
	v.Cond.L.Lock()
	defer v.Cond.L.Unlock()

	v.voiceSpeakingUpdateHandlers = append(v.voiceSpeakingUpdateHandlers, h)
	v.Cond.Broadcast()
}

// VoiceSpeakingUpdate is a struct for a VoiceSpeakingUpdate event.
type VoiceSpeakingUpdate struct {
	UserID   string `json:"user_id"`
	SSRC     int    `json:"ssrc"`
	Speaking int    `json:"speaking"`
}

// ------------------------------------------------------------------------------------------------
// Unexported Internal Functions Below.
// ------------------------------------------------------------------------------------------------

// unrecoverable error handling
// VoiceConnection should be unlocked before calling this
func (v *VoiceConnection) failure(err error) {
	v.log(LogError, "voice unrecoverable error, %v", err.Error())
	v.log(LogDebug, "voice struct: %#v\n", v)
	v.Cond.L.Lock()
	if v.Err == nil {
		v.Status = VoiceConnectionStatusDead
		v.Err = err
		v.Cond.Broadcast()
	}
	v.Cond.L.Unlock()
	// cleanup
	v.Kill()
	v.Disconnect(context.Background())
}

// voiceWebsocketMessage is basic message struct of voice websocket
type voiceWebsocketMessage struct {
	Operation int             `json:"op"`
	RawData   json.RawMessage `json:"d"`
	Sequence  *int            `json:"seq"`
}

// A voiceOP2 stores the data for the voice operation 2 websocket event
// which is sort of like the voice READY packet
type voiceOP2 struct {
	SSRC  uint32   `json:"ssrc"`
	Port  int      `json:"port"`
	Modes []string `json:"modes"`
	IP    string   `json:"ip"`
}

// A voiceOP4 stores the data for the voice operation 4 websocket event
// which provides us with the NaCl SecretBox encryption key
type voiceOP4 struct {
	SecretKey           []byte `json:"secret_key"`
	Mode                string `json:"mode"`
	DAVEProtocolVersion int    `json:"dave_protocol_version"`
}

// A voiceOP8 stores the data for the voice operation 8 websocket event HELLO
type voiceOP8 struct {
	HeartbeatInterval int `json:"heartbeat_interval"`
}

// waitUntilStatus waits for connection to be in given VoiceConnectionStatus
// returns error if context timeout or VoiceConnection.Err
func (v *VoiceConnection) waitUntilStatus(ctx context.Context, status VoiceConnectionStatus) error {
	v.log(LogInformational, "called")

	ch := make(chan error)

	go func() {
		defer close(ch)
		v.Cond.L.Lock()
		defer v.Cond.L.Unlock()
		for v.Status != status && v.Status != VoiceConnectionStatusDead {
			select {
			case <-ctx.Done():
				return
			default:
			}
			v.Cond.Wait()
		}
		ch <- v.Err
	}()

	select {
	case err := <-ch:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}

}

// onVoiceServerUpdate handles a VOICE_SERVER_UPDATE event of main gateway.
// wait for VOICE_SERVER_UPDATE and open voice websocket connection.
func (v *VoiceConnection) onVoiceServerUpdate(ev *VoiceServerUpdate) (err error) {

	v.log(LogInformational, "called")

	v.Cond.L.Lock()
	defer v.Cond.L.Unlock()

	// Close a websocket if one is already open
	if v.wsCancel != nil {
		v.wsCancel()
	}

	// If no endpoint, just wait for next event
	if ev.Endpoint == nil {
		return
	}

	go v.websocket(context.TODO(), *ev.Endpoint, ev.Token)

	return
}

// ErrVoiceNoSessionID means timed out to receive voice Session ID
var ErrVoiceNoSessionID = errors.New("did not receive voice Session ID in time")

// ErrVoiceReconnectionLimit means reached a hard limit to reconnect
var ErrVoiceReconnectionLimit = errors.New("reconnection limit reached")

// ErrVoiceUnknownEncryptionMode means Discord requested encryption mode which is not supported
var ErrVoiceUnknownEncryptionMode = errors.New("unknown encryption mode")

// ErrVoiceE2EERequired reports voice close code 4017: the channel requires a
// client that speaks the DAVE protocol, and this connection did not negotiate
// it. Discord enforces this on every non-stage voice channel, so there is
// nothing to retry and no version to fall back to.
var ErrVoiceE2EERequired = errors.New("voice channel requires end-to-end encryption (DAVE)")

// daveProtocolVersion is the end-to-end encryption version advertised when
// identifying to a voice server.
//
// There is no fallback to choose here. Discord enforces DAVE on every non-stage
// voice channel and answers version 0 with close code 4017 — and 0 is also what
// this field marshals to when a struct literal omits it. Such a literal still
// compiles and the socket still opens, so the mistake stays invisible until a
// real voice channel refuses to admit the bot, which is why the payload is
// built in one place with a test over its JSON.
const daveProtocolVersion = 1

// voiceHandshakeData is the voice identify payload (op 0).
type voiceHandshakeData struct {
	ServerID               string `json:"server_id"`
	UserID                 string `json:"user_id"`
	SessionID              string `json:"session_id"`
	Token                  string `json:"token"`
	MaxDAVEProtocolVersion int    `json:"max_dave_protocol_version"`
}

type voiceHandshakeOp struct {
	Op   int                `json:"op"` // Always 0
	Data voiceHandshakeData `json:"d"`
}

// newVoiceHandshake builds the identify sent when a voice websocket opens.
func newVoiceHandshake(guildID, userID, sessionID, token string) voiceHandshakeOp {
	return voiceHandshakeOp{Op: 0, Data: voiceHandshakeData{
		ServerID:               guildID,
		UserID:                 userID,
		SessionID:              sessionID,
		Token:                  token,
		MaxDAVEProtocolVersion: daveProtocolVersion,
	}}
}

// websocket open the voice websocket, handle reconnect, and listens on it for messages and passes them to the voice event handler.
// This is automatically called by the Open func.
func (v *VoiceConnection) websocket(ctx context.Context, endpoint string, token string) {

	v.log(LogInformational, "called")

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	v.Cond.L.Lock()
	// Close a websocket if one is already open
	if v.wsCancel != nil {
		v.wsCancel()
	}
	v.wsCancel = cancel
	v.Cond.L.Unlock()

	sessionIDDone := make(chan struct{})
	go func() {
		v.Cond.L.Lock()
		defer v.Cond.L.Unlock()
		for v.sessionID == "" {
			v.Cond.Wait()
		}
		close(sessionIDDone)
	}()
	timeout := time.NewTimer(1 * time.Second)

	select {
	case <-sessionIDDone:
	case <-timeout.C:
		v.failure(ErrVoiceNoSessionID)
		return
	}

	// avoid resource leak before Go 1.23
	if !timeout.Stop() {
		<-timeout.C
	}

	v.Cond.L.Lock()
	v.seqAck = -1
	v.Cond.L.Unlock()

	for i := 0; i < 100; i++ {
		select {
		case <-ctx.Done():
			return
		default:
		}

		ctx, cancel := context.WithCancel(ctx)
		defer cancel() // this cancel() is not needed actually, but do it to suppress lint warning

		v.Cond.L.Lock()
		v.Status = VoiceConnectionStatusConnecting
		v.Cond.Broadcast()
		v.Cond.L.Unlock()

		vg := "wss://" + endpoint + "?v=8"
		v.log(LogInformational, "connecting to voice endpoint %s", vg)
		wsConn, _, err := v.session.Dialer.Dial(vg, nil)
		if err != nil {
			err = fmt.Errorf("error connecting to voice endpoint %s, %w", vg, err)
			v.failure(err)
			return
		}
		go func() {
			<-ctx.Done()
			// don't do graceful closing of websocket because it's not needed
			err := wsConn.Close()
			v.log(LogDebug, "closed voice websocket due to context done: %v", err)
		}()

		v.Cond.L.Lock()
		v.wsConn = wsConn
		v.Cond.L.Unlock()

		if i == 0 {
			data := newVoiceHandshake(v.GuildID, v.session.State.User.ID, v.sessionID, token)

			v.wsMu.Lock()
			err = wsConn.WriteJSON(data)
			v.wsMu.Unlock()
			if err != nil {
				err = fmt.Errorf("error sending identify packet, %w", err)
				v.failure(err)
				return
			}
		} else {
			type voiceResumeData struct {
				ServerID  string `json:"server_id"`
				SessionID string `json:"session_id"`
				Token     string `json:"token"`
				SeqAck    int    `json:"seq_ack"`
			}
			type voiceResumeOp struct {
				Op   int             `json:"op"` // Always 7
				Data voiceResumeData `json:"d"`
			}

			v.Cond.L.Lock()
			data := voiceResumeOp{7, voiceResumeData{
				ServerID:  v.GuildID,
				SessionID: v.sessionID,
				Token:     token,
				SeqAck:    v.seqAck,
			}}
			v.Cond.L.Unlock()

			// Reset DAVE on reconnection, in place. A resume does not repeat
			// opcode 4, so the session has to survive the reconnect: dropping it
			// would leave the send path with no session at all, which reads as
			// "this channel is not encrypted" and is how plaintext used to get
			// out. Sessions that cannot be reset this way keep whatever state
			// they have, which is the safe direction.
			if dave := v.daveSession(); dave != nil {
				if resettable, ok := dave.(daveResetter); ok {
					resettable.Reset()
				}
			}

			v.log(LogInformational, "resuming voice websocket")
			v.log(LogDebug, "resume packet, %#v", data)

			v.wsMu.Lock()
			err = wsConn.WriteJSON(data)
			v.wsMu.Unlock()
			if err != nil {
				err = fmt.Errorf("error sending resume packet, %w", err)
				v.failure(err)
				return
			}

			// reopen UDP connection because WebSocket broken likely meaning UDP broken too.
			err = v.udpOpen(ctx)
			if err != nil {
				err = fmt.Errorf("failed to resume UDP connection, %w", err)
				v.failure(err)
				return
			}
		}

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			messageType, message, err := wsConn.ReadMessage()
			if err != nil {
				// Abandon the voice WS connection
				v.Cond.L.Lock()
				if v.wsConn == wsConn {
					v.wsConn = nil
				}
				v.Cond.Broadcast()
				v.Cond.L.Unlock()

				select {
				case <-ctx.Done():
					return
				default:
				}

				// 4017 is the channel demanding end-to-end encryption we did not
				// negotiate. It fails the connection rather than returning
				// quietly like its neighbours below: leaving the status alone
				// means the join sits until its own deadline and reports a
				// timeout, which says nothing about a cause that cannot be
				// retried or worked around.
				if websocket.IsCloseError(err, 4017) {
					v.failure(ErrVoiceE2EERequired)
					return
				}

				// 4014 is someone else ending it (kicked, channel deleted, or
				// the main gateway session dropped), 4021 is rate limiting and
				// 4022 is the call being terminated. None are worth
				// reconnecting into, but they are three different stories, and
				// the one line a user can paste read "disconnected" for all
				// three — which is why issue #11's logs cannot say whether that
				// bot was removed by a person or dropped by the server.
				if websocket.IsCloseError(err, 4014, 4021, 4022) {
					v.log(LogInformational, "received close code %s", voiceCloseReason(err))

					return
				}

				// 4015 indicates that voice server crashed so we should reconnect.
				// 1006 (CloseAbnormalClosure) indicates a network-level disruption
				// (unexpected EOF, TCP reset, etc.) — also recoverable via reconnect.
				// Other code is our bad, should never happen, we stop reconnecting to avoid loop.
				if websocket.IsUnexpectedCloseError(err, 4015, websocket.CloseAbnormalClosure) {
					err := fmt.Errorf("voice websocket closed, %w", err)
					v.failure(err)
					return
				}

				v.log(LogInformational, "voice socket disconnected, reconnecting, %v", err)

				// close goroutine related to websocket
				cancel()

				// reconnect
				break
			}

			// Pass received message to voice event handler
			v.onEvent(ctx, messageType == websocket.BinaryMessage, message)
		}
	}

	v.failure(ErrVoiceReconnectionLimit)
}

// voiceCloseReason renders a websocket close error as "<code> (<meaning>)" so
// that a pasted log line can be read without the voice gateway's close-code
// table open. Only the codes this file acts on are named; anything else keeps
// its number, which is still more than the caller had before.
func voiceCloseReason(err error) string {
	var ce *websocket.CloseError
	if !errors.As(err, &ce) {
		return err.Error()
	}

	meaning := "unknown"
	switch ce.Code {
	case 4014:
		meaning = "kicked, channel deleted, or gateway session dropped"
	case 4021:
		meaning = "rate limited"
	case 4022:
		meaning = "call terminated"
	}
	return fmt.Sprintf("%d (%s)", ce.Code, meaning)
}

// wsEvent handles any voice websocket events. This is only called by the
// wsListen() function.
func (v *VoiceConnection) onEvent(ctx context.Context, binary bool, message []byte) {

	if binary {
		v.log(LogDebug, "received binary: %x", message)
	} else {
		v.log(LogDebug, "received string: %s", string(message))
	}

	if binary {
		v.handleDAVEBinary(message)
		return
	} else {

		var e voiceWebsocketMessage
		if err := json.Unmarshal(message, &e); err != nil {
			v.log(LogError, "unmarshall error, %s", err)
			return
		}

		if e.Sequence != nil {

			v.Cond.L.Lock()
			v.seqAck = *e.Sequence
			v.Cond.L.Unlock()
		}

		v.log(LogDebug, "voice operation %d", e.Operation)

		switch e.Operation {

		case 2: // READY
			op2 := voiceOP2{}

			if err := json.Unmarshal(e.RawData, &op2); err != nil {
				err := fmt.Errorf("OP2 unmarshal error, %w, %s", err, string(e.RawData))
				v.failure(err)
				return
			}

			v.Cond.L.Lock()
			v.op2 = op2
			dave := v.dave
			v.Cond.Broadcast()
			v.Cond.L.Unlock()

			// A reconnect re-announces the SSRC without necessarily rebuilding
			// the DAVE session, and a session holding the previous SSRC cannot
			// encrypt for this one. Outside the lock, always.
			v.assignDAVECodec(dave, op2.SSRC)

			// Start the UDP connection
			err := v.udpOpen(ctx)
			if err != nil {
				err := fmt.Errorf("error opening udp connection, %w", err)
				v.failure(err)
				return
			}

			return

		case 4: // udp encryption secret key
			op4 := voiceOP4{}
			if err := json.Unmarshal(e.RawData, &op4); err != nil {
				err := fmt.Errorf("OP4 unmarshal error, %w, %s", err, string(e.RawData))
				v.failure(err)
				return
			}

			v.Cond.L.Lock()
			v.op4 = op4
			switch op4.Mode {
			case "aead_aes256_gcm_rtpsize":
				block, err := aes.NewCipher(op4.SecretKey)
				if err != nil {
					v.Cond.L.Unlock()
					v.failure(err)
					return
				}
				v.cipher, err = cipher.NewGCM(block)
				if err != nil {
					v.Cond.L.Unlock()
					v.failure(err)
					return
				}
			case "aead_xchacha20_poly1305_rtpsize":
				var err error
				v.cipher, err = chacha20poly1305.NewX(op4.SecretKey)
				if err != nil {
					v.Cond.L.Unlock()
					v.failure(err)
					return
				}
			default:
				err := fmt.Errorf("%w: %s", ErrVoiceUnknownEncryptionMode, op4.Mode)
				v.Cond.L.Unlock()
				v.failure(err)
				return
			}

			var (
				daveSession godave.Session
				daveSSRC    uint32
				daveRoster  []string
			)
			v.log(LogInformational, "DAVE protocol version %d", op4.DAVEProtocolVersion)
			if op4.DAVEProtocolVersion > 0 {
				// Built here because v.dave has to be published under the lock;
				// told anything only after it is released.
				session := v.newDAVESession()
				v.dave = session
				daveSession = session
				daveSSRC = v.op2.SSRC
				daveRoster = make([]string, 0, len(v.daveMembers))
				for userID := range v.daveMembers {
					daveRoster = append(daveRoster, userID)
				}
			}

			if v.OpusSend == nil {
				v.OpusSend = make(chan []byte, 16)
			}
			go v.opusSender(ctx, 48000, 960)

			if !v.deaf {
				if v.OpusRecv == nil {
					v.OpusRecv = make(chan *Packet, 2)
				}

				go v.opusReceiver(ctx)
			}

			v.Status = VoiceConnectionStatusReady

			v.Cond.Broadcast()
			v.Cond.L.Unlock()

			// Outside the lock: the session answers this by sending a key
			// package, and sending takes the same lock.
			if daveSession != nil {
				v.assignDAVECodec(daveSession, daveSSRC)
				v.addKnownDAVEMembers(daveSession, daveRoster)
				daveSession.OnSelectProtocolAck(uint16(op4.DAVEProtocolVersion))
			}
			return

		case 5:
			voiceSpeakingUpdate := &VoiceSpeakingUpdate{}
			if err := json.Unmarshal(e.RawData, voiceSpeakingUpdate); err != nil {
				v.log(LogError, "OP5 unmarshall error, %s, %s", err, string(e.RawData))
				return
			}

			v.Cond.L.Lock()
			if v.ssrcToUserID == nil {
				v.ssrcToUserID = make(map[uint32]string)
			}
			v.ssrcToUserID[uint32(voiceSpeakingUpdate.SSRC)] = voiceSpeakingUpdate.UserID
			v.Cond.L.Unlock()

			for _, h := range v.voiceSpeakingUpdateHandlers {
				h(v, voiceSpeakingUpdate)
			}

		case 6: // HEARTBEAT response
			// add code to use this to track latency?
			v.log(LogDebug, "recieved heartbeat ACK")
			return

		case 12: // CLIENT CONNECT
			var op12 struct {
				UserID    string `json:"user_id"`
				AudioSSRC uint32 `json:"audio_ssrc"`
			}
			if err := json.Unmarshal(e.RawData, &op12); err != nil {
				v.log(LogError, "OP12 unmarshal error, %s, %s", err, string(e.RawData))
				return
			}
			if op12.AudioSSRC != 0 {
				v.Cond.L.Lock()
				if v.ssrcToUserID == nil {
					v.ssrcToUserID = make(map[uint32]string)
				}
				v.ssrcToUserID[op12.AudioSSRC] = op12.UserID
				v.Cond.L.Unlock()
			}
			return

		case 8: // HELLO

			op8 := voiceOP8{}

			if err := json.Unmarshal(e.RawData, &op8); err != nil {
				v.log(LogError, "OP6 unmarshall error, %s, %s", err, string(e.RawData))
				return
			}
			// Start the voice websocket heartbeat to keep the connection alive
			go v.wsHeartbeat(ctx, v.wsConn, op8.HeartbeatInterval)

		case 9: // resumed
			v.log(LogInformational, "resumed voice websocket")
			return

		case 11: // CLIENTS CONNECT
			// Discord sends this one, plural, and not the singular opcode 12
			// below. Measured on a live channel: 11 arrived with the listener's
			// ID and 12 never arrived at all, which is why a DAVE session was
			// being asked to commit an add proposal for a user it had never
			// heard of, and declined.
			var op11 struct {
				UserIDs []string `json:"user_ids"`
			}
			if err := json.Unmarshal(e.RawData, &op11); err != nil {
				v.log(LogError, "OP11 unmarshal error, %s, %s", err, string(e.RawData))
				return
			}

			v.Cond.L.Lock()
			if v.daveMembers == nil {
				v.daveMembers = make(map[string]struct{}, len(op11.UserIDs))
			}
			for _, userID := range op11.UserIDs {
				v.daveMembers[userID] = struct{}{}
			}
			dave := v.dave
			v.Cond.L.Unlock()

			v.log(LogDebug, "voice clients connect: %v", op11.UserIDs)
			if dave != nil {
				for _, userID := range op11.UserIDs {
					dave.AddUser(godave.UserID(userID))
				}
			}
			return

		case 13: // Client Disconnect
			var op13 struct {
				UserID string `json:"user_id"`
			}
			if err := json.Unmarshal(e.RawData, &op13); err == nil && op13.UserID != "" {
				v.Cond.L.Lock()
				delete(v.daveMembers, op13.UserID)
				dave := v.dave
				v.Cond.L.Unlock()
				if dave != nil {
					dave.RemoveUser(godave.UserID(op13.UserID))
				}
			}
			v.log(LogDebug, "user disconnected: %s", string(e.RawData))
			return

		case 21: // DAVE prepare_transition
			v.handleDAVEPrepareTransition(e.RawData)
			return

		case 22: // DAVE execute_transition
			v.handleDAVEExecuteTransition(e.RawData)
			return

		case 24: // DAVE prepare_epoch
			v.handleDAVEPrepareEpoch(ctx, e.RawData)
			return

		default:
			v.log(LogDebug, "unknown voice operation, %d, %s", e.Operation, string(e.RawData))
		}
	}

}

type voiceHeartbeatOp struct {
	Op   int                `json:"op"` // Always 3
	Data voiceHeartbeatData `json:"d"`
}

type voiceHeartbeatData struct {
	T      int64 `json:"t"`
	SeqAck int   `json:"seq_ack"`
}

// wsHeartbeat sends regular heartbeats to voice Discord so it knows the client
// is still connected.  If you do not send these heartbeats Discord will
// disconnect the websocket connection after a few seconds.
func (v *VoiceConnection) wsHeartbeat(ctx context.Context, wsConn *websocket.Conn, interval int) {

	if wsConn == nil {
		return
	}

	var err error
	ticker := time.NewTicker(time.Duration(interval) * time.Millisecond)
	defer ticker.Stop()
	for {
		v.log(LogDebug, "sending heartbeat packet")
		v.Cond.L.Lock()
		seqAck := v.seqAck
		v.Cond.L.Unlock()
		v.wsMu.Lock()
		err = wsConn.WriteJSON(voiceHeartbeatOp{3, voiceHeartbeatData{time.Now().Unix(), seqAck}})
		v.wsMu.Unlock()
		if err != nil {
			v.log(LogError, "error sending heartbeat to voice endpoint, %s", err)
			return
		}

		select {
		case <-ticker.C:
			// continue loop and send heartbeat
		case <-ctx.Done():
			return
		}
	}
}

// ------------------------------------------------------------------------------------------------
// Code related to the VoiceConnection UDP connection
// ------------------------------------------------------------------------------------------------

type voiceUDPData struct {
	Address string `json:"address"` // Public IP of machine running this code
	Port    uint16 `json:"port"`    // UDP Port of machine running this code
	Mode    string `json:"mode"`    // Encryption mode
}

type voiceUDPD struct {
	Protocol string       `json:"protocol"` // Always "udp" ?
	Data     voiceUDPData `json:"data"`
}

type voiceUDPOp struct {
	Op   int       `json:"op"` // Always 1
	Data voiceUDPD `json:"d"`
}

// udpOpen opens a UDP connection to the voice server and completes the
// initial required handshake.  This connection is left open in the session
// and can be used to send or receive audio.  This should only be called
// from voice.wsEvent OP2
func (v *VoiceConnection) udpOpen(ctx context.Context) (err error) {

	v.Cond.L.Lock()

	host := v.op2.IP + ":" + strconv.Itoa(v.op2.Port)
	addr, err := net.ResolveUDPAddr("udp", host)
	if err != nil {
		v.log(LogWarning, "error resolving udp host %s, %s", host, err)
		return
	}

	v.log(LogInformational, "connecting to udp addr %s", addr.String())
	udpConn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		v.log(LogWarning, "error connecting to udp addr %s, %s", addr.String(), err)
		return
	}

	v.udpConn = udpConn

	v.Cond.Broadcast()
	v.Cond.L.Unlock()

	// close if context done
	go func() {
		<-ctx.Done()
		err := udpConn.Close()
		v.log(LogDebug, "closed voice UDP due to context done, %v", err)
	}()

	// Create a 74 byte array to store the packet data
	sb := make([]byte, 74)
	binary.BigEndian.PutUint16(sb, 1)              // Packet type (0x1 is request, 0x2 is response)
	binary.BigEndian.PutUint16(sb[2:], 70)         // Packet length (excluding type and length fields)
	binary.BigEndian.PutUint32(sb[4:], v.op2.SSRC) // The SSRC code from the Op 2 VoiceConnection event

	// And send that data over the UDP connection to Discord.
	_, err = v.udpConn.Write(sb)
	if err != nil {
		v.log(LogWarning, "udp write error to %s, %s", addr.String(), err)
		return
	}

	// Create a 74 byte array and listen for the initial handshake response
	// from Discord.  Once we get it parse the IP and PORT information out
	// of the response.  This should be our public IP and PORT as Discord
	// saw us.
	rb := make([]byte, 74)
	rlen, _, err := v.udpConn.ReadFromUDP(rb)
	if err != nil {
		v.log(LogWarning, "udp read error, %s, %s", addr.String(), err)
		return
	}

	if rlen < 74 {
		v.log(LogWarning, "received udp packet too small")
		return fmt.Errorf("received udp packet too small")
	}

	// Loop over position 8 through 71 to grab the IP address.
	var ip string
	for i := 8; i < len(rb)-2; i++ {
		if rb[i] == 0 {
			ip = string(rb[8:i])
		}
	}

	// Grab port from position 72 and 73
	port := binary.BigEndian.Uint16(rb[len(rb)-2:])

	encryptionMode := ""
encryptionModeLoop:
	for _, mode := range v.op2.Modes {
		switch mode {
		case "aead_aes256_gcm_rtpsize":
			encryptionMode = mode
			break encryptionModeLoop // prefer
		case "aead_xchacha20_poly1305_rtpsize":
			encryptionMode = mode
		}
	}

	// Take the data from above and send it back to Discord to finalize
	// the UDP connection handshake.
	data := voiceUDPOp{1, voiceUDPD{"udp", voiceUDPData{ip, port, encryptionMode}}}

	v.Cond.L.Lock()
	wsConn := v.wsConn
	v.Cond.L.Unlock()
	if wsConn == nil {
		return
	}
	v.wsMu.Lock()
	err = wsConn.WriteJSON(data)
	v.wsMu.Unlock()
	if err != nil {
		v.log(LogWarning, "udpop write error, %#v, %s", data, err)
		return
	}

	// start udpKeepAlive
	go v.udpKeepAlive(ctx, v.udpConn, 5*time.Second)
	// TODO: find a way to check that it fired off okay

	return
}

// udpKeepAlive sends a udp packet to keep the udp connection open
// This is still a bit of a "proof of concept"
func (v *VoiceConnection) udpKeepAlive(ctx context.Context, udpConn *net.UDPConn, i time.Duration) {
	var err error
	var sequence uint64

	packet := make([]byte, 8)

	ticker := time.NewTicker(i)
	defer ticker.Stop()
	for {

		binary.LittleEndian.PutUint64(packet, sequence)
		sequence++

		_, err = udpConn.Write(packet)
		if err != nil {
			v.log(LogError, "write error, %s", err)
			return
		}

		select {
		case <-ticker.C:
			// continue loop and send keepalive
		case <-ctx.Done():
			return
		}
	}
}

// opusSender will listen on the given channel and send any
// pre-encoded opus audio to Discord.  Supposedly.
func (v *VoiceConnection) opusSender(ctx context.Context, rate, size int) {

	v.log(LogInformational, "called")

	v.Cond.L.Lock()
	udpConn := v.udpConn
	opusCap := 0
	if v.OpusSend != nil {
		opusCap = cap(v.OpusSend)
	}
	v.Cond.L.Unlock()
	v.log(LogInformational, "opus sender starting udp_ready=%v opus_cap=%d", udpConn != nil, opusCap)

	var sequence uint16
	var timestamp uint32
	var recvbuf []byte
	var ok bool
	udpHeader := make([]byte, 12)

	var nonce = make([]byte, v.cipher.NonceSize())

	// build the parts that don't change in the udpHeader
	udpHeader[0] = 0x80
	udpHeader[1] = 0x78
	binary.BigEndian.PutUint32(udpHeader[8:], v.op2.SSRC)

	// start a send loop that loops until buf chan is closed
	ticker := time.NewTicker(time.Millisecond * time.Duration(size/(rate/1000)))
	defer ticker.Stop()
	for i := uint32(0); true; i++ {

		// Get data from chan.  If chan is closed, return.
		select {
		case <-ctx.Done():
			return
		case recvbuf, ok = <-v.OpusSend:
			if !ok {
				return
			}
			// else, continue loop
		}

		v.Cond.L.Lock()
		dave := v.dave
		daveSSRC := v.op2.SSRC
		speaking := v.speaking
		opusQueued := 0
		if v.OpusSend != nil {
			opusQueued = len(v.OpusSend)
		}
		v.Cond.L.Unlock()

		// Asked after the unlock, never before it. See the lock order note on
		// daveSession.
		daveActive := dave != nil && dave.Ready()
		sampleLog := i < 5 || i%50 == 0
		if sampleLog {
			v.log(LogDebug, "opus sender dequeued idx=%d opus_len=%d queued=%d seq=%d timestamp=%d dave_active=%v speaking=%v", i, len(recvbuf), opusQueued, sequence, timestamp, daveActive, speaking)
		}

		// Encryption is expected here and we have none, so this frame does not
		// go out. See holdFrames. The wait on the ticker still happens, so the
		// hold keeps 20ms time rather than draining OpusSend as fast as the
		// player can fill it; the counters still advance, so resuming looks
		// like the packet loss it resembles instead of a jump in time. We stay
		// silent to the server too — announcing speaking while sending nothing
		// is a claim we are not backing.
		if holdFrames(dave) {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			if sampleLog {
				v.log(LogWarning, "opus frame held, no DAVE epoch idx=%d seq=%d timestamp=%d opus_len=%d", i, sequence, timestamp, len(recvbuf))
			}
			sequence++
			timestamp += uint32(size)
			continue
		}

		if !speaking {
			err := v.Speaking(true)
			if err != nil {
				v.log(LogError, "error sending speaking packet idx=%d seq=%d timestamp=%d, %s", i, sequence, timestamp, err)
			}
		}

		// Add sequence and timestamp to udpPacket
		binary.BigEndian.PutUint16(udpHeader[2:], sequence)
		binary.BigEndian.PutUint32(udpHeader[4:], timestamp)

		if daveActive {
			encryptBuf := make([]byte, dave.MaxEncryptedFrameSize(len(recvbuf)))
			n, err := dave.Encrypt(daveSSRC, recvbuf, encryptBuf)
			if err != nil {
				// The channel is encrypted and this frame is not, so it does not
				// go out. This is the same leak holdFrames exists to prevent,
				// reached through a door holdFrames cannot see: the session
				// reports Ready, and encrypting fails anyway. Falling through
				// used to send the plaintext Opus, which the transport cipher
				// then wrapped and the server happily forwarded to receivers
				// that discarded it -- a leak and silence at the same time.
				//
				// Held on the ticker like every other skipped frame, so the
				// stream keeps 20ms time. Sampled, because a session that
				// refuses one frame refuses all of them, fifty times a second.
				if sampleLog {
					v.log(LogError, "DAVE encrypt failed, frame held idx=%d seq=%d timestamp=%d opus_len=%d: %s", i, sequence, timestamp, len(recvbuf), err)
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
				sequence++
				timestamp += uint32(size)
				continue
			}
			recvbuf = encryptBuf[:n]
			if sampleLog {
				v.log(LogDebug, "opus sender encrypted idx=%d encrypted_len=%d seq=%d timestamp=%d", i, len(recvbuf), sequence, timestamp)
			}
		}

		binary.LittleEndian.PutUint32(nonce, i)
		sendbuf := make([]byte, len(udpHeader), len(udpHeader)+len(nonce)+len(recvbuf)+v.cipher.Overhead())
		copy(sendbuf, udpHeader)
		v.Cond.L.Lock()
		sendbuf = v.cipher.Seal(sendbuf, nonce, recvbuf, udpHeader)
		v.Cond.L.Unlock()
		sendbuf = append(sendbuf, nonce[:4]...)

		// block here until we're exactly at the right time :)
		// Then send rtp audio packet to Discord over UDP
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// continue
		}
		_, err := udpConn.Write(sendbuf)

		if err != nil {
			v.log(LogError, "udp write failed idx=%d seq=%d timestamp=%d opus_len=%d udp_len=%d dave_active=%v: %s", i, sequence, timestamp, len(recvbuf), len(sendbuf), daveActive, err)
			err := fmt.Errorf("udp write error, %w", err)
			v.failure(err)
			return
		}
		if sampleLog {
			v.log(LogDebug, "udp write ok idx=%d seq=%d timestamp=%d opus_len=%d udp_len=%d dave_active=%v", i, sequence, timestamp, len(recvbuf), len(sendbuf), daveActive)
		}

		// don't care if it overflows because it is already defined in Go spec
		// https://go.dev/ref/spec#Integer_overflow
		sequence++
		timestamp += uint32(size)
	}
}

// A Packet contains the headers and content of a received voice packet.
type Packet struct {
	Flags       byte // first byte of RTP header
	PayloadType byte // second byte of RTP header
	Sequence    uint16
	Timestamp   uint32
	SSRC        uint32
	CSRC        []uint32
	Extension   []byte // RTP header extension with extension header, can be nil
	Opus        []byte
}

// opusReceiver listens on the UDP socket for incoming packets
// and sends them across the given channel
// NOTE :: This function may change names later.
func (v *VoiceConnection) opusReceiver(ctx context.Context) {

	v.log(LogInformational, "called")

	v.Cond.L.Lock()
	udpConn := v.udpConn
	ch := v.OpusRecv
	v.Cond.L.Unlock()

	recvbuf := make([]byte, 1024)
	var nonce = make([]byte, v.cipher.NonceSize())

	for {
		rlen, err := udpConn.Read(recvbuf)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				err := fmt.Errorf("udp read error, %w", err)
				v.failure(err)
				return
			}
		}

		select {
		case <-ctx.Done():
			return
		default:
			// continue loop
		}

		// For now, skip anything except audio.
		if rlen < 12 || (recvbuf[0] != 0x80 && recvbuf[0] != 0x90) {
			continue
		}

		// build a audio packet struct
		p := Packet{}
		p.Flags = recvbuf[0]
		p.PayloadType = recvbuf[1]
		extentionExist := (p.Flags & 0x10) != 0 // RFC 3550 5.1
		csrcCount := (p.Flags & 0x0f)           // RFC 3550 5.1
		p.Sequence = binary.BigEndian.Uint16(recvbuf[2:4])
		p.Timestamp = binary.BigEndian.Uint32(recvbuf[4:8])
		p.SSRC = binary.BigEndian.Uint32(recvbuf[8:12])
		p.CSRC = make([]uint32, csrcCount)
		for i := range p.CSRC {
			p.CSRC[i] = binary.BigEndian.Uint32(recvbuf[12+4*i : 12+4*(i+1)])
		}
		plainLength := 12 + 4*int(csrcCount)
		if extentionExist {
			plainLength += 4
		}

		// decrypt opus data
		copy(nonce, recvbuf[rlen-4:rlen])

		v.Cond.L.Lock()
		p.Opus, err = v.cipher.Open(recvbuf[plainLength:plainLength], nonce, recvbuf[plainLength:rlen-4], recvbuf[:plainLength])
		v.Cond.L.Unlock()
		if err != nil {
			v.log(LogInformational, "failed to open udp packet, %v", err)
			continue
		}

		if extentionExist {
			extensionBegin := 12 + 4*int(csrcCount)
			extensionLength := binary.BigEndian.Uint16(recvbuf[extensionBegin+2 : extensionBegin+4])
			p.Extension = recvbuf[extensionBegin : extensionBegin+4+int(extensionLength)*4]
			p.Opus = p.Opus[int(extensionLength)*4:]
		}

		v.Cond.L.Lock()
		dave := v.dave
		userID := v.ssrcToUserID[p.SSRC]
		v.Cond.L.Unlock()
		if dave != nil {
			decryptBuf := make([]byte, dave.MaxDecryptedFrameSize(godave.UserID(userID), len(p.Opus)))
			n, err := dave.Decrypt(godave.UserID(userID), p.Opus, decryptBuf)
			if err != nil {
				v.log(LogDebug, "DAVE decrypt error for SSRC %d: %s", p.SSRC, err)
				continue
			}
			p.Opus = decryptBuf[:n]
		}

		if ch != nil {
			select {
			case ch <- &p:
			case <-ctx.Done():
				return
			}
		}
	}
}

// ------------------------------------------------------------------------------------------------
// DAVE E2EE Protocol Handlers
// ------------------------------------------------------------------------------------------------

// handleDAVEBinary decodes a DAVE binary frame and hands it to the session.
// What any of them mean, and what has to be sent back, is the session's
// business -- see legacySession and godave.Session.
func (v *VoiceConnection) handleDAVEBinary(message []byte) {
	if len(message) < 3 {
		v.log(LogWarning, "DAVE binary message too short: %d bytes", len(message))
		return
	}

	opcode := message[2]
	payload := message[3:]
	v.log(LogDebug, "DAVE binary opcode=%d len=%d", opcode, len(payload))

	v.Cond.L.Lock()
	dave := v.dave
	v.Cond.L.Unlock()
	if dave == nil {
		v.log(LogWarning, "DAVE binary opcode %d received but no session", opcode)
		return
	}

	// The two opcodes carrying a transition ID put it in the first two bytes,
	// ahead of the MLS message itself.
	transitionID := func() (uint16, bool) {
		if len(payload) < 2 {
			v.log(LogWarning, "DAVE opcode %d payload too short", opcode)
			return 0, false
		}
		return binary.BigEndian.Uint16(payload[0:2]), true
	}

	switch opcode {
	case daveOpMLSExternalSenderPackage:
		dave.OnDaveMLSExternalSenderPackage(payload)

	case daveOpMLSProposals:
		dave.OnDaveMLSProposals(payload)

	case daveOpMLSAnnounceCommitTransition:
		if id, ok := transitionID(); ok {
			dave.OnDaveMLSPrepareCommitTransition(id, payload[2:])
		}

	case daveOpMLSWelcome:
		if id, ok := transitionID(); ok {
			dave.OnDaveMLSWelcome(id, payload[2:])
		}

	default:
		v.log(LogDebug, "DAVE unknown binary opcode %d (%d bytes)", opcode, len(payload))
	}
}

func (v *VoiceConnection) handleDAVEPrepareTransition(data json.RawMessage) {
	var msg struct {
		TransitionID        uint16 `json:"transition_id"`
		DAVEProtocolVersion uint16 `json:"protocol_version"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		v.log(LogError, "DAVE prepare_transition unmarshal error: %s", err)
		return
	}

	if dave := v.daveSession(); dave != nil {
		dave.OnDavePrepareTransition(msg.TransitionID, msg.DAVEProtocolVersion)
	}
}

func (v *VoiceConnection) handleDAVEExecuteTransition(data json.RawMessage) {
	var msg struct {
		TransitionID uint16 `json:"transition_id"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		v.log(LogError, "DAVE execute_transition unmarshal error: %s", err)
		return
	}

	if dave := v.daveSession(); dave != nil {
		dave.OnDaveExecuteTransition(msg.TransitionID)
	}
}

func (v *VoiceConnection) handleDAVEPrepareEpoch(_ context.Context, data json.RawMessage) {
	var msg struct {
		Epoch               int    `json:"epoch"`
		DAVEProtocolVersion uint16 `json:"protocol_version"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		v.log(LogError, "DAVE prepare_epoch unmarshal error: %s", err)
		return
	}

	if dave := v.daveSession(); dave != nil {
		dave.OnDavePrepareEpoch(msg.Epoch, msg.DAVEProtocolVersion)
	}
}

// daveSession reads the session under the lock that guards it.
func (v *VoiceConnection) daveSession() godave.Session {
	v.Cond.L.Lock()
	defer v.Cond.L.Unlock()
	return v.dave
}

// The DAVE binary opcodes this client receives. The JSON ones (4, 21, 22, 24)
// arrive through onEvent instead.
const (
	daveOpMLSExternalSenderPackage    = 25
	daveOpMLSProposals                = 27
	daveOpMLSAnnounceCommitTransition = 29
	daveOpMLSWelcome                  = 30
)

// The DAVE opcodes this client sends. These four are the only messages that
// travel the other way, and they are exactly what godave.Callbacks asks for.
const (
	daveOpReadyForTransition      = 23
	daveOpMLSKeyPackage           = 26
	daveOpMLSCommitWelcome        = 28
	daveOpMLSInvalidCommitWelcome = 31
)

// errDAVENoConnection is returned when there is a DAVE message to send and no
// voice websocket to send it on. Reported rather than swallowed, which is what
// the old senders did: a session that has just committed needs to know its
// commit never left, or it sits waiting out a transition nobody else has heard
// of. Implementations behind godave.Session retry on this.
var errDAVENoConnection = errors.New("dave: no voice websocket")

// sendDAVEBinary writes one DAVE binary frame: a single opcode byte followed by
// the payload.
func (v *VoiceConnection) sendDAVEBinary(opcode byte, payload []byte) error {
	v.Cond.L.Lock()
	wsConn := v.wsConn
	v.Cond.L.Unlock()
	if wsConn == nil {
		return errDAVENoConnection
	}

	msg := make([]byte, 1+len(payload))
	msg[0] = opcode
	copy(msg[1:], payload)

	v.wsMu.Lock()
	defer v.wsMu.Unlock()
	return wsConn.WriteMessage(websocket.BinaryMessage, msg)
}

// sendDAVETransitionOp writes one of the two JSON opcodes whose entire payload
// is a transition ID.
func (v *VoiceConnection) sendDAVETransitionOp(op int, transitionID uint16) error {
	v.Cond.L.Lock()
	wsConn := v.wsConn
	v.Cond.L.Unlock()
	if wsConn == nil {
		return errDAVENoConnection
	}

	var msg struct {
		Op   int `json:"op"`
		Data struct {
			TransitionID uint16 `json:"transition_id"`
		} `json:"d"`
	}
	msg.Op = op
	msg.Data.TransitionID = transitionID

	v.wsMu.Lock()
	defer v.wsMu.Unlock()
	return wsConn.WriteJSON(msg)
}

func (v *VoiceConnection) sendDAVEKeyPackageBinary(kpData []byte) error {
	v.log(LogInformational, "DAVE sending key package (%d bytes)", len(kpData))
	return v.sendDAVEBinary(daveOpMLSKeyPackage, kpData)
}

// sendDAVECommitWelcome answers proposals with a commit plus a Welcome for
// whoever is joining. Nothing in this package calls it, because the hand-rolled
// MLS cannot commit — that is why op 27 is dropped and op 29 is rejected. It
// exists because godave.Callbacks requires it, and a session that can answer
// proposals is the entire point of moving to that interface.
func (v *VoiceConnection) sendDAVECommitWelcome(commitWelcome []byte) error {
	v.log(LogInformational, "DAVE sending commit welcome (%d bytes)", len(commitWelcome))
	return v.sendDAVEBinary(daveOpMLSCommitWelcome, commitWelcome)
}

func (v *VoiceConnection) sendDAVEReadyForTransition(transitionID uint16) error {
	v.log(LogDebug, "DAVE sending ready_for_transition id=%d", transitionID)
	return v.sendDAVETransitionOp(daveOpReadyForTransition, transitionID)
}

func (v *VoiceConnection) sendDAVEInvalidCommitWelcome(transitionID uint16) error {
	v.log(LogInformational, "DAVE sending invalid_commit_welcome id=%d", transitionID)
	return v.sendDAVETransitionOp(daveOpMLSInvalidCommitWelcome, transitionID)
}
