package managesieve

import "fmt"

// Mechanism names the client knows. They match what the imap and smtp layers
// authenticate with: a password over PLAIN (LOGIN when PLAIN is not offered)
// and an oauth token over XOAUTH2.
const (
	MechPlain   = "PLAIN"
	MechLogin   = "LOGIN"
	MechXOAuth2 = "XOAUTH2"
)

// saslClient is one SASL exchange, the same shape as the go-sasl clients the
// imap and smtp layers use.
type saslClient interface {
	Start() (mech string, ir []byte, err error)
	Next(challenge []byte) ([]byte, error)
}

type plainClient struct{ username, password string }

func (c plainClient) Start() (string, []byte, error) {
	return MechPlain, []byte("\x00" + c.username + "\x00" + c.password), nil
}

func (c plainClient) Next([]byte) ([]byte, error) {
	return nil, fmt.Errorf("managesieve: unexpected PLAIN challenge")
}

// loginClient answers the two LOGIN prompts in order.
type loginClient struct {
	username, password string
	step               int
}

func (c *loginClient) Start() (string, []byte, error) { return MechLogin, nil, nil }

func (c *loginClient) Next([]byte) ([]byte, error) {
	c.step++
	switch c.step {
	case 1:
		return []byte(c.username), nil
	case 2:
		return []byte(c.password), nil
	}
	return nil, fmt.Errorf("managesieve: unexpected LOGIN challenge")
}

type xoauth2Client struct{ username, token string }

func (c xoauth2Client) Start() (string, []byte, error) {
	return MechXOAuth2, []byte("user=" + c.username + "\x01auth=Bearer " + c.token + "\x01\x01"), nil
}

// Next answers the error payload a server sends after refusing the token with
// an empty response, which ends the exchange with NO.
func (c xoauth2Client) Next([]byte) ([]byte, error) {
	return []byte{}, nil
}

// pickMechanism chooses the SASL client for the credentials in cfg among what
// the server advertises.
func pickMechanism(cfg Config, caps Capabilities) (saslClient, error) {
	if cfg.OAuth2Token != "" {
		if !caps.HasSASL(MechXOAuth2) {
			return nil, fmt.Errorf("%w: the server does not accept oauth sign-in (XOAUTH2)", ErrNoMechanism)
		}
		return xoauth2Client{username: cfg.Username, token: cfg.OAuth2Token}, nil
	}
	if caps.HasSASL(MechPlain) {
		return plainClient{username: cfg.Username, password: cfg.Password}, nil
	}
	if caps.HasSASL(MechLogin) {
		return &loginClient{username: cfg.Username, password: cfg.Password}, nil
	}
	return nil, fmt.Errorf("%w: the server offers %v", ErrNoMechanism, caps.SASL)
}
