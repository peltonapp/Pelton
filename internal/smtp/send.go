package smtp

import (
	"context"
	"log/slog"
	"time"

	"github.com/peltonapp/Pelton/internal/crypto"
	"github.com/peltonapp/Pelton/internal/outbox"
)

// Enqueue builds the message (applying crypto when requested) and adds it to the
// outbox for the worker to transmit. Because BuildRaw hard-fails when crypto is
// requested but cannot be completed, an encrypted message is only ever enqueued
// in its encrypted form, and a protected message is never queued as plaintext.
//
// notBefore holds the message queued until that time (the undo-send delay). Pass
// the zero time to send as soon as the worker picks it up.
func Enqueue(ctx context.Context, queue *outbox.Queue, accountID int64, msg *Message, engine crypto.Engine, mode crypto.Mode, opts crypto.Options, notBefore time.Time) (int64, error) {
	raw, err := BuildRaw(msg, engine, mode, opts)
	if err != nil {
		return 0, err
	}
	return queue.Enqueue(ctx, outbox.Message{
		AccountID:    accountID,
		EnvelopeFrom: msg.From.Email,
		Recipients:   msg.Recipients(),
		Raw:          raw,
		NotBefore:    notBefore,
	})
}

// SentAppender appends an already-sent message to the Sent folder. It is
// satisfied by a closure over the imap client, keeping smtp from depending on a
// live imap connection. Returns the folder used.
type SentAppender func(raw []byte) (string, error)

// Sender transmits queued messages over SMTP and, on success, appends them to
// the Sent folder. It implements outbox.Transmitter so the worker can drive it.
type Sender struct {
	cfg        Config
	appendSent SentAppender
	background func(func())
	log        *slog.Logger
}

// SenderOption configures a Sender.
type SenderOption func(*Sender)

// WithSentAppender wires the append-to-Sent step run after a successful send.
// Without it, the step is skipped (with a debug log) and sending still succeeds.
func WithSentAppender(appender SentAppender) SenderOption {
	return func(s *Sender) { s.appendSent = appender }
}

// WithBackground sets how the append-to-Sent step is started once a message is
// delivered. The default runs it on a goroutine of its own.
func WithBackground(start func(func())) SenderOption {
	return func(s *Sender) { s.background = start }
}

// WithLogger sets the sender's logger.
func WithLogger(log *slog.Logger) SenderOption {
	return func(s *Sender) { s.log = log }
}

// NewSender builds a Sender for one account's submission config.
func NewSender(cfg Config, opts ...SenderOption) *Sender {
	s := &Sender{cfg: cfg, log: slog.New(slog.DiscardHandler), background: func(fn func()) { go fn() }}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Transmit dials, authenticates and sends the message, then starts the append
// to Sent in the background and returns. The message counts as sent once the
// server has accepted it: the copy to Sent can wait on other mail work for a
// long time, and holding the outbox until then left delivered mail showing as
// "sending" and the rest of the queue stuck behind it. It also meant a restart
// in the meantime queued the delivered message again. An append failure is
// only logged.
func (s *Sender) Transmit(ctx context.Context, m outbox.Message) error {
	client, err := Dial(s.cfg)
	if err != nil {
		return err
	}

	if err := client.Authenticate(); err != nil {
		_ = client.Close()
		return err
	}
	if err := client.Send(ctx, m.EnvelopeFrom, m.Recipients, m.Raw); err != nil {
		_ = client.Close()
		return err
	}
	if err := client.Close(); err != nil {
		s.log.Debug("smtp quit after a delivered message", "err", err)
	}

	raw := m.Raw
	s.background(func() { s.appendToSent(raw) })
	return nil
}

// appendToSent runs the optional append-to-Sent step, never failing the send.
func (s *Sender) appendToSent(raw []byte) {
	if s.appendSent == nil {
		s.log.Debug("no sent appender configured, skipping append to Sent")
		return
	}
	folder, err := s.appendSent(raw)
	if err != nil {
		s.log.Warn("append to Sent failed, message was still sent", "err", err)
		return
	}
	s.log.Info("appended to Sent", "folder", folder)
}

// compile-time check that Sender satisfies the worker's contract.
var _ outbox.Transmitter = (*Sender)(nil)
