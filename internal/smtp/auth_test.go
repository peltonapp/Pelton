package smtp

import (
	"bufio"
	"net"
	"strings"
	"testing"
)

// authServer advertises the given AUTH list, accepts any advertised mechanism
// and answers the others with 504 like Exim. It reports the mechanism used.
func authServer(advertised string, used chan<- string) func(net.Conn) {
	return func(conn net.Conn) {
		defer conn.Close()
		r := bufio.NewReader(conn)
		reply := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
		reply("220 test ready")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			fields := strings.Fields(line)
			switch {
			case len(fields) == 0:
			case fields[0] == "EHLO" && advertised == "":
				reply("250 test")
			case fields[0] == "EHLO":
				reply("250-test")
				reply("250 AUTH " + advertised)
			case fields[0] == "AUTH":
				used <- fields[1]
				switch {
				case !strings.Contains(" "+advertised+" ", " "+fields[1]+" "):
					reply("504 " + fields[1] + " authentication mechanism not supported")
				case fields[1] == "LOGIN":
					// go-sasl sends the username as the initial response.
					reply("334 UGFzc3dvcmQ6")
					_, _ = r.ReadString('\n')
					reply("235 ok")
				default:
					reply("235 ok")
				}
			default:
				reply("221 bye")
				return
			}
		}
	}
}

// Authenticate always sent AUTH PLAIN, so servers that advertise only LOGIN
// rejected every send with 504. Servers that offer PLAIN, or no AUTH list,
// must keep getting PLAIN.
func TestAuthenticateFollowsAdvertisedMechanism(t *testing.T) {
	cases := []struct {
		advertised string
		want       string
		wantErr    bool
	}{
		{"PLAIN LOGIN", "PLAIN", false},
		{"LOGIN", "LOGIN", false},
		{"", "PLAIN", true},
	}
	for _, tc := range cases {
		t.Run(tc.advertised, func(t *testing.T) {
			used := make(chan string, 1)
			port, _ := selfSignedSubmission(t, authServer(tc.advertised, used))
			client, err := Dial(Config{Host: "127.0.0.1", Port: port, TLS: TLSImplicit, InsecureSkipVerify: true, Username: "user", Password: "secret"})
			if err != nil {
				t.Fatalf("Dial: %v", err)
			}
			defer client.Close()

			if err := client.Authenticate(); (err != nil) != tc.wantErr {
				t.Fatalf("Authenticate() error = %v, wantErr %v", err, tc.wantErr)
			}
			if got := <-used; got != tc.want {
				t.Errorf("client used AUTH %s, want %s", got, tc.want)
			}
		})
	}
}
