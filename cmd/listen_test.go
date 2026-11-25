package cmd

import "testing"

func TestListenGate(t *testing.T) {
	cases := []struct {
		name    string
		addr    string
		token   string
		opts    backendOpts
		wantErr bool
	}{
		{"loopback, nothing required", "127.0.0.1:9999", "", backendOpts{}, false},
		{"require-token without token", "127.0.0.1:9999", "", backendOpts{requireToken: true}, true},
		{"require-token with token", "127.0.0.1:9999", "t", backendOpts{requireToken: true}, false},
		{"webhook on loopback without token", "127.0.0.1:9999", "", backendOpts{enableWebhook: true}, true},
		{"webhook with token", "0.0.0.0:9999", "t", backendOpts{enableWebhook: true}, false},
		{"non-loopback without token", "0.0.0.0:9999", "", backendOpts{}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := listenGate(c.addr, c.token, c.opts); (err != nil) != c.wantErr {
				t.Errorf("listenGate(%q, %q, %+v) = %v, wantErr %v", c.addr, c.token, c.opts, err, c.wantErr)
			}
		})
	}
}

func TestCheckListenSafety(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		token   string
		wantErr bool
	}{
		{"loopback ipv4 no token", "127.0.0.1:9999", "", false},
		{"loopback localhost no token", "localhost:9999", "", false},
		{"loopback ipv6 no token", "[::1]:9999", "", false},
		{"all-interfaces no token", ":9999", "", true},
		{"public ip no token", "192.168.1.10:9999", "", true},
		{"all-interfaces with token", ":9999", "secret", false},
		{"public ip with token", "0.0.0.0:9999", "secret", false},
		{"unparseable no token", "not-an-addr", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := checkListenSafety(tc.addr, tc.token)
			if (err != nil) != tc.wantErr {
				t.Errorf("checkListenSafety(%q, token=%q) err=%v, wantErr=%v", tc.addr, tc.token, err, tc.wantErr)
			}
		})
	}
}
